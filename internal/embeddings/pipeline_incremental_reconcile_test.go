package embeddings

// IncrementalSync same-SHA fast-path reconciliation tests (#720).
//
// The boot autoindex calls IncrementalSync, not IndexRepo. On v1.59.12,
// 72 of 77 repos took mode=skip-sha-match (the same-SHA fast path in
// IncrementalSync), but that path never invoked the dry-run path
// reconciliation — the hook was added only to checkSameSHAFastPath
// (reached via IndexRepo, which the autoindex almost never takes). So
// the reconcile never fired for those 72 repos, the stale-path gauge
// stayed at the warmed 0, and pathless keys (empty source_path) never
// got their self-heal backfill.
//
// The fix routes both same-SHA paths through ONE shared seam
// (runFastPathDryRunReconcile) so the reconcile + backfill cannot be
// added to one path and missed on the other.
//
// RED guarantee (anti-vacuous):
//   - Remove the runFastPathDryRunReconcile call from IncrementalSync's
//     same-SHA branch ⇒ reconcileCalls stays 0 ⇒ test 1 RED.
//   - Change the dry-run to dryRun=false ⇒ stale rows drop to 0 ⇒ test 2 RED.
//   - Remove the backfill from the shared seam ⇒ backfillCalls stays 0 ⇒
//     test 3 RED.

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIncrementalSync_SameSHA_FastPathReconcileFires is test 1: drive
// IncrementalSync on a git repo whose SHA is unchanged and which has ≥1
// embedding row, and assert the dry-run reconciliation ran.
//
// This is the core #720 assertion: the boot autoindex path (IncrementalSync)
// MUST invoke the same dry-run reconciliation that checkSameSHAFastPath
// (IndexRepo) already invokes. Before the fix, reconcileCalls stays 0.
//
// DB-gated: needs a real store + embed server to reach the same-SHA fast path.
func TestIncrementalSync_SameSHA_FastPathReconcileFires(t *testing.T) {
	p, store := testPipeline(t)
	ctx := context.Background()
	const repo = "test/inc-samesha-reconcile-fires"
	cleanRepoFull(t, store, repo)

	root := initGitRepo(t, map[string]string{
		"main.go": goFile("ReconcileSym"),
	})

	// Bootstrap via IncrementalSync — the boot autoindex path.
	_, err := p.IncrementalSync(ctx, repo, root)
	require.NoError(t, err, "bootstrap must succeed")

	countFirst, _ := store.CountEmbeddings(ctx, repo)
	require.Greater(t, countFirst, 0, "setup: bootstrap must write rows")

	// Inject stale rows under a file_path that does NOT resolve under root.
	insertSymbols(t, store, repo, "stale/other_project.go", []string{"StaleA"})

	// Warm the gauge to 0 — simulates the boot WarmStalePathRatioGauge call.
	SetStalePathRatioGauge(repo, 0.0)

	var (
		reconcileCalls int32
		gotDryRun      atomic.Bool
	)
	p.reconcilePaths = func(ctx context.Context, repoKey, sourcePath string, dryRun bool) (*ReconcileResult, error) {
		atomic.AddInt32(&reconcileCalls, 1)
		gotDryRun.Store(dryRun)
		// Delegate to the real store so the gauge is actually set.
		return store.ReconcileRepoPaths(ctx, repoKey, sourcePath, dryRun)
	}

	// Second call: same SHA + populated embeddings ⇒ skip-sha-match fast path.
	// The dry-run reconciliation MUST fire here (it didn't before #720).
	result, err := p.IncrementalSync(ctx, repo, root)
	require.NoError(t, err, "fast-path IncrementalSync must succeed")
	assert.Equal(t, IncrementalSyncSkipSHAMatch, result.Mode,
		"second call with unchanged SHA must take the skip-sha-match fast path")

	assert.Equal(t, int32(1), atomic.LoadInt32(&reconcileCalls),
		"IncrementalSync same-SHA fast path MUST call reconcilePaths exactly once — "+
			"if 0, the dry-run reconcile hook was not added to IncrementalSync (#720: "+
			"the hook was on checkSameSHAFastPath/IndexRepo only, which the boot "+
			"autoindex never reaches)")

	assert.True(t, gotDryRun.Load(),
		"fast-path reconciliation must be dry-run (dryRun=true) — "+
			"a dry-run that deletes passes a gauge-only test")
}

// TestIncrementalSync_SameSHA_FastPathReconcileDryRunPreservesRows is test 2:
// seed a file_path row that does not exist on disk, assert the row count is
// UNCHANGED after IncrementalSync (proves dry-run — DeleteRowsByFilePaths is
// unreachable from the IncrementalSync same-SHA fast path; the IndexRepo
// same-SHA fast path is covered separately by
// TestFastPath_DryRunReconcile_UpdatesGaugeAndDeletesNothing in
// reconcile_paths_fastpath_test.go).
//
// DB-gated: needs a real store + embed server.
func TestIncrementalSync_SameSHA_FastPathReconcileDryRunPreservesRows(t *testing.T) {
	p, store := testPipeline(t)
	ctx := context.Background()
	const repo = "test/inc-samesha-dryrun-preserve"
	cleanRepoFull(t, store, repo)

	root := initGitRepo(t, map[string]string{
		"main.go": goFile("PreserveSym"),
	})

	_, err := p.IncrementalSync(ctx, repo, root)
	require.NoError(t, err, "bootstrap must succeed")

	// Seed a file_path row that does NOT exist on disk.
	insertSymbols(t, store, repo, "ghost/nonexistent.go", []string{"Ghost1", "Ghost2"})

	preRows, err := store.GetSymbolsForFile(ctx, repo, "ghost/nonexistent.go")
	require.NoError(t, err)
	require.Len(t, preRows, 2, "setup: stale rows must exist before sync")

	// Use the real store's ReconcileRepoPaths so the dry-run actually runs
	// against the DB (not a mock that always preserves rows).
	p.reconcilePaths = store.ReconcileRepoPaths
	p.backfillSourcePath = store.BackfillRepoSourcePath

	// Same-SHA fast path.
	result, err := p.IncrementalSync(ctx, repo, root)
	require.NoError(t, err)
	assert.Equal(t, IncrementalSyncSkipSHAMatch, result.Mode)

	// Row count MUST be unchanged — dry-run deletes nothing.
	postRows, err := store.GetSymbolsForFile(ctx, repo, "ghost/nonexistent.go")
	require.NoError(t, err)
	assert.Len(t, postRows, 2,
		"same-SHA fast-path dry-run must NOT delete stale rows — "+
			"if 0, dryRun was false (real delete): DeleteRowsByFilePaths must be "+
			"unreachable from the IncrementalSync same-SHA fast path (the IndexRepo "+
			"path is guarded by TestFastPath_DryRunReconcile_UpdatesGaugeAndDeletesNothing)")
}

// TestIncrementalSync_SameSHA_FastPathBackfillPathlessKey is test 3: for a
// key whose code_repo_state.source_path is empty, assert the backfill fired.
// 72 of 77 repos on v1.59.12 can ONLY reach the backfill through this path
// (the full index path is unreachable when the SHA matches), so it needs its
// own assertion.
//
// DB-gated: needs a real store + embed server.
func TestIncrementalSync_SameSHA_FastPathBackfillPathlessKey(t *testing.T) {
	p, store := testPipeline(t)
	ctx := context.Background()
	const repo = "test/inc-samesha-backfill-pathless"
	cleanRepoFull(t, store, repo)

	root := initGitRepo(t, map[string]string{
		"main.go": goFile("BackfillSym"),
	})

	// Bootstrap via IncrementalSync — writes state with source_path=root.
	_, err := p.IncrementalSync(ctx, repo, root)
	require.NoError(t, err, "bootstrap must succeed")

	countFirst, _ := store.CountEmbeddings(ctx, repo)
	require.Greater(t, countFirst, 0, "setup: bootstrap must write rows")

	// Clobber source_path to "" — simulate a pathless key (45/131 on prod).
	_, err = store.pool.Exec(ctx,
		`UPDATE public.code_repo_state SET source_path = '' WHERE repo_key = $1`, repo)
	require.NoError(t, err)

	spBefore, _ := store.GetRepoSourcePath(ctx, repo)
	require.Empty(t, spBefore, "setup: key must be pathless before sync")

	// Use real reconcile + hook backfill to assert it fires.
	var (
		backfillCalls   int32
		gotBackfillPath atomic.Value
	)
	p.reconcilePaths = store.ReconcileRepoPaths
	p.backfillSourcePath = func(ctx context.Context, repoKey, sourcePath string) error {
		atomic.AddInt32(&backfillCalls, 1)
		gotBackfillPath.Store(sourcePath)
		return store.BackfillRepoSourcePath(ctx, repoKey, sourcePath)
	}

	// Same-SHA fast path — the ONLY way a pathless key with unchanged SHA
	// can reach the backfill (72/77 repos on v1.59.12, #720).
	result, err := p.IncrementalSync(ctx, repo, root)
	require.NoError(t, err)
	assert.Equal(t, IncrementalSyncSkipSHAMatch, result.Mode)

	assert.GreaterOrEqual(t, atomic.LoadInt32(&backfillCalls), int32(1),
		"same-SHA fast path MUST backfill source_path for a pathless key — "+
			"72/77 repos can only reach the backfill through this path (#720)")

	if sp, ok := gotBackfillPath.Load().(string); ok {
		assert.Equal(t, root, sp,
			"backfill must write the caller's root as source_path")
	}

	// Verify the DB was actually updated — the key stops being pathless.
	spAfter, _ := store.GetRepoSourcePath(ctx, repo)
	assert.Equal(t, root, spAfter,
		"source_path must be backfilled in the DB so the key stops being pathless")
}
