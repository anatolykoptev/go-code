package main

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	"log/slog"
	"os"
	"time"

	kitcache "github.com/anatolykoptev/go-kit/cache"
	"github.com/anatolykoptev/go-kit/embed"
	"github.com/anatolykoptev/go-kit/env"
	"github.com/anatolykoptev/go-kit/llm"
	kitmetrics "github.com/anatolykoptev/go-kit/metrics"
	"github.com/anatolykoptev/go-kit/sparse"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/cache"
	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/codegraph"
	"github.com/anatolykoptev/vaelor/internal/designmd"
	"github.com/anatolykoptev/vaelor/internal/embeddings"
	"github.com/anatolykoptev/vaelor/internal/forge"
	"github.com/anatolykoptev/vaelor/internal/graphx"
	"github.com/anatolykoptev/vaelor/internal/ingest"
	"github.com/anatolykoptev/vaelor/internal/learnings"
	"github.com/anatolykoptev/vaelor/internal/oxcodes"
	"github.com/anatolykoptev/vaelor/internal/websearch"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	// agePoolMaxConns sizes the Apache AGE pool: graph build + concurrent queries need
	// more than pgx's default 4.
	agePoolMaxConns int32 = 8
	// dataPoolMaxConns sizes the pgvector / relational pool: read-mostly semantic
	// queries, lighter than graph build.
	dataPoolMaxConns int32 = 6
)

// newGocodePool opens a pgxpool against dsn with maxConns connections.
//
// resetOnRelease=true installs the SR-A AfterRelease hook that runs RESET ALL on every
// connection return. It is REQUIRED for the AGE pool, whose connections get their
// search_path (acquireAGE / ageExpandSetup) and session GUCs (synchronous_commit,
// statement_timeout — the bulk-copy path) dirtied by user code. RESET ALL resets those
// GUCs to the role default but deliberately does NOT run DEALLOCATE ALL: pgx's default
// exec mode (QueryExecModeCacheStatement) caches prepared statements per connection, and
// DISCARD ALL's DEALLOCATE would invalidate them server-side → `prepared statement
// "stmtcache_…" does not exist (SQLSTATE 26000)` on reuse (see PR #176).
//
// resetOnRelease=false is for the data pool: nothing on it ever runs SET search_path or
// dirties GUCs, so its connections are pristine by construction and a reset hook would be
// dead weight. This is what makes the search_path leak structurally impossible on the
// data path — a data query cannot inherit ag_catalog because no code path ever sets it
// on a dataPool connection.
func newGocodePool(ctx context.Context, dsn string, maxConns int32, resetOnRelease bool) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	poolCfg.MaxConns = maxConns
	if resetOnRelease {
		poolCfg.AfterRelease = func(conn *pgx.Conn) bool {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if _, err := conn.Exec(ctx, "RESET ALL"); err != nil {
				// Connection is unhealthy; destroy it rather than returning it.
				return false
			}
			return true
		}
	}
	return pgxpool.NewWithConfig(ctx, poolCfg)
}

// llmCooldownDuration resolves the per-model cooldown TTL from the environment.
// LLM_COOLDOWN_SECONDS is read as an integer number of seconds; if unset or ≤0
// the built-in default of 5 minutes is used.
func llmCooldownDuration() time.Duration {
	if s := env.Int("LLM_COOLDOWN_SECONDS", 0); s > 0 {
		return time.Duration(s) * time.Second
	}
	return 5 * time.Minute
}

// registerTools registers all MCP tool handlers on the server.
// Each tool has its own file: tool_<name>.go
// Returns the analyze.Deps for use by other components (e.g., webhook handler)
// and the embeddings Pipeline (nil when EMBED_URL is unset) for the file watcher.
func registerTools(ctx context.Context, server *mcp.Server, cfg Config, reg *kitmetrics.Registry) (analyze.Deps, *embeddings.Pipeline, *codegraph.Store) {
	parseCacheSize := env.Int("PARSE_CACHE_SIZE", cache.DefaultParseCacheSize)
	llmCacheSize := env.Int("LLM_CACHE_SIZE", cache.DefaultLLMCacheSize)
	llmCacheTTLMin := env.Int("LLM_CACHE_TTL_MIN", defaultLLMCacheTTL)

	parseCache := cache.NewParseCache(parseCacheSize)
	llmCache := cache.NewLLMCache(llmCacheSize, time.Duration(llmCacheTTLMin)*time.Minute)

	toolCacheTTL := time.Duration(env.Int("TOOL_CACHE_TTL_MIN", defaultToolCacheTTL)) * time.Minute
	toolCache := kitcache.New(kitcache.Config{
		L1MaxItems:    env.Int("TOOL_CACHE_SIZE", defaultToolCacheSize),
		L1TTL:         toolCacheTTL,
		L2TTL:         toolCacheTTL,
		RedisURL:      cfg.RedisURL,
		Prefix:        "gc:",
		JitterPercent: 0.1,
	})

	// Wire optional Redis L2 for the two process-level object caches.
	// When RedisURL is empty these are no-ops and behavior is byte-identical
	// to the L1-only implementation.
	ingest.SetL2(cfg.RedisURL)
	callgraph.SetL2(cfg.RedisURL)

	// WithEndpoints owns per-endpoint retry; WithFallbackKeys keys same-model retries.
	// When chain is configured, use it (cross-provider failure-domain via cliproxyapi
	// model routing). Otherwise fall back to key-rotation for single-provider pools.
	modelChain := llm.ParseModelFallbackChain(cfg.LLMModelFallback)
	llmOpts := []llm.Option{
		llm.WithMaxTokens(cfg.LLMMaxTokens),
		llm.WithCircuitBreaker(llm.CircuitConfig{
			FailThreshold:  5,                // 5 consecutive failures trip the breaker
			OpenDuration:   30 * time.Second, // fail-fast for 30s, then probe
			HalfOpenProbes: 1,                // one probe request before closing
		}),
		llm.WithPerAttemptTimeout(cfg.LLMPerAttemptTimeout), // 0 = disabled; per-attempt cap on WithEndpoints chains
		llm.WithMiddleware(newLLMObs(reg).middleware),       // records gocode_llm_calls_total / gocode_llm_request_seconds
	}
	if len(modelChain) > 0 {
		// Each model in the chain is already a retry layer; cap per-endpoint retries
		// to 1 to avoid O(chain_len × retries) wall time on full outage.
		llmOpts = append(llmOpts,
			llm.WithEndpoints(llm.BuildModelChainEndpointsFiltered(context.Background(), llm.NewModelRegistry(), cfg.LLMURL, cfg.LLMAPIKey, cfg.LLMModel, modelChain, newModelFilterObserver(reg))),
			llm.WithMaxRetries(1),
			llm.WithModelCooldown(llm.CooldownConfig{Default: llmCooldownDuration()}),
		)
		slog.Info("llm: model chain enabled",
			slog.String("primary", cfg.LLMModel),
			slog.String("chain", cfg.LLMModelFallback),
		)
	} else {
		llmOpts = append(llmOpts, llm.WithFallbackKeys(cfg.LLMFallbackKeys))
	}

	llmClient, hasKey := llm.NewOptional(cfg.LLMURL, cfg.LLMAPIKey, cfg.LLMModel, llmOpts...)
	if !hasKey {
		slog.Warn("llm: disabled (LLM_API_KEY unset) — code_graph/repo_search/debug_investigate will error; narratives in call_trace/dead_code/impact omitted")
	}

	deps := analyze.Deps{
		LLM:               llmClient,
		LLMHasKey:         hasKey,
		MaxFileBytes:      cfg.MaxFileBytes,
		MaxRepoBytes:      cfg.MaxRepoBytes,
		GithubSearchRepos: cfg.GithubSearchRepos,
		GithubToken:       cfg.GithubToken,
		CloneTokenFunc:    buildCloneTokenFunc(cfg),
		WorkspaceDir:      cfg.WorkspaceDir,
		PathMappings:      cfg.PathMappings,
		LocalRepoDirs:     autoIndexDirs(cfg),
		ParseCache:        parseCache,
		LLMCache:          llmCache,
		Forges:            buildForgeRegistry(cfg, toolCache),
		WebSearch:         buildWebSearchClient(cfg),
		ToolCache:         toolCache,
		OxCodes:           buildOxCodesClient(cfg),
		Learnings:         buildLearningsStore(cfg),
	}

	// Database pools (optional — need DATABASE_URL). Tier-2: TWO pools, separated by
	// session-state needs so the search_path leak is structurally impossible on the
	// data path — see newGocodePool for the per-pool hook decision.
	//   agePool  — Apache AGE consumers (codegraph.Store, embeddings.Expander). They run
	//              `SET search_path TO ag_catalog,…` on every acquire and dirty session
	//              GUCs in the bulk-copy path, so agePool carries the RESET ALL release hook.
	//   dataPool — pure relational / pgvector consumers (embeddings.Store, designmd.Store).
	//              Nothing on this pool ever runs SET search_path or touches session GUCs,
	//              so its connections are pristine by construction: a data query CANNOT
	//              inherit ag_catalog because no code path sets it on a dataPool conn.
	//              (SR-B public.* qualification on the data tables remains as a backstop.)
	// Both pools share one DSN, so they succeed or fail together; on partial failure we
	// disable both to preserve the "both or neither" invariant the gates below rely on.
	var graphStore *codegraph.Store
	var agePool, dataPool *pgxpool.Pool
	if cfg.DatabaseURL != "" {
		var ageErr, dataErr error
		agePool, ageErr = newGocodePool(context.Background(), cfg.DatabaseURL, agePoolMaxConns, true)
		dataPool, dataErr = newGocodePool(context.Background(), cfg.DatabaseURL, dataPoolMaxConns, false)
		if ageErr != nil || dataErr != nil {
			slog.Warn("database: pool init failed, code_graph and semantic_search disabled",
				slog.Any("age_error", ageErr), slog.Any("data_error", dataErr))
			if agePool != nil {
				agePool.Close()
			}
			if dataPool != nil {
				dataPool.Close()
			}
			agePool, dataPool = nil, nil
		} else {
			graphStore = codegraph.NewStore(agePool)
			// Preflight verifies AGE is server-preloaded (#111: per-connection LOAD removed)
			// and that the role has ag_catalog USAGE + database CREATE privileges (#112).
			// Fail fast at startup so operators get clear instructions rather than a
			// cryptic permission error on the first repo index request.
			if err := graphStore.Preflight(context.Background()); err != nil {
				slog.Error("database: preflight failed", slog.Any("error", err))
				os.Exit(1)
			}
			// SR-OBS: boot-time drift guard — detect any table that leaked into
			// ag_catalog and bump gocode_schema_drift_total so alerts fire immediately.
			graphStore.AssertSchemaDrift(context.Background())
		}
	}

	// Wire graph signals — always non-nil (Noop when no store available).
	deps.Graph, deps.Refs = buildGraphDeps(graphStore, cfg.PathMappings)

	// RRF weights: published once at startup so /metrics records the deployed
	// values, and threaded into SemanticDeps so MergeRRF picks them up. Logged
	// for ops visibility — Sparse defaults to 0.0 (dark-launched P4).
	rrfWeights := cfg.RRFWeights()
	embeddings.PublishRRFWeights(rrfWeights)
	slog.Info("rrf weights",
		slog.Float64("semantic", rrfWeights.Semantic),
		slog.Float64("keyword", rrfWeights.Keyword),
		slog.Float64("sparse", rrfWeights.Sparse),
		slog.Float64("graph", rrfWeights.Graph),
		slog.Float64("hotspot", rrfWeights.Hotspot),
		slog.Float64("recency", rrfWeights.Recency),
	)

	// Keyword arm: published at startup (gauge + log) so ops can see which arm
	// is live without issuing a query. Default "grep" = byte-identical to pre-P4.
	publishKeywordArm(cfg.KeywordArm)
	slog.Info("keyword arm",
		slog.String("arm", cfg.KeywordArm),
		slog.String("note", "default; override with KEYWORD_ARM=grep"),
	)

	// Semantic deps (optional — needs EMBED_URL + DATABASE_URL).
	// Created early so tools can use semantic fallback.
	// Extracted to newSemanticDeps (semantic_deps.go) so both the MCP serve
	// path and the CLI search subcommand share a single wiring site.
	semDeps := newSemanticDeps(cfg, deps, dataPool, agePool, graphStore, rrfWeights)

	// Wire pg_trgm symbol boosting for repo_analyze when embeddings are available.
	if semDeps.Store != nil {
		deps.SymbolBooster = &symbolBoostAdapter{store: semDeps.Store}
		deps.RepoKeyFunc = codegraph.GraphNameFor
		// Wire indexed-SHA resolver for WithFreshness staleness signal.
		// Captures embedStore by closure; errors collapse to "" (cold-path guarantee).
		embedStore := semDeps.Store
		deps.IndexedSHAFunc = func(ctx context.Context, repoKey string) string {
			sha, err := embedStore.GetRepoState(ctx, repoKey)
			if err != nil {
				slog.Debug("freshness: GetRepoState failed", "repo_key", repoKey, "err", err)
				return ""
			}
			return sha
		}
	}

	registerRepoAnalyze(server, cfg, deps)
	registerFileParse(server, cfg, deps)
	registerCodeCompare(server, cfg, deps, &semDeps, graphStore)
	registerDepGraph(server, cfg, deps)
	registerSymbolSearch(server, cfg, deps, &semDeps)
	registerCallTrace(server, cfg, deps, &semDeps, graphStore)
	registerImpact(server, cfg, deps, &semDeps)
	registerDeadCode(server, cfg, deps, graphStore)
	registerExplore(server, cfg, deps)
	registerCodeHealth(server, cfg, deps, &semDeps, graphStore)
	registerCodeGraph(server, cfg, deps, graphStore)
	registerRememberGraphInsights(server, cfg, deps, graphStore)
	registerRepoSearch(server, cfg, deps)
	registerGithubCodeSearch(server, cfg, deps)
	registerCodeSearch(server, cfg, deps, &semDeps)
	registerWPPluginSearch(server, cfg, deps)
	registerSemanticSearch(server, cfg, semDeps)
	registerSparseBackfill(server, cfg, semDeps)
	registerOrphanSweep(server, semDeps)
	registerReconcilePaths(server, semDeps)
	registerListFlows(server, graphStore, semDeps)
	registerFindDuplicates(server, semDeps)
	registerCodeResearch(server, cfg, deps, &semDeps)
	registerSiteAnalyze(server, cfg)
	registerSiteCrawl(server, cfg)
	registerUnderstand(server, cfg, deps, &semDeps, graphStore)
	registerPrepareChange(server, cfg, deps, &semDeps)
	registerReviewDelta(server, cfg, deps, graphStore)
	registerReviewPR(server, cfg, deps, graphStore)
	registerRewrite(server, cfg, deps)
	registerDataflow(server, cfg, deps)
	// Design search deps (optional — needs DESIGN_EMBED_URL + DATABASE_URL).
	var designDeps DesignDeps
	if cfg.DesignEmbedURL != "" && dataPool != nil {
		dc, err := newDesignEmbedder(cfg)
		if err != nil {
			slog.Warn("embed: design client disabled", slog.Any("error", err))
		} else {
			designDeps = DesignDeps{
				Client: dc,
				Store:  designmd.NewStore(dataPool),
			}
		}
	}
	registerDesignSearch(server, cfg, designDeps)
	registerDebugInvestigate(server, cfg, deps)
	registerFleetVersions(server, cfg, deps)

	// Register ox-codes cache stats Prometheus collector (if ox-codes is configured).
	if deps.OxCodes != nil {
		prometheus.MustRegister(newOxCodesCollector(deps.OxCodes))
	}
	registerResolveFrame(server, cfg)
	registerFileHealth(server, cfg, deps)
	registerSuggestReviewers(server, cfg, deps)
	registerFederatedCoChange(server, cfg, deps)

	// Publish the unified capability gauge for every subsystem whose
	// enable/disable state is resolved here in registerTools (the subsystem
	// functions above publish their own: semantic_search, sparse_retrieval,
	// learnings_store, github_app_auth). Pre-touch at init() guarantees every
	// series exists from a cold start; these calls flip the enabled ones to 1.
	setCapability("code_graph", graphStore != nil)
	setCapability("graph_signals", graphStore != nil)
	setCapability("find_duplicates", semDeps.Store != nil)
	setCapability("sparse_backfill", semDeps.SparseClient != nil && semDeps.Store != nil)
	setCapability("design_search", designDeps.Client != nil)
	setCapability("resolve_frame", len(cfg.SourcemapAllowedHosts) > 0)
	setCapability("dataflow_analyze", deps.OxCodes != nil)
	setCapability("web_search", deps.WebSearch != nil)
	setCapability("llm", hasKey)

	// Auto-index local repos in background.
	if semDeps.Pipeline != nil && len(cfg.AutoIndexDirs) > 0 {
		opts := embeddings.AutoIndexOpts{
			Concurrency: cfg.AutoIndexConcurrency,
			RetryMax:    cfg.AutoIndexRetryMax,
			RetryBase:   cfg.AutoIndexRetryBase,
		}
		go embeddings.AutoIndex(semDeps.Pipeline, autoIndexDirs(cfg), codegraph.GraphNameFor, opts)
	}

	// Orphan gauge — boot + periodic ticker so gocode_orphan_repo_keys reflects
	// reality continuously rather than only after an operator-run orphan_sweep.
	// The gauge previously read 0 while Postgres had 17 orphan repo_keys; the fix
	// exposes the true count within 5 min of boot (2026-06-13 observability gap).
	// Threaded through the lifecycle ctx (#596) so the goroutine exits on
	// SIGINT/SIGTERM instead of leaking on every shutdown/re-init.
	startOrphanGaugeWarm(ctx, semDeps.Store)

	// Code-graph age gauge + zero-embeddings desync counter — boot warm, both
	// extracted to their own functions rather than inlined here: registerTools
	// already exceeds the gocognit threshold (baseline 40 > 20 on main before
	// this change) and two more inline `if + go func` blocks would add to that
	// debt for no benefit — see each function's doc comment for the incident
	// writeup (2026-07-01 metrics audit).
	// Threaded through the lifecycle ctx (#597) so the goroutine exits on
	// SIGINT/SIGTERM instead of leaking on every shutdown/re-init.
	startCodeGraphAgeGaugeWarm(ctx, graphStore, autoIndexDirs(cfg))
	startZeroEmbeddingsCounterWarm(semDeps.Store)

	return deps, semDeps.Pipeline, graphStore
}

// gaugeTickerInterval is the publication cadence for both background gauge
// warmers (orphan repo_keys, code-graph age). Matches the prior inline tickers.
const gaugeTickerInterval = 5 * time.Minute

// runGaugeTicker runs fn once immediately (boot-warm), then on every tick of
// interval, until ctx is cancelled. It returns a done channel that closes when
// the goroutine has fully exited (ticker stopped, loop returned) so callers and
// tests can confirm no goroutine leak (#596, #597).
//
// This replaces the bare `for range t.C` loops that had no cancellation path
// and leaked on every shutdown/re-init. The done channel is closed in all
// exit paths: ctx cancellation, or the immediate-call-only case (interval
// callers still get a goroutine that exits after the first fn() if ctx is
// already cancelled before the first tick).
func runGaugeTicker(ctx context.Context, interval time.Duration, fn func()) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				fn()
			case <-ctx.Done():
				return
			}
		}
	}()
	return done
}

// startOrphanGaugeWarm launches the boot + periodic-ticker goroutine that
// keeps gocode_orphan_repo_keys populated from the real orphan-repo-key count.
// No-ops (returns a pre-closed done channel) when store is nil — EMBED_URL /
// DATABASE_URL unset, semantic_search already disabled in that case.
//
// Threaded through the lifecycle ctx (#596) so the goroutine exits on
// SIGINT/SIGTERM instead of leaking.
func startOrphanGaugeWarm(ctx context.Context, store *embeddings.Store) <-chan struct{} {
	if store == nil {
		c := make(chan struct{})
		close(c)
		return c
	}
	return runGaugeTicker(ctx, gaugeTickerInterval, func() { publishOrphanGauge(store) })
}

// startCodeGraphAgeGaugeWarm launches the boot + periodic-ticker goroutine
// (same 5-min cadence as the orphan gauge above) that keeps
// gocode_code_graph_age_seconds populated from the real code_graph_meta
// snapshot. Without this the series vanished on every restart until the
// next successful build completed — defeating GocodeCodeGraphStale exactly
// when a repo's graph was ALREADY stale (confirmed live on the v1.22.1
// deploy). No-ops when graphStore is nil (DATABASE_URL unset or pool init
// failed — code_graph is already disabled in that case).
//
// scopeDirs (the resolved AUTO_INDEX_DIRS, see autoIndexDirs(cfg)) restricts
// publication to tracked repos — see publishCodeGraphAgeGauge's doc comment
// for why untracked code_graph_meta rows (WORKSPACE_DIR clones, test
// sentinels) must not carry a series.
//
// Threaded through the lifecycle ctx (#597) so the goroutine exits on
// SIGINT/SIGTERM instead of leaking.
func startCodeGraphAgeGaugeWarm(ctx context.Context, graphStore *codegraph.Store, scopeDirs []string) <-chan struct{} {
	if graphStore == nil {
		c := make(chan struct{})
		close(c)
		return c
	}
	return runGaugeTicker(ctx, gaugeTickerInterval, func() {
		publishCodeGraphAgeGauge(ctx, graphStore, scopeDirs)
	})
}

// zeroEmbeddingsWarmTimeout bounds the boot-time ListRepoKeys query so a
// slow/unreachable store cannot hang the warm goroutine indefinitely.
const zeroEmbeddingsWarmTimeout = 30 * time.Second

// startZeroEmbeddingsCounterWarm pre-touches
// gocode_repo_state_advanced_with_zero_embeddings_total{repo} at boot for
// every repo already known via code_repo_state. Prometheus increase() over
// a series that has just come into existence has nothing to subtract from
// and reads 0, so a repo's FIRST desync in a fresh process would be
// invisible to GocodeRepoStateAdvancedZeroEmbeddings until its SECOND
// desync in the same process lifetime; pre-touching closes that gap.
// One-shot (no ticker needed): a repo indexed for the first time after boot
// gets its series established the normal way, via SetRepoState. No-ops when
// store is nil (EMBED_URL/DATABASE_URL unset — semantic_search is already
// disabled in that case).
func startZeroEmbeddingsCounterWarm(store *embeddings.Store) {
	if store == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), zeroEmbeddingsWarmTimeout)
		defer cancel()
		keys, err := store.ListRepoKeys(ctx)
		if err != nil {
			slog.Warn("zero-embeddings counter warm failed", slog.Any("error", err))
			return
		}
		embeddings.WarmRepoStateAdvancedZeroEmbeddings(keys)
		embeddings.WarmStalePathRatioGauge(keys)
	}()
}

// publishOrphanGauge queries PG for orphan repo_keys and updates the gauge.
// Called at boot and on a ticker so gocode_orphan_repo_keys is continuously
// truthful rather than only updated by operator-initiated orphan_sweep calls.
func publishOrphanGauge(store *embeddings.Store) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	n, err := store.CountOrphanRepoKeys(ctx)
	if err != nil {
		slog.Warn("orphan gauge: count failed", slog.Any("error", err))
		return
	}
	embeddings.SetOrphanRepoKeysGauge(float64(n))
}

// buildWebSearchClient creates a go-search client if configured.
func buildWebSearchClient(cfg Config) *websearch.Client {
	if cfg.GoSearchURL == "" {
		return nil
	}
	return websearch.NewClient(cfg.GoSearchURL)
}

// buildOxCodesClient creates an ox-codes client if configured.
func buildOxCodesClient(cfg Config) *oxcodes.Client {
	if cfg.OxCodesURL == "" {
		return nil
	}
	return oxcodes.NewClient(cfg.OxCodesURL)
}

// buildForgeRegistry creates a forge registry from config.
func buildForgeRegistry(cfg Config, toolCache *kitcache.Cache) *forge.Registry {
	reg := forge.NewRegistry()
	reg.Register(forge.GitHub, forge.NewGitHubForge(cfg.GithubToken, cfg.GithubAppConfig, forge.WithCache(toolCache)))
	if cfg.GitLabToken != "" || cfg.GitLabURL != "" {
		reg.Register(forge.GitLab, forge.NewGitLabForge(cfg.GitLabToken, cfg.GitLabURL))
	}
	return reg
}

// buildGraphDeps wires graphx.Analytics and graphx.CrossRefs from an optional
// codegraph.Store. Returns Noop{} for both when the store is nil (no DATABASE_URL
// or pool construction failed).
func buildGraphDeps(store *codegraph.Store, mappings []analyze.PathMapping) (graphx.Analytics, graphx.CrossRefs) {
	if store == nil {
		return graphx.Noop{}, graphx.Noop{}
	}
	slog.Info("graph signals enabled via codegraph.Store")
	return codegraph.NewAnalyticsAdapter(store, mappings), codegraph.NewCrossRefsAdapter(store, mappings)
}

// buildLearningsStore opens a learnings.Store if configured.
// Returns nil (disabled) when LearningsDSN is empty or the pool fails to open.
func buildLearningsStore(cfg Config) *learnings.Store {
	if cfg.LearningsDSN == "" {
		slog.Warn("config: learnings store disabled — neither LEARNINGS_DATABASE_URL nor DATABASE_URL set; remember_graph_insights and prior_learnings in understand will be unavailable",
			slog.String("env_var", "LEARNINGS_DATABASE_URL"),
		)
		setCapability("learnings_store", false)
		return nil
	}
	ls, err := learnings.New(context.Background(), cfg.LearningsDSN, nil)
	if err != nil {
		slog.Warn("learnings store disabled", "err", err)
		setCapability("learnings_store", false)
		return nil
	}
	setCapability("learnings_store", true)
	return ls
}

// embeddingDims pin per-client vector dimensions for clarity/auditing. The HTTP
// backend does not validate response dims against this value (it is only
// surfaced via Dimension()), but pinning it here documents the contract:
// the code embedder MUST stay 768d to match the pgvector(768) code_embeddings
// schema; the design embedder MUST stay 1024d to match design_embeddings.
const (
	codeEmbedDim   = 768
	designEmbedDim = 1024
)

// newCodeEmbedder constructs the code-search embedder (768d; active model set via EMBED_MODEL).
// Powers semantic_search, code_health, and codegraph indexing. Writes into the
// pgvector(768) code_embeddings table — must NOT be swapped for a 1024d model.
//
// WithTimeout overrides the go-kit default (30s) when cfg.EmbedHTTPTimeout > 0.
// The default 30s causes background index aborts under boot-time load on the shared
// external embed host (embed.krolik.tools), where 32-text sub-batches exceed 30s
// p14 — triggering 3× retry (~90s total) then goroutine exit before SHA advance.
func newCodeEmbedder(cfg Config) (*embed.Client, error) {
	opts := []embed.Opt{
		embed.WithBackend("http"),
		embed.WithModel(cfg.EmbedModel),
		embed.WithDim(codeEmbedDim),
	}
	if cfg.EmbedHTTPTimeout > 0 {
		opts = append(opts, embed.WithTimeout(cfg.EmbedHTTPTimeout))
	}
	return embed.NewClient(cfg.EmbedURL, opts...)
}

// newDesignEmbedder constructs the design-search embedder (multilingual-e5-large, 1024d).
// Powers design_search and the index-designs CLI. Writes into the
// pgvector(1024) design_embeddings table — must NOT be swapped for the
// code-trained 768d jina model.
func newDesignEmbedder(cfg Config) (*embed.Client, error) {
	return embed.NewClient(cfg.DesignEmbedURL,
		embed.WithBackend("http"),
		embed.WithModel(cfg.DesignEmbedModel),
		embed.WithDim(designEmbedDim),
	)
}

// newSparseEmbedder constructs the SPLADE sparse embedder when SPARSE_EMBED_URL
// is configured. Returns nil when URL is empty — the Pipeline then uses the
// dense-only cold-path (byte-identical to pre-P2 behaviour). Bearer token is
// auto-resolved from EMBED_TOKEN env by go-kit/sparse v2 NewHTTPSparseEmbedder.
func newSparseEmbedder(cfg Config) sparse.SparseEmbedder {
	if cfg.SparseEmbedURL == "" {
		return nil
	}
	return sparse.NewHTTPSparseEmbedder(
		cfg.SparseEmbedURL,
		cfg.SparseEmbedModel,
		nil, // logger: nil → slog.Default()
		sparse.WithBearerToken(os.Getenv("EMBED_TOKEN")),
	)
}

// symbolBoostAdapter wraps *embeddings.Store to satisfy analyze.SymbolNameSearcher.
// analyze.SymbolNameSearcher returns []analyze.SymbolHit (FilePath only), while
// embeddings.Store.SearchBySymbolName returns []embeddings.SearchResult (full record).
// This adapter lives here — co-located with the wiring — instead of a separate file.
type symbolBoostAdapter struct {
	store *embeddings.Store
}

func (a *symbolBoostAdapter) SearchBySymbolName(
	ctx context.Context,
	repoKey string,
	keywords []string,
	language string,
	limit int,
) ([]analyze.SymbolHit, error) {
	results, err := a.store.SearchBySymbolName(ctx, repoKey, keywords, language, limit)
	if err != nil {
		return nil, err
	}
	hits := make([]analyze.SymbolHit, len(results))
	for i, r := range results {
		hits[i] = analyze.SymbolHit{FilePath: r.FilePath}
	}
	return hits, nil
}

// buildCloneTokenFunc returns a CloneTokenFunc for analyze.Deps.
// When GitHub App credentials are fully configured, returns appTokenSource.Token
// (issues ghs_ installation tokens with auto-refresh).
// Otherwise returns a static closure that yields the configured PAT.
func buildCloneTokenFunc(cfg Config) func(ctx context.Context) (string, error) {
	if cfg.GithubAppConfig.IsConfigured() {
		src, err := forge.NewAppTokenSource(forge.AppAuthConfig{
			AppID:          cfg.GithubAppConfig.AppID,
			InstallationID: cfg.GithubAppConfig.InstallationID,
			PrivateKeyPEM:  cfg.GithubAppConfig.KeyPEM,
		})
		if err != nil {
			slog.Warn("github app token source init failed; clone will use PAT fallback",
				slog.Any("error", err),
			)
			// Fall through to PAT.
		} else {
			return src.Token
		}
	}
	pat := cfg.GithubToken
	return func(_ context.Context) (string, error) {
		return pat, nil
	}
}
