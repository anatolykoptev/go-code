package embeddings

// Fast-path dry-run reconciliation tests (#711 + #714).
//
// The same-SHA fast path skips the full parse+embed cycle when the repo's
// head has not moved. Before this change, path-existence reconciliation sat
// AFTER the parse, gated by the shrink guard, so it was never reached on the
// fast path. A dormant contaminated repo (SHA unchanged, stale paths present)
// stayed contaminated and its gauge read 0 (warmed at boot, never refreshed).
//
// The fix runs a DRY-RUN reconciliation on the fast path using the
// source_path recorded in code_repo_state. Dry-run deletes nothing but
// refreshes gocode_index_stale_path_ratio so a dormant contaminated repo
// becomes alertable.
//
// RED guarantee (anti-vacuous):
//   - Remove the fast-path dry-run call from checkSameSHAFastPath ⇒
//     reconcileCalls stays 0 and the gauge stays at the warmed 0.0 ⇒ both
//     assertions RED.
//   - Change the dry-run to dryRun=false (real delete) ⇒ staleRowsSurvive
//     drops to 0 ⇒ the "0 rows deleted" assertion RED.

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFastPath_DryRunReconcile_UpdatesGaugeAndDeletesNothing is test 1 from
// the task: same-SHA fast path with stale paths present ⇒ gauge updated to
// the correct non-zero ratio AND zero rows deleted.
//
// Setup: index a real git repo (populates embeddings + state row with
// source_path), then inject stale rows under a file_path that does not
// resolve under the repo root. The second IndexRepo call takes the fast
// path (same SHA). The dry-run reconciliation must:
//   - be called with dryRun=true (NOT false — a dry-run that deletes passes
//     a gauge-only test),
//   - set the gauge to the correct non-zero ratio,
//   - delete ZERO rows (the stale rows survive).
//
// DB-gated: needs a real store + embed server to reach the fast path.
func TestFastPath_DryRunReconcile_UpdatesGaugeAndDeletesNothing(t *testing.T) {
	p, store := testPipeline(t)
	ctx := context.Background()
	const repo = "test/fastpath-dryrun-reconcile"
	cleanRepoFull(t, store, repo)

	// Real git repo with one Go file — needed so repoMainBranchSHA returns a
	// non-empty SHA and the same-SHA fast path is reachable.
	root := initGitRepo(t, map[string]string{
		"main.go": goFile("FastPathSym"),
	})

	// First index: populates embeddings and writes state row with source_path=root.
	_, err := p.IndexRepo(ctx, repo, root)
	require.NoError(t, err, "first index must succeed")

	countFirst, _ := store.CountEmbeddings(ctx, repo)
	require.Greater(t, countFirst, 0, "setup: first index must write rows")

	// Inject stale rows under a file_path that does NOT resolve under root.
	// These represent the #708 rename-collision contamination class.
	insertSymbols(t, store, repo, "stale/other_project.go", []string{"StaleA", "StaleB", "StaleC"})

	// Warm the gauge to 0.0 — simulates the boot WarmStalePathRatioGauge call
	// that resets every repo to 0 after a restart. The fast-path dry-run must
	// overwrite this 0 with the real ratio.
	SetStalePathRatioGauge(repo, 0.0)

	var (
		reconcileCalls int32
		gotDryRun      atomic.Bool
		gotSourcePath  atomic.Value
	)
	p.reconcilePaths = func(ctx context.Context, repoKey, sourcePath string, dryRun bool) (*ReconcileResult, error) {
		atomic.AddInt32(&reconcileCalls, 1)
		gotDryRun.Store(dryRun)
		gotSourcePath.Store(sourcePath)
		// Delegate to the real store so the gauge is actually set.
		return store.ReconcileRepoPaths(ctx, repoKey, sourcePath, dryRun)
	}

	// Second call: same SHA + populated embeddings ⇒ fast path.
	// The fast-path dry-run reconciliation must fire here.
	result, err := p.IndexRepo(ctx, repo, root)
	require.NoError(t, err, "fast-path index must succeed")
	assert.Equal(t, 0, result.Indexed, "fast path must not re-embed")

	// The dry-run reconciliation MUST have been called.
	assert.Equal(t, int32(1), atomic.LoadInt32(&reconcileCalls),
		"fast-path dry-run reconciliation must be called exactly once — "+
			"if 0, the fast-path dry-run hook was not added to checkSameSHAFastPath")

	// It MUST be a dry-run (dryRun=true), NOT a real delete.
	assert.True(t, gotDryRun.Load(),
		"fast-path reconciliation must be dry-run (dryRun=true) — "+
			"a dry-run that deletes passes a gauge-only test")

	// It MUST use the source_path from state, not some other root.
	if sp, ok := gotSourcePath.Load().(string); ok {
		assert.Equal(t, root, sp,
			"fast-path dry-run must use source_path from code_repo_state")
	}

	// The gauge MUST reflect the real ratio (3 stale rows / total).
	// total = countFirst + 3 stale rows. ratio = 3 / (countFirst + 3).
	totalRows := countFirst + 3
	expectedRatio := float64(3) / float64(totalRows)
	gauge := gaugeValue(t, stalePathRatioGauge.WithLabelValues(repo))
	assert.InDelta(t, expectedRatio, gauge, 0.001,
		"gauge must reflect the real stale-path ratio (%.4f), not the warmed 0.0", expectedRatio)

	// ZERO rows deleted — the stale rows survive (dry-run).
	staleRows, err := store.GetSymbolsForFile(ctx, repo, "stale/other_project.go")
	require.NoError(t, err)
	assert.Len(t, staleRows, 3,
		"fast-path dry-run must NOT delete stale rows — if 0, dryRun was false (real delete)")
}

// TestFastPath_DryRunReconcile_RestartSemantics is test 4: after a warm-up
// followed by a fast-path pass, the ratio reflects reality rather than the
// warmed 0.
//
// This is the core #711 restart-semantics assertion: WarmStalePathRatioGauge
// sets every repo to 0 at boot, and without the fast-path dry-run nothing
// repopulates it. The fix: the fast-path dry-run overwrites the warmed 0
// with the real ratio.
//
// DB-gated: needs a real store + embed server.
func TestFastPath_DryRunReconcile_RestartSemantics(t *testing.T) {
	p, store := testPipeline(t)
	ctx := context.Background()
	const repo = "test/fastpath-restart-semantics"
	cleanRepoFull(t, store, repo)

	root := initGitRepo(t, map[string]string{
		"main.go": goFile("RestartSym"),
	})

	// Warm-up: first index populates embeddings + state.
	_, err := p.IndexRepo(ctx, repo, root)
	require.NoError(t, err)

	// Inject stale rows (contamination present alongside a matching SHA).
	insertSymbols(t, store, repo, "ghost/renamed.go", []string{"Ghost1", "Ghost2"})

	// Simulate a restart: warm the gauge to 0 for all known repos.
	// In production, WarmStalePathRatioGauge is called at boot.
	SetStalePathRatioGauge(repo, 0.0)
	require.Equal(t, 0.0, gaugeValue(t, stalePathRatioGauge.WithLabelValues(repo)),
		"setup: gauge must read 0 after warm-up (simulating restart)")

	// Fast-path pass: same SHA, embeddings present.
	// The dry-run must overwrite the warmed 0 with the real ratio.
	p.reconcilePaths = store.ReconcileRepoPaths // real store, real gauge write

	_, err = p.IndexRepo(ctx, repo, root)
	require.NoError(t, err)

	gauge := gaugeValue(t, stalePathRatioGauge.WithLabelValues(repo))
	assert.Greater(t, gauge, 0.0,
		"after a fast-path pass, the gauge must reflect reality (>0), not the warmed 0 — "+
			"if 0, the fast-path dry-run did not refresh the gauge (#711 restart semantics)")
}

// TestFastPath_DryRunReconcile_Latency measures the added fast-path latency
// for the dry-run reconciliation. The fast path exists to make an unchanged
// repo free; the dry-run adds one GROUP BY + one os.Stat per distinct path.
// This test logs the duration so the report can cite a measured number.
//
// It does NOT assert a hard threshold — the task says "if material, propose
// a cheaper shape." The measurement is the deliverable.
//
// DB-gated: needs a real store + embed server.
func TestFastPath_DryRunReconcile_Latency(t *testing.T) {
	p, store := testPipeline(t)
	ctx := context.Background()
	const repo = "test/fastpath-dryrun-latency"
	cleanRepoFull(t, store, repo)

	root := initGitRepo(t, map[string]string{
		"main.go": goFile("LatencySym"),
	})

	// First index.
	_, err := p.IndexRepo(ctx, repo, root)
	require.NoError(t, err)

	// Inject a realistic number of stale paths (~50) to simulate a
	// moderately-sized repo's dry-run cost. The largest live repo (vaelor)
	// has ~1000 distinct paths; this test uses 50 to keep the test fast
	// while still measuring the per-path stat cost.
	for i := 0; i < 50; i++ {
		insertSymbols(t, store, repo, "stale/dir/file_"+padNum(i)+".go", []string{"S" + padNum(i)})
	}

	p.reconcilePaths = store.ReconcileRepoPaths

	// Measure the fast-path pass (which includes the dry-run).
	start := time.Now()
	_, err = p.IndexRepo(ctx, repo, root)
	elapsed := time.Since(start)
	require.NoError(t, err)

	t.Logf("fast-path dry-run latency for 50 stale paths: %v (GROUP BY + 50 os.Stat calls)", elapsed)
	// No hard assert — the measurement is reported in the test output.
}

func padNum(n int) string {
	const width = 4
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	for len(s) < width {
		s = "0" + s
	}
	return s
}
