package main

import (
	"context"
	"fmt"
	"sort"
	"sync/atomic"
	"time"

	"github.com/anatolykoptev/go-kit/embed"
	"github.com/anatolykoptev/go-kit/sparse"
	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/codegraph"
	"github.com/anatolykoptev/vaelor/internal/embeddings"
	"github.com/anatolykoptev/vaelor/internal/graphx"
	"github.com/anatolykoptev/vaelor/internal/mcpmeta"
	"github.com/anatolykoptev/vaelor/internal/oxcodes"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// SemanticSearchInput is the input schema for the semantic_search tool.
type SemanticSearchInput struct {
	Repo        string  `json:"repo,omitempty" jsonschema:"GitHub repo (owner/repo) or local path to search in. Required in practice — omitting it returns a short error naming recently-indexed repos."`
	Query       string  `json:"query" jsonschema:"Natural language description of what you're looking for (e.g. 'function that validates JWT tokens', 'error handling for database connections')"`
	Language    string  `json:"language,omitempty" jsonschema:"Filter by language (e.g. go, python, typescript)"`
	TopK        int     `json:"top_k,omitempty" jsonschema:"Number of results (default 10, max 50)"`
	MaxDistance float32 `json:"max_distance,omitempty" jsonschema:"Maximum cosine distance (0.0-1.0, default 0.75). Lower = stricter matching"`
	MaxBytes    int     `json:"max_bytes,omitempty" jsonschema:"Response budget in bytes (default 8192). When the response exceeds this, the ranked head is returned with a continuation footer."`
}

// SemanticDeps holds dependencies for semantic search.
type SemanticDeps struct {
	Client *embed.Client
	// QueryClient is the model-aware query embedder. For code-rank-embed it
	// wraps Client with the required retrieval prefix; for other models it is
	// identical to Client. Always use QueryClient (not Client) for user-query
	// embedding so the prefix asymmetry is applied correctly.
	QueryClient embeddings.QueryEmbedder
	Store       *embeddings.Store
	Pipeline    *embeddings.Pipeline
	AnalyzeDeps analyze.Deps
	Expander    *embeddings.Expander
	GraphStore  *codegraph.Store // nil when DATABASE_URL is unset; used by hotspot/recency arms
	OxCodes     *oxcodes.Client
	// RRFWeights are the per-retriever weights threaded into MergeRRF.
	// Defaults to (1.0, 1.0, 0.0, 0.25, 0.15, 0.1) — Sparse dark-launched at 0.0.
	RRFWeights embeddings.RRFWeights
	// GraphStalenessThreshold is the max graph age before the retrieval path
	// considers the AGE graph stale (#691). Zero = use the default (30 min).
	// When stale, the gate triggers self-heal + a degradation marker, and
	// (when DropStaleGraphArms is true) drops the graph+hotspot arms.
	GraphStalenessThreshold time.Duration
	// DropStaleGraphArms is the dark-launch flag for the drop-and-renormalise
	// sub-change (#691 C). Default false — changes ranking, needs A/B first.
	DropStaleGraphArms bool
	// GraphIndexCfg is the codegraph.IndexConfig passed to the self-heal
	// background build. Mirrors the tool gate's indexCfg. Zero value is safe —
	// IndexRepo applies defaults via applyConfigDefaults.
	GraphIndexCfg codegraph.IndexConfig
	// SparseClient is the SPLADE sparse embedder used for query-time retrieval
	// (P4 dark-launch). Nil when SPARSE_EMBED_URL is unset — arm is bypassed
	// entirely, yielding byte-identical behavior to the 2-arm baseline.
	SparseClient sparse.SparseEmbedder
	// KeywordArm selects the lexical retriever for the Keyword slot of MergeRRF.
	// "grep" (default) → byte-identical to pre-BM25F behavior.
	// "bm25f" → BM25F over trigram-prefiltered candidates (BM25F P4 dark-launch).
	// runKeywordArm() reads this field and falls back to grep on bm25f error.
	KeywordArm string
	// storeSearcher is the interface used by semanticSuggest for trigram name
	// lookup. Production leaves this nil and semanticSuggest falls back to Store.
	// Tests wire a spy here to avoid a real Postgres connection.
	storeSearcher symbolNameSearcher
	// bm25searcher is the BM25Search test seam. Production leaves this nil and
	// runKeywordArm falls back to Store. Tests wire a spy to avoid a live pool.
	bm25searcher bm25Searcher
	// graphCandidatesFunc is the graph-arm test seam. Production leaves this nil and
	// handleSemanticHits calls Expander.GraphCandidates directly. Tests wire a spy
	// to avoid a live AGE connection.
	graphCandidatesFunc graphCandidatesFn
	// storeSearcherSeam is the test seam for Store.Search (the vector search
	// call). Production leaves this nil and handleSemanticSearch falls back to
	// deps.Store. Tests wire a fake to avoid a live Postgres pool.
	storeSearcherSeam vectorSearcher
	// staleModelChecker is the stale-hit guard test seam for store.GetStoredModel.
	// Production leaves this nil and the guard falls back to deps.Store directly.
	staleModelChecker modelChecker
	// pipelineInvalidatorFunc is the stale-hit guard test seam for the pipeline
	// operations (EmbedModel, InvalidateIfModelChanged, IsIndexing,
	// IndexRepoAsyncWithTool). Production leaves this nil and the guard uses
	// deps.Pipeline directly.
	pipelineInvalidatorSeam pipelineInvalidator
}

// graphCandidatesFn is the function type for graph candidate generation,
// extracted for test-seam injection without a live AGE pool.
type graphCandidatesFn func(
	ctx context.Context,
	graphName string,
	queryTerms []string,
	seeds []embeddings.SearchResult,
	prSignals []graphx.Signal,
	opts *embeddings.GraphCandidatesOpts,
) []embeddings.GraphHit

const (
	defaultSemanticTopK = 10
	maxSemanticTopK     = 50
	// semanticRerankCandidates is the minimum candidate pool for CE reranker.
	// Ensures at least 20 candidates regardless of topK.
	semanticRerankCandidates = 20
	// semanticSearchGraphHint is shown in indexing status responses so the
	// caller knows how to enable the graph/hotspot/recency RRF arms.
	semanticSearchGraphHint = "To enable graph/hotspot/recency arms, run code_graph."
	semanticSearchRetryHint = "Please retry in 30-60 seconds."
)

// semanticFormatCount is a test-only seam for the render-count laziness
// assertion. Nil in production (zero overhead); tests set it to an int64
// counter that the three formatSemanticResults* functions increment via
// atomic.AddInt64. The test then asserts EXACTLY ONE increment when rung 1
// fits — proving the unreached rungs were never rendered. Without this, the
// eager-render form (pre-computing all three renderings before the ladder
// runs) comes straight back with a green suite, because
// TestPickFitting_UnreachedRungClosureNeverCalled tests PickFitting in
// isolation and cannot see what the caller does before calling it.
var semanticFormatCount *int64

// registerSemanticSearch registers the semantic_search MCP tool.
func registerSemanticSearch(server *mcp.Server, cfg Config, deps SemanticDeps) {
	outputDir := cfg.OutputDir

	addTool(server, &mcp.Tool{
		Name: "semantic_search",
		Description: "Find code by meaning using natural language queries. " +
			"Uses hybrid RRF (semantic + keyword + graph-candidate + hotspot + recency) with 1-hop graph expansion via Apache AGE. " +
			"Works best after the repository has been indexed via code_graph or repo_analyze. " +
			"Returns ranked results with file paths, symbol names, and similarity scores.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input SemanticSearchInput) (*mcp.CallToolResult, error) {
		return handleSemanticSearch(ctx, input, deps, outputDir)
	})
}

func handleSemanticSearch(
	ctx context.Context, input SemanticSearchInput, deps SemanticDeps, outputDir string,
) (*mcp.CallToolResult, error) {
	if input.Repo == "" {
		return errResult(shortMissingRepoMsg(ctx, deps.Store, deps.AnalyzeDeps.LocalRepoDirs)), nil
	}
	if input.Query == "" {
		return errResult("query is required"), nil
	}
	if deps.Client == nil || deps.QueryClient == nil || (deps.Store == nil && deps.storeSearcherSeam == nil) {
		return textResult(buildStatusResponse(input, "disabled",
			"Semantic search is not available: embedding service not configured. "+
				"Set EMBED_URL and EMBED_MODEL environment variables to enable.")), nil
	}

	topK := input.TopK
	if topK <= 0 {
		topK = defaultSemanticTopK
	}
	if topK > maxSemanticTopK {
		topK = maxSemanticTopK
	}

	maxDist := input.MaxDistance
	if maxDist <= 0 {
		maxDist = 0.85 // CE reranker filters noise; higher threshold improves recall
	}

	// Resolve repo root.
	root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps.AnalyzeDeps)
	if err != nil {
		return errResult(fmt.Sprintf("resolve repo: %s", err)), nil
	}
	defer cleanup()

	t0 := time.Now()

	// Soft deadline: 25s default, below the 30s client timeout. On expiry,
	// return whatever results we have so far with a partial footer instead
	// of computing past the point anyone is listening (#572).
	softCtx, softCancel := mcpmeta.SoftDeadline(ctx)
	defer softCancel()

	repoKey := codegraph.GraphNameFor(root)

	// Embed query first (fast, ~1s).
	// Use QueryClient (not Client) so model-specific prefixes (e.g. code-rank-embed
	// retrieval prefix) are applied on the query path only. Document embedding in
	// the Pipeline always uses Client.Embed without any prefix.
	vector, err := deps.QueryClient.EmbedQuery(softCtx, input.Query)
	if err != nil {
		if softCtx.Err() != nil {
			return softDeadlineResult(
				fmt.Sprintf("semantic_search: timed out during query embedding after %s — retry with a simpler query.", time.Since(t0).Round(time.Second)),
				"query embedding, vector search, hybrid merge (soft deadline)",
				time.Since(t0),
			), nil
		}
		return errResult(fmt.Sprintf("embed query: %s", err)), nil
	}

	// Try searching existing embeddings.
	// Seam: storeSearcherSeam lets tests inject a fake Store.Search without a
	// live Postgres pool. Production leaves it nil and falls back to deps.Store.
	searcher := deps.storeSearcherSeam
	if searcher == nil {
		searcher = deps.Store
	}
	results, err := searcher.Search(softCtx, vector, embeddings.SearchOpts{
		RepoKey:     repoKey,
		Language:    input.Language,
		TopK:        topK,
		MaxDistance: maxDist,
	})
	if err != nil {
		if softCtx.Err() != nil {
			return softDeadlineResult(
				fmt.Sprintf("semantic_search: timed out during vector search after %s — query was embedded but pgvector search exceeded the soft deadline.", time.Since(t0).Round(time.Second)),
				"vector search, hybrid merge (soft deadline)",
				time.Since(t0),
			), nil
		}
		return errResult(fmt.Sprintf("search: %s", err)), nil
	}

	if len(results) > 0 {
		// Stale-space guard: results returned from a repo whose stored embed_model
		// differs from the active model are in the wrong embedding space (boot-window
		// mixed-space hit OR lazy-only-forever stale index). Discard them, purge the
		// stale vectors, and trigger a full reindex — treating the stale hit as a MISS.
		//
		// Common-case cost: one cheap SELECT on code_repo_state (negligible next to
		// the vector scan). The guard is skipped when Pipeline is nil or EmbedModel
		// is "" (legacy pipelines with no model tracking).
		//
		// Seams: staleModelChecker and pipelineInvalidatorSeam are nil in production
		// and resolve to deps.Store / deps.Pipeline respectively. Tests wire fakes to
		// avoid live Postgres / Pipeline.
		checker := deps.staleModelChecker
		if checker == nil && deps.Store != nil {
			checker = deps.Store
		}
		invalidator := deps.pipelineInvalidatorSeam
		if invalidator == nil && deps.Pipeline != nil {
			invalidator = deps.Pipeline
		}
		if checker != nil && invalidator != nil && invalidator.EmbedModel() != "" {
			storedModel := checker.GetStoredModel(softCtx, repoKey)
			// Defense-in-depth: when code_repo_state has no row for this repo_key
			// (e.g. orphan vectors from a removed checkout), GetStoredModel returns "".
			// Fall back to reading embed_model from code_embeddings rows directly so
			// the guard fires even for repos with no state row.
			if storedModel == "" {
				if prc, ok := checker.(perRowModelChecker); ok {
					storedModel = prc.GetEmbedModelForRepo(softCtx, repoKey)
				}
			}
			if storedModel != "" && storedModel != invalidator.EmbedModel() {
				// Stale-space hit: invalidate (purge old vectors) and reindex.
				invalidator.InvalidateIfModelChanged(softCtx, repoKey) // purges atomically
				if invalidator.IsIndexing(repoKey) {
					done, total, _ := invalidator.IndexProgress(repoKey)
					msg := "Repository is being re-indexed (embedding model changed). " +
						semanticSearchGraphHint + " " + semanticSearchRetryHint
					if total > 0 {
						msg = fmt.Sprintf("Re-indexing in progress (model changed): %d/%d symbols. %s %s",
							done, total, semanticSearchGraphHint, semanticSearchRetryHint)
					}
					return semanticSearchIndexingResponse(input, msg), nil
				}
				invalidator.IndexRepoAsyncWithTool("semantic_search", repoKey, root)
				return semanticSearchIndexingResponse(input,
					"Embedding model changed — re-indexing started. "+
						semanticSearchGraphHint+" "+semanticSearchRetryHint), nil
			}
		}
		return handleSemanticHits(softCtx, input, deps, repoKey, root, results, topK, maxDist, outputDir, t0)
	}

	// No results — start background indexing if not already running.
	if deps.Pipeline != nil {
		if deps.Pipeline.IsIndexing(repoKey) {
			done, total, _ := deps.Pipeline.IndexProgress(repoKey)
			msg := "Repository is being indexed in the background. " +
				semanticSearchGraphHint + " " + semanticSearchRetryHint
			if total > 0 {
				msg = fmt.Sprintf("Indexing in progress: %d/%d symbols embedded. %s %s",
					done, total, semanticSearchGraphHint, semanticSearchRetryHint)
			}
			return semanticSearchIndexingResponse(input, msg), nil
		}
		deps.Pipeline.IndexRepoAsyncWithTool("semantic_search", repoKey, root)
		return semanticSearchIndexingResponse(input,
			"Repository indexing started in the background. "+
				semanticSearchGraphHint+" "+semanticSearchRetryHint), nil
	}

	return textResult(buildStatusResponse(input, "not_indexed",
		"No indexed code found and embedding pipeline is not configured. "+
			"Ensure EMBED_URL is set and retry.")), nil
}

// handleSemanticHits handles the path where semantic search returned results:
// graph expansion, hybrid keyword merge via RRF, and final formatting.
func handleSemanticHits(
	ctx context.Context, input SemanticSearchInput, deps SemanticDeps,
	repoKey, root string, results []embeddings.SearchResult, topK int, maxDist float32,
	outputDir string, t0 time.Time,
) (*mcp.CallToolResult, error) {
	// Trigger background re-index for freshness.
	if deps.Pipeline != nil {
		deps.Pipeline.IndexRepoAsyncWithTool("semantic_search", repoKey, root)
	}

	// #691: Graph freshness gate for the retrieval path. The graph (0.25) +
	// hotspot (0.15) arms — 40% of fused weight — read the AGE graph with no
	// freshness check, silently blending an arbitrarily stale graph. Gate:
	// check freshness (cached, cheap), self-heal if stale, mark the response,
	// and optionally drop+renormalise the stale arms (dark flag). The fresh
	// path is byte-identical to pre-#691 — no metric, no marker, no rebuild.
	// Only runs when graph-dependent arms are active (Graph or Hotspot > 0).
	var graphStaleAgeS float64
	if deps.GraphStore != nil && (deps.RRFWeights.Graph > 0 || deps.RRFWeights.Hotspot > 0) {
		threshold := deps.GraphStalenessThreshold
		if threshold <= 0 {
			threshold = time.Duration(defaultGraphStalenessThresholdS) * time.Second
		}
		weights := deps.RRFWeights
		graphStaleAgeS = gateRetrievalGraphFreshness(
			ctx, deps.GraphStore, root, repoKey,
			retrievalIsRemote(input.Repo), deps.GraphIndexCfg,
			threshold, deps.DropStaleGraphArms, &weights,
		)
		if graphStaleAgeS > 0 {
			deps.RRFWeights = weights
		}
	}

	// Graph expansion: add 1-hop CALLS neighbors before hybrid merge
	// so graph-expanded symbols can participate in RRF naturally.
	if deps.Expander != nil {
		const maxGraphExtra = 5
		extra := deps.Expander.Expand(ctx, repoKey, results, maxGraphExtra)
		results = append(results, extra...)
	}

	// Symbol name search: find functions whose names contain query keywords.
	// Fills recall gaps where vector distance misses well-named private functions.
	if deps.Store != nil {
		kws := embeddings.ExtractQueryKeywords(input.Query)
		if nameHits, nerr := deps.Store.SearchBySymbolName(ctx, repoKey, kws, input.Language, 20); nerr == nil {
			results = append(results, nameHits...)
		}
	}

	// Hybrid: run keyword search and (P4 dark-launch) sparse retrieval, then
	// merge all arms with 3-way weighted RRF.
	// Overretrieve before CE reranking so the reranker sees more candidates.
	rerankCap := max(topK*2, semanticRerankCandidates)

	// Keyword arm: flag-gated (KEYWORD_ARM=grep|bm25f, default grep).
	// runKeywordArm returns []KeywordHit ready for MergeRRF — no MatchKeywordHits
	// needed when bm25f supplies hits directly (it already maps to KeywordHit).
	// grep path still returns FileLineHit and requires MatchKeywordHits (below).
	keyHits, matched := runKeywordArm(ctx, deps, input.Query, repoKey, root, input.Language, rerankCap)

	// SPLADE sparse arm (P4 dark-launch): nil client → no DB hit, empty slice.
	// Failure inside SearchSparse is logged + counter-bumped there; we always
	// get back a (possibly empty) slice — never an error we must handle here.
	// Empty sparse arm + weight 0.0 → byte-identical 2-arm output (guaranteed
	// by WeightedRRF math, verified by TestMergeRRF_EmptySparseArmIdentical).
	var sparseHits []embeddings.SparseHit
	if deps.SparseClient != nil && deps.Store != nil {
		sparseHits, _ = deps.Store.SearchSparse(ctx, input.Query, deps.SparseClient, embeddings.SearchOpts{
			RepoKey:  repoKey,
			Language: input.Language,
			TopK:     rerankCap,
		})
	}

	// Fetch TopPageRank batch once — reused by both the graph-candidate arm (sub-arm a)
	// and annotateWithPageRank. A single batch query per request regardless of arm weight;
	// annotateWithPageRank is always called, so this fetch is never wasted.
	var prSignals []graphx.Signal
	if deps.AnalyzeDeps.Graph != nil {
		const prBatch = 200
		if sigs, err := deps.AnalyzeDeps.Graph.TopPageRank(ctx, repoKey, prBatch); err == nil {
			prSignals = sigs
		}
	}

	// Graph-candidate arm (Phase 1 dark-launch): only called when RRF_WEIGHT_GRAPH > 0.
	// At weight 0 (default) this block is skipped → ZERO added hot-path latency.
	// prSignals already fetched above — sub-arm (a) is free (no extra AGE round-trip).
	// Empty graph arm + weight 0.0 → byte-identical output (WeightedRRF math, verified
	// by TestMergeRRF_EmptyGraphArmIdentical). Graceful nil on any AGE error.
	var graphHits []embeddings.GraphHit
	if deps.RRFWeights.Graph > 0 {
		graphHits = runGraphArm(ctx, deps, repoKey, input.Query, results, prSignals, rerankCap)
	}

	// grep path: FileLineHit → KeywordHit via MatchKeywordHits (DB symbol resolve).
	// bm25f path: already KeywordHit, keyHits is nil.
	if len(keyHits) > 0 && deps.Store != nil {
		if resolved, err := deps.Store.MatchKeywordHits(ctx, repoKey, keyHits); err == nil {
			matched = resolved
		}
	}

	if len(matched) > 0 || len(sparseHits) > 0 || len(graphHits) > 0 {
		return hybridResult(ctx, input, deps, repoKey, root, results, matched, sparseHits, graphHits, prSignals, rerankCap, topK, graphStaleAgeS, outputDir, t0)
	}
	return semanticOnlyResult(ctx, input, deps, repoKey, root, results, prSignals, topK, maxDist, graphStaleAgeS, outputDir, t0)
}

// runGraphArm generates graph-arm candidates for MergeRRF.
// Only called when deps.RRFWeights.Graph > 0 (dark-launch gate).
// prSignals is the already-fetched TopPageRank batch from handleSemanticHits —
// sub-arm (a) reuses it for free (zero additional AGE round-trips).
// Non-fatal: any AGE error inside GraphCandidates returns nil.
func runGraphArm(
	ctx context.Context,
	deps SemanticDeps,
	repoKey, query string,
	seeds []embeddings.SearchResult,
	prSignals []graphx.Signal,
	topK int,
) []embeddings.GraphHit {
	if deps.Expander == nil {
		return nil
	}

	kws := embeddings.ExtractQueryKeywords(query)

	fn := deps.graphCandidatesFunc
	if fn == nil {
		fn = deps.Expander.GraphCandidates
	}

	return fn(ctx, repoKey, kws, seeds, prSignals, &embeddings.GraphCandidatesOpts{TopK: topK})
}

// hybridResult runs the hybrid RRF merge → CE rerank → annotate → format pipeline.
// Called when at least one non-semantic arm (keyword, sparse, or graph) produced hits.
// prSignals is the already-fetched TopPageRank batch (may be nil when graph is cold).
func hybridResult(
	ctx context.Context, input SemanticSearchInput, deps SemanticDeps,
	repoKey, root string, semantic []embeddings.SearchResult,
	matched []embeddings.KeywordHit, sparse []embeddings.SparseHit, graph []embeddings.GraphHit,
	prSignals []graphx.Signal,
	rerankCap, topK int, graphStaleAgeS float64, outputDir string, t0 time.Time,
) (*mcp.CallToolResult, error) {
	// Build the union of candidate symbols so the signal arms (hotspot/recency)
	// can rank the same pool the primary retrievers produced.
	candidates := buildHybridCandidates(semantic, matched, sparse, graph)
	hotspot, recency := buildSignalHits(ctx, deps, repoKey, root, candidates, rerankCap)

	// Merge with an enlarged pool so CE reranker can pick the best topK.
	hybrid := embeddings.MergeRRF(semantic, matched, sparse, graph, hotspot, recency, rerankCap, deps.RRFWeights)

	// Flatten HybridResult → SearchResult for CE reranker.
	flat := make([]embeddings.SearchResult, len(hybrid))
	for i, h := range hybrid {
		flat[i] = h.SearchResult
		flat[i].Source = h.Source
	}
	return finalResult(ctx, input, deps, repoKey, root, flat, prSignals, topK, graphStaleAgeS, outputDir, t0)
}

// semanticOnlyResult filters by distance then applies CE rerank → annotate → format.
// Called when keyword and sparse arms yielded no hits.
// prSignals is the already-fetched TopPageRank batch (may be nil when graph is cold).
//
// MergeRRF (the hybrid path) deduplicates by FilePath+":"+SymbolName. This
// path skips MergeRRF, so both the dense-cosine arm and the trigram-name arm
// (appended by handleSemanticHits via SearchBySymbolName) can return the same
// symbol at different distances. We dedup here using the same key form, keeping
// the entry with the lowest Distance (best match) and preserving relative order.
func semanticOnlyResult(
	ctx context.Context, input SemanticSearchInput, deps SemanticDeps,
	repoKey, root string, results []embeddings.SearchResult,
	prSignals []graphx.Signal,
	topK int, maxDist float32, graphStaleAgeS float64, outputDir string, t0 time.Time,
) (*mcp.CallToolResult, error) {
	// Fallback to pure semantic — filter by distance (graph results have Distance=1.0).
	// Dedup by FilePath+":"+SymbolName, keeping the lowest Distance (best match).
	// Key form matches MergeRRF (internal/embeddings/rrf.go:98).
	seen := make(map[string]int, len(results)) // key → index in filtered
	filtered := make([]embeddings.SearchResult, 0, len(results))
	for _, r := range results {
		if maxDist > 0 && r.Distance >= maxDist {
			continue
		}
		key := r.FilePath + ":" + r.SymbolName
		if idx, ok := seen[key]; ok {
			// Already in filtered — keep the lower Distance (better cosine match).
			if r.Distance < filtered[idx].Distance {
				filtered[idx] = r
			}
			codegraph.RecordSemanticDupCollapsed("semantic_only")
			continue
		}
		seen[key] = len(filtered)
		filtered = append(filtered, r)
	}

	// If the signal arms are enabled, fuse them with the filtered semantic list.
	// This keeps the semantic-only path equivalent to the hybrid path when the
	// other retrievers are empty, while letting hotspot/recency boost ranking.
	rerankCap := max(topK*2, semanticRerankCandidates)
	candidates := make([]embeddings.GraphHit, 0, len(filtered))
	for _, r := range filtered {
		candidates = append(candidates, embeddings.GraphHit{
			FilePath:   r.FilePath,
			SymbolName: r.SymbolName,
			SymbolKind: r.SymbolKind,
			Line:       r.StartLine,
		})
	}
	hotspot, recency := buildSignalHits(ctx, deps, repoKey, root, candidates, rerankCap)

	hybrid := embeddings.MergeRRF(filtered, nil, nil, nil, hotspot, recency, rerankCap, deps.RRFWeights)
	flat := make([]embeddings.SearchResult, len(hybrid))
	for i, h := range hybrid {
		flat[i] = h.SearchResult
		flat[i].Source = h.Source
	}
	return finalResult(ctx, input, deps, repoKey, root, flat, prSignals, topK, graphStaleAgeS, outputDir, t0)
}

// finalResult runs stale-demote → CE reranking → PageRank annotation → freshness wrap → format.
// Shared terminal step for both the hybrid and semantic-only paths.
// prSignals is the already-fetched TopPageRank batch from handleSemanticHits.
// Passing it in avoids a second TopPageRank round-trip inside annotateWithPageRank.
func finalResult(
	ctx context.Context, input SemanticSearchInput, deps SemanticDeps,
	repoKey, root string, candidates []embeddings.SearchResult,
	prSignals []graphx.Signal,
	topK int, graphStaleAgeS float64, outputDir string, t0 time.Time,
) (*mcp.CallToolResult, error) {
	// Stale-demote safety-net (defense-in-depth on top of Bug B orphan hard-delete):
	// partition fresh-then-stale so missed orphan rows surface at the bottom, not
	// at rank 1-5. Binary signal: updated_at vs indexed_at generation. Non-op when
	// generation is zero (store unavailable) or STALE_DEMOTE=off.
	if deps.Store != nil {
		generation := deps.Store.GetIndexedAt(ctx, repoKey)
		candidates = embeddings.ApplyStaleDemote(candidates, generation, embeddings.StaleDemoteEnabled())
	}
	reranked := codegraph.RerankSemanticResults(ctx, root, input.Query, candidates, topK)
	reranked = annotateWithPageRank(reranked, prSignals)
	hint := mcpmeta.HintAfterCodeSearch(input.Query, len(reranked), symbolNameFromResults(reranked))
	env := mcpmeta.Wrap(time.Since(t0), hint)
	if sha := deps.AnalyzeDeps.IndexedSHA(ctx, repoKey); sha != "" {
		env = mcpmeta.WithFreshness(env, root, sha)
	}
	// #691: degradation marker — when the retrieval path fused a stale AGE
	// graph, carry the graph age on the response envelope so a caller can
	// tell it received degraded ranking. Zero (omitted via omitempty) when
	// the graph is fresh — byte-identical to pre-#691 behavior.
	if graphStaleAgeS > 0 {
		env.GraphStaleAgeS = graphStaleAgeS
	}
	// Progressive result-shortening ladder (#685 part 2): full → compact
	// (drop auxiliary attrs) → counts (per-file hit counts). renderLadder
	// owns the five invariants so this tool cannot forget one.
	//
	// Budget ownership: the LADDER owns the budget, not ShapeWithHint. The
	// ladder's budget is the per-call budget (ResolveBudget(max_bytes,
	// DefaultBudget)) — a caller passing max_bytes gets a ladder fitted to
	// that number, not a hardcoded DefaultBudget. ShapeWithHint is REMOVED
	// from this path: it and the ladder would both act on the same text
	// (double-shaping). The ladder replaces ShapeWithHint's truncation-with-
	// hint behaviour with a cheaper-but-complete rung — a better answer than
	// a hard-truncated fragment.
	//
	// Double-shaping prevention: when max_bytes > 0, MarkBudgetApplied
	// appends the budget-applied sentinel so the addTool wrapper's IsShaped
	// check returns true and it skips re-shaping at DefaultBudget. This is
	// critical when max_bytes > DefaultBudget (the body may be up to
	// max_bytes > DefaultBudget, and the wrapper's Shape at DefaultBudget
	// would truncate the tail). When max_bytes <= 0, the ladder fits to
	// DefaultBudget and the wrapper's Shape is a no-op (text fits). The
	// wrapper strips the marker (StripBudgetMarker) before the agent sees
	// it. Invariant 2 holds against the SAME budget the ladder used: the
	// ladder guarantees len(body) <= budget, and Shape(body, budget, "") is
	// a no-op because body fits.
	mappings := deps.AnalyzeDeps.PathMappings
	ladder := mcpmeta.Ladder{
		{Name: "full", Render: func() string {
			return appendMetaFooter(formatSemanticResults(input, reranked, mappings), env)
		}},
		{Name: "no-snippet", Render: func() string {
			return appendMetaFooter(formatSemanticResultsCompact(input, reranked, mappings), env)
		}},
		{Name: "counts", Render: func() string {
			return appendMetaFooter(formatSemanticResultsCounts(input, reranked, mappings), env)
		}},
	}
	budget := mcpmeta.ResolveBudget(input.MaxBytes, mcpmeta.DefaultBudget)
	body := renderLadder(ladder, "semantic_search", outputDir, budget)
	if input.MaxBytes > 0 {
		body = mcpmeta.MarkBudgetApplied(body)
	}
	return textResult(body), nil
}

// symbolNameFromResults returns the symbol name from the first result when there
// is exactly one result, or "" otherwise. Used to build calibrated hints.
func symbolNameFromResults(results []embeddings.SearchResult) string {
	if len(results) == 1 {
		return results[0].SymbolName
	}
	return ""
}

// annotateWithPageRank adds PageRank signals to results for architectural awareness.
// signals is the pre-fetched TopPageRank batch from handleSemanticHits; passing it
// in avoids a redundant AGE round-trip (the batch was already fetched for runGraphArm).
// Non-fatal: results without PageRank data keep zero value and are not shown in output.
func annotateWithPageRank(results []embeddings.SearchResult, signals []graphx.Signal) []embeddings.SearchResult {
	if len(signals) == 0 || len(results) == 0 {
		return results
	}

	prMap := make(map[string]float64, len(signals))
	for _, sig := range signals {
		key := sig.Symbol.File + ":" + sig.Symbol.Name
		prMap[key] = sig.PageRank
	}

	annotated := make([]embeddings.SearchResult, len(results))
	copy(annotated, results)
	for i, r := range annotated {
		if r.SymbolName == "" {
			continue
		}
		if pr, ok := prMap[r.FilePath+":"+r.SymbolName]; ok {
			annotated[i].PageRank = float32(pr)
		}
	}
	return annotated
}

func formatSemanticResults(input SemanticSearchInput, results []embeddings.SearchResult, mappings []analyze.PathMapping) string {
	if semanticFormatCount != nil {
		atomic.AddInt64(semanticFormatCount, 1)
	}
	resp := semanticRespXML{
		Tool:    "semantic_search",
		Query:   input.Query,
		Repo:    input.Repo,
		Results: semanticResultsXML{Count: len(results)},
	}
	for i, r := range results {
		source := r.Source
		if source == "" {
			source = "semantic"
		}
		res := semanticResultXML{
			Rank:     i + 1,
			Distance: fmt.Sprintf("%.4f", r.Distance),
			Source:   source,
			File:     reverseToHost(r.FilePath, mappings),
			Symbol:   semanticSymbolXML{Kind: r.SymbolKind, Value: r.SymbolName},
			Line:     r.StartLine,
			Language: r.Language,
		}
		if r.PageRank > 0 {
			pr := fmt.Sprintf("%.6f", r.PageRank)
			res.PageRank = &pr
		}
		resp.Results.Results = append(resp.Results.Results, res)
	}
	return xmlMarshalFragment(resp)
}

// formatSemanticResultsCompact is ladder rung 2: file/line/symbol/distance per
// hit, every hit listed, auxiliary attrs (pagerank/source/language) dropped.
func formatSemanticResultsCompact(input SemanticSearchInput, results []embeddings.SearchResult, mappings []analyze.PathMapping) string {
	if semanticFormatCount != nil {
		atomic.AddInt64(semanticFormatCount, 1)
	}
	resp := semanticCompactRespXML{
		Tool:    "semantic_search",
		Query:   input.Query,
		Repo:    input.Repo,
		Results: semanticCompactResults{Count: len(results)},
	}
	for i, r := range results {
		resp.Results.Results = append(resp.Results.Results, semanticCompactResult{
			Rank:     i + 1,
			Distance: fmt.Sprintf("%.4f", r.Distance),
			File:     reverseToHost(r.FilePath, mappings),
			Symbol:   semanticSymbolXML{Kind: r.SymbolKind, Value: r.SymbolName},
			Line:     r.StartLine,
		})
	}
	return xmlMarshalFragment(resp)
}

// formatSemanticResultsCounts is ladder rung 3: per-file hit counts + total,
// ordered by descending count.
func formatSemanticResultsCounts(input SemanticSearchInput, results []embeddings.SearchResult, mappings []analyze.PathMapping) string {
	if semanticFormatCount != nil {
		atomic.AddInt64(semanticFormatCount, 1)
	}
	counts := make(map[string]int, len(results))
	order := make([]string, 0, len(results))
	for _, r := range results {
		host := reverseToHost(r.FilePath, mappings)
		if _, seen := counts[host]; !seen {
			order = append(order, host)
		}
		counts[host]++
	}
	sort.SliceStable(order, func(i, j int) bool {
		return counts[order[i]] > counts[order[j]]
	})
	items := make([]semanticFileCount, len(order))
	for i, f := range order {
		items[i] = semanticFileCount{File: f, Count: counts[f]}
	}
	resp := semanticCountsRespXML{
		Tool:  "semantic_search",
		Query: input.Query,
		Repo:  input.Repo,
		Results: semanticCountsBody{
			Count:      len(results),
			Files:      len(order),
			FileCounts: items,
		},
	}
	return xmlMarshalFragment(resp)
}

// semanticSearchIndexingResponse returns an "indexing" status result and bumps
// gocode_tool_cold_return_total{tool="semantic_search",status="indexing"} so
// cold-start rates are comparable across tools.
func semanticSearchIndexingResponse(input SemanticSearchInput, message string) *mcp.CallToolResult {
	recordToolColdReturn("semantic_search", "indexing")
	return textResult(buildStatusResponse(input, "indexing", message))
}

func buildStatusResponse(input SemanticSearchInput, status, message string) string {
	return xmlMarshalFragment(semanticStatusXML{
		Tool:    "semantic_search",
		Query:   input.Query,
		Repo:    input.Repo,
		Status:  status,
		Message: message,
	})
}
