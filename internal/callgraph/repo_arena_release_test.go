package callgraph

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/parser"
)

// TestEnrichWithTypedResolution_GoTypesArenaReleasedAfterRequest is the
// release-proof acceptance test for issue #747: the go/types arena loaded by
// EnrichWithTypedResolution (a full NeedDeps *packages.Package set carrying
// .Types/.TypesInfo/.Syntax) must be collectable once the request returns.
//
// A value shared between the two sequential steps of one request (CALLS via
// tryGoTypesResolution, IMPLEMENTS via ExtractGoImplements) is a parameter,
// not process-global state. If it is pinned in a process-global cache
// (packagesLoadCache, the pre-fix retainer), runtime.GC cannot reclaim it and
// the heap stays inflated by the arena size after the request ends — the
// OOM-kill root cause on the 3 GiB indexer container (#747: ten OOM-kills in
// two days, one load ~2 GB, cache size 8).
//
// The test builds a real Go module's graph through the production seam
// (EnrichWithTypedResolution, NOT BuildFromRepo — BuildFromRepo additionally
// pins the *CallGraph in cgCache, which would conflate two retainers), drops
// every caller-side reference, runs runtime.GC(), and asserts the heap returns
// near its pre-build level. RED on main (the cache pins the arena); GREEN
// after the cache is removed and the *LoadResult is passed through the seam.
//
// The fixture is deliberately sizable (40 files, ~120 types, ~120 cross-type
// calls) so the go/types arena is large enough to breach the tolerance
// unambiguously on main — a tiny one-file fixture's arena is smaller than Go
// runtime heap noise and would false-green on main, measuring nothing.
func TestEnrichWithTypedResolution_GoTypesArenaReleasedAfterRequest(t *testing.T) {
	dir := buildArenaFixture(t)

	// Baseline: GC twice to settle the heap, then read.
	runtime.GC()
	runtime.GC()
	var base runtime.MemStats
	runtime.ReadMemStats(&base)

	// Build + enrich through the production seam, discarding the result. The
	// inner func scope ensures no caller-side reference to the returned
	// *CallGraph escapes; the only thing that can retain the arena after this
	// block is a process-global cache.
	func() {
		baseCG := &CallGraph{
			Symbols: []*parser.Symbol{{Name: "main", Kind: parser.KindFunction, File: filepath.Join(dir, "main.go")}},
			Tier:    "basic",
			Backend: BackendTreeSitter,
		}
		_ = EnrichWithTypedResolution(context.Background(), dir, baseCG, baseCG.Symbols, nil)
	}()

	// Force collection of everything not pinned by a global.
	runtime.GC()
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	retained := int64(after.HeapInuse) - int64(base.HeapInuse)
	// 2 MiB tolerance: absorbs Go runtime heap noise and the small
	// callgraph/ConvertToCallGraph allocations that may survive in stray
	// per-goroutine mcache, but is well under the ~5.6 MiB go/types arena a
	// 150-file NeedDeps load pins (measured on main). On main the cache holds
	// the *LoadResult and retained ~= arena size (RED); after the fix nothing
	// pins it and retained ~= 0 (GREEN).
	const tolerance = 2 << 20
	t.Logf("MemStats baseline HeapInuse=%d, after HeapInuse=%d, retained=%d bytes (tolerance=%d)",
		base.HeapInuse, after.HeapInuse, retained, tolerance)
	if retained > tolerance {
		t.Errorf("go/types arena not released after request: heap retained %d bytes above baseline after runtime.GC() "+
			"(baseline=%d, after=%d); a process-global cache is pinning the *LoadResult (issue #747)",
			retained, base.HeapInuse, after.HeapInuse)
	}
}

// buildArenaFixture writes a Go module of 40 files under dir, each declaring
// an interface, a struct satisfying it, and a function that calls through the
// interface — enough typed surface (interfaces, methods, satisfaction, call
// edges) to produce a go/types NeedDeps arena well above the test's tolerance.
func buildArenaFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/arena\n\ngo 1.22\n"), 0o600); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	mainSrc := "package main\n\nfunc main() {\n"
	for i := 0; i < 150; i++ {
		fn := filepath.Join(dir, fmt.Sprintf("iface%d.go", i))
		src := fmt.Sprintf(`package main

type Iface%d interface {
	Do%d() int
	Do%dEx() int
	Do%dEx2() int
	Do%dEx3() int
	Do%dEx4() int
}

type Impl%d struct{}

func (Impl%d) Do%d() int { return %d }
func (Impl%d) Do%dEx() int { return %d }
func (Impl%d) Do%dEx2() int { return %d }
func (Impl%d) Do%dEx3() int { return %d }
func (Impl%d) Do%dEx4() int { return %d }

func use%d(g Iface%d) int { return g.Do%d() + g.Do%dEx() + g.Do%dEx2() + g.Do%dEx3() + g.Do%dEx4() }
`, i, i, i, i, i, i, i, i, i, i, i, i, i, i, i, i, i, i, i, i, i, i, i, i, i, i, i, i, i)
		if err := os.WriteFile(fn, []byte(src), 0o600); err != nil {
			t.Fatalf("write iface%d.go: %v", i, err)
		}
		mainSrc += fmt.Sprintf("\tuse%d(Impl%d{})\n", i, i)
	}
	mainSrc += "}\n"
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainSrc), 0o600); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	return dir
}
