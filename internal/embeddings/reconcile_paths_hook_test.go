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
