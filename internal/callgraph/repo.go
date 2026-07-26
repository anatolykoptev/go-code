package callgraph

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/anatolykoptev/vaelor/internal/goanalysis"
	"github.com/anatolykoptev/vaelor/internal/ingest"
	"github.com/anatolykoptev/vaelor/internal/parser"
	"github.com/anatolykoptev/vaelor/internal/parser/preproc"
)

const maxFileBytes = 512 * 1024

// Backend identifies which resolution pass(es) contributed edges to a
// CallGraph. Plain strings by design (see docs/adr, "cut tier/backend
// provenance stamping" — internal/tier is orphaned and neither AGE-graph
// fixture needs a richer vocabulary); named as consts here only to keep the
// literal that SETS CallGraph.Backend and the literal that later COMPARES
// against it from drifting apart. Exported (not package-private) because
// codegraph/index.go's buildAGECallGraph also stamps and compares against
// these exact values — a second unexported copy in that package would be
// the same literal-drift risk these consts exist to prevent, just moved one
// package over.
const (
	BackendTreeSitter = "tree-sitter"
	BackendGoTypes    = "tree-sitter+go/types"
	BackendSCIP       = "tree-sitter+scip"
)

// TraceRepoInput configures a full repo call chain trace.
type TraceRepoInput struct {
	Root     string
	Symbol   string
	Focus    string
	Language string
	Opts     TraceOpts

	// IncludeFieldAccess keeps heuristic argref/field-access call sites even
	// when they don't resolve to a known function symbol. Default false —
	// unresolved argref captures (`opts.Slug`, `ctx`, `localPath`) are
	// dropped. Set via the `field_access=true` MCP tool flag for legacy
	// permissive behaviour.
	IncludeFieldAccess bool

	// Refresh forces a cache bypass — re-parses the repo and re-runs
	// SCIP/go/types enrichment instead of returning the cached call graph.
	// Use when the repo has changed (git checkout, new commit) and the
	// in-memory cgCache is stale.
	Refresh bool
}

type parseResult struct {
	symbols []*parser.Symbol
	calls   []parser.CallSite
	rels    []parser.TypeRelationship
	imports []string // import paths declared in the file
	src     []byte   // raw file bytes, needed for template-ref resolution
	fileRel string   // file path relative to repo root
	tplRefs []preproc.TemplateRef
}

// BuildFromRepo ingests a repo, parses files, and returns the call graph
// without tracing a specific symbol.
//
// Delegates to BuildAndEnrich (the unified pipeline, issue #463) and caches
// the result. The background go/types warm-up is kept here because it is
// call_trace-specific (it targets the cgCache entry, not the pipeline result).
func BuildFromRepo(ctx context.Context, input TraceRepoInput) (*CallGraph, error) {
	// Check cache first — parsing all repo files is expensive (15-60s on cold start).
	cacheKey := cgCacheKey(input)
	if !input.Refresh {
		if cached, entryAt, ok := cgCache.getWithAt(cacheKey, input.Root); ok {
			// Sibling-stale Warming detection (round-7, issue #735; round-8
			// loop guard). The background warm's single-flight guard is
			// root-keyed (goTypesWarmingSet, repo.go:288), so on a cold repo
			// only ONE warm runs and only the warm's OWN key gets its
			// Warming flag cleared in place (repo.go:344-379). Sibling keys
			// — same root, different Focus/Language/IncludeFieldAccess
			// scope — keep Warming=true for the full cgCacheTTL because
			// their warm was suppressed as `skipped` and their entry was
			// never touched. A cache hit on such a sibling returns a stale
			// Warming=true graph whose "retry for the enhanced tier" note
			// can never resolve.
			//
			// If the root has been warmed (goTypesWarmedSet stores the
			// completion instant) but this cached entry still advertises
			// Warming AND was cached before the warm completed
			// (entryAt predates warmedAt), treat it as a MISS and fall
			// through to the normal rebuild. The rebuild runs against a
			// now-warm GOCACHE so it is fast and returns at the enhanced
			// tier honestly, with IMPLEMENTS present. Clearing the flag
			// alone would leave a basic-tier answer that merely stops
			// advertising it could be better — a silent downgrade.
			//
			// Round-8 loop guard: an entry re-cached AFTER the warm (e.g.
			// a rebuild whose typed resolution failed again) carries a
			// fresh entryAt that is NOT before warmedAt, so the divert
			// does NOT fire — the cache hit is returned as-is. Without
			// this timestamp check, round 7's divert looped forever on
			// scopes whose typed resolution keeps failing (every request
			// rebuilt, 15-60s each, for the process lifetime). Do NOT
			// mutate the cached entry: cgCache.getWithAt releases c.mu
			// before returning and the *CallGraph is shared with
			// concurrent readers (round-5 defect, repo_cache.go:66-72).
			if cached.Warming {
				if warmedAtVal, warmed := goTypesWarmedSet.Load(input.Root); warmed {
					warmedAt, _ := warmedAtVal.(time.Time)
					if warmedAt.After(entryAt) {
						slog.Info("callgraph: cache hit with stale Warming flag after successful background warm; rebuilding",
							slog.String("root", input.Root))
						// Fall through to rebuild below.
					} else {
						slog.Debug("callgraph: BuildFromRepo cache hit (warming note is fresh, not stale)",
							slog.String("root", input.Root))
						return cached, nil
					}
				} else {
					slog.Debug("callgraph: BuildFromRepo cache hit", slog.String("root", input.Root))
					return cached, nil
				}
			} else {
				slog.Debug("callgraph: BuildFromRepo cache hit", slog.String("root", input.Root))
				return cached, nil
			}
		}
	}

	result, err := BuildAndEnrich(ctx, PipelineOpts{
		Root:               input.Root,
		Focus:              input.Focus,
		Language:           input.Language,
		IncludeFieldAccess: input.IncludeFieldAccess,
		MaxFileBytes:       maxFileBytes,
		TypedEnrich:        true,
	})
	if err != nil {
		return nil, err
	}
	cg := result.CG

	// Filter stdlib method calls (clone, unwrap, to_string, iter, …) that
	// tree-sitter captures as unresolved "external" nodes. SCIP applies the
	// same filter at conversion time (convert.go); this covers the
	// tree-sitter-only path and any edges that survived enrichment unresolved.
	// See issue #466.
	cg.Edges = FilterStdlibCalls(cg.Edges)

	// The seam bounds its go/types attempt to a 10s warm-path (fast when
	// GOCACHE is already warm). If that didn't land — Backend still isn't
	// BackendGoTypes — kick off a background goroutine that warms GOCACHE
	// and upgrades this cache entry once done (the next call_trace/
	// impact_analysis against this root will complete in <10s instead of
	// 3+ minutes).
	if goanalysis.HasGoModule(input.Root) && cg.Backend != BackendGoTypes {
		go warmGoTypesCache(input.Root, result.Symbols, cgCacheKey(input))
	}

	// Cache the result for subsequent calls within the same session.
	cgCache.set(cacheKey, cg, input.Root)
	slog.Debug("callgraph: BuildFromRepo cached", slog.String("root", input.Root),
		slog.String("tier", cg.Tier))
	return cg, nil
}

// TraceRepo ingests a repo, extracts symbols and calls, builds call graph, traces from symbol.
func TraceRepo(ctx context.Context, input TraceRepoInput) (*TraceResult, error) {
	g, err := BuildFromRepo(ctx, input)
	if err != nil {
		return nil, err
	}

	result := Trace(ctx, g, input.Symbol, input.Opts)
	result.Tier = g.Tier
	result.Warming = g.Warming

	return &result, nil
}

// EnrichWithTypedResolution is the single shared composition seam for typed
// call-edge enrichment: given a base (tree-sitter-only) CallGraph, it
// attempts go/types resolution for Go modules, then — only if that made no
// progress — SCIP resolution for non-Go languages, merging any successful
// pass additively via MergeCallGraphs. Route ANY new typed-edge source
// through this seam (and MergeCallGraphs) rather than composing typed
// enrichment ad hoc at a second call site; BuildFromRepo is the reference
// caller.
//
// This seam OWNS the go/packages load for the request (issue #747): it loads
// once and hands the same *LoadResult to both tryGoTypesResolution (CALLS)
// and ExtractGoImplements (IMPLEMENTS), which run sequentially. The load was
// previously shared between the two via a process-global TTL+LRU cache
// (goanalysis.CachedLoadPackages), which pinned the full go/types arena
// (NeedDeps: .Types/.TypesInfo/.Syntax for every package) for the cache TTL
// — ~2 GB per entry, size 8, OOM-killing the 3 GiB indexer ten times in two
// days. A value shared between two sequential steps of one request is a
// parameter, not process-global state; passing it makes the arena's lifetime
// the request's, so it becomes collectable the moment the request ends, with
// no TTL, no LRU, and no way for eight arenas to coexist.
//
// Both passes are bounded and non-fatal: on any failure (no go.mod, cold
// GOCACHE, no indexer, timeout) base is returned with Tier/Backend
// unchanged, exactly the tree-sitter-only degrade contract callers already
// depend on. root and files are needed independently of base/symbols
// because SCIP resolution walks the raw ingested file set for the dominant
// language, not the already-parsed symbol table.
func EnrichWithTypedResolution(ctx context.Context, root string, base *CallGraph, symbols []*parser.Symbol, files []*ingest.File) *CallGraph {
	cg := base

	if goanalysis.HasGoModule(root) {
		warmCtx, warmCancel := context.WithTimeout(context.Background(), 10*time.Second)
		lr, loadErr := goanalysis.LoadPackages(warmCtx, root, goanalysis.LoadOpts{})
		warmCancel()
		if loadErr != nil {
			// Cold cache: the go/packages LOAD failed. The background warm
			// (warmGoTypesCache in BuildFromRepo) is running and will upgrade
			// the cached entry; a retry will return the enhanced tier. Mark the
			// graph so callers can surface a "type-aware enrichment is warming,
			// retry for the enhanced tier" note. Do NOT call
			// ExtractGoImplements here — it would block on the same slow
			// packages.Load that already failed, burning the request's
			// remaining deadline (issue #735).
			recordGotypesFallback(loadErr)
			slog.Warn("go/packages load failed; falling back to tree-sitter", "err", loadErr)
			cg.Warming = true
		} else {
			// Load succeeded (with or without typed call edges). In either case
			// ExtractGoImplements reuses the SAME *LoadResult the load just
			// produced — passed through the seam, not re-loaded — so it cannot
			// block on a cold NeedDeps load here. Running it unconditionally on
			// the success path restores the pre-round-1 behaviour for the
			// zero-edge case (a Go module with only type declarations and no
			// function calls) — round 1's single nil-return silently dropped
			// IMPLEMENTS on that case, regressing issue #467's whole feature.
			typedCG := tryGoTypesResolution(lr, symbols)
			if typedCG != nil {
				cg = MergeCallGraphs(cg, typedCG)
				cg.Tier = "enhanced"
				cg.Backend = BackendGoTypes
			}
			// When typedCG is nil, the load succeeded with zero typed CALL
			// edges — Tier stays "basic" for CALLS (honest), and IMPLEMENTS
			// enrichment still runs below.
			cg.TypeRels = append(cg.TypeRels, ExtractGoImplements(ctx, root, lr)...)
		}
	}

	// Attempt SCIP resolution for non-Go languages (or when go/types failed).
	if cg.Tier == "basic" {
		if scipCG := trySCIPResolution(ctx, root, files, symbols); scipCG != nil {
			cg = MergeCallGraphs(cg, scipCG)
			cg.Tier = "enhanced"
			cg.Backend = BackendSCIP
		}
	}

	return cg
}

// tryGoTypesResolution resolves typed call edges from an ALREADY-LOADED
// *LoadResult (the caller — EnrichWithTypedResolution or warmGoTypesCache —
// owns the load and passes it through the seam, issue #747). It no longer
// loads go/packages itself; the process-global cache that previously shared
// one load between this CALLS pass and ExtractGoImplements' IMPLEMENTS pass
// has been removed (it pinned the go/types arena for the cache TTL,
// OOM-killing the indexer — issue #747).
//
// Returns:
//
//   - (nil, nil): goanalysis.Resolve produced zero typed call edges (e.g. a
//     Go module with only type declarations and no function calls). Nothing
//     is warming — the load already succeeded — so callers must NOT set
//     Warming, and SHOULD still call ExtractGoImplements (it reuses the same
//     *LoadResult; skipping it silently drops issue #467's IMPLEMENTS
//     feature). Bumps gocode_callgraph_gotypes_fallback_total{reason="no_edges"}
//     so this case is no longer invisible.
//
//   - (cg, nil): load succeeded with typed call edges; callers merge and mark
//     the enhanced tier.
//
// The (nil, err) load-failed case is gone — the load is upstream now, and
// callers handle its failure (EnrichWithTypedResolution sets Warming;
// warmGoTypesCache records outcome="failed") before reaching this function.
func tryGoTypesResolution(lr *goanalysis.LoadResult, tsSymbols []*parser.Symbol) *CallGraph {
	if lr == nil {
		return nil
	}
	typedEdges := goanalysis.Resolve(lr.Packages)
	if len(typedEdges) == 0 {
		recordGotypesNoEdges()
		return nil
	}
	return ConvertToCallGraph(typedEdges, tsSymbols)
}

// buildUsesIndex resolves Astro template refs from all parse results and returns
// a map from target-file → []using-file (all paths relative to root).
func buildUsesIndex(results []parseResult, root string) map[string][]string {
	idx := make(map[string][]string)
	for _, r := range results {
		if len(r.tplRefs) == 0 {
			continue
		}
		for _, u := range ResolveTemplateRefs(r.src, r.tplRefs, r.fileRel, root) {
			idx[u.To] = append(idx[u.To], u.From)
		}
	}
	if len(idx) == 0 {
		return nil
	}
	return idx
}

// goTypesWarmingSet tracks repos currently being warmed to avoid duplicate goroutines.
var goTypesWarmingSet sync.Map

// goTypesWarmedSet tracks repos whose background go/types warm has COMPLETED
// successfully, storing the completion instant (time.Time). Keyed by root
// (not by cacheKey) deliberately: the single-flight guard
// (goTypesWarmingSet) is root-keyed too, so on a cold repo only ONE warm
// runs and only ONE key — the warm's own — gets its Warming flag cleared in
// place (repo.go:344-379). Sibling keys (same root, different
// Focus/Language/IncludeFieldAccess scope) keep Warming=true for the full
// cgCacheTTL because their warm was suppressed as `skipped` (repo.go:288)
// and their entry was never touched. BuildFromRepo's cache-hit path checks
// the stored timestamp: if a hit still advertises Warming but the entry's
// at predates the warm completion, it treats the hit as a miss and rebuilds
// (repo.go:76) — the rebuild runs against a now-warm GOCACHE and returns at
// the enhanced tier honestly. An entry re-cached AFTER the warm (e.g. a
// rebuild whose typed resolution failed again) carries a fresh at that is
// NOT before the warm completion, so the divert terminates itself after one
// rebuild — the round-8 loop guard.
//
// Growth: one entry per repo root for the process lifetime (~39 Go repos on
// the box). Lost on restart — after a restart the pre-existing sibling-stale
// behaviour returns for one TTL window. Both are acceptable; no eviction.
var goTypesWarmedSet sync.Map

// buildPrewarmEnv returns the environment for the go build pre-warm subprocess.
// CGO_ENABLED=0 is required: tree-sitter grammars need C headers that are absent
// outside the container build context. Without it the build fails instantly and
// GOCACHE stays empty. With CGO_ENABLED=0 the pure-Go packages still produce
// typed object files — exactly what packages.Load needs to skip its cold-start work.
func buildPrewarmEnv() []string {
	// CGO_ENABLED=0 must come AFTER os.Environ() — append order matters in
	// exec.Cmd.Env (later entries win), and ambient CGO_ENABLED=1 must be
	// shadowed so the prewarm builds without the missing tree_sitter headers.
	return append(os.Environ(),
		"CGO_ENABLED=0",
		"GOCACHE=/tmp/go-build-cache",
		"GOPATH=/tmp/gopath",
		"GOWORK=off",
		"GOFLAGS=-mod=vendor",
		"GIT_TERMINAL_PROMPT=0",
	)
}

// warmGoTypesCache runs go/types analysis in background to warm GOCACHE.
// When complete, it upgrades the cached CallGraph from basic to enhanced tier.
//
// The pre-warm `go build` step was removed (issue #735 Part 2): it ran
// `go build -mod=vendor ./...` with CGO_ENABLED=0, which fails on every cgo
// repo (build constraints exclude all Go files for cgo-requiring packages).
// The runtime image lacks gcc/musl-dev so CGO_ENABLED=1 is not viable either.
// GOCACHE is now persistent (ops-side fix), so packages.Load in this
// background warm handles warming without the pre-build. A failing warm is
// counted by gocode_callgraph_background_warm_total{outcome="failed} so
// operators can alert on repos that never reach the enhanced tier.
//
// This goroutine owns its own go/packages load and passes the *LoadResult
// through the seam to both tryGoTypesResolution (CALLS) and ExtractGoImplements
// (IMPLEMENTS), exactly as EnrichWithTypedResolution does for the synchronous
// request path (issue #747). The load's lifetime is this goroutine's: the
// go/types arena becomes collectable when the warm returns, with no
// process-global cache pinning it. The previous InvalidateCachedLoad call
// (which existed only to defeat the now-removed packagesLoadCache so the
// patient retry wasn't short-circuited by a stale negative-cached cold
// failure) is gone with the cache — there is nothing to evict.
func warmGoTypesCache(root string, symbols []*parser.Symbol, cacheKey string) {
	_, alreadyWarming := goTypesWarmingSet.LoadOrStore(root, true)
	if alreadyWarming {
		recordBackgroundWarm("skipped")
		return
	}
	defer goTypesWarmingSet.Delete(root)

	slog.Info("go/types: warming GOCACHE in background", "root", root)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	// Load once with the patient 15-minute budget and hand the *LoadResult to
	// both consumers. The synchronous 10s probe in EnrichWithTypedResolution
	// that triggered this background warm already ran (and failed) against
	// this same root; this retry re-attempts with a now-warm GOCACHE and a
	// much longer budget. No cache eviction is needed — there is no
	// process-global load cache anymore (issue #747).
	lr, loadErr := goanalysis.LoadPackages(ctx, root, goanalysis.LoadOpts{})
	if loadErr != nil {
		recordGotypesFallback(loadErr)
		slog.Error("go/types: background warm failed — cache stays at basic tier", "root", root, "err", loadErr)
		recordBackgroundWarm("failed")
		return
	}
	typedCG := tryGoTypesResolution(lr, symbols)

	// A successful warm (loadErr == nil) must always refresh the cached entry,
	// not only when typedCG != nil. Two states reach this goroutine (gated
	// on cg.Backend != BackendGoTypes in BuildFromRepo, repo.go:108):
	//
	//   1. Cold sync load FAILED (EnrichWithTypedResolution set Warming=true):
	//      the cached entry has Warming=true and NO IMPLEMENTS edges
	//      (ExtractGoImplements was skipped on the cold path, issue #735).
	//      The background warm exists precisely for this case.
	//   2. Sync load SUCCEEDED with zero typed call edges (tryGoTypesResolution
	//      returned nil): the cached entry already has Warming=false
	//      and IMPLEMENTS present (EnrichWithTypedResolution ran
	//      ExtractGoImplements on the load-succeeded branch).
	//
	// When the patient retry SUCCEEDS but yields zero typed edges (typedCG ==
	// nil, loadErr == nil), the pre-fix code guarded the cache update behind
	// `if typedCG != nil` and never touched the entry. For state 1 that left
	// Warming=true and IMPLEMENTS absent for the full 5-minute cgCacheTTL —
	// even though recordBackgroundWarm("completed") fired and the warm was
	// done. The round-3 comment "IMPLEMENTS edges were already added on the
	// synchronous request path" was true for state 2 only — inverted for the
	// case the background warm was written for. Every consumer serving from
	// that stale entry kept emitting "type-aware enrichment is warming, retry
	// for the enhanced tier", and the retry hit the same stale cache — the
	// note became a lie for five minutes, defeating its purpose.
	//
	// On a successful warm we now always: clear Warming (the warm is done),
	// and ensure IMPLEMENTS edges are present. IMPLEMENTS extraction reuses
	// the SAME *LoadResult the warm just produced (passed through the seam),
	// but state 2 already carries them — check first (hasImplementsEdge) to
	// avoid duplicates rather than dedupe after the fact.
	if cached, ok := cgCache.get(cacheKey, root); ok {
		if typedCG != nil {
			cached = MergeCallGraphs(cached, typedCG)
			cached.Tier = "enhanced"
			cached.Backend = BackendGoTypes
		} else {
			// MergeCallGraphs returns a fresh *CallGraph, so the typedCG
			// != nil branch cannot alias the LRU entry. The zero-edge
			// branch (typedCG == nil) does NOT — `cached` still points at
			// the LRU entry's own *CallGraph, which is shared with any
			// concurrent BuildFromRepo cache hit (repo.go:76) reading it
			// while this background goroutine (repo.go:109) runs. Mutating
			// it in place races on Warming and the TypeRels slice header
			// + backing array (round-5 defect, introduced by c2a7cf11).
			//
			// Build a shallow struct copy and give it a fresh TypeRels
			// backing array before appending. A shallow copy alone is NOT
			// enough: it shares the TypeRels slice header and its backing
			// array, so append on the copy writes into the shared array
			// whenever capacity allows. Allocating a new slice and
			// copying the elements severs both the header alias and the
			// backing-array alias. The other slice/map fields (Edges,
			// Symbols, HookCallbacks, UsesIndex) are carried over
			// read-only — this branch never writes them, and concurrent
			// reads of a shared slice/map are safe in Go, so they need no
			// copy here. Tier and Backend are scalars left unchanged on
			// this branch.
			cp := *cached
			cp.TypeRels = append([]parser.TypeRelationship(nil), cached.TypeRels...)
			cached = &cp
		}
		cached.Warming = false
		if !hasImplementsEdge(cached) {
			cached.TypeRels = append(cached.TypeRels, ExtractGoImplements(ctx, root, lr)...)
		}
		cgCache.set(cacheKey, cached, root)
	}
	slog.Info("go/types: GOCACHE warmed", "root", root)
	// Record the root as warmed so BuildFromRepo's cache-hit path can detect
	// sibling keys (different scope, same root) whose Warming flag was never
	// cleared — their warm was suppressed as `skipped` by the root-keyed
	// single-flight guard. The stored time.Time is the warm completion
	// instant; BuildFromRepo diverts only entries whose at predates it, so
	// an entry re-cached after a failed rebuild (fresh at) is NOT diverted
	// again — the round-8 loop guard. Stored ONLY on success: a failed warm
	// must NOT store, so siblings keep their honest "retry for the enhanced
	// tier" note.
	goTypesWarmedSet.Store(root, time.Now())
	recordBackgroundWarm("completed")
}

// hasImplementsEdge reports whether cg already carries at least one
// IMPLEMENTS TypeRelationship. Used by warmGoTypesCache to decide whether to
// call ExtractGoImplements on a successful zero-edge warm: state 2 (sync load
// succeeded with zero call edges) already appended IMPLEMENTS on the
// synchronous path, so re-running extraction here would duplicate them; state
// 1 (sync load failed) has none and needs them restored.
func hasImplementsEdge(cg *CallGraph) bool {
	for _, rel := range cg.TypeRels {
		if rel.Kind == parser.RelImplements {
			return true
		}
	}
	return false
}
