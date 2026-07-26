package callgraph

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	"github.com/anatolykoptev/vaelor/internal/ingest"
	"github.com/anatolykoptev/vaelor/internal/parser"
	sciplib "github.com/sourcegraph/scip/bindings/go/scip"
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

// TestEnrichWithTypedResolution_LoadFailWarn verifies that the composition
// seam emits a slog.Warn when the go/packages load it owns fails (e.g. no
// go.mod present). This gives operators a "why is my repo stuck at basic
// tier" signal. tryGoTypesResolution no longer loads go/packages itself
// (issue #747: the load moved up to the seam and the *LoadResult is passed
// through), so the warn moved with it — this test exercises the new seam
// rather than the old tryGoTypesResolution load path. The assertion itself
// (a warn containing "go/packages load failed" on a failing packages.Load) is
// unchanged; only the call site moved.
func TestEnrichWithTypedResolution_LoadFailWarn(t *testing.T) {
	// A go.mod with a syntax error: HasGoModule is true (the file exists) so
	// EnrichWithTypedResolution enters the go/types branch, but packages.Load
	// fails at go.mod parse time.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module example.com/loadwarn\n\ngo 1.21\n\nthis is not valid go.mod syntax\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	prev := slog.Default()
	slog.SetDefault(logger)
	defer slog.SetDefault(prev)

	cg := EnrichWithTypedResolution(context.Background(), dir,
		&CallGraph{Tier: "basic", Backend: BackendTreeSitter},
		[]*parser.Symbol{{Name: "main", Kind: parser.KindFunction, File: filepath.Join(dir, "main.go")}},
		nil)
	if !cg.Warming {
		t.Error("expected Warming=true on failing packages.Load")
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

	// Run the background warm synchronously. The retry re-runs packages.Load
	// (now succeeds on the tiny module), gets zero typed call edges, and must
	// refresh the cached entry: clear Warming and restore IMPLEMENTS.
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

// TestWarmGoTypesCache_ZeroEdgePath_NoRaceWithConcurrentReader pins the
// round-5 fix: warmGoTypesCache's zero-edge branch (typedCG == nil, err == nil)
// must NOT mutate the *CallGraph pointer returned by cgCache.get in place.
// That pointer is shared with any concurrent BuildFromRepo cache hit
// (repo.go:76) reading the same object, and warmGoTypesCache runs in a
// background goroutine (repo.go:109) — in-place writes to Warming and
// TypeRels are a data race.
//
// The test seeds the cache, grabs the shared pointer the way a cache hit
// would, runs a reader goroutine that continuously reads Warming and ranges
// TypeRels on that shared pointer, and concurrently runs the real zero-edge
// warm path (which, under the bug, writes through the shared pointer). Under
// -race the bug reddens with WARNING: DATA RACE on Warming and/or TypeRels;
// after the fix the warm path writes to a fresh *CallGraph and the test is
// green. Without -race the test passes either way (the race is invisible to
// the repo's non-race gate — Makefile:28 — which is the point of filing the
// gap separately).
func TestWarmGoTypesCache_ZeroEdgePath_NoRaceWithConcurrentReader(t *testing.T) {
	dir := t.TempDir()

	gomod := "module example.com/round5race\n\ngo 1.22\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600); err != nil {
		t.Fatal(err)
	}
	// Interface + satisfying type, no function calls → goanalysis.Resolve
	// yields zero typed call edges → typedCG == nil, err == nil on the
	// background retry, exercising the zero-edge branch exactly.
	src := `package main

type Greeter interface {
	Greet() string
}

type Hello struct{}

func (h Hello) Greet() string { return "hello" }

func main() {}`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	InvalidateBuildCache()

	key := cgCacheKey(TraceRepoInput{Root: dir})

	// Seed state 1 (cold sync load failed): Warming=true, no IMPLEMENTS.
	// Pre-allocate TypeRels with capacity so the in-place append under the
	// bug writes into the shared backing array (the race is on both the
	// slice header AND the backing array); without capacity append would
	// allocate a fresh array and shrink the race window to the header only.
	seeded := &CallGraph{
		Symbols:  []*parser.Symbol{{Name: "main", Kind: parser.KindFunction, File: filepath.Join(dir, "main.go")}},
		Tier:     "basic",
		Backend:  BackendTreeSitter,
		Warming:  true,
		TypeRels: make([]parser.TypeRelationship, 0, 8),
	}
	cgCache.set(key, seeded, dir)

	// Grab the shared pointer the way a concurrent BuildFromRepo cache hit
	// would (repo.go:76). This is the exact pointer warmGoTypesCache will
	// get from the cache and, under the bug, mutate in place.
	sharedCg, ok := cgCache.get(key, dir)
	if !ok {
		t.Fatal("expected seeded cache hit")
	}

	// Reader goroutine: continuously read Warming and range TypeRels on the
	// shared pointer for the entire duration of the warm. No sleep — the
	// loop runs until the writer signals completion, guaranteeing genuine
	// overlap with the writer's mutation.
	writerDone := make(chan struct{})
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			select {
			case <-writerDone:
				return
			default:
				_ = sharedCg.Warming
				for range sharedCg.TypeRels {
				}
				runtime.Gosched()
			}
		}
	}()

	// Writer: the real zero-edge warm path. Under the bug it writes
	// sharedCg.Warming = false and appends to sharedCg.TypeRels in place
	// while the reader reads them → DATA RACE. After the fix it builds a
	// fresh *CallGraph and writes there → no race.
	warmGoTypesCache(dir, seeded.Symbols, key)
	close(writerDone)
	<-readerDone
}

// writeSCIPStreamingIndex serializes a sciplib.Index into the length-delimited
// streaming format that ReadIndex (IndexVisitor.ParseStreaming) expects. Each
// sub-message (Metadata, Document, ExternalSymbol) is written as
// tag + varint-length + data. This is the on-disk format of a .scip index file.
func writeSCIPStreamingIndex(path string, idx *sciplib.Index) error {
	var buf []byte
	if idx.Metadata != nil {
		data, err := proto.Marshal(idx.Metadata)
		if err != nil {
			return err
		}
		buf = protowire.AppendTag(buf, 1, protowire.BytesType)
		buf = protowire.AppendVarint(buf, uint64(len(data)))
		buf = append(buf, data...)
	}
	for _, doc := range idx.Documents {
		data, err := proto.Marshal(doc)
		if err != nil {
			return err
		}
		buf = protowire.AppendTag(buf, 2, protowire.BytesType)
		buf = protowire.AppendVarint(buf, uint64(len(data)))
		buf = append(buf, data...)
	}
	for _, sym := range idx.ExternalSymbols {
		data, err := proto.Marshal(sym)
		if err != nil {
			return err
		}
		buf = protowire.AppendTag(buf, 3, protowire.BytesType)
		buf = protowire.AppendVarint(buf, uint64(len(data)))
		buf = append(buf, data...)
	}
	return os.WriteFile(path, buf, 0o644)
}

// TestEnrichWithTypedResolution_ColdGo_SCIPSucceeds_PreservesWarming is the
// integration test for the round-6 defect on issue #735: on a polyglot Go
// repo where the go/packages LOAD FAILS (cold cache → Warming=true) but SCIP
// resolution SUCCEEDS (non-Go language indexed), MergeCallGraphs must carry
// Warming from the base graph. Before the fix, MergeCallGraphs dropped
// Warming, and the caller set Tier="enhanced" — producing a degraded Go
// answer labelled enhanced with no retry note, the exact failure this PR
// exists to prevent.
//
// No real SCIP indexer binary is available in the test environment. The test
// drives the REAL trySCIPResolution → RunIndexerSafe → RunIndexer →
// ReadIndex → ConvertToEdges → ConvertToCallGraph → MergeCallGraphs path by
// installing a fake "scip-typescript" script in PATH that writes a
// pre-serialized SCIP index (constructed via proto.Marshal on a sciplib.Index
// with a simple main→greet call). This exercises every conversion step
// except the external indexer's own analysis — the index content is
// synthetic, not produced by scip-typescript itself.
func TestEnrichWithTypedResolution_ColdGo_SCIPSucceeds_PreservesWarming(t *testing.T) {
	dir := t.TempDir()

	// go.mod with a syntax error — packages.Load fails at go.mod parse time
	// (the genuine load-failed path: tryGoTypesResolution returns (nil, err)).
	gomod := "module example.com/coldscip\n\ngo 1.21\n\nthis is not valid go.mod syntax\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600); err != nil {
		t.Fatal(err)
	}

	// Trivial main.go — the load fails at go.mod parsing, so content is
	// irrelevant; it exists so HasGoModule returns true.
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// 3+ TypeScript files so polyglot.DetectedLanguages includes "typescript"
	// (minLangFiles=3). Content is irrelevant — the fake indexer ignores the
	// source and writes a pre-built index.
	for i := 0; i < 3; i++ {
		fn := filepath.Join(dir, "file"+string(rune('0'+i))+".ts")
		if err := os.WriteFile(fn, []byte("export function f"+string(rune('0'+i))+"() {}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// Build a synthetic SCIP index with one document (main.ts) containing a
	// main→greet call. ConvertToEdges will extract one typed edge, so
	// trySCIPResolution returns non-nil.
	scipIdx := &sciplib.Index{
		Documents: []*sciplib.Document{
			{
				RelativePath: "main.ts",
				Occurrences: []*sciplib.Occurrence{
					{
						Range:       []int32{0, 5, 9},
						Symbol:      "ts . testpkg main().",
						SymbolRoles: int32(sciplib.SymbolRole_Definition),
					},
					{
						Range:       []int32{5, 5, 10},
						Symbol:      "ts . testpkg greet().",
						SymbolRoles: int32(sciplib.SymbolRole_Definition),
					},
					{
						Range:  []int32{2, 4, 9},
						Symbol: "ts . testpkg greet().",
					},
				},
				Symbols: []*sciplib.SymbolInformation{
					{Symbol: "ts . testpkg main().", Kind: sciplib.SymbolInformation_Function, DisplayName: "main"},
					{Symbol: "ts . testpkg greet().", Kind: sciplib.SymbolInformation_Function, DisplayName: "greet"},
				},
			},
		},
	}

	// Serialize the index to a file the fake indexer script will copy.
	indexBytesPath := filepath.Join(dir, "prebuilt_index.scip")
	if err := writeSCIPStreamingIndex(indexBytesPath, scipIdx); err != nil {
		t.Fatalf("writeSCIPStreamingIndex: %v", err)
	}

	// Create a fake "scip-typescript" binary in a temp bin dir. The real
	// RunIndexer invokes `scip-typescript index` with cmd.Dir=repo root and
	// expects index.scip at filepath.Join(dir, "index.scip"). The script
	// copies the pre-built index there.
	binDir := t.TempDir()
	fakeIndexer := filepath.Join(binDir, "scip-typescript")
	script := "#!/bin/sh\ncp \"" + indexBytesPath + "\" ./index.scip\n"
	if err := os.WriteFile(fakeIndexer, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	// Add the fake binary to PATH so exec.LookPath finds it.
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	// Isolate the SCIP cache so the test doesn't pollute /tmp/scip-cache.
	// NOTE: scipCache is a package-level var initialized at init time from
	// scipCacheDir(). t.Setenv("SCIP_CACHE_DIR", ...) does NOT recreate it,
	// so the cache dir is still /tmp/scip-cache. This is acceptable: the
	// cache key is content-addressed (lang + hash of repo files), and
	// t.TempDir gives a unique repo path with unique file contents, so the
	// key won't collide across test runs. A cache HIT on a second run still
	// returns the same edges (the cached index is a copy of what the fake
	// indexer produced), so the test remains valid.

	// Construct the ingest.File list for language detection. trySCIPResolution
	// uses this to detect languages; the actual indexing runs against the dir.
	files := []*ingest.File{
		{Path: filepath.Join(dir, "file0.ts"), RelPath: "file0.ts", Language: "typescript"},
		{Path: filepath.Join(dir, "file1.ts"), RelPath: "file1.ts", Language: "typescript"},
		{Path: filepath.Join(dir, "file2.ts"), RelPath: "file2.ts", Language: "typescript"},
	}

	base := &CallGraph{
		Edges:   []CallEdge{{CalleeName: "tsFunc"}},
		Symbols: []*parser.Symbol{{Name: "main", Kind: parser.KindFunction, File: filepath.Join(dir, "main.go")}},
		Tier:    "basic",
		Backend: BackendTreeSitter,
	}

	cg := EnrichWithTypedResolution(context.Background(), dir, base, base.Symbols, files)

	if !cg.Warming {
		t.Error("expected Warming=true preserved through SCIP merge on cold Go path, got false")
	}
	if cg.Tier != "enhanced" {
		t.Errorf("expected Tier=enhanced (SCIP succeeded), got %q", cg.Tier)
	}
	if cg.Backend != BackendSCIP {
		t.Errorf("expected Backend=%q, got %q", BackendSCIP, cg.Backend)
	}
}

// TestBuildFromRepo_SiblingKey_StaleWarmingClearedAfterSuccessfulWarm is the
// RED test for the round-7 defect on issue #735: the background warm's
// single-flight guard is root-keyed (goTypesWarmingSet, repo.go:288), so on a
// cold repo only ONE warm runs and only the warm's OWN key gets its Warming
// flag cleared in place (repo.go:344-379). Sibling keys — same root, different
// Focus/Language/IncludeFieldAccess scope — keep Warming=true for the full
// 5-minute cgCacheTTL because their warm was suppressed as `skipped` and their
// entry was never touched. A cache hit on such a sibling returns a stale
// Warming=true graph whose "retry for the enhanced tier" note can never
// resolve — the retry hits the same stale cache.
//
// This test seeds TWO sibling cache keys (differ by IncludeFieldAccess) on one
// cold root, runs the warm for keyA only, then calls BuildFromRepo for keyB's
// scope. The fix detects root in goTypesWarmedSet + cached.Warming=true and
// treats the hit as a miss → rebuilds at the enhanced tier honestly. On
// 6b5a435b (without the fix) the cache hit returns the stale Warming=true
// entry directly → both assertions RED.
func TestBuildFromRepo_SiblingKey_StaleWarmingClearedAfterSuccessfulWarm(t *testing.T) {
	dir := t.TempDir()

	gomod := "module example.com/siblingwarm\n\ngo 1.22\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600); err != nil {
		t.Fatal(err)
	}
	// Interface + satisfying type, no function calls → goanalysis.Resolve
	// yields zero typed call edges → warm succeeds with typedCG == nil,
	// err == nil (the zero-edge branch). IMPLEMENTS is still extracted.
	src := `package main

type Greeter interface {
	Greet() string
}

type Hello struct{}

func (h Hello) Greet() string { return "hello" }

func main() {}`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	InvalidateBuildCache()
	// Clean package-level state from prior tests.
	goTypesWarmingSet.Delete(dir)
	goTypesWarmedSet.Delete(dir)
	t.Cleanup(func() { goTypesWarmedSet.Delete(dir) })

	// Two sibling cache keys — same root, different IncludeFieldAccess scope.
	keyA := cgCacheKey(TraceRepoInput{Root: dir, IncludeFieldAccess: false})
	keyB := cgCacheKey(TraceRepoInput{Root: dir, IncludeFieldAccess: true})

	// Seed BOTH as state 1 (cold sync load failed): Warming=true, no IMPLEMENTS.
	seedA := &CallGraph{
		Symbols: []*parser.Symbol{{Name: "main", Kind: parser.KindFunction, File: filepath.Join(dir, "main.go")}},
		Tier:    "basic", Backend: BackendTreeSitter, Warming: true,
	}
	seedB := &CallGraph{
		Symbols: []*parser.Symbol{{Name: "main", Kind: parser.KindFunction, File: filepath.Join(dir, "main.go")}},
		Tier:    "basic", Backend: BackendTreeSitter, Warming: true,
	}
	cgCache.set(keyA, seedA, dir)
	cgCache.set(keyB, seedB, dir)

	// Run the warm for keyA only (the warm's own key). It succeeds, stores
	// root in goTypesWarmedSet, and refreshes keyA in place. keyB is never
	// touched — its warm was suppressed as `skipped`.
	warmGoTypesCache(dir, seedA.Symbols, keyA)

	// Call BuildFromRepo for keyB's scope. The cache hit returns the stale
	// entry (Warming=true). The fix detects root in goTypesWarmedSet and
	// treats it as a miss → rebuilds.
	cg, err := BuildFromRepo(context.Background(), TraceRepoInput{Root: dir, IncludeFieldAccess: true})
	if err != nil {
		t.Fatalf("BuildFromRepo: %v", err)
	}

	// Assertion A: Warming must be false — the rebuild ran the real path
	// against a now-warm GOCACHE, which does not set Warming.
	if cg.Warming {
		t.Error("expected Warming=false on sibling key after successful warm; the cache hit returned a stale Warming=true entry (round-7: warm's single-flight guard is root-keyed, so sibling scopes never got their flag cleared)")
	}

	// Assertion B: IMPLEMENTS must be present — proves the rebuild actually
	// ran (not just a flag clear), and ExtractGoImplements executed on the
	// warm load-succeeded path.
	var hasImplements bool
	for _, rel := range cg.TypeRels {
		if rel.Kind == parser.RelImplements {
			hasImplements = true
			break
		}
	}
	if !hasImplements {
		t.Error("expected at least one IMPLEMENTS edge on sibling key rebuild; TypeRels is empty (the rebuild should have run ExtractGoImplements on the warm path)")
	}
}

// TestBuildFromRepo_SiblingKey_FailedWarm_PreservesHonestWarmingNote pins the
// honest-failure path: when the background warm FAILS, goTypesWarmedSet is NOT
// stored, so sibling keys keep their honest "retry for the enhanced tier"
// note. The fix must NOT divert on a failed warm — the cache hit is returned
// as-is with Warming=true, so a later retry can still upgrade the entry.
func TestBuildFromRepo_SiblingKey_FailedWarm_PreservesHonestWarmingNote(t *testing.T) {
	dir := t.TempDir()

	// Broken go.mod — packages.Load fails at parse time on both the sync and
	// background paths. warmGoTypesCache records "failed" and does NOT store
	// root in goTypesWarmedSet.
	gomod := "module example.com/siblingfail\n\ngo 1.21\n\nthis is not valid go.mod syntax\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	InvalidateBuildCache()
	goTypesWarmingSet.Delete(dir)
	goTypesWarmedSet.Delete(dir)
	t.Cleanup(func() { goTypesWarmedSet.Delete(dir) })

	keyA := cgCacheKey(TraceRepoInput{Root: dir, IncludeFieldAccess: false})
	keyB := cgCacheKey(TraceRepoInput{Root: dir, IncludeFieldAccess: true})

	seedA := &CallGraph{
		Symbols: []*parser.Symbol{{Name: "main", Kind: parser.KindFunction, File: filepath.Join(dir, "main.go")}},
		Tier:    "basic", Backend: BackendTreeSitter, Warming: true,
	}
	seedB := &CallGraph{
		Symbols: []*parser.Symbol{{Name: "main", Kind: parser.KindFunction, File: filepath.Join(dir, "main.go")}},
		Tier:    "basic", Backend: BackendTreeSitter, Warming: true,
	}
	cgCache.set(keyA, seedA, dir)
	cgCache.set(keyB, seedB, dir)

	// Run the warm for keyA — it FAILS (broken go.mod), records "failed",
	// does NOT store root in goTypesWarmedSet.
	warmGoTypesCache(dir, seedA.Symbols, keyA)

	// Root must NOT be in goTypesWarmedSet after a failed warm.
	if _, warmed := goTypesWarmedSet.Load(dir); warmed {
		t.Fatal("goTypesWarmedSet must NOT contain root after a failed warm (only successful warms store)")
	}

	// Call BuildFromRepo for keyB's scope. The cache hit returns the stale
	// entry (Warming=true). Root is NOT in goTypesWarmedSet, so the fix does
	// NOT divert — returns the stale entry as-is.
	cg, err := BuildFromRepo(context.Background(), TraceRepoInput{Root: dir, IncludeFieldAccess: true})
	if err != nil {
		t.Fatalf("BuildFromRepo: %v", err)
	}

	// Warming must STILL be true — nothing was warmed, so the note stays
	// honest and a later retry can still upgrade the entry.
	if !cg.Warming {
		t.Error("expected Warming=true PRESERVED on sibling key after a FAILED warm (nothing was warmed; the note must stay honest for a later retry)")
	}
}

// TestBuildFromRepo_WarmOwnKey_NotDivertedByWarmedSet is the regression guard
// for rounds 3-6: the warm's OWN key is refreshed in place by warmGoTypesCache
// (Warming cleared, Tier/Backend set, IMPLEMENTS present), and the round-7 fix
// must NOT divert it — cached.Warming is false after the refresh, so the
// fix's condition (cached.Warming && root in goTypesWarmedSet) is false and
// the cache hit is returned as-is.
func TestBuildFromRepo_WarmOwnKey_NotDivertedByWarmedSet(t *testing.T) {
	dir := t.TempDir()

	gomod := "module example.com/warmown\n\ngo 1.22\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600); err != nil {
		t.Fatal(err)
	}
	// Interface + satisfying type + a concrete method call → goanalysis.Resolve
	// yields typed call edges → typedCG != nil on the warm → Tier="enhanced",
	// Backend=BackendGoTypes. The interface also gives IMPLEMENTS edges.
	src := `package main

type Greeter interface {
	Greet() string
}

type Hello struct{}

func (h Hello) Greet() string { return "hello" }

func main() {
	h := Hello{}
	h.Greet()
}`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	InvalidateBuildCache()
	goTypesWarmingSet.Delete(dir)
	goTypesWarmedSet.Delete(dir)
	t.Cleanup(func() { goTypesWarmedSet.Delete(dir) })

	keyA := cgCacheKey(TraceRepoInput{Root: dir})

	// Seed state 1: Warming=true, no IMPLEMENTS, basic tier.
	seedA := &CallGraph{
		Symbols: []*parser.Symbol{{Name: "main", Kind: parser.KindFunction, File: filepath.Join(dir, "main.go")}},
		Tier:    "basic", Backend: BackendTreeSitter, Warming: true,
	}
	cgCache.set(keyA, seedA, dir)

	// Run the warm for keyA. It succeeds with typedCG != nil, refreshes keyA
	// in place: Warming=false, Tier="enhanced", Backend=BackendGoTypes,
	// IMPLEMENTS present. Root is stored in goTypesWarmedSet.
	warmGoTypesCache(dir, seedA.Symbols, keyA)

	// Call BuildFromRepo for keyA's scope. Cache hit returns the refreshed
	// entry (Warming=false). Root IS in goTypesWarmedSet, but cached.Warming
	// is false → fix condition is false → NOT diverted. Returns as-is.
	cg, err := BuildFromRepo(context.Background(), TraceRepoInput{Root: dir})
	if err != nil {
		t.Fatalf("BuildFromRepo: %v", err)
	}

	if cg.Warming {
		t.Error("expected Warming=false on warm's own key (refreshed in place by warmGoTypesCache)")
	}
	if cg.Tier != "enhanced" {
		t.Errorf("expected Tier=enhanced on warm's own key (typedCG != nil), got %q", cg.Tier)
	}
	if cg.Backend != BackendGoTypes {
		t.Errorf("expected Backend=%q on warm's own key, got %q", BackendGoTypes, cg.Backend)
	}
	var hasImplements bool
	for _, rel := range cg.TypeRels {
		if rel.Kind == parser.RelImplements {
			hasImplements = true
			break
		}
	}
	if !hasImplements {
		t.Error("expected at least one IMPLEMENTS edge on warm's own key (ExtractGoImplements runs on the warm path)")
	}
}

// TestBuildFromRepo_DivertTerminatesAfterFailedRebuild is the loop-guard
// RED test for the round-8 fix on issue #735. Round 7 diverts a cache hit to
// a full rebuild when the entry has Warming=true and the root is in
// goTypesWarmedSet. Its three tests all cover the case where the rebuild
// SUCCEEDS, so Warming comes back false and the next request is a normal
// hit. The untested branch: the rebuild FAILS typed resolution again. Then
// Warming=true is re-cached, the root is still in goTypesWarmedSet (which
// never evicts), and the next request diverts again — forever. Round 7
// turns a bounded 5-minute stale note into an unbounded full rebuild on
// EVERY request for that scope, for the process lifetime.
//
// This test seeds a Warming=true entry whose at predates the warm
// completion, with a broken go.mod so typed resolution keeps failing. Two
// BuildFromRepo calls: the first must rebuild (divert), the second must be
// a cache hit (not diverted). Discriminate on pointer identity — a rebuild
// produces a fresh *CallGraph, a cache hit returns the same pointer. On
// 0584b60f the second call diverts again → different pointer → RED.
func TestBuildFromRepo_DivertTerminatesAfterFailedRebuild(t *testing.T) {
	dir := t.TempDir()

	// Broken go.mod — packages.Load fails on both sync and background paths,
	// so the rebuild's typed resolution fails and the re-cached entry keeps
	// Warming=true. Round 7's FailedWarm test uses the same shape.
	gomod := "module example.com/loopguard\n\ngo 1.21\n\nthis is not valid go.mod syntax\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	InvalidateBuildCache()
	goTypesWarmingSet.Delete(dir)
	goTypesWarmedSet.Delete(dir)
	t.Cleanup(func() {
		goTypesWarmedSet.Delete(dir)
		goTypesWarmingSet.Delete(dir)
	})

	key := cgCacheKey(TraceRepoInput{Root: dir})

	// Seed a Warming=true entry (at = t0). This is the "stale" entry —
	// cached before the warm completed.
	seed := &CallGraph{
		Symbols: []*parser.Symbol{{Name: "main", Kind: parser.KindFunction, File: filepath.Join(dir, "main.go")}},
		Tier:    "basic", Backend: BackendTreeSitter, Warming: true,
	}
	cgCache.set(key, seed, dir)

	// Mark the root as warmed (warmedAt = t1 > t0). Simulates a successful
	// warm that completed at some earlier point — the root is genuinely warm
	// even though this scope's typed resolution will fail (e.g. request-path
	// timeout under load, issue #735).
	goTypesWarmedSet.Store(dir, time.Now())

	// Call 1: entry at (t0) predates warmedAt (t1) → divert → rebuild.
	// The rebuild fails typed resolution (broken go.mod) → re-caches with
	// Warming=true and a fresh at (t2 > t1).
	first, err := BuildFromRepo(context.Background(), TraceRepoInput{Root: dir})
	if err != nil {
		t.Fatalf("BuildFromRepo call 1: %v", err)
	}

	// Call 2: entry at (t2) is AFTER warmedAt (t1) → NOT diverted → cache
	// hit. On 0584b60f this diverts again → rebuild → different pointer.
	second, err := BuildFromRepo(context.Background(), TraceRepoInput{Root: dir})
	if err != nil {
		t.Fatalf("BuildFromRepo call 2: %v", err)
	}

	// Pointer identity: a cache hit returns the same *CallGraph; a rebuild
	// produces a fresh one. This is the only reliable discriminator — a
	// rebuild can produce an equal-looking graph.
	if first != second {
		t.Errorf("expected call 2 to be a cache hit (same pointer as call 1); got different pointers — first=%p second=%p (round-8 loop guard: the divert must terminate after one rebuild, not repeat forever)", first, second)
	}
}

// TestBuildFromRepo_EntryCachedAfterWarm_NotDiverted pins the round-8
// invariant in its own right: an entry cached AFTER the warm completed is
// never diverted, even with Warming=true. The "stale" predicate is "cached
// before the warm completed" (entry.at predates warmedAt), not "Warming
// flag is set". An entry re-cached after a failed rebuild carries a fresh
// at — it is not stale, and diverting it would loop forever.
func TestBuildFromRepo_EntryCachedAfterWarm_NotDiverted(t *testing.T) {
	dir := t.TempDir()

	gomod := "module example.com/afterwarm\n\ngo 1.21\n\nthis is not valid go.mod syntax\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	InvalidateBuildCache()
	goTypesWarmingSet.Delete(dir)
	goTypesWarmedSet.Delete(dir)
	t.Cleanup(func() {
		goTypesWarmedSet.Delete(dir)
		goTypesWarmingSet.Delete(dir)
	})

	// Mark the root as warmed FIRST (warmedAt = t_warm).
	goTypesWarmedSet.Store(dir, time.Now())

	key := cgCacheKey(TraceRepoInput{Root: dir})

	// Seed a Warming=true entry AFTER the warm (at = t_entry > t_warm).
	// This is the post-rebuild state: typed resolution failed again, the
	// entry was re-cached with Warming=true and a fresh at.
	seed := &CallGraph{
		Symbols: []*parser.Symbol{{Name: "main", Kind: parser.KindFunction, File: filepath.Join(dir, "main.go")}},
		Tier:    "basic", Backend: BackendTreeSitter, Warming: true,
	}
	cgCache.set(key, seed, dir)

	// BuildFromRepo: cache hit with Warming=true, but entry.at is AFTER
	// warmedAt → not stale → NOT diverted → returned as-is.
	cg, err := BuildFromRepo(context.Background(), TraceRepoInput{Root: dir})
	if err != nil {
		t.Fatalf("BuildFromRepo: %v", err)
	}

	// Pointer identity: the cache hit must return the seeded *CallGraph,
	// not a rebuilt one.
	if cg != seed {
		t.Errorf("expected cache hit (same pointer as seed %p); got %p — entry cached AFTER warm was diverted (round-8 invariant: only entries cached BEFORE the warm are stale)", seed, cg)
	}
	// Warming must still be true — the entry is returned as-is.
	if !cg.Warming {
		t.Error("expected Warming=true preserved (entry cached after warm is not diverted)")
	}
}
