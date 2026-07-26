// go-code — Code intelligence MCP server.
//
// Provides multi-language AST parsing via tree-sitter, repository analysis,
// code comparison, and dependency graph visualization.
// Runs as HTTP MCP server (default) or stdio transport (--stdio flag).
//
// Tools: repo_analyze, file_parse, code_compare, dep_graph, symbol_search, call_trace,
// impact_analysis, dead_code, explore, code_health, code_graph, repo_search, code_search,
// semantic_search
package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/anatolykoptev/go-kit/env"
	kitmetrics "github.com/anatolykoptev/go-kit/metrics"
	"github.com/anatolykoptev/go-kit/metrics/mcpmw"
	"github.com/anatolykoptev/go-kit/tracing"
	"github.com/anatolykoptev/go-kit/tracing/httpmw"
	tracemcpmw "github.com/anatolykoptev/go-kit/tracing/mcpmw"
	"github.com/anatolykoptev/go-kit/tracing/slogh"
	"github.com/anatolykoptev/go-mcpserver"
	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/argnorm"
	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/designmd"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel/attribute"
)

// version is set at build time via -ldflags "-X main.version=...".
// Falls back to "dev" for local `go run` / `go build` without flags.
var version = "dev"

// toolTimeouts maps MCP tool names to per-tool deadline overrides, wired
// into mcpserver.Config.ToolTimeouts. Tools absent from this map ride the
// 90s harness default.
//
// Exported as a package-level var (not inlined in main) so tests can assert
// the deployed config — regression guard for the 2026-05-27 incident where
// "understand" was missing and silently inherited the 90s default.
var toolTimeouts = map[string]time.Duration{
	"code_research":     90 * time.Second,
	"repo_analyze":      90 * time.Second,
	"review_delta":      120 * time.Second, // #391: cold code-graph build on a large delta exceeds the 90s default
	"code_compare":      95 * time.Second,  // compareTimeout is 90s; leave headroom for XML marshal
	"code_graph":        120 * time.Second, // Cypher query + LLM narrative on large graphs can exceed 90s default
	"call_trace":        90 * time.Second,  // cold cache parse (5min TTL) + LLM narrative; was 60s, not enough on cold start
	"code_health":       60 * time.Second,
	"understand":        30 * time.Second, // Fix #3: dead embed server + AGE lookups complete well within 30s (Fix #2 caps embed at 5s)
	"debug_investigate": 5 * time.Minute,
	// semantic_search: 30s cap (belt-and-suspenders; the index trigger detaches
	// to context.Background() via IndexRepoAsync so client disconnect does NOT
	// abort indexing — but the tool response itself must return within 30s so
	// the embed query + store search leg don't hold the connection open forever).
	"semantic_search": 30 * time.Second,
}

const (
	serviceName = "vaelor"

	defaultPort = "8897"

	// metricsNamespace is the Prometheus namespace for all gocode metrics.
	// Exported as a package-level constant so tests can verify the production
	// namespace is used (closes the synthetic-green gap where a test with a
	// disconnected "gocodetest" namespace would pass even if main changed it).
	metricsNamespace = "gocode"

	// workspaceDirPerm is the permission mode for the workspace directory.
	workspaceDirPerm = 0o750
)

func main() {
	cfg, err := loadConfig()
	if err != nil {
		slog.Error("failed to load config", slog.Any("error", err))
		os.Exit(1)
	}

	// Surface ANALYZE_RANK_WEIGHT_* knobs that are set but inert in the default
	// minmax mode (weights only apply to the rrf path). Makes the no-op
	// explicit instead of advertising the knob as global (#606).
	warnInertRankWeights(cfg)

	// Wire the analyze package's fusion config + publish gocode_analyze_fusion_mode
	// gauge before any analyze call path runs. Default minmax = byte-identical
	// legacy; rrf is opt-in via ANALYZE_RANK_FUSION_MODE=rrf.
	analyze.SetFusionConfig(analyze.FusionConfig{
		Mode:           cfg.AnalyzeRankFusionMode,
		WeightBM25:     cfg.AnalyzeRankWeightBM25,
		WeightPageRank: cfg.AnalyzeRankWeightPageRank,
		WeightSeed:     cfg.AnalyzeRankWeightSeed,
	})

	// Strangler-fig fallback (ADR-3): keep the raw os.Args index-designs path
	// for one release so existing scripts keep working. The cobra subcommand
	// (registered in newRootCmd) is the primary path; this intercept runs first
	// and returns. Both call the same runIndexDesigns(cfg, dir).
	if len(os.Args) >= 3 && os.Args[1] == "index-designs" {
		runIndexDesigns(cfg, os.Args[2])
		return
	}

	root := newRootCmd(cfg)
	if err := root.Execute(); err != nil {
		slog.Error("cli failed", slog.Any("error", err))
		os.Exit(1)
	}
}

// runMCPServe starts the MCP server — byte-identical to the legacy main() body.
// Extracted so the cobra root command's default path (no subcommand) can call
// it without duplicating the startup sequence (ADR-3 strangler-fig).
func runMCPServe(cfg Config) {
	if err := os.MkdirAll(cfg.WorkspaceDir, workspaceDirPerm); err != nil {
		slog.Error("failed to create workspace dir", slog.Any("error", err))
		os.Exit(1)
	}

	slog.Info("starting "+serviceName,
		slog.String("llm_model", cfg.LLMModel),
		slog.String("llm_url", cfg.LLMURL),
	)

	// Lifecycle context: cancelled on SIGINT/SIGTERM. Passed to mcpserver.Run
	// (so it owns shutdown) and to EagerWarmRepos (so in-flight `go build`
	// subprocesses are cancelled on graceful shutdown instead of running to
	// the per-repo 5-min timeout).
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	shutdownTracing, err := tracing.Setup(ctx, serviceName,
		tracing.WithSampleRatio(1.0),
		tracing.WithAttributes(attribute.String("version", version)),
	)
	if err != nil {
		slog.Warn("otel tracing setup failed; continuing without traces", "err", err)
	} else {
		defer func() {
			sctx, scancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer scancel()
			_ = shutdownTracing(sctx)
		}()
	}

	// Wrap the default slog handler with slogh.Handler so every log record
	// emitted with a context carrying an active span gets trace_id + span_id
	// attrs. Enables log↔trace correlation in Jaeger without changing call sites.
	slog.SetDefault(slog.New(slogh.NewHandler(slog.NewTextHandler(os.Stderr, nil))))

	reg := kitmetrics.NewPrometheusRegistry(metricsNamespace)
	startPrometheusScrape(ctx, slog.Default())

	server := mcpserver.NewServer(&mcp.Implementation{
		Name:    serviceName,
		Version: version,
	}, mcpserver.Config{
		SchemaCache: mcp.NewSchemaCache(),
	})

	deps, pipeline, graphStore := registerTools(ctx, server, cfg, reg)
	slog.Info("tools registered", slog.Int("count", argnorm.Default().Count()))

	// Eager GOCACHE pre-warm for AUTO_INDEX_DIRS Go repos. Runs in a
	// background goroutine so it does not block MCP serve. Eliminates the
	// cold-cache `tier: basic` window on the first call_trace per repo.
	// Default-on when AUTO_INDEX_DIRS is set (the explicit indexing signal);
	// set EAGER_WARM=false to disable. Accepts "false", "0", "f", "FALSE",
	// etc. via strconv.ParseBool; invalid values warn and default to true.
	eager := true
	if v := os.Getenv("EAGER_WARM"); v != "" {
		if parsed, err := strconv.ParseBool(v); err == nil {
			eager = parsed
		} else {
			slog.Warn("invalid EAGER_WARM value, defaulting to true", "value", v)
		}
	}
	if len(cfg.AutoIndexDirs) > 0 && eager {
		go callgraph.EagerWarmRepos(ctx, autoIndexDirs(cfg))
	}

	// Opt-in file watcher (ADR-9): starts a background goroutine that watches
	// discovered repos for save events and calls Pipeline.IndexFile per
	// debounced event (ADR-4). Default off — one-way door (security_cost).
	// Graceful degradation (ADR-16): watcher failure does NOT crash the MCP
	// server. Reuses the signal.NotifyContext ctx for cancellation so
	// SIGINT/SIGTERM triggers graceful watcher shutdown.
	if cfg.WatchEnabled {
		go startFileWatcher(ctx, cfg, pipeline, reg, graphStore)
	}

	// Webhook handler registered via mcpserver.Config.Routes below so it shares
	// the server's mux (http.DefaultServeMux is unused by mcpserver).
	var webhookHandler http.Handler
	if secret := os.Getenv("GITHUB_WEBHOOK_SECRET"); secret != "" {
		enabled := os.Getenv("REVIEW_POST_ENABLED") == "true"
		botUser := os.Getenv("REVIEW_BOT_USER")
		sink := func(event string, payload []byte) {
			DispatchGitHubEvent(event, payload, dispatchDeps{
				botUser: botUser,
				postReview: func(slug string, pr int) error {
					if !enabled {
						log.Printf("review_post disabled; would review %s#%d", slug, pr)
						return nil
					}
					ctx := context.Background()
					_, err := handleReviewPR(ctx, ReviewPRInput{Repo: slug, PR: pr, Event: "COMMENT"}, deps, nil)
					return err
				},
				postPushReview: func(slug, before, after string) error {
					if !enabled {
						log.Printf("review_post disabled; would review push %s %s..%s", slug, before[:8], after[:8])
						return nil
					}
					return handlePushReview(slug, before, after, deps)
				},
			})
		}
		handler := newGitHubWebhook(secret, sink)
		webhookHandler = handler
		slog.Info("webhook registered", slog.String("path", "/webhook/github"))
	}

	// Build the combined HTTP routes: webhook (conditional) + resolve (conditional).
	// Wrap mux in httpmw.Mux so each Handle call auto-registers code.* OTEL
	// attrs via reflect — no manual RegisterRoute calls needed.
	resolveHosts := cfg.SourcemapAllowedHosts
	combinedRoutes := func(mux *http.ServeMux) {
		wrapped := &httpmw.Mux{ServeMux: mux}
		if webhookHandler != nil {
			wrapped.Handle("POST /webhook/github", webhookHandler)
		}
		if len(resolveHosts) > 0 && resolveFrameResolver != nil {
			wrapped.Handle("POST /resolve", resolveHTTPHandler(resolveHosts, resolveFrameResolver, resolveHTTPRateLimiter))
			slog.Info("resolve endpoint registered", slog.String("path", "/resolve"))
		}
	}

	hooks := mcpserver.MCPHooks{
		OnToolCall: func(ctx context.Context, name string) {
			slog.InfoContext(ctx, "tool_call", slog.String("tool", name))
		},
		OnToolResult: func(ctx context.Context, name string, dur time.Duration, isErr bool) {
			slog.InfoContext(ctx, "tool_result", slog.String("tool", name), slog.Duration("duration", dur), slog.Bool("error", isErr))
		},
	}

	// Merge static toolTimeouts with runtime-configurable overrides.
	// sparse_backfill deadline is driven by SPARSE_BACKFILL_DEADLINE_S because
	// the default 90s harness value silently cancelled mid-batch on large repos
	// (440 rows lost, root cause: 90s < time for 207-page full backfill).
	runtimeTimeouts := make(map[string]time.Duration, len(toolTimeouts)+1)
	for k, v := range toolTimeouts {
		runtimeTimeouts[k] = v
	}
	runtimeTimeouts["sparse_backfill"] = cfg.SparseBackfillDeadline

	if err := mcpserver.Run(server, mcpserver.Config{
		Name:                       serviceName,
		Version:                    version,
		Port:                       cfg.Port,
		Context:                    ctx,
		SchemaCache:                mcp.NewSchemaCache(),
		DisableLocalhostProtection: true,
		SessionTimeout:             10 * time.Minute,
		Logger:                     slog.Default(), // preserve slogh wrapper; mcpserver would otherwise replace it
		MCPLogger:                  slog.Default(),
		MCPReceivingMiddleware: []mcp.Middleware{
			argnorm.Middleware(argnorm.Default()), // first (outermost): normalize args + tool names before metrics/tracing observe
			tracemcpmw.Middleware(serviceName),
			hooks.Middleware(),
			mcpmw.Middleware(reg, "tool"),
		},
		Middleware:   []mcpserver.Middleware{func(next http.Handler) http.Handler { return httpmw.Handler(serviceName, next) }},
		RESTBridge:   true,
		Routes:       combinedRoutes,
		LogSkipPaths: []string{"/health", "/health/live", "/health/ready", "/metrics"},
		ToolTimeouts: runtimeTimeouts,
		// SSE (text/event-stream) mode. Long tool calls (code_research, debug_investigate,
		// code_graph, etc.) emit no bytes until they finish; in stateless mode the
		// server can't send ping requests, so a client/proxy idle-timeout would
		// abandon the call while the server keeps working. Instead,
		// ToolKeepaliveInterval emits a progress notification on the request
		// stream every 10s to keep it warm. (The old KeepAlive:30s ping was
		// inert in stateless mode — rejected, then closed the session at 30s and
		// truncated the response; removed.) Caddy forces HTTP/1.1 to the
		// upstream, fixing the h2 stream-reset that originally motivated JSON.
		JSONResponse:          false,
		ToolKeepaliveInterval: 10 * time.Second,
	}); err != nil {
		slog.Error("server failed", slog.Any("error", err))
	}
}

// startPrometheusScrape runs an HTTP server exposing /metrics on PROM_PORT
// (default 9897 = MCP_PORT+1000) for prometheus scrape. Separate port avoids
// BearerAuth on scrape traffic; bound to all interfaces for container scrape.
func startPrometheusScrape(ctx context.Context, logger *slog.Logger) {
	promPort := env.Str("PROM_PORT", "9897")
	mux := buildPromMux()
	srv := &http.Server{
		Addr:              ":" + promPort,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		logger.Info("prometheus scrape endpoint", slog.String("addr", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("prom endpoint", slog.Any("error", err))
		}
	}()
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()
}

// buildPromMux constructs the metrics-listener mux (PROM_PORT, 9897). It owns
// its own *http.ServeMux — NOT http.DefaultServeMux — so the pprof handlers
// registered here live on the metrics listener only and never leak onto the
// MCP listener (8897), which uses a separate mux inside mcpserver.Run.
//
// pprof is registered unconditionally (issue #754): a flag that must be set
// before a profile can be taken is a flag nobody sets during the incident, and
// the incident is the only time it matters. Both ports are loopback-only
// (docker port mapping), so this adds no external surface beyond /metrics.
// Handlers are wired explicitly rather than via `_ "net/http/pprof"` (whose
// init registers on DefaultServeMux) to keep the debug surface on this mux
// alone. allocs is registered alongside heap because a retention bug and a
// churn bug are indistinguishable in inuse_space alone.
func buildPromMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/metrics", kitmetrics.MetricsHandler())
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	mux.Handle("/debug/pprof/allocs", pprof.Handler("allocs"))
	mux.Handle("/debug/pprof/goroutine", pprof.Handler("goroutine"))
	mux.Handle("/debug/pprof/heap", pprof.Handler("heap"))
	return mux
}

func runIndexDesigns(cfg Config, dir string) {
	// Fallback to code embed config when design-specific URL is unset (legacy
	// behavior). The factory uses cfg.DesignEmbedURL / cfg.DesignEmbedModel, so
	// patch cfg in place for the CLI path only.
	if cfg.DesignEmbedURL == "" {
		cfg.DesignEmbedURL = cfg.EmbedURL
		cfg.DesignEmbedModel = cfg.EmbedModel
	}
	if cfg.DesignEmbedURL == "" {
		slog.Error("DESIGN_EMBED_URL (or EMBED_URL) is required for indexing")
		os.Exit(1)
	}
	if cfg.DatabaseURL == "" {
		slog.Error("DATABASE_URL is required for indexing")
		os.Exit(1)
	}

	pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		slog.Error("database connect failed", slog.Any("error", err))
		os.Exit(1)
	}
	defer pool.Close()

	client, err := newDesignEmbedder(cfg)
	if err != nil {
		slog.Error("embed client failed", slog.Any("error", err))
		os.Exit(1)
	}
	store := designmd.NewStore(pool)

	result, err := designmd.Index(context.Background(), dir, client, store)
	if err != nil {
		slog.Error("indexing failed", slog.Any("error", err))
		os.Exit(1)
	}

	slog.Info("indexing complete",
		slog.Int("brands", result.Brands),
		slog.Int("indexed", result.Indexed),
		slog.Int("skipped", result.Skipped),
	)
}
