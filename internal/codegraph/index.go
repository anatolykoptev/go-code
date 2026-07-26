package codegraph

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/anatolykoptev/go-kit/env"
	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/ingest"
	"github.com/anatolykoptev/vaelor/internal/parser"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	defaultTTLLocal   = 3600
	defaultTTLRemote  = 86400
	defaultBatchSize  = 500
	maxIndexFileBytes = 512 * 1024
)

// IndexConfig controls caching behaviour for IndexRepo.
type IndexConfig struct {
	TTLLocal  int
	TTLRemote int
	BatchSize int

	// EnableSurpriseIndex triggers the two-phase surprise persistence pass
	// (IndexSurpriseEdges + IndexSurpriseNodes) after the pagerank/community
	// pass.  Gated behind CODEGRAPH_SURPRISE_INDEX=1 (default off).
	EnableSurpriseIndex bool

	// FlowsMax caps the total number of flows extracted per repo.
	// Env: FLOWS_MAX (default 50). Zero means use default.
	FlowsMax int

	// FlowsDFSDepth bounds the DFS traversal depth per flow.
	// Env: FLOWS_DFS_DEPTH (default 8). Zero means use default.
	FlowsDFSDepth int
}

// GraphMeta describes a built code graph stored in code_graph_meta.
type GraphMeta struct {
	RepoKey     string    `json:"repo_key"`
	RepoPath    string    `json:"repo_path"`
	GraphName   string    `json:"graph_name"`
	FileCount   int       `json:"file_count"`
	SymbolCount int       `json:"symbol_count"`
	EdgeCount   int       `json:"edge_count"`
	BuiltAt     time.Time `json:"built_at"`
	TTLSeconds  int       `json:"ttl_seconds"`
	ContentHash string    `json:"content_hash"`
}

// vertexData holds label and properties for one graph vertex.
type vertexData struct {
	Label string
	Props map[string]string
}

// edgeData holds all fields needed to express one directed graph edge.
type edgeData struct {
	FromLabel string
	FromKey   string
	ToLabel   string
	ToKey     string
	EdgeLabel string
	Props     map[string]string
}

// IndexRepo builds (or returns cached) a code graph for the repo at root.
func IndexRepo(ctx context.Context, store *Store, root string, isRemote bool, cfg IndexConfig) (*GraphMeta, error) {
	cfg = applyConfigDefaults(cfg)
	repoKey := graphName(root)
	gname := repoKey
	t0 := time.Now()

	// Memory guard: refuse to build if the host is under memory pressure.
	// Prevents OOM-kill cascade on resource-constrained boxes (issue #428).
	// On non-Linux or when /proc is unavailable, this is a no-op.
	if memSt := CheckMemoryPressure(); !memSt.Sufficient {
		buildStatus := "memguard_refused"
		recordGraphBuild(repoKey, buildStatus)
		slog.Warn("codegraph: IndexRepo refused by memory guard",
			slog.String("repo", root), slog.String("reason", memSt.Reason),
			slog.Uint64("available_bytes", memSt.AvailableBytes),
			slog.Float64("psi_avg10", memSt.PSIAvg10))
		return nil, fmt.Errorf("graph build skipped: %s", memSt.Reason)
	}

	// Deferred outcome recorder: bumps graphBuildTotal{repo, status} exactly once
	// per return path.  The variable is set to "error" by default and overridden to
	// "skip" (cache-fresh) or "ok" (successful build+persist) at the appropriate
	// terminal.  This avoids a double-bump when multiple error returns exist.
	buildStatus := "error"
	defer func() { recordGraphBuild(repoKey, buildStatus) }()

	if cached, err := checkCache(ctx, store, repoKey, gname, root); err != nil {
		return nil, err
	} else if cached != nil {
		buildStatus = "skip"
		if ierr := store.EnsureIndexes(ctx, gname); ierr != nil {
			slog.Warn("codegraph: ensure indexes on cache hit", slog.Any("error", ierr))
		}
		return cached, nil
	}

	if err := store.EnsureGraph(ctx, gname); err != nil {
		return nil, fmt.Errorf("ensure graph: %w", err)
	}
	if err := store.EnsureLabels(ctx, gname); err != nil {
		return nil, fmt.Errorf("ensure labels: %w", err)
	}

	// Create vertex indexes before inserts so UNWIND+MERGE uses index-based lookup
	// rather than full table scans. Creating indexes on empty tables is instant;
	// PostgreSQL maintains them incrementally during inserts.
	t_idx := time.Now()
	if err := store.EnsureIndexes(ctx, gname); err != nil {
		slog.Warn("codegraph: pre-insert EnsureIndexes", slog.Any("error", err))
	}
	slog.Info("codegraph: pre-insert EnsureIndexes done",
		slog.String("repo", root), slog.Duration("elapsed", time.Since(t_idx)))

	t1 := time.Now()
	// Unified pipeline: ingest → parse → build → enrich (issue #463).
	// BuildAndEnrich replaces the separate ingestAndParse + buildAGECallGraph
	// + extractGoImplements + extractHookRoutes sequence with one call.
	// TypedEnrich is gated by CODEGRAPH_TYPED_ENRICH (same gate as the old
	// buildAGECallGraph path).
	pr, err := callgraph.BuildAndEnrich(ctx, callgraph.PipelineOpts{
		Root:         root,
		MaxFileBytes: maxIndexFileBytes,
		TypedEnrich:  typedEnrichEnabled(),
	})
	if err != nil {
		return nil, err
	}
	cg := pr.CG
	allFiles := pr.Files
	allSymbols := pr.Symbols
	allRels := pr.Rels
	fileImports := pr.FileImports
	skippedReasons := map[string]int{} // ingestAndParse's skip reasons now come from the pipeline
	slog.Info("codegraph: BuildAndEnrich done",
		slog.Any("skipped_reasons", skippedReasons),
		slog.String("repo", root), slog.Int("files", len(allFiles)),
		slog.Duration("elapsed", time.Since(t1)))

	// Convert pipeline's TemplateRefs into code_graph's templateFileRef.
	var allTplRefs []templateFileRef
	for _, tr := range pr.TemplateRefs {
		allTplRefs = append(allTplRefs, templateFileRef{
			relFile:    tr.RelFile,
			resolvedTo: tr.ResolvedTo,
			line:       tr.Line,
		})
	}

	// Filter stdlib method calls (clone, unwrap, to_string, iter, …) that
	// tree-sitter captures as unresolved "external" nodes. SCIP applies the
	// same filter at conversion time (convert.go); this covers the
	// tree-sitter-only path and any edges that survived enrichment unresolved.
	// See issue #466. BuildAndEnrich does NOT apply this filter (call_trace
	// applies it in BuildFromRepo); code_graph applies it here.
	cg.Edges = callgraph.FilterStdlibCalls(cg.Edges)

	t2 := time.Now()

	// allRels already includes cg.TypeRels (IMPLEMENTS from EnrichWithTypedResolution,
	// via pr.Rels). Now unify SCIP trait-impl edges from cg.Edges into allRels too,
	// and remove them from cg.Edges so they don't also appear as CALLS.
	// This ensures a single IMPLEMENTS edge construction path
	// (buildRelationshipEdges) for both Go (ExtractGoImplements) and SCIP.
	allRels = append(allRels, callEdgesToRels(cg)...)
	cg.Edges = removeImplEdges(cg.Edges)

	vertices, edges, prScores := buildGraph(buildGraphInput{
		Root: root, Files: allFiles, Symbols: allSymbols,
		CallGraph: cg, FileImports: fileImports, Rels: allRels, TplRefs: allTplRefs,
	})
	injectCommunities(vertices, edges)
	slog.Info("codegraph: buildGraph done",
		slog.String("repo", root), slog.Int("vertices", len(vertices)), slog.Int("edges", len(edges)),
		slog.Duration("elapsed", time.Since(t2)))

	// Phase 2: extract named execution flows (index-time precompute).
	// Runs after injectCommunities. prScores reused from buildGraph — no second PageRank pass.
	// No AGE I/O — pure in-memory DFS over cg. Non-fatal: logs error and bumps counter.
	communityMap := buildCommunityMap(vertices)
	crossVerticesEarly, crossEdgesEarly := buildCrossLanguageData(root, allFiles, allSymbols)
	handlesTargets := extractHandlesTargets(root, crossEdgesEarly)
	{
		t_flows := time.Now()
		flows := ExtractFlows(root, cg, communityMap, prScores, handlesTargets, cfg.FlowsMax, cfg.FlowsDFSDepth)
		flowsExtractedTotal.WithLabelValues(repoKey).Add(float64(len(flows)))
		slog.Info("codegraph: flow extraction done",
			slog.String("repo", root), slog.Int("flows", len(flows)),
			slog.Duration("elapsed", time.Since(t_flows)))
		if len(flows) > 0 {
			if uErr := store.UpsertFlows(ctx, repoKey, flows); uErr != nil {
				slog.Warn("codegraph: flow upsert failed (non-fatal)",
					slog.String("repo", root), slog.Any("error", uErr))
				flowsDBErrorTotal.WithLabelValues(repoKey).Inc()
			}
		}
		flowsExtractDuration.WithLabelValues(repoKey).Observe(time.Since(t_flows).Seconds())
	}
	// Use the early cross-language data for the main insert pass.
	crossVertices, crossEdges := crossVerticesEarly, crossEdgesEarly

	// Open a bulk write session (synchronous_commit=off, single connection).
	// 5x faster than per-call pool acquire. Falls back to Store on error.
	var writer CypherWriter = store
	bw, bwErr := store.NewBulkWriter(ctx)
	if bwErr != nil {
		slog.Warn("codegraph: BulkWriter unavailable, using standard writes", slog.Any("error", bwErr))
	} else {
		defer bw.Close(ctx)
		writer = bw
	}

	t3 := time.Now()
	// Attempt direct COPY INSERT (bypasses Cypher parser, 10-20x faster).
	// Falls back to UNWIND inserts on any error.
	if err := store.BulkCopyInsert(ctx, gname, vertices, edges); err != nil {
		slog.Warn("codegraph: BulkCopyInsert failed, falling back to UNWIND inserts",
			slog.String("repo", root), slog.Any("error", err))
		if fbErr := insertBatches(ctx, writer, gname, cfg.BatchSize, vertices, buildVertexBatch); fbErr != nil {
			return nil, fmt.Errorf("insert vertices (fallback): %w", fbErr)
		}
		if fbErr := insertEdgeBatches(ctx, writer, gname, cfg.BatchSize, edges); fbErr != nil {
			return nil, fmt.Errorf("insert edges (fallback): %w", fbErr)
		}
	}
	slog.Info("codegraph: insert done",
		slog.String("repo", root),
		slog.Int("vertices", len(vertices)), slog.Int("edges", len(edges)),
		slog.Duration("elapsed", time.Since(t3)))

	// Cross-language analysis (non-fatal). Data already computed above for flow
	// extraction; reuse the hoisted crossVertices/crossEdges variables.
	if len(crossVertices) > 0 {
		if err := insertBatches(ctx, writer, gname, cfg.BatchSize, crossVertices, buildVertexBatch); err != nil {
			slog.Warn("codegraph: cross-language vertices", slog.Any("error", err))
		}
	}
	if len(crossEdges) > 0 {
		if err := insertEdgeBatches(ctx, writer, gname, cfg.BatchSize, crossEdges); err != nil {
			slog.Warn("codegraph: cross-language edges", slog.Any("error", err))
		}
	}

	// Indexes already created before edge inserts; run again as IF NOT EXISTS
	// for cross-language vertices added after the main pass.
	if len(crossVertices) > 0 {
		if err := store.EnsureIndexes(ctx, gname); err != nil {
			slog.Warn("codegraph: post-cross EnsureIndexes", slog.Any("error", err))
		}
	}

	ttl := cfg.TTLLocal
	if isRemote {
		ttl = cfg.TTLRemote
	}
	meta := &GraphMeta{
		RepoKey: repoKey, RepoPath: root, GraphName: gname,
		FileCount: len(allFiles), SymbolCount: len(allSymbols), EdgeCount: len(edges),
		BuiltAt: time.Now().UTC(), TTLSeconds: ttl,
		ContentHash: ingest.RepoContentHash(root),
	}
	if err := upsertMeta(ctx, store, meta); err != nil {
		return nil, fmt.Errorf("upsert meta: %w", err)
	}

	t6 := time.Now()
	storeFileMtimes(ctx, store, repoKey, allFiles)
	slog.Info("codegraph: storeFileMtimes done",
		slog.String("repo", root), slog.Int("files", len(allFiles)),
		slog.Duration("elapsed", time.Since(t6)))

	// Pre-score dead_code candidates so query-time reranking is instant.
	// Non-fatal: errors are logged but do not fail IndexRepo.
	t7 := time.Now()
	if scoreErr := store.ScoreDeadCodeCandidates(ctx, gname, repoKey, len(allSymbols)); scoreErr != nil {
		slog.Warn("codegraph: dead_code pre-scoring failed (non-fatal)",
			slog.String("repo", root), slog.Any("error", scoreErr))
	} else {
		slog.Info("codegraph: dead_code pre-scoring done",
			slog.String("repo", root), slog.Duration("elapsed", time.Since(t7)))
	}

	slog.Info("codegraph: IndexRepo complete",
		slog.String("repo", root), slog.Duration("total", time.Since(t0)))

	if cfg.EnableSurpriseIndex {
		if err := IndexSurpriseEdges(ctx, store, gname); err != nil {
			slog.Warn("codegraph: surprise edge index failed", slog.Any("error", err))
		} else if err := IndexSurpriseNodes(ctx, store, gname); err != nil {
			slog.Warn("codegraph: surprise node index failed", slog.Any("error", err))
		}
	}

	buildStatus = "ok"
	return meta, nil
}

// agegraphTypedEnrichTotal counts CODEGRAPH_TYPED_ENRICH attempts on the
// AGE-graph indexing path (buildAGECallGraph), labelled "applied" (go/types
// resolution landed and the AGE graph's CALLS edges now include the typed
// pass, fixing BUG A) or "degraded" (the gate was on but the seam fell back
// to the pre-existing tree-sitter-only graph — no go.mod, cold GOCACHE, load
// timeout, or load failure; same result as the gate being off). A rising
// "degraded" share among Go-module repos with the gate on means dead_code /
// code_health are still seeing BUG A on the repos where it matters most —
// the signal the P4 applied-ratio SLO alert watches.
//
//	gocode_agegraph_typed_enrich_total{result="applied"} 4
//	gocode_agegraph_typed_enrich_total{result="degraded"} 1
var agegraphTypedEnrichTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gocode_agegraph_typed_enrich_total",
		Help: "Count of CODEGRAPH_TYPED_ENRICH attempts on the AGE-graph indexing path, labelled by result (applied|degraded).",
	},
	[]string{"result"},
)

func init() {
	// Pre-touch so /metrics exports the series at boot, before any repo is
	// indexed with the gate on (mirrors implements_metrics.go's init()).
	agegraphTypedEnrichTotal.WithLabelValues("applied").Add(0)
	agegraphTypedEnrichTotal.WithLabelValues("degraded").Add(0)
}

// typedEnrichEnabled reports whether the AGE-graph indexing path
// (buildAGECallGraph) should additionally attempt go/types-based typed
// call-edge resolution. Default ON: the production canary (2026-07-02) showed
// a healthy applied ratio and no load regression, so the gate is now on by
// default. Set CODEGRAPH_TYPED_ENRICH=0 to disable and fall back to the
// untyped tree-sitter-only build.
func typedEnrichEnabled() bool {
	return env.Bool("CODEGRAPH_TYPED_ENRICH", true)
}

// buildAGECallGraph builds the CallGraph the AGE-graph indexing path
// persists as CALLS edges. It always runs the untyped tree-sitter builder
// (callgraph.BuildCallGraph) first. With typed enrichment disabled this is
// the entire function, byte-identical to calling callgraph.BuildCallGraph
// directly as IndexRepo did before this change.
//
// When typed enrichment is enabled (default) AND root is a Go module, it
// additionally routes the graph through callgraph.EnrichWithTypedResolution —
// the SAME single seam BuildFromRepo (the call_trace/impact_analysis path)
// already uses — so the untyped builder's name-only call resolution (BUG A: a
// call through a package-level var resolves to the wrong same-named method
// when a sibling type in the same directory exposes a method of the same name;
// see TestAGEGraphMissesHomonymousPkgVarMethodCall) gets the same typed fix on
// the indexing path that call_trace/impact_analysis already have.
//
// Bounded and non-fatal, mirroring ExtractGoImplements's degrade contract
// (callgraph/satisfaction.go): EnrichWithTypedResolution itself bounds the
// go/types attempt to a 10s warm-path and degrades to the untyped graph
// unchanged on any failure (no go.mod, cold GOCACHE, load timeout, load
// error) — this wrapper adds no additional timeout, only the gate and the
// landed/degraded counter. IMPLEMENTS (ExtractGoImplements) and CALLS (here)
// share the SAME *LoadResult: EnrichWithTypedResolution loads once and hands
// it to both passes through the seam, so the go/packages load is paid once
// per request and the arena is released when the request ends (issue #747).
//
// Stamps cg.Tier/cg.Backend before enriching, matching BuildFromRepo's own
// setup (repo.go:97-99) — the in-memory CallGraph struct's existing fields,
// NOT a new persisted schema (Tier/Backend are never written to AGE; see the
// "cut tier/backend provenance stamping" ADR). Without this, cg.Tier stays
// the zero value "" and EnrichWithTypedResolution's SCIP-fallback gate
// (`if cg.Tier == "basic"`) is always false, silently disabling SCIP
// enrichment for this caller on every mixed-language repo where go/types
// alone makes no progress.
func buildAGECallGraph(ctx context.Context, root string, symbols []*parser.Symbol, calls []parser.CallSite, files []*ingest.File) *callgraph.CallGraph {
	cg := callgraph.BuildCallGraph(symbols, calls)

	if !typedEnrichEnabled() {
		return cg
	}

	cg.Tier = "basic"
	cg.Backend = callgraph.BackendTreeSitter

	enriched := callgraph.EnrichWithTypedResolution(ctx, root, cg, symbols, files)

	// "applied" specifically means the go/types-specific BUG A fix landed.
	// A SCIP landing (enriched.Backend == callgraph.BackendSCIP, reachable
	// on a Go-minority go.mod repo whose dominant language SCIP targets
	// instead of go/types) does not fix the homonymous-method /
	// var-func-binding shapes this counter exists to monitor, so it counts
	// as "degraded" even though some enrichment landed — otherwise a rising
	// SCIP-landing share on mixed-language repos would silently mask BUG A
	// still being live there, defeating the counter's own purpose.
	if enriched.Backend == callgraph.BackendGoTypes {
		agegraphTypedEnrichTotal.WithLabelValues("applied").Inc()
	} else {
		agegraphTypedEnrichTotal.WithLabelValues("degraded").Inc()
	}
	return enriched
}

// applyConfigDefaults fills in zero-value fields with sensible defaults.
func applyConfigDefaults(cfg IndexConfig) IndexConfig {
	if cfg.TTLLocal <= 0 {
		cfg.TTLLocal = defaultTTLLocal
	}
	if cfg.TTLRemote <= 0 {
		cfg.TTLRemote = defaultTTLRemote
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = defaultBatchSize
	}
	if cfg.FlowsMax <= 0 {
		cfg.FlowsMax = flowsMax
	}
	if cfg.FlowsDFSDepth <= 0 {
		cfg.FlowsDFSDepth = flowsDFSDepth
	}
	return cfg
}
