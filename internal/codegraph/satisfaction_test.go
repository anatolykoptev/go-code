package codegraph

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/goanalysis"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

// writeTinyForgeModule writes a self-contained Go module into dir with a local
// Forge interface and two concrete implementers in separate files, mirroring the
// real shape the find_duplicates 2-hop filter (PR #221) walks over IMPLEMENTS.
// Two files (one type each) deliberately exercise per-file FileSet resolution.
func writeTinyForgeModule(t *testing.T, dir string) {
	t.Helper()
	files := map[string]string{
		"go.mod": "module example.com/forge\n\ngo 1.21\n",
		"forge.go": `package forge

// Forge is the interface both concrete forges satisfy structurally.
type Forge interface {
	FetchREADME() string
}
`,
		"github.go": `package forge

type GitHubForge struct{}

func (GitHubForge) FetchREADME() string { return "github" }
`,
		"gitlab.go": `package forge

type GitLabForge struct{}

func (GitLabForge) FetchREADME() string { return "gitlab" }
`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}

// symbolKeySet reconstructs, from buildSymbolGraph's vertices, the set of
// composite Symbol vertex keys (name + compositeKeyDelim + relFile) exactly as
// the persisted CONTAINS-edge endpoints carry them. These are the ground-truth
// node keys an ingested symbol gets; an IMPLEMENTS edge endpoint MUST match one
// byte-for-byte or the edge is orphaned at persist time.
func symbolKeySet(t *testing.T, root string) (map[string]struct{}, []*parser.Symbol) {
	t.Helper()
	_, syms, _, _, _, _, _, err := ingestAndParse(context.Background(), root)
	if err != nil {
		t.Fatalf("ingestAndParse: %v", err)
	}
	if len(syms) == 0 {
		t.Fatal("ingestAndParse produced no symbols")
	}
	verts, _ := buildSymbolGraph(root, syms, nil)
	keys := make(map[string]struct{}, len(verts))
	for _, v := range verts {
		if v.Label != "Symbol" {
			continue
		}
		keys[v.Props["name"]+compositeKeyDelim+v.Props["file"]] = struct{}{}
	}
	return keys, syms
}

// TestImplementsEdgeKeysMatchSymbolGraph is the make-or-break contract PR #221's
// 2-hop interface-sibling filter depends on: the IMPLEMENTS edge subject endpoint
// (keyed from a go/types FileSet ABSOLUTE path via relPath) MUST be byte-identical
// to the Symbol vertex key the tree-sitter ingest path produces for the SAME type.
//
// This is NOT asserted-by-construction (cf. TestBuildGraphImplementsEdges, which
// feeds synthetic rels whose File literal equals the synthetic symbol File). Here
// two INDEPENDENT absolute-path producers must agree after relPath(abs, root):
//   - go/types FileSet  → Satisfaction.TypeFile → TypeRelationship.File (the edge subject)
//   - ingest WalkDir     → ingest.File.Path → parser.Symbol.File (the vertex)
//
// If a future change makes go/types and ingest disagree on the absolute path form
// (symlink-resolved prefix, trailing-slash root, case folding, etc.), filepath.Rel
// yields a different relative string, the edge FromKey no longer matches any Symbol
// vertex, the IMPLEMENTS edge silently orphans, and #221's 2-hop returns empty —
// with zero error. This test red-fails on exactly that drift.
func TestImplementsEdgeKeysMatchSymbolGraph(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTinyForgeModule(t, root)

	// Real index path: go/types satisfaction → TypeRelationship with an ABSOLUTE
	// FileSet TypeFile (not a synthetic literal). The caller owns the load and
	// passes the *LoadResult through the seam (issue #747); ExtractGoImplements
	// no longer loads go/packages itself.
	lr, err := goanalysis.LoadPackages(context.Background(), root, goanalysis.LoadOpts{})
	if err != nil {
		t.Fatalf("goanalysis.LoadPackages: %v", err)
	}
	rels := callgraph.ExtractGoImplements(context.Background(), root, lr)
	if len(rels) == 0 {
		t.Fatal("callgraph.ExtractGoImplements produced no IMPLEMENTS relationships (go/types load failed or found no satisfaction)")
	}
	// Sanity: every subject's File must be an ABSOLUTE path (proves we are
	// exercising the go/types-abs → relPath transform, not a pre-relativized one).
	for _, r := range rels {
		if !filepath.IsAbs(r.File) {
			t.Fatalf("expected absolute go/types subject file, got %q", r.File)
		}
	}

	// Ground-truth Symbol vertex keys from the tree-sitter ingest path.
	symKeys, syms := symbolKeySet(t, root)

	// Build the IMPLEMENTS edges through the production keying function.
	edges := buildRelationshipEdges(root, rels, syms)

	var sawGitHub, sawGitLab bool
	for _, e := range edges {
		if e.EdgeLabel != edgeLabelImplements {
			continue
		}
		// THE assertion: the go/types-derived subject endpoint key is one of the
		// tree-sitter-derived Symbol vertex keys, byte-for-byte.
		if _, ok := symKeys[e.FromKey]; !ok {
			t.Errorf("IMPLEMENTS subject key %q has no matching Symbol vertex key (orphaned edge); known keys: %v",
				e.FromKey, keysOf(symKeys))
		}
		// The target (interface) endpoint must also match a Symbol vertex key.
		if _, ok := symKeys[e.ToKey]; !ok {
			t.Errorf("IMPLEMENTS target key %q has no matching Symbol vertex key (orphaned edge); known keys: %v",
				e.ToKey, keysOf(symKeys))
		}
		switch e.FromKey {
		case "GitHubForge" + compositeKeyDelim + "github.go":
			sawGitHub = true
		case "GitLabForge" + compositeKeyDelim + "gitlab.go":
			sawGitLab = true
		}
	}
	if !sawGitHub {
		t.Errorf("missing IMPLEMENTS edge for GitHubForge (expected subject key %q)",
			"GitHubForge"+compositeKeyDelim+"github.go")
	}
	if !sawGitLab {
		t.Errorf("missing IMPLEMENTS edge for GitLabForge (expected subject key %q)",
			"GitLabForge"+compositeKeyDelim+"gitlab.go")
	}
}

func keysOf(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestAGEGraphMissesHomonymousPkgVarMethodCall is the P0/P2a regression fixture
// (a) for BUG A (deploy-config go-code repo-review-council report, reviews/
// repo-council/2026-07-01.md, HIGH finding): "Dead-code + code-health emit false
// 'dead' verdicts for functions reachable only via package-level var dispatch."
//
// Mirrors the LIVE-CONFIRMED production shape at internal/compare/coupling.go:33
// (`globalCouplingCache.get(key)`) alongside its sibling churn/touches caches in
// the same package: several distinct *T package-level vars each expose a
// same-named method (here `Get`), invoked from a THIRD file. codegraph/index.go
// builds the AGE graph's CALLS edges via buildAGECallGraph; with typed enrichment
// disabled its path is the untyped `callgraph.BuildCallGraph` (tree-sitter) — NOT
// `callgraph.BuildFromRepo` (the call_trace/impact_analysis path, which
// additionally merges go/types-resolved edges and would disambiguate this
// correctly). BuildCallGraph's resolveCall (graph.go) matches purely by method
// NAME — same file -> same dir -> global — with zero receiver-type awareness, so
// when a call site's own file has no local symbol of that name, a cross-file,
// same-directory lookup returns whichever homonymous method comes first in the
// symbol list, not the one the pkgVar's static type actually points at. The
// other method then gets ZERO CALLS edges in the AGE graph despite a real
// caller — dead_code's `OPTIONAL MATCH (caller:Symbol)-[:CALLS]->(s) WHERE caller
// IS NULL` (store_dead_code.go) sees it as an orphan.
//
// This test exercises the EXACT seam codegraph/index.go uses to build the AGE
// graph — ingestAndParse -> buildAGECallGraph -> buildGraph — and asserts on the
// resulting []edgeData (the literal CALLS-edge representation persisted to
// Apache AGE and read back by dead_code), NOT callgraph.TraceRepo/BuildFromRepo
// (the call_trace path, already typed-aware and not what index.go uses for CALLS).
//
// Two subtests prove the gate is load-bearing in both directions (falsifiable
// either way — deleting the gate check in buildAGECallGraph, or deleting the
// CODEGRAPH_TYPED_ENRICH call site entirely, fails one of these):
//   - "gate disabled" (CODEGRAPH_TYPED_ENRICH unset, the default): BUG A is
//     still present — RED on ad7d6e2, and stays this way today: proves P2a's
//     gate-off path is byte-identical to the pre-existing untyped behaviour.
//   - "gate enabled": CODEGRAPH_TYPED_ENRICH=1 routes through
//     callgraph.EnrichWithTypedResolution, whose go/types pass resolves
//     globalB's static type and produces the correct edge — GREEN, the P2a
//     acceptance criterion for fixture (a).
//
// Not run with t.Parallel(): t.Setenv (used by the "gate enabled" subtest) may
// not be combined with parallel tests/ancestors.
func TestAGEGraphMissesHomonymousPkgVarMethodCall(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"go.mod": "module example.com/pkgvarmethod\n\ngo 1.21\n",
		// Two distinct concrete types, each with a same-named method, each
		// exposed via its own package-level *T var — mirrors couplingCache /
		// churnCache / touchesCache all exposing get/set in internal/compare.
		"cachea.go": `package main

type CacheA struct{}

func (c *CacheA) Get(key string) string { return "a" }

var globalA = &CacheA{}
`,
		"cacheb.go": `package main

type CacheB struct{}

func (c *CacheB) Get(key string) string { return "b" }

var globalB = &CacheB{}
`,
		// Callers live in a THIRD file with no local "Get" symbol, forcing
		// resolveCall's cross-file same-directory fallback (graph.go) — the
		// exact ambiguous path that misattributes the call.
		"main.go": `package main

func UseCacheA() string {
	return globalA.Get("k")
}

func UseCacheB() string {
	return globalB.Get("k")
}

func main() {
	_ = UseCacheA()
	_ = UseCacheB()
}
`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	// Exact codegraph/index.go seam (index.go: ingestAndParse ->
	// buildAGECallGraph -> buildGraph -> []edgeData), the AGE representation
	// dead_code's Cypher query reads. Shared by both subtests below; only the
	// CODEGRAPH_TYPED_ENRICH env var differs between them.
	runFixture := func(t *testing.T) []edgeData {
		t.Helper()
		allFiles, allSymbols, allCalls, fileImports, allRels, allTplRefs, _, err := ingestAndParse(context.Background(), root)
		if err != nil {
			t.Fatalf("ingestAndParse: %v", err)
		}

		cg := buildAGECallGraph(context.Background(), root, allSymbols, allCalls, allFiles)
		_, edges, _ := buildGraph(buildGraphInput{
			Root: root, Files: allFiles, Symbols: allSymbols,
			CallGraph: cg, FileImports: fileImports, Rels: allRels, TplRefs: allTplRefs,
		})
		return edges
	}

	wantFrom := "UseCacheB" + compositeKeyDelim + "main.go"
	wantTo := "Get" + compositeKeyDelim + "cacheb.go"

	hasWantEdge := func(edges []edgeData) bool {
		for _, e := range edges {
			if e.EdgeLabel == "CALLS" && e.FromKey == wantFrom && e.ToKey == wantTo {
				return true
			}
		}
		return false
	}

	t.Run("gate disabled - BUG A still present", func(t *testing.T) {
		t.Setenv("CODEGRAPH_TYPED_ENRICH", "0")
		edges := runFixture(t)
		if hasWantEdge(edges) {
			t.Errorf("expected CALLS edge %s -> %s to be MISSING with the gate off (byte-identical "+
				"to the pre-P2a untyped path); it was found — the gate-off path changed "+
				"behaviour unexpectedly. CALLS edges seen: %v", wantFrom, wantTo, callsEdgeSummary(edges))
		}
	})

	t.Run("gate enabled - BUG A fixed", func(t *testing.T) {
		t.Setenv("CODEGRAPH_TYPED_ENRICH", "1")
		appliedCounter := agegraphTypedEnrichTotal.WithLabelValues("applied")
		before := readCounter(t, appliedCounter)
		edges := runFixture(t)
		after := readCounter(t, appliedCounter)
		if !hasWantEdge(edges) {
			t.Errorf("AGE graph missing CALLS edge %s -> %s with CODEGRAPH_TYPED_ENRICH=1; dead_code "+
				"will falsely report CacheB.Get as dead despite the real caller UseCacheB (BUG A: typed "+
				"enrichment did not land); CALLS edges seen: %v",
				wantFrom, wantTo, callsEdgeSummary(edges))
		}
		if after-before != 1 {
			t.Errorf("expected gocode_agegraph_typed_enrich_total{result=applied} to increment by 1 "+
				"when the go/types typed pass lands; before=%v after=%v", before, after)
		}
	})
}

// callsEdgeSummary formats every CALLS edgeData's From->To key pair for
// failure-message readability.
func callsEdgeSummary(edges []edgeData) []string {
	out := make([]string, 0, len(edges))
	for _, e := range edges {
		if e.EdgeLabel == "CALLS" {
			out = append(out, e.FromKey+"->"+e.ToKey)
		}
	}
	return out
}

// TestAGEGraphMissesVarFuncBindingCallee is the P3 regression fixture (b) for
// BUG A (deploy-config go-code repo-review-council report, reviews/repo-council/
// 2026-07-01.md, HIGH finding): "dead_code (focus=internal/callgraph)
// tier=enhanced flags recordEagerWarm as high-confidence dead... though invoked
// 6x via `var recordEagerWarmFn = recordEagerWarm`" — the exact real shape at
// eager_warm.go:31,69 (this repo's own callgraph package).
//
// Sibling of TestAGEGraphMissesHomonymousPkgVarMethodCall (fixture (a)): same
// codegraph/index.go seam (ingestAndParse -> buildAGECallGraph -> buildGraph),
// same two-subtest gate-off/gate-on structure, different BUG-A sub-shape — a
// package-level var bound directly to a function value (`var workFn =
// realWork`), not an ambiguous same-named method. BuildCallGraph's resolveCall
// (internal/callgraph/graph.go findByName) only matches
// parser.KindFunction/parser.KindMethod symbols, so a KindVar-typed callee name
// never resolves at all — CallEdge.Callee stays nil for the `workFn()` call
// site, and buildGraph's CALLS-edge loop (`if ce.Caller == nil || ce.Callee ==
// nil { continue }`) drops the edge before it reaches the AGE graph; realWork
// then shows zero incoming CALLS edges and dead_code/code_health falsely
// report it dead.
//
// This replaces the earlier internal/callgraph/repo_test.go
// TestBuildCallGraph_VarFuncBindingCalleeUnresolved, which asserted this same
// shape against raw callgraph.BuildCallGraph directly — the ONE seam the fix
// (EnrichWithTypedResolution gated by CODEGRAPH_TYPED_ENRICH, "zero
// tree-sitter-builder changes" per the design) deliberately does not touch, so
// that fixture could never turn GREEN. This test asserts against the actual
// fixed seam instead — buildAGECallGraph — mirroring fixture (a). See
// internal/goanalysis/resolver_hardred_test.go's
// TestResolve_VarFuncBindingAlias for the resolver-level proof that
// goanalysis.Resolve itself emits the UseWorkFn -> realWork typed edge.
//
// Not run with t.Parallel(): t.Setenv (used by the "gate enabled" subtest) may
// not be combined with parallel tests/ancestors.
func TestAGEGraphMissesVarFuncBindingCallee(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"go.mod": "module example.com/varfuncbinding\n\ngo 1.22\n",
		// Mirrors eager_warm.go's shape: a package-level var bound to a
		// function value, invoked only through the var — never by the
		// function's own name.
		"main.go": `package main

func realWork() int { return 42 }

var workFn = realWork

func UseWorkFn() int {
	return workFn()
}

func main() {
	_ = UseWorkFn()
}
`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	// Exact codegraph/index.go seam (index.go: ingestAndParse ->
	// buildAGECallGraph -> buildGraph -> []edgeData), the AGE representation
	// dead_code's Cypher query reads. Shared by both subtests below; only the
	// CODEGRAPH_TYPED_ENRICH env var differs between them.
	runFixture := func(t *testing.T) []edgeData {
		t.Helper()
		allFiles, allSymbols, allCalls, fileImports, allRels, allTplRefs, _, err := ingestAndParse(context.Background(), root)
		if err != nil {
			t.Fatalf("ingestAndParse: %v", err)
		}

		cg := buildAGECallGraph(context.Background(), root, allSymbols, allCalls, allFiles)
		_, edges, _ := buildGraph(buildGraphInput{
			Root: root, Files: allFiles, Symbols: allSymbols,
			CallGraph: cg, FileImports: fileImports, Rels: allRels, TplRefs: allTplRefs,
		})
		return edges
	}

	wantFrom := "UseWorkFn" + compositeKeyDelim + "main.go"
	wantTo := "realWork" + compositeKeyDelim + "main.go"

	hasWantEdge := func(edges []edgeData) bool {
		for _, e := range edges {
			if e.EdgeLabel == "CALLS" && e.FromKey == wantFrom && e.ToKey == wantTo {
				return true
			}
		}
		return false
	}

	t.Run("gate disabled - BUG A still present", func(t *testing.T) {
		t.Setenv("CODEGRAPH_TYPED_ENRICH", "0")
		edges := runFixture(t)
		if hasWantEdge(edges) {
			t.Errorf("expected CALLS edge %s -> %s to be MISSING with the gate off (byte-identical "+
				"to the pre-P2a untyped path); it was found — the gate-off path changed "+
				"behaviour unexpectedly. CALLS edges seen: %v", wantFrom, wantTo, callsEdgeSummary(edges))
		}
	})

	t.Run("gate enabled - BUG A fixed", func(t *testing.T) {
		t.Setenv("CODEGRAPH_TYPED_ENRICH", "1")
		appliedCounter := agegraphTypedEnrichTotal.WithLabelValues("applied")
		before := readCounter(t, appliedCounter)
		edges := runFixture(t)
		after := readCounter(t, appliedCounter)
		if !hasWantEdge(edges) {
			t.Errorf("AGE graph missing CALLS edge %s -> %s with CODEGRAPH_TYPED_ENRICH=1; dead_code "+
				"will falsely report realWork as dead despite the real caller UseWorkFn (BUG A: typed "+
				"enrichment did not land); CALLS edges seen: %v",
				wantFrom, wantTo, callsEdgeSummary(edges))
		}
		if after-before != 1 {
			t.Errorf("expected gocode_agegraph_typed_enrich_total{result=applied} to increment by 1 "+
				"when the go/types typed pass lands; before=%v after=%v", before, after)
		}
	})
}
