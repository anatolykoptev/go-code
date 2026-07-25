package callgraph

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/anatolykoptev/vaelor/internal/goanalysis"
	"github.com/anatolykoptev/vaelor/internal/ingest"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

func TestTraceRepo_Integration(t *testing.T) {
	dir := t.TempDir()
	mainGo := `package main

func main() {
	result := compute(42)
	logResult(result)
}

func compute(x int) int {
	return transform(x) + 1
}

func transform(x int) int {
	return x * 2
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainGo), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := TraceRepo(context.Background(), TraceRepoInput{
		Root:   dir,
		Symbol: "main",
		Opts:   TraceOpts{Direction: "callees", MaxDepth: 5},
	})
	if err != nil {
		t.Fatalf("TraceRepo: %v", err)
	}

	if result.Root == nil || result.Root.Name != "main" {
		t.Fatalf("root = %v, want main", result.Root)
	}
	if result.TotalNodes < 3 {
		t.Errorf("totalNodes = %d, want >= 3 (main, compute, transform)", result.TotalNodes)
	}
	if result.Unresolved < 1 {
		t.Errorf("unresolved = %d, want >= 1 (logResult)", result.Unresolved)
	}
}

func TestTraceRepo_Callers(t *testing.T) {
	dir := t.TempDir()
	mainGo := `package main

func main() {
	serve()
}

func serve() {
	handle()
}

func handle() {
	println("done")
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainGo), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := TraceRepo(context.Background(), TraceRepoInput{
		Root:   dir,
		Symbol: "handle",
		Opts:   TraceOpts{Direction: "callers", MaxDepth: 5},
	})
	if err != nil {
		t.Fatalf("TraceRepo: %v", err)
	}

	if result.Root == nil || result.Root.Name != "handle" {
		t.Fatalf("root = %v, want handle", result.Root)
	}
	if result.TotalNodes < 2 {
		t.Errorf("totalNodes = %d, want >= 2", result.TotalNodes)
	}
}

func TestTraceRepo_SymbolNotFound(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := TraceRepo(context.Background(), TraceRepoInput{
		Root:   dir,
		Symbol: "nonexistent",
		Opts:   TraceOpts{Direction: "callees", MaxDepth: 5},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Root != nil {
		t.Errorf("root should be nil for nonexistent symbol")
	}
}

func TestBuildFromRepo_GoTypesEnhanced(t *testing.T) {
	dir := t.TempDir()

	gomod := "module example.com/enhanced\n\ngo 1.22\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600); err != nil {
		t.Fatal(err)
	}

	// Interface + implementation — go/types resolves the dispatch.
	src := `package main

type Greeter interface {
	Greet() string
}

type Hello struct{}

func (h Hello) Greet() string { return "hello" }

func useGreeter(g Greeter) string {
	return g.Greet()
}

func main() {
	h := Hello{}
	useGreeter(h)
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	cg, err := BuildFromRepo(context.Background(), TraceRepoInput{Root: dir})
	if err != nil {
		t.Fatalf("BuildFromRepo: %v", err)
	}

	if cg.Tier != "enhanced" {
		t.Errorf("expected tier 'enhanced', got %q", cg.Tier)
	}
	if len(cg.Edges) == 0 {
		t.Fatal("expected edges from go/types resolution")
	}

	// Verify main -> useGreeter edge exists.
	hasMainToUse := slices.ContainsFunc(cg.Edges, func(e CallEdge) bool {
		return e.Caller != nil && e.Caller.Name == "main" && e.CalleeName == "useGreeter"
	})
	if !hasMainToUse {
		t.Error("expected main->useGreeter edge")
	}
}

// TestBuildFromRepo_CloneRepoCalleesFiltered is a regression test for the
// callees noise issue: `understand`/`call_trace` on a CloneRepo-shaped
// function previously reported member access (`opts.Slug`, `opts.Ref`,
// `opts.DestDir`, `opts.GithubToken`), local variables (`ctx`, `localPath`,
// `slug`, `err`), and builtins (`append`, `string`) as callees. The default
// build (IncludeFieldAccess=false) must drop those while keeping real calls
// (`NormalizeSlug`, `refreshClone`, `os.Stat`, `os.MkdirAll`,
// `os.RemoveAll`, `filepath.Base`, `filepath.Join`, `strings.ReplaceAll`,
// `fmt.Sprintf`, `fmt.Errorf`, `exec.CommandContext`).
func TestBuildFromRepo_CloneRepoCalleesFiltered(t *testing.T) {
	dir := t.TempDir()

	gomod := "module example.com/cloneshape\n\ngo 1.22\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600); err != nil {
		t.Fatal(err)
	}

	// Mirror of internal/ingest/clone.go's CloneRepo — same call shape, same
	// field-access patterns. Standalone (no exec.Command etc.) so go/types
	// can resolve everything in-package.
	src := `package main

import (
	"fmt"
	"os"
)

type CloneOpts struct {
	Slug        string
	Ref         string
	DestDir     string
	GithubToken string
}

type CloneResult struct {
	LocalPath string
	Ref       string
}

func NormalizeSlug(s string) (string, error) { return s, nil }

func refreshClone(localPath, ref string) error { return nil }

func CloneRepo(ctx int, opts CloneOpts) (*CloneResult, error) {
	slug, err := NormalizeSlug(opts.Slug)
	if err != nil {
		return nil, err
	}

	localPath := opts.DestDir + "/" + slug

	if _, statErr := os.Stat(localPath); statErr == nil {
		if err := refreshClone(localPath, opts.Ref); err == nil {
			return &CloneResult{LocalPath: localPath, Ref: opts.Ref}, nil
		}
		_ = os.RemoveAll(localPath)
	}

	if err := os.MkdirAll(opts.DestDir, 0o755); err != nil {
		return nil, fmt.Errorf("create dest dir: %w", err)
	}

	cloneURL := fmt.Sprintf("https://github.com/%s.git", slug)
	if opts.GithubToken != "" {
		cloneURL = fmt.Sprintf("https://%s@github.com/%s.git", opts.GithubToken, slug)
	}
	_ = cloneURL
	_ = ctx

	return &CloneResult{LocalPath: localPath, Ref: opts.Ref}, nil
}

func main() {}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	cg, err := BuildFromRepo(context.Background(), TraceRepoInput{Root: dir})
	if err != nil {
		t.Fatalf("BuildFromRepo: %v", err)
	}

	// Collect callees of CloneRepo from the tree-sitter tier (go/types tier
	// uses different selector resolution and doesn't emit argref entries
	// either, so this is independent of which tier wins the merge).
	calleeNames := map[string]bool{}
	for _, e := range cg.Edges {
		if e.Caller != nil && e.Caller.Name == "CloneRepo" {
			calleeNames[e.CalleeName] = true
		}
	}

	// Real calls — must be present.
	wantPresent := []string{"NormalizeSlug", "refreshClone", "Stat", "MkdirAll", "RemoveAll", "Sprintf", "Errorf"}
	for _, name := range wantPresent {
		if !calleeNames[name] {
			t.Errorf("expected callee %q from CloneRepo, got: %v", name, calleeNames)
		}
	}

	// Noise that must be dropped — member access of opts and locals.
	wantAbsent := []string{"Slug", "Ref", "DestDir", "GithubToken", "ctx", "localPath", "slug", "err", "dirPerm", "statErr", "cloneURL"}
	for _, name := range wantAbsent {
		if calleeNames[name] {
			t.Errorf("noise leaked into CloneRepo callees: %q (full set: %v)", name, calleeNames)
		}
	}
}

// TestBuildFromRepo_CloneRepoFieldAccessOptIn confirms that with
// IncludeFieldAccess=true, the legacy permissive behaviour is restored:
// unresolved argrefs (`opts.Slug` etc.) reappear as callees.
func TestBuildFromRepo_CloneRepoFieldAccessOptIn(t *testing.T) {
	dir := t.TempDir()

	gomod := "module example.com/cloneshape2\n\ngo 1.22\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600); err != nil {
		t.Fatal(err)
	}

	src := `package main

type Opts struct{ Slug string }

func helper(s string) {}

func F(opts Opts) {
	helper(opts.Slug)
}

func main() { F(Opts{}) }
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	// Bust cache so opt-in path isn't masked by default-path cache hit.
	InvalidateBuildCache()
	cg, err := BuildFromRepo(context.Background(), TraceRepoInput{
		Root:               dir,
		IncludeFieldAccess: true,
	})
	if err != nil {
		t.Fatalf("BuildFromRepo: %v", err)
	}

	hasSlug := false
	for _, e := range cg.Edges {
		if e.Caller != nil && e.Caller.Name == "F" && e.CalleeName == "Slug" {
			hasSlug = true
		}
	}
	if !hasSlug {
		t.Errorf("with IncludeFieldAccess=true, expected `Slug` argref edge from F; edges=%+v", cg.Edges)
	}
}

// TestTryGoTypesResolution_WarnOnFailure verifies that tryGoTypesResolution
// emits a slog.Warn when packages.Load fails (e.g. no go.mod present).
// This gives operators a "why is my repo stuck at basic tier" signal.
func TestTryGoTypesResolution_WarnOnFailure(t *testing.T) {
	// A bare directory with no go.mod forces packages.Load to fail.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main(){}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	prev := slog.Default()
	slog.SetDefault(logger)
	defer slog.SetDefault(prev)

	// tempdir has no go.mod — LoadPackages errors immediately on missing module
	// file, before any context check is reached.
	result, err := tryGoTypesResolution(context.Background(), dir, nil)
	if result != nil {
		t.Error("expected nil result for failing packages.Load")
	}
	if err == nil {
		t.Error("expected non-nil error for failing packages.Load")
	}

	got := buf.String()
	if !strings.Contains(got, "go/packages load failed") {
		t.Errorf("expected warn log containing 'go/packages load failed'; got: %q", got)
	}
}

// TestBuildPrewarmEnv_ContainsCGODisabled verifies that buildPrewarmEnv includes
// CGO_ENABLED=0, which is required to prevent the prewarm go build from failing
// on missing tree-sitter C headers.
func TestBuildPrewarmEnv_ContainsCGODisabled(t *testing.T) {
	env := buildPrewarmEnv()
	if !slices.Contains(env, "CGO_ENABLED=0") {
		t.Errorf("buildPrewarmEnv() missing CGO_ENABLED=0; got: %v", env)
	}
	if !slices.Contains(env, "GOWORK=off") {
		t.Errorf("buildPrewarmEnv() missing GOWORK=off; got: %v", env)
	}
	if !slices.Contains(env, "GIT_TERMINAL_PROMPT=0") {
		t.Errorf("buildPrewarmEnv() missing GIT_TERMINAL_PROMPT=0; got: %v", env)
	}
}

// TestTrySCIPResolution_GoIsNoop asserts that trySCIPResolution returns nil for
// a Go-dominant file set. Go analysis is handled by go/types (goanalysis package)
// so scip-go was removed from the indexer registry. DetectIndexer("go") now
// returns false, causing trySCIPResolution to return nil without invoking any
// external binary.
func TestTrySCIPResolution_GoIsNoop(t *testing.T) {
	files := []*ingest.File{
		{Path: "/tmp/main.go", Language: "go"},
		{Path: "/tmp/util.go", Language: "go"},
	}
	result := trySCIPResolution(context.Background(), t.TempDir(), files, nil)
	if result != nil {
		t.Errorf("trySCIPResolution for Go files returned non-nil %+v; expected no-op", result)
	}
}

func TestBuildFromRepo_FallbackToTreeSitter(t *testing.T) {
	dir := t.TempDir()

	// Python file only — no go.mod, so no go/types resolution.
	src := `def helper():
    pass

def main():
    helper()
`
	if err := os.WriteFile(filepath.Join(dir, "main.py"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	cg, err := BuildFromRepo(context.Background(), TraceRepoInput{Root: dir})
	if err != nil {
		t.Fatalf("BuildFromRepo: %v", err)
	}

	if cg.Tier != "basic" {
		t.Errorf("expected tier 'basic', got %q", cg.Tier)
	}
}

// NOTE: the var-func-binding BUG A fixture (b) formerly here
// (TestBuildCallGraph_VarFuncBindingCalleeUnresolved) asserted against raw
// callgraph.BuildCallGraph — the one seam the fix (EnrichWithTypedResolution
// gated by CODEGRAPH_TYPED_ENRICH on the AGE-graph indexing path) deliberately
// does not touch, so it could never turn GREEN. It has been replaced by
// internal/codegraph/satisfaction_test.go's TestAGEGraphMissesVarFuncBindingCallee,
// which asserts against the actual fixed seam (buildAGECallGraph), mirroring
// TestAGEGraphMissesHomonymousPkgVarMethodCall (fixture (a)). See
// internal/goanalysis/resolver_hardred_test.go's TestResolve_VarFuncBindingAlias
// for the resolver-level proof that goanalysis.Resolve itself emits the edge.

// TestEnrichWithTypedResolution_ColdCache_SetsWarmingAndSkipsImplements is the
// RED test for issue #735 Part 1: when the go/packages LOAD genuinely fails
// (tryGoTypesResolution returns (nil, err)), EnrichWithTypedResolution must set
// Warming=true and must NOT call ExtractGoImplements (which blocks on the same
// slow packages.Load that already failed). Before the fix, Warming was always
// false and ExtractGoImplements was called unconditionally, blocking the request
// path for up to 30s on a cold repo — converting a usable tree-sitter answer
// into a timeout error.
//
// The fixture is a go.mod with a syntax error: packages.Load fails fast at
// go.mod parsing time (no network, no GOCACHE warm-up, no partial-TypeInfo
// recovery) — tryGoTypesResolution returns (nil, err) without hanging. The test
// then asserts the cold-path contract: Warming=true and zero IMPLEMENTS edges
// (ExtractGoImplements was skipped).
//
// Round 1's fixture (broken replace + syntax-broken main.go) did NOT reliably
// trigger a load error: go/packages still returns a package with partial
// TypesInfo for syntax-broken .go files, so LoadPackages succeeded and the
// case was actually the zero-edge path (now covered by
// TestEnrichWithTypedResolution_WarmCache_ZeroCallEdges_NoWarmingButImplements).
// The broken-go.mod fixture is the one that genuinely exercises the load-failed
// branch.
func TestEnrichWithTypedResolution_ColdCache_SetsWarmingAndSkipsImplements(t *testing.T) {
	dir := t.TempDir()

	// go.mod with a syntax error — packages.Load fails at go.mod parse time
	// before any type-checking, returning a hard error. This is the genuine
	// load-failed path (tryGoTypesResolution returns (nil, err)).
	gomod := "module example.com/cold\n\ngo 1.21\n\nthis is not valid go.mod syntax\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600); err != nil {
		t.Fatal(err)
	}

	// A trivially valid main.go — the load fails at go.mod parsing, so main.go
	// content is irrelevant; it exists only so the dir is not empty.
	src := `package main

func main() {}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	// Evict any stale cache entry from a prior test against the same root path.
	// t.TempDir() gives a unique path, but the singleflight + negative-cache
	// in goanalysis is keyed by root — clear it to be safe.
	goanalysis.InvalidateCachedLoad(dir)

	base := &CallGraph{
		Edges:   []CallEdge{{CalleeName: "useMissing"}},
		Symbols: []*parser.Symbol{{Name: "main", Kind: parser.KindFunction, File: filepath.Join(dir, "main.go")}},
		Tier:    "basic",
		Backend: BackendTreeSitter,
	}

	cg := EnrichWithTypedResolution(context.Background(), dir, base, base.Symbols, nil)

	if !cg.Warming {
		t.Error("expected Warming=true on cold cache (tryGoTypesResolution returned nil)")
	}
	if cg.Tier != "basic" {
		t.Errorf("expected Tier=basic on cold path, got %q", cg.Tier)
	}
	// ExtractGoImplements must NOT have been called on the cold path — it would
	// block on the same slow packages.Load that already failed. Verify zero
	// IMPLEMENTS edges were added.
	for _, rel := range cg.TypeRels {
		if rel.Kind == parser.RelImplements {
			t.Errorf("expected no IMPLEMENTS edges on cold path (ExtractGoImplements should be skipped), found %+v", rel)
		}
	}
}

// TestEnrichWithTypedResolution_WarmCache_NoWarmingFlag verifies that when
// go/types resolution succeeds (warm cache or fast load), Warming is NOT set
// and IMPLEMENTS edges are computed normally. This is the warm-path contract:
// the warming note must only appear on the degraded path.
func TestEnrichWithTypedResolution_WarmCache_NoWarmingFlag(t *testing.T) {
	dir := t.TempDir()

	gomod := "module example.com/warm\n\ngo 1.22\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600); err != nil {
		t.Fatal(err)
	}

	src := `package main

type Greeter interface {
	Greet() string
}

type Hello struct{}

func (h Hello) Greet() string { return "hello" }

func useGreeter(g Greeter) string {
	return g.Greet()
}

func main() {
	useGreeter(Hello{})
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	goanalysis.InvalidateCachedLoad(dir)

	base := &CallGraph{
		Symbols: []*parser.Symbol{{Name: "main", Kind: parser.KindFunction, File: filepath.Join(dir, "main.go")}},
		Tier:    "basic",
		Backend: BackendTreeSitter,
	}

	cg := EnrichWithTypedResolution(context.Background(), dir, base, base.Symbols, nil)

	if cg.Warming {
		t.Error("expected Warming=false on warm cache (tryGoTypesResolution succeeded)")
	}
}

// TestEnrichWithTypedResolution_WarmCache_ZeroCallEdges_NoWarmingButImplements
// is the RED test for the round-3 defect on issue #735: when go/packages loads
// successfully but goanalysis.Resolve produces ZERO typed call edges (a Go
// module with an interface, a concrete type that satisfies it, and NO function
// calls), EnrichWithTypedResolution must:
//
//   - NOT set cg.Warming — nothing is warming; the load already succeeded. The
//     background warmGoTypesCache would re-run the same resolution, get zero
//     edges again, and pin Warming=true for the whole cache TTL (the #709
//     failure reintroduced here).
//   - STILL call ExtractGoImplements — IMPLEMENTS enrichment shares the same
//     CachedLoadPackages and is cheap on a warm load. Skipping it (round-1's
//     single-nil-return path) silently drops issue #467's whole feature on any
//     Go repo that happens to have zero typed CALL edges.
//
// Real fixture (no mock): a tiny Go module in t.TempDir() with an interface and
// a concrete type satisfying it, and no function calls at all. go/packages loads
// it cleanly, goanalysis.Resolve yields zero typed call edges, and
// goanalysis.ComputeSatisfactions yields a genuine Hello→Greeter IMPLEMENTS
// edge — exercising the exact path rather than simulating it.
func TestEnrichWithTypedResolution_WarmCache_ZeroCallEdges_NoWarmingButImplements(t *testing.T) {
	dir := t.TempDir()

	gomod := "module example.com/zeroedges\n\ngo 1.22\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600); err != nil {
		t.Fatal(err)
	}

	// No function calls anywhere — goanalysis.Resolve returns zero typed call
	// edges. Hello satisfies Greeter structurally — ComputeSatisfactions
	// produces a Hello→Greeter IMPLEMENTS relationship.
	src := `package main

type Greeter interface {
	Greet() string
}

type Hello struct{}

func (h Hello) Greet() string { return "hello" }

func main() {}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	goanalysis.InvalidateCachedLoad(dir)

	base := &CallGraph{
		Symbols: []*parser.Symbol{{Name: "main", Kind: parser.KindFunction, File: filepath.Join(dir, "main.go")}},
		Tier:    "basic",
		Backend: BackendTreeSitter,
	}

	cg := EnrichWithTypedResolution(context.Background(), dir, base, base.Symbols, nil)

	if cg.Warming {
		t.Error("expected Warming=false on warm load with zero typed call edges (load succeeded, nothing is warming)")
	}

	var hasImplements bool
	for _, rel := range cg.TypeRels {
		if rel.Kind == parser.RelImplements {
			hasImplements = true
			break
		}
	}
	if !hasImplements {
		t.Error("expected at least one IMPLEMENTS edge on warm-zero-edge path (ExtractGoImplements must run); TypeRels is empty")
	}
}

// TestWarmGoTypesCache_NoPrewarmBuild verifies that warmGoTypesCache does NOT
// shell out to `go build` as a pre-warm step (issue #735 Part 2). The pre-warm
// was dropped because it could never succeed on cgo repos under CGO_ENABLED=0
// (build constraints exclude all Go files) and the runtime image lacks gcc for
// CGO_ENABLED=1. GOCACHE is now persistent (ops-side fix), so packages.Load in
// the background warm handles warming without the pre-build.
//
// The test captures slog output and asserts the "pre-warming GOCACHE via go
// build" log does NOT appear — proving the pre-warm step was removed.
func TestWarmGoTypesCache_NoPrewarmBuild(t *testing.T) {
	dir := t.TempDir()

	gomod := "module example.com/nowarm\n\ngo 1.21\n\nrequire example.com/missing v1.0.0\n\nreplace example.com/missing => ./does-not-exist\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	goanalysis.InvalidateCachedLoad(dir)
	InvalidateBuildCache()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	prev := slog.Default()
	slog.SetDefault(logger)
	defer slog.SetDefault(prev)

	// warmGoTypesCache is normally called as a goroutine; call it synchronously
	// to inspect its behaviour. It will fail (broken deps) but must NOT attempt
	// a `go build` pre-warm.
	warmGoTypesCache(dir, nil, cgCacheKey(TraceRepoInput{Root: dir}))

	logOutput := buf.String()
	if strings.Contains(logOutput, "pre-warming GOCACHE via go build") {
		t.Errorf("expected NO pre-warm go build log, but found it in output: %s", logOutput)
	}
	if strings.Contains(logOutput, "go build pre-warm") {
		t.Errorf("expected NO go build pre-warm log, but found it in output: %s", logOutput)
	}
}

// TestWarmGoTypesCache_ColdFailThenSuccessfulZeroEdgeWarm_ClearsWarmingAndRestoresImplements
// is the RED test for the round-4 defect on issue #735: warmGoTypesCache is
// started only when the synchronous path did NOT reach BackendGoTypes
// (repo.go:108). Two states reach it:
//
//   - state 1: cold sync load FAILED (tryGoTypesResolution returned (nil, err))
//     → cached entry has Warming=true, NO IMPLEMENTS (ExtractGoImplements was
//     skipped on the cold path).
//   - state 2: sync load SUCCEEDED with zero typed call edges (nil, nil) →
//     cached entry has Warming=false, IMPLEMENTS present.
//
// When the patient background retry SUCCEEDS but yields zero typed edges
// (typedCG == nil, err == nil), the pre-fix code at repo.go:320 guarded the
// cache update behind `if typedCG != nil` and never touched the entry. For
// state 1 that means Warming stays true and IMPLEMENTS stays absent for the
// full 5-minute cgCacheTTL — even though recordBackgroundWarm("completed")
// fired and the warm was done. The round-3 comment justifying the skip
// ("IMPLEMENTS edges were already added on the synchronous request path")
// was true for state 2 only — inverted for the case the background warm was
// written for.
//
// This test seeds state 1 manually (Warming=true, no IMPLEMENTS), runs a real
// background warm against a tiny Go module with an interface + satisfying
// type and NO function calls (so goanalysis.Resolve yields zero typed call
// edges → typedCG == nil, err == nil), and asserts the cached entry ends
// with Warming == false AND at least one IMPLEMENTS edge. The two assertions
// are independent so each can redden on its own when its branch is reverted.
func TestWarmGoTypesCache_ColdFailThenSuccessfulZeroEdgeWarm_ClearsWarmingAndRestoresImplements(t *testing.T) {
	dir := t.TempDir()

	gomod := "module example.com/round4warmsuccess\n\ngo 1.22\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600); err != nil {
		t.Fatal(err)
	}

	// No function calls — goanalysis.Resolve returns zero typed call edges
	// (typedCG == nil, err == nil on the background retry). Hello satisfies
	// Greeter structurally — ComputeSatisfactions produces a Hello→Greeter
	// IMPLEMENTS relationship, which ExtractGoImplements must restore.
	src := `package main

type Greeter interface {
	Greet() string
}

type Hello struct{}

func (h Hello) Greet() string { return "hello" }

func main() {}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	goanalysis.InvalidateCachedLoad(dir)
	InvalidateBuildCache()

	key := cgCacheKey(TraceRepoInput{Root: dir})

	// Seed state 1: the synchronous path failed (cold load), so the cached
	// entry has Warming=true and NO IMPLEMENTS edges. This is exactly what
	// BuildFromRepo would have cached before kicking off the background warm.
	seeded := &CallGraph{
		Symbols: []*parser.Symbol{{Name: "main", Kind: parser.KindFunction, File: filepath.Join(dir, "main.go")}},
		Tier:    "basic",
		Backend: BackendTreeSitter,
		Warming: true,
	}
	cgCache.set(key, seeded, dir)

	// Run the background warm synchronously. The retry invalidates the
	// negative-cached cold failure, re-runs packages.Load (now succeeds on
	// the tiny module), gets zero typed call edges, and must refresh the
	// cached entry: clear Warming and restore IMPLEMENTS.
	warmGoTypesCache(dir, seeded.Symbols, key)

	got, ok := cgCache.get(key, dir)
	if !ok {
		t.Fatal("expected cached entry to still be present after warm")
	}

	// Assertion A (independent): Warming must be cleared — the warm is done.
	if got.Warming {
		t.Error("expected Warming=false after successful zero-edge background warm; the warm completed and recordBackgroundWarm(\"completed\") fired, but the cached entry was never refreshed (round-4 defect)")
	}

	// Assertion B (independent): at least one IMPLEMENTS edge must be present
	// — state 1 had none (ExtractGoImplements was skipped on the cold sync
	// path), and the successful warm must restore them.
	var hasImplements bool
	for _, rel := range got.TypeRels {
		if rel.Kind == parser.RelImplements {
			hasImplements = true
			break
		}
	}
	if !hasImplements {
		t.Error("expected at least one IMPLEMENTS edge after successful zero-edge warm (state 1 had none; ExtractGoImplements must run on the warm path); TypeRels is empty")
	}
}

// TestWarmGoTypesCache_ColdFailThenFailedWarm_PreservesCacheAndRecordsFailed
// pins today's failure-path behaviour so a future change cannot silently alter
// it. When the patient background retry FAILS (err != nil), warmGoTypesCache
// must log, record outcome="failed", and return WITHOUT touching the cached
// entry — the cache stays at whatever the synchronous path left (state 1:
// Warming=true, no IMPLEMENTS). This is correct: nothing was warmed, so the
// warming note stays honest and a later retry can still upgrade the entry.
func TestWarmGoTypesCache_ColdFailThenFailedWarm_PreservesCacheAndRecordsFailed(t *testing.T) {
	dir := t.TempDir()

	// go.mod with a syntax error — packages.Load fails at go.mod parse time
	// (no network, no GOCACHE warm-up), so tryGoTypesResolution returns
	// (nil, err) on the background retry too.
	gomod := "module example.com/round4warmfail\n\ngo 1.21\n\nthis is not valid go.mod syntax\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	goanalysis.InvalidateCachedLoad(dir)
	InvalidateBuildCache()

	key := cgCacheKey(TraceRepoInput{Root: dir})

	// Seed state 1: Warming=true, no IMPLEMENTS — what the cold sync path
	// cached before kicking off the background warm.
	seeded := &CallGraph{
		Symbols: []*parser.Symbol{{Name: "main", Kind: parser.KindFunction, File: filepath.Join(dir, "main.go")}},
		Tier:    "basic",
		Backend: BackendTreeSitter,
		Warming: true,
	}
	cgCache.set(key, seeded, dir)

	failedBefore := testutil.ToFloat64(backgroundWarmTotal.WithLabelValues("failed"))

	// Run the background warm synchronously. It must fail (broken go.mod),
	// record "failed", and leave the cache untouched.
	warmGoTypesCache(dir, seeded.Symbols, key)

	failedAfter := testutil.ToFloat64(backgroundWarmTotal.WithLabelValues("failed"))
	if failedAfter != failedBefore+1 {
		t.Errorf("expected backgroundWarmTotal{failed} to increment by 1 (%v → %v); the failure path must still record outcome=failed", failedBefore, failedAfter)
	}

	got, ok := cgCache.get(key, dir)
	if !ok {
		t.Fatal("expected cached entry to still be present after a FAILED warm (cache must not be evicted on failure)")
	}

	// Cache must be unchanged: Warming still true (nothing was warmed), no
	// IMPLEMENTS (ExtractGoImplements must not run on the failure path).
	if !got.Warming {
		t.Error("expected Warming=true to be PRESERVED after a failed background warm (nothing was warmed; the warming note must stay honest for a later retry)")
	}
	for _, rel := range got.TypeRels {
		if rel.Kind == parser.RelImplements {
			t.Errorf("expected NO IMPLEMENTS edges after a failed warm (ExtractGoImplements must not run on the failure path); found %+v", rel)
		}
	}
}
