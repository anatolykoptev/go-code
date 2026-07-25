package embeddings

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// -- Pure logic: CheckStalePaths (no DB needed) --

// TestCheckStalePaths_PresentSurviveAbsentStale is the primary two-sided
// guard: a root containing only SOME of the indexed paths ⇒ the absent ones
// are stale, the present ones are NOT. A reconciler that deletes everything
// passes a one-sided test; this asserts both directions.
//
// Falsifiable: if CheckStalePaths marks a present file as stale, the
// assert.NotContains for "present.go" fails. If it marks an absent file as
// clean, the assert.Contains for "gone.go" fails.
func TestCheckStalePaths_PresentSurviveAbsentStale(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "present.go"), []byte("x"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "sub", "deep.go"), []byte("x"), 0o644))

	counts := []PathCount{
		{Path: "present.go", Count: 3},
		{Path: "sub/deep.go", Count: 1},
		{Path: "gone.go", Count: 5},
		{Path: "pkg/agent/main.go", Count: 2},
	}

	stale, staleRows, ok := CheckStalePaths(root, counts)
	require.True(t, ok, "root exists ⇒ ok must be true")

	var stalePaths []string
	for _, s := range stale {
		stalePaths = append(stalePaths, s.Path)
	}
	sort.Strings(stalePaths)
	assert.Equal(t, []string{"gone.go", "pkg/agent/main.go"}, stalePaths, "only absent paths are stale")
	assert.Equal(t, int64(7), staleRows, "staleRows = 5 + 2")
}

// TestCheckStalePaths_RootMissingGuardIsTheDataLossGuard is the MOST IMPORTANT
// test in this file: when the root itself does not exist, CheckStalePaths MUST
// return ok=false so the caller deletes NOTHING. A mount blip must not wipe a
// good index.
//
// Falsifiable (anti-vacuous): remove the root-existence guard from
// CheckStalePaths (so it proceeds to stat individual files under a nonexistent
// root — every file will be "stale") and ok becomes true with all paths stale
// ⇒ this test goes RED. This is the exact data-loss scenario the guard
// prevents.
func TestCheckStalePaths_RootMissingGuardIsTheDataLossGuard(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.RemoveAll(root), "remove the temp dir so the root does not exist")

	counts := []PathCount{
		{Path: "a.go", Count: 10},
		{Path: "b.go", Count: 20},
	}

	stale, staleRows, ok := CheckStalePaths(root, counts)
	assert.False(t, ok, "root missing ⇒ ok MUST be false (data-loss guard)")
	assert.Nil(t, stale, "root missing ⇒ no stale paths returned")
	assert.Equal(t, int64(0), staleRows, "root missing ⇒ zero stale rows")
}

// TestCheckStalePaths_EmptyCountsNoOp verifies that an empty path list returns
// no stale paths and ok=true (root exists, just nothing to check).
func TestCheckStalePaths_EmptyCountsNoOp(t *testing.T) {
	root := t.TempDir()
	stale, staleRows, ok := CheckStalePaths(root, nil)
	require.True(t, ok)
	assert.Nil(t, stale)
	assert.Equal(t, int64(0), staleRows)
}

// TestCheckStalePaths_DirectoryExistsCountsAsPresent verifies that a
// file_path that resolves to a directory (not a regular file) is still
// considered present — os.Stat succeeds on directories. This covers the edge
// where a file_path in the DB happens to be a directory path.
func TestCheckStalePaths_DirectoryExistsCountsAsPresent(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "pkg"), 0o755))

	counts := []PathCount{
		{Path: "pkg", Count: 1},
		{Path: "missing.go", Count: 2},
	}
	stale, staleRows, ok := CheckStalePaths(root, counts)
	require.True(t, ok)
	require.Len(t, stale, 1)
	assert.Equal(t, "missing.go", stale[0].Path)
	assert.Equal(t, int64(2), staleRows)
}

// -- DB-backed integration tests (skipped without PR_TEST_DATABASE_URL) --

// TestReconcileRepoPaths_DeletesStaleKeepsPresent is the DB-backed two-sided
// guard (test 1 from the issue): seed rows under a repo_key with file_paths
// some of which exist under a temp root and some don't; run reconciliation;
// assert the absent ones are deleted and the present ones survive.
//
// DB-gated: skips without PR_TEST_DATABASE_URL.
func TestReconcileRepoPaths_DeletesStaleKeepsPresent(t *testing.T) {
	s := testStore(t)
	const repo = "test/reconcile-stale-keeps-present"
	cleanRepo(t, s, repo)
	ctx := context.Background()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "keep.go"), []byte("x"), 0o644))

	// Seed: keep.go exists, gone.go does not.
	insertSymbols(t, s, repo, "keep.go", []string{"A", "B"})
	insertSymbols(t, s, repo, "gone.go", []string{"C", "D", "E"})

	// Record the source_path so ReconcileRepoPaths can find the root.
	require.NoError(t, s.SetRepoStateWithPath(ctx, repo, "deadbeef", "test-model", root))

	res, err := s.ReconcileRepoPaths(ctx, repo, root, false)
	require.NoError(t, err)
	assert.False(t, res.Skipped, "root exists ⇒ not skipped")
	assert.Equal(t, int64(5), res.TotalRows, "2 + 3 = 5 total rows")
	assert.Equal(t, int64(3), res.StaleRows, "gone.go has 3 rows")
	assert.Equal(t, int64(3), res.Deleted, "3 rows deleted")

	// Present rows survive.
	keepRows, err := s.GetSymbolsForFile(ctx, repo, "keep.go")
	require.NoError(t, err)
	assert.Len(t, keepRows, 2, "keep.go rows must survive")

	// Absent rows gone.
	goneRows, err := s.GetSymbolsForFile(ctx, repo, "gone.go")
	require.NoError(t, err)
	assert.Len(t, goneRows, 0, "gone.go rows must be deleted")
}

// TestReconcileRepoPaths_RootMissingDeletesNothing is the DB-backed data-loss
// guard (test 2): if the root does not exist, ZERO rows are deleted even
// though stale paths exist.
//
// Falsifiable (anti-vacuous): remove the root-missing guard from
// ReconcileRepoPaths and every row becomes "stale" (the join path doesn't
// exist) ⇒ Deleted > 0 ⇒ this test goes RED.
//
// DB-gated: skips without PR_TEST_DATABASE_URL.
func TestReconcileRepoPaths_RootMissingDeletesNothing(t *testing.T) {
	s := testStore(t)
	const repo = "test/reconcile-root-missing"
	cleanRepo(t, s, repo)
	ctx := context.Background()

	root := t.TempDir()
	require.NoError(t, os.RemoveAll(root), "root does not exist")

	insertSymbols(t, s, repo, "a.go", []string{"A", "B"})

	res, err := s.ReconcileRepoPaths(ctx, repo, root, false)
	require.NoError(t, err)
	assert.True(t, res.Skipped, "root missing ⇒ Skipped must be true")
	assert.Equal(t, "root_missing", res.SkipReason)
	assert.Equal(t, int64(0), res.Deleted, "root missing ⇒ ZERO deletions (data-loss guard)")

	// Rows survive.
	rows, err := s.GetSymbolsForFile(ctx, repo, "a.go")
	require.NoError(t, err)
	assert.Len(t, rows, 2, "rows must survive when root is missing")
}

// TestReconcileRepoPaths_EmptySourcePathSkipped is test 3: a key whose
// source_path is empty ⇒ skipped, nothing deleted, skip is logged (verified
// via SkipReason).
//
// DB-gated: skips without PR_TEST_DATABASE_URL.
func TestReconcileRepoPaths_EmptySourcePathSkipped(t *testing.T) {
	s := testStore(t)
	const repo = "test/reconcile-empty-source-path"
	cleanRepo(t, s, repo)
	ctx := context.Background()

	insertSymbols(t, s, repo, "a.go", []string{"A"})

	res, err := s.ReconcileRepoPaths(ctx, repo, "", false)
	require.NoError(t, err)
	assert.True(t, res.Skipped, "empty source_path ⇒ Skipped must be true")
	assert.Equal(t, "source_path_empty", res.SkipReason)
	assert.Equal(t, int64(0), res.Deleted, "empty source_path ⇒ nothing deleted")

	rows, err := s.GetSymbolsForFile(ctx, repo, "a.go")
	require.NoError(t, err)
	assert.Len(t, rows, 1, "rows must survive when source_path is empty")
}

// TestReconcileRepoPaths_IdempotentSecondRunDeletes0 is test 4: after a
// reconciliation, a second run deletes 0 (the stale paths are gone).
//
// DB-gated: skips without PR_TEST_DATABASE_URL.
func TestReconcileRepoPaths_IdempotentSecondRunDeletes0(t *testing.T) {
	s := testStore(t)
	const repo = "test/reconcile-idempotent"
	cleanRepo(t, s, repo)
	ctx := context.Background()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "keep.go"), []byte("x"), 0o644))

	insertSymbols(t, s, repo, "keep.go", []string{"A"})
	insertSymbols(t, s, repo, "gone.go", []string{"B"})

	res1, err := s.ReconcileRepoPaths(ctx, repo, root, false)
	require.NoError(t, err)
	assert.Equal(t, int64(1), res1.Deleted, "first run deletes gone.go's 1 row")

	res2, err := s.ReconcileRepoPaths(ctx, repo, root, false)
	require.NoError(t, err)
	assert.Equal(t, int64(0), res2.Deleted, "second run deletes 0 (idempotent)")
	assert.Equal(t, int64(0), res2.StaleRows, "second run reports 0 stale rows")
}

// TestReconcileRepoPaths_DryRunMutatesNothing is test 6 (MCP tool side): a
// dry-run reports the count it would delete but does NOT delete.
//
// DB-gated: skips without PR_TEST_DATABASE_URL.
func TestReconcileRepoPaths_DryRunMutatesNothing(t *testing.T) {
	s := testStore(t)
	const repo = "test/reconcile-dry-run"
	cleanRepo(t, s, repo)
	ctx := context.Background()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "keep.go"), []byte("x"), 0o644))

	insertSymbols(t, s, repo, "keep.go", []string{"A"})
	insertSymbols(t, s, repo, "gone.go", []string{"B", "C"})

	res, err := s.ReconcileRepoPaths(ctx, repo, root, true)
	require.NoError(t, err)
	assert.True(t, res.DryRun)
	assert.Equal(t, int64(2), res.StaleRows, "dry-run reports 2 stale rows")
	assert.Equal(t, int64(0), res.Deleted, "dry-run deletes nothing")

	// Rows survive.
	goneRows, err := s.GetSymbolsForFile(ctx, repo, "gone.go")
	require.NoError(t, err)
	assert.Len(t, goneRows, 2, "dry-run must not delete gone.go rows")
}

// TestReconcileRepoPaths_GaugeSetCorrectly is test 5: the gauge reports the
// correct fraction, and is present-and-zero for a clean repo.
//
// DB-gated: skips without PR_TEST_DATABASE_URL.
func TestReconcileRepoPaths_GaugeSetCorrectly(t *testing.T) {
	s := testStore(t)
	const repo = "test/reconcile-gauge"
	cleanRepo(t, s, repo)
	ctx := context.Background()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "keep.go"), []byte("x"), 0o644))

	insertSymbols(t, s, repo, "keep.go", []string{"A", "B"})
	insertSymbols(t, s, repo, "gone.go", []string{"C"})

	// 1 of 3 rows stale ⇒ ratio 0.333...
	res, err := s.ReconcileRepoPaths(ctx, repo, root, false)
	require.NoError(t, err)
	ratio := gaugeValue(t, stalePathRatioGauge.WithLabelValues(repo))
	assert.InDelta(t, 1.0/3.0, ratio, 0.01, "gauge = 1/3 after deleting 1 of 3 rows")

	// Clean repo: second run ⇒ ratio 0.0.
	_, err = s.ReconcileRepoPaths(ctx, repo, root, false)
	require.NoError(t, err)
	ratio = gaugeValue(t, stalePathRatioGauge.WithLabelValues(repo))
	assert.Equal(t, 0.0, ratio, "gauge = 0.0 for a clean repo")

	_ = res
}
