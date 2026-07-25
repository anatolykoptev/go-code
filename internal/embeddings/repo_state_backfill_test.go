package embeddings

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBackfillRepoSourcePath_DoesNotClobberRealPath is the clobber guard (#718):
// a concurrent indexer that already wrote a real source_path must win — our
// backfill is a no-op. The guard lives ENTIRELY in the SQL WHERE clause
//
//	AND (source_path IS NULL OR source_path = '')
//
// which is NOT redundant with the Go-level call guard (the Go guard reads
// state, sees empty, decides to backfill; a concurrent indexer can write a
// real path between that read and this UPDATE). Only the SQL clause prevents
// the clobber.
//
// Falsification (anti-vacuous): delete the
//
//	AND (source_path IS NULL OR source_path = '')
//
// clause from BackfillRepoSourcePath's SQL ⇒ the UPDATE matches the row on
// repo_key alone and overwrites source_path with "/other/root" ⇒ the
// assert.Equal(t, "/real/root", sp) goes RED.
//
// DB-gated: skips without PR_TEST_DATABASE_URL (same convention as
// repo_state_list_test.go and reconcile_paths_test.go).
func TestBackfillRepoSourcePath_DoesNotClobberRealPath(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	const repo = "test/backfill-clobber-guard"
	cleanRepoFull(t, s, repo)

	// Seed a state row whose source_path is a REAL path (non-empty) —
	// simulates a concurrent indexer that won the race.
	require.NoError(t, s.SetRepoStateWithPath(ctx, repo, "deadbeef", "test-model", "/real/root"))

	spBefore, err := s.GetRepoSourcePath(ctx, repo)
	require.NoError(t, err)
	require.Equal(t, "/real/root", spBefore, "setup: seed must write a real source_path")

	// Backfill attempts to write a DIFFERENT root. Must be a no-op.
	require.NoError(t, s.BackfillRepoSourcePath(ctx, repo, "/other/root"),
		"backfill must return nil even when it does not update")

	spAfter, err := s.GetRepoSourcePath(ctx, repo)
	require.NoError(t, err)
	assert.Equal(t, "/real/root", spAfter,
		"a concurrent indexer's real source_path must NOT be clobbered by the backfill — "+
			"if /other/root, the (source_path IS NULL OR source_path = '') guard clause is gone")
}

// TestBackfillRepoSourcePath_NoRow_InsertsNothing is the UPDATE-only shape
// guard (#718): with NO code_repo_state row for the key, the backfill must
// return nil AND insert nothing. This shape is load-bearing — an INSERT here
// would create a code_repo_state row during a first index and defeat
// compensateFirstIndexOrphan (pipeline.go), which skips its rollback when
// RepoStateExists reports true, leaving partial embeddings un-rolled-back on
// an embedChunks failure.
//
// This is an INVARIANT GUARD, not red-on-revert for the clobber clause:
// removing the (source_path IS NULL OR source_path = ”) clause does NOT
// change behaviour here because there is no row to UPDATE — the statement
// affects 0 rows either way. The test goes RED only if the UPDATE is changed
// to an INSERT (the production change it guards against).
//
// DB-gated: skips without PR_TEST_DATABASE_URL.
func TestBackfillRepoSourcePath_NoRow_InsertsNothing(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	const repo = "test/backfill-no-row"
	cleanRepoFull(t, s, repo)

	// No row exists for this key (cleanRepoFull removed it).
	existsBefore, err := s.RepoStateExists(ctx, repo)
	require.NoError(t, err)
	require.False(t, existsBefore, "setup: no row must exist before backfill")

	require.NoError(t, s.BackfillRepoSourcePath(ctx, repo, "/any/root"),
		"backfill on a missing row must return nil (UPDATE-only, no INSERT)")

	existsAfter, err := s.RepoStateExists(ctx, repo)
	require.NoError(t, err)
	assert.False(t, existsAfter,
		"backfill must NOT insert a code_repo_state row — "+
			"if true, the statement is an INSERT, which would defeat "+
			"compensateFirstIndexOrphan's first-index rollback")
}
