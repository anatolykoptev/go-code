package embeddings

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIndexRepoWithTool_CallsReconcilePaths verifies that the path-existence
// reconciliation hook is called during a full index pass. After #708 round 2
// finding 0, the hook runs AFTER the parse (not at the top of
// indexRepoWithTool) so it can be gated by the same shrink-guard as the orphan
// delete. It does NOT run on the same-SHA fast path — a fast-path skip means
// the repo hasn't changed, so no stale paths can have appeared.
//
// #714: the hook now resolves source_path from code_repo_state (not the
// caller's root), so the test seeds a state row with source_path=dir before
// the index. A pathless key (empty source_path) would take the skip branch
// instead — covered by TestHandleReconcilePaths_PathlessKeyReachedViaEnumeration.
//
// DB-gated: needs a real store to reach the post-parse hook.
//
// Falsifiable: remove the `if p.reconcilePaths != nil && !shrinkGuardFires`
// block from indexRepoWithTool → reconcileCalls stays 0 → assert.Equal(1, ...) fails.
func TestIndexRepoWithTool_CallsReconcilePaths(t *testing.T) {
	p, store := testPipeline(t)
	ctx := context.Background()
	const repo = "test/reconcile-hook-called"
	cleanRepoFull(t, store, repo)

	dir := t.TempDir()
	writeTempGoFile(t, dir, "main.go", []string{"FuncA"})

	// Seed a prior state row with source_path=dir so the hook resolves the
	// root from state (#714). Use a SHA that won't match any real git ref
	// (dir is not a git repo) so the fast path is skipped and the full walk
	// runs, reaching the post-parse hook.
	require.NoError(t, store.SetRepoStateWithPath(ctx, repo, "prior-sha-not-matching", "test-model", dir))

	var reconcileCalls int32
	p.reconcilePaths = func(ctx context.Context, repoKey, sourcePath string, dryRun bool) (*ReconcileResult, error) {
		atomic.AddInt32(&reconcileCalls, 1)
		assert.Equal(t, repo, repoKey, "hook must receive the repoKey")
		assert.Equal(t, dir, sourcePath, "hook must receive the source_path from state")
		assert.False(t, dryRun, "index-pass hook must call with dryRun=false (real delete)")
		return &ReconcileResult{RepoKey: repoKey}, nil
	}

	_, err := p.IndexRepo(ctx, repo, dir)
	require.NoError(t, err)

	assert.Equal(t, int32(1), atomic.LoadInt32(&reconcileCalls),
		"reconcilePaths must be called exactly once during a full index pass — "+
			"if this is 0, the index-pass reconciliation hook was removed or moved "+
			"before the parse where it is unreachable")
}

// TestIndexRepoWithTool_NilReconcilePathsNoPanic verifies that when
// reconcilePaths is nil (e.g. a Pipeline constructed without the default),
// the hook is skipped gracefully and indexing proceeds without panic.
//
// DB-gated: needs a real store to reach the post-parse hook.
func TestIndexRepoWithTool_NilReconcilePathsNoPanic(t *testing.T) {
	p, store := testPipeline(t)
	p.reconcilePaths = nil // explicitly nil — hook must be skipped
	ctx := context.Background()
	const repo = "test/reconcile-nil-hook"
	cleanRepo(t, store, repo)

	dir := t.TempDir()
	writeTempGoFile(t, dir, "main.go", []string{"FuncB"})

	// Must not panic on the nil reconciliation hook.
	_, err := p.IndexRepo(ctx, repo, dir)
	require.NoError(t, err, "nil reconcilePaths must not cause an index failure")
}

// TestIndexRepoWithTool_ReconcileSkippedOnShrinkGuard verifies that when the
// shrink-guard fires (seen < 70% of existing), the path-existence
// reconciliation is SKIPPED — it does not delete the legacy rows the
// shrink-guard exists to protect (#708 round 2 finding 0).
//
// This is the anti-vacuity guard for the shrink-gate on the reconciliation:
// remove the `!shrinkGuardFires(seen, existing)` condition from the
// reconciliation call in indexRepoWithTool and the hook deletes all legacy
// rows before deleteIntraKeyOrphans can fire the guard → this test goes RED
// (reconcileDeleteCalls > 0, legacy rows gone).
//
// DB-gated: needs a real store to run a full IndexRepo.
func TestIndexRepoWithTool_ReconcileSkippedOnShrinkGuard(t *testing.T) {
	p, store := testPipeline(t)
	ctx := context.Background()
	const repo = "test/reconcile-shrink-guard"
	cleanRepo(t, store, repo)

	// Seed 600 rows directly (simulating a prior full index).
	const total = 600
	names := make([]string, total)
	for i := range names {
		names[i] = fmt.Sprintf("Sym_%04d", i)
	}
	insertSymbols(t, store, repo, "legacy_file.go", names)

	// Index against a fresh source with only ~16 symbols (16/600 < 0.7 → guard fires).
	dir := t.TempDir()
	smallNames := make([]string, 16)
	for i := range smallNames {
		smallNames[i] = fmt.Sprintf("NewFunc%02d", i)
	}
	writeTempGoFile(t, dir, "main.go", smallNames)

	var reconcileDeleteCalls int32
	p.reconcilePaths = func(ctx context.Context, repoKey, sourcePath string, dryRun bool) (*ReconcileResult, error) {
		atomic.AddInt32(&reconcileDeleteCalls, 1)
		// Delegate to the real store to actually attempt the delete.
		return store.ReconcileRepoPaths(ctx, repoKey, sourcePath, dryRun)
	}

	_, err := p.IndexRepo(ctx, repo, dir)
	require.NoError(t, err)

	// The reconciliation hook must NOT have been called (shrink guard skipped it).
	assert.Equal(t, int32(0), atomic.LoadInt32(&reconcileDeleteCalls),
		"reconcilePaths must be skipped when shrinkGuardFires — "+
			"if this is 1, the shrink-guard gate was removed from the reconciliation call")

	// Legacy rows must survive.
	legacyRows, err := store.GetSymbolsForFile(ctx, repo, "legacy_file.go")
	require.NoError(t, err)
	assert.Len(t, legacyRows, total,
		"all 600 legacy rows must survive when shrink-guard fires (reconciliation + orphan-delete both skipped)")
}

// -- #714 round 2: three-way root resolution + self-heal backfill --

// TestIndexRepoWithTool_PathlessKeyBackfilledAndStaleDeleted is test 1 from
// the round-2 task: a pathless key (empty source_path) indexed with a
// non-empty root ⇒ the root fallback kicks in, stale rows ARE deleted, AND
// source_path is backfilled to that root so the key stops being pathless.
//
// Asserts BOTH directions: a test that only checks the delete misses the
// backfill and vice versa.
//
// Anti-vacuity: remove the step-2 root fallback (reconcileRoot stays "") ⇒
// ReconcileRepoPaths takes the source_path_empty skip branch, stale rows
// survive, source_path stays "" ⇒ BOTH assertions RED. Restore ⇒ GREEN.
//
// DB-gated: needs a real store + embed server to reach the post-parse hook.
func TestIndexRepoWithTool_PathlessKeyBackfilledAndStaleDeleted(t *testing.T) {
	p, store := testPipeline(t)
	ctx := context.Background()
	const repo = "test/pathless-backfill-selfheal"
	cleanRepoFull(t, store, repo)

	// Seed a pathless key: state row with source_path='', plus stale
	// embeddings under a file_path that does NOT resolve under the index root.
	insertSymbols(t, store, repo, "stale/other_project.go", []string{"StaleA", "StaleB", "StaleC"})
	require.NoError(t, store.SetRepoStateWithPath(ctx, repo, "deadbeef", "test-model", ""))

	// Verify the key IS pathless before the index.
	spBefore, err := store.GetRepoSourcePath(ctx, repo)
	require.NoError(t, err)
	require.Empty(t, spBefore, "setup: key must be pathless before index")

	// Index root: a temp dir with a Go file whose symbols do NOT collide with
	// the stale rows. Non-git dir ⇒ currentSHA="" ⇒ full walk, no fast path.
	// 3 parsed symbols vs 3 existing stale keys ⇒ seen=3, existing=3 ⇒
	// 3 >= 0.7*3=2.1 ⇒ shrink guard does NOT fire ⇒ reconcile hook runs.
	dir := t.TempDir()
	writeTempGoFile(t, dir, "main.go", []string{"RealFunc1", "RealFunc2", "RealFunc3"})

	// Use the real store's ReconcileRepoPaths and BackfillRepoSourcePath so
	// the delete + backfill actually execute against the DB.
	p.reconcilePaths = store.ReconcileRepoPaths
	p.backfillSourcePath = store.BackfillRepoSourcePath

	// Warm the ratio gauge to 0 (simulates boot). The reconcile hook must
	// overwrite it with the real ratio when the root fallback fires.
	SetStalePathRatioGauge(repo, 0.0)

	_, err = p.IndexRepo(ctx, repo, dir)
	require.NoError(t, err, "index must succeed")

	// Direction 1: stale rows ARE deleted. NOTE: deleteIntraKeyOrphans is a
	// second cleanup path that also catches these rows, so this assertion
	// alone is NOT anti-vacuous for the root fallback — the gauge assertion
	// below is the one that fails when the fallback is removed.
	staleRows, err := store.GetSymbolsForFile(ctx, repo, "stale/other_project.go")
	require.NoError(t, err)
	assert.Len(t, staleRows, 0,
		"stale rows under a pathless key MUST be deleted when the root fallback fires — "+
			"if 3, neither the reconcile hook nor deleteIntraKeyOrphans ran")

	// Direction 2: source_path IS backfilled to the index root.
	spAfter, err := store.GetRepoSourcePath(ctx, repo)
	require.NoError(t, err)
	assert.Equal(t, dir, spAfter,
		"source_path MUST be backfilled to the index root so the key stops being pathless — "+
			"if empty, the backfill was removed or the root fallback was removed (backfill is coupled to it)")

	// Direction 3 (anti-vacuity): the ratio gauge MUST be set to a non-zero
	// value by the reconcile hook. Without the root fallback, ReconcileRepoPaths
	// takes the source_path_empty skip branch and the gauge stays at the warmed
	// 0.0 — this is the assertion that goes RED when the fallback is removed,
	// even though deleteIntraKeyOrphans still deletes the stale rows.
	gauge := gaugeValue(t, stalePathRatioGauge.WithLabelValues(repo))
	assert.Greater(t, gauge, 0.0,
		"stale-path ratio gauge MUST be set by the reconcile hook (root fallback) — "+
			"if 0, the root fallback was removed and ReconcileRepoPaths took the skip branch "+
			"(deleteIntraKeyOrphans still deletes the rows, but the gauge is the anti-vacuity signal)")
}

// TestIndexRepoWithTool_NonEmptySourcePathBackfillNotCalled is test 3 from
// the round-2 task: a non-empty state source_path ⇒ behaviour unchanged, and
// the backfill is NOT called (state is not rewritten by the backfill).
//
// The hook resolves the root from state (non-empty ⇒ step 1), the backfill
// guard (sourcePath == "" && root != "") is false, so BackfillRepoSourcePath
// is never invoked. writeRepoState may still write source_path=root on a git
// repo — that is the NORMAL path, not the backfill, and is out of scope here.
//
// Anti-vacuity: if the backfill guard were inverted (e.g. sourcePath != "" ⇒
// backfill), the injected backfill would fire and the assertion REDS.
//
// DB-gated: needs a real store + embed server to reach the post-parse hook.
func TestIndexRepoWithTool_NonEmptySourcePathBackfillNotCalled(t *testing.T) {
	p, store := testPipeline(t)
	ctx := context.Background()
	const repo = "test/nonempty-sourcepath-no-backfill"
	cleanRepoFull(t, store, repo)

	// Seed a NON-pathless key: state row with source_path=originalDir.
	originalDir := t.TempDir()
	writeTempGoFile(t, originalDir, "main.go", []string{"OrigFunc"})
	require.NoError(t, store.SetRepoStateWithPath(ctx, repo, "prior-sha-not-matching", "test-model", originalDir))

	// Verify the key is NOT pathless before the index.
	spBefore, err := store.GetRepoSourcePath(ctx, repo)
	require.NoError(t, err)
	require.Equal(t, originalDir, spBefore, "setup: key must have a non-empty source_path")

	// Inject a backfill that FAILS the test if called — the guard must skip it.
	var backfillCalls int32
	p.backfillSourcePath = func(ctx context.Context, repoKey, sourcePath string) error {
		atomic.AddInt32(&backfillCalls, 1)
		t.Fatalf("backfillSourcePath must NOT be called when state source_path is non-empty, got repo=%s path=%s", repoKey, sourcePath)
		return nil
	}

	// Capture the sourcePath the reconcile hook received — must be from state.
	var gotSourcePath atomic.Value
	p.reconcilePaths = func(ctx context.Context, repoKey, sourcePath string, dryRun bool) (*ReconcileResult, error) {
		gotSourcePath.Store(sourcePath)
		assert.False(t, dryRun, "index-pass hook must call with dryRun=false")
		return &ReconcileResult{RepoKey: repoKey}, nil
	}

	// Index with a DIFFERENT root than originalDir — the hook must still use
	// the state's source_path (step 1), not the caller's root.
	indexDir := t.TempDir()
	writeTempGoFile(t, indexDir, "main.go", []string{"NewFunc"})

	_, err = p.IndexRepo(ctx, repo, indexDir)
	require.NoError(t, err, "index must succeed")

	// The reconcile hook must have received the STATE's source_path, not the
	// caller's root.
	if sp, ok := gotSourcePath.Load().(string); ok {
		assert.Equal(t, originalDir, sp,
			"hook must use the state's source_path (step 1), not the caller's root — "+
				"if this is indexDir, the root resolution was inverted")
	}

	// The backfill must NOT have been called.
	assert.Equal(t, int32(0), atomic.LoadInt32(&backfillCalls),
		"backfillSourcePath must NOT be called when state source_path is non-empty")
}
