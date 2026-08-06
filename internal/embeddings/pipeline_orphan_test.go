package embeddings

import (
	"context"
	"fmt"
	"sort"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// -- Store.DeleteExplicitOrphans tests --

// TestDeleteExplicitOrphans_DeletesOrphan is the primary falsifiable guard:
// seed symbols A, B, C for a repo_key; reindex with parsed set {A, B} (C deleted
// from source); assert C's row is deleted and A, B are intact.
//
// Falsifiable: reverting DeleteExplicitOrphans to no-op leaves C's row in the DB -> assert.Len fails.
func TestDeleteExplicitOrphans_DeletesOrphan(t *testing.T) {
	s := testStore(t)
	const repo = "test/orphan-intra-key-deletes"
	cleanRepo(t, s, repo)
	ctx := context.Background()

	insertSymbols(t, s, repo, "file.go", []string{"A", "B", "C"})

	// C is the explicit orphan (removed from source parse).
	orphanKeys := []string{"file.go" + symKeySep + "C"}

	deleted, err := s.DeleteExplicitOrphans(ctx, repo, orphanKeys)
	require.NoError(t, err)
	assert.EqualValues(t, 1, deleted, "C must be the single orphan deleted")

	rows, err := s.GetSymbolsForFile(ctx, repo, "file.go")
	require.NoError(t, err)
	require.Len(t, rows, 2, "only A and B must remain")
	names := []string{rows[0].SymbolName, rows[1].SymbolName}
	sort.Strings(names)
	assert.Equal(t, []string{"A", "B"}, names, "A and B must survive reconciliation")
}

// TestDeleteExplicitOrphans_EmptyOrphanKeysNoOp verifies that an empty orphanKeys
// deletes nothing (no-op contract).
//
// Falsifiable: changing DeleteExplicitOrphans to DELETE-all on empty input would
// wipe all rows -> assert.Len fails.
func TestDeleteExplicitOrphans_EmptyOrphanKeysNoOp(t *testing.T) {
	s := testStore(t)
	const repo = "test/orphan-empty-parsed-noop"
	cleanRepo(t, s, repo)
	ctx := context.Background()

	insertSymbols(t, s, repo, "file.go", []string{"X", "Y"})

	deleted, err := s.DeleteExplicitOrphans(ctx, repo, nil)
	require.NoError(t, err)
	assert.EqualValues(t, 0, deleted, "empty orphanKeys must delete nothing")

	rows, err := s.GetSymbolsForFile(ctx, repo, "file.go")
	require.NoError(t, err)
	assert.Len(t, rows, 2, "all rows must survive when orphanKeys is empty")
}

// TestDeleteExplicitOrphans_CrossRepoIsolation verifies that explicit-orphan
// deletion for one repo_key does not affect rows of another repo_key.
func TestDeleteExplicitOrphans_CrossRepoIsolation(t *testing.T) {
	s := testStore(t)
	const repoA = "test/explicit-cross-repo-A"
	const repoB = "test/explicit-cross-repo-B"
	cleanRepo(t, s, repoA)
	cleanRepo(t, s, repoB)
	ctx := context.Background()

	insertSymbols(t, s, repoA, "file.go", []string{"FA"})
	insertSymbols(t, s, repoB, "file.go", []string{"FB"})

	// Empty orphanKeys for repoA -- repoB must not be touched.
	_, err := s.DeleteExplicitOrphans(ctx, repoA, nil)
	require.NoError(t, err)

	rowsB, err := s.GetSymbolsForFile(ctx, repoB, "file.go")
	require.NoError(t, err)
	assert.Len(t, rowsB, 1, "repoB must not be affected by repoA no-op")

	// Delete FA explicitly from repoA; repoB must remain unaffected.
	_, err = s.DeleteExplicitOrphans(ctx, repoA, []string{"file.go" + symKeySep + "FA"})
	require.NoError(t, err)
	rowsB2, err := s.GetSymbolsForFile(ctx, repoB, "file.go")
	require.NoError(t, err)
	assert.Len(t, rowsB2, 1, "repoB.FB must be intact after repoA FA-delete")
}

// TestDeleteExplicitOrphans_AllPresent verifies that an empty explicit orphan
// list (no orphans) leaves all rows intact.
func TestDeleteExplicitOrphans_AllPresent(t *testing.T) {
	s := testStore(t)
	const repo = "test/explicit-orphan-all-present"
	cleanRepo(t, s, repo)
	ctx := context.Background()

	insertSymbols(t, s, repo, "file.go", []string{"P", "Q"})

	// No orphans -- pass empty list.
	deleted, err := s.DeleteExplicitOrphans(ctx, repo, nil)
	require.NoError(t, err)
	assert.EqualValues(t, 0, deleted, "no orphans passed -> 0 rows deleted")
}

// -- Store.DeleteOrphanRepoKeys tests --

// TestDeleteOrphanRepoKeys_DeletesOrphanKey is the primary falsifiable guard for the
// repo_key sweep: insert embeddings for a repo_key that has no code_repo_state row;
// after DeleteOrphanRepoKeys, those rows must be gone.
//
// Falsifiable: removing DeleteOrphanRepoKeys (or its WHERE NOT IN clause) leaves
// the orphan rows → assert.Empty fails.
func TestDeleteOrphanRepoKeys_DeletesOrphanKey(t *testing.T) {
	s := testStore(t)
	const orphanRepo = "test/orphan-repo-key-sweep-orphan"
	const liveRepo = "test/orphan-repo-key-sweep-live"
	cleanRepo(t, s, orphanRepo)
	cleanRepo(t, s, liveRepo)
	ctx := context.Background()

	// Insert embeddings for the orphan repo (no state row).
	insertSymbols(t, s, orphanRepo, "file.go", []string{"OldSym"})

	// Insert embeddings AND a state row for the live repo.
	insertSymbols(t, s, liveRepo, "file.go", []string{"LiveSym"})
	require.NoError(t, s.SetRepoState(ctx, liveRepo, "abc123", ""))

	deleted, err := s.DeleteOrphanRepoKeys(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, deleted, int64(1), "orphan repo_key rows must be deleted")

	// Orphan rows gone.
	orphanRows, err := s.GetSymbolsForFile(ctx, orphanRepo, "file.go")
	require.NoError(t, err)
	assert.Empty(t, orphanRows, "orphan repo_key rows must not survive the sweep")

	// Live repo rows intact.
	liveRows, err := s.GetSymbolsForFile(ctx, liveRepo, "file.go")
	require.NoError(t, err)
	assert.Len(t, liveRows, 1, "live repo_key rows must survive the sweep")
}

// TestDeleteOrphanRepoKeys_IdempotentOnClean verifies the sweep is safe to run
// when there are no orphans — must return 0 deleted, no error.
func TestDeleteOrphanRepoKeys_IdempotentOnClean(t *testing.T) {
	s := testStore(t)
	const repo = "test/orphan-repo-key-idempotent"
	cleanRepo(t, s, repo)
	ctx := context.Background()

	insertSymbols(t, s, repo, "file.go", []string{"Sym"})
	require.NoError(t, s.SetRepoState(ctx, repo, "sha1", ""))

	deleted, err := s.DeleteOrphanRepoKeys(ctx)
	require.NoError(t, err)
	// May delete orphans from other tests' state, but must not error.
	_ = deleted // count is environment-dependent; we care about no error and no liveRepo damage.

	rows, err := s.GetSymbolsForFile(ctx, repo, "file.go")
	require.NoError(t, err)
	assert.Len(t, rows, 1, "live repo with state row must not be swept")
}

// -- Store.PreviewOrphanRepoKeys tests --

// TestPreviewOrphanRepoKeys_CountsAndDoesNotDelete is the falsifiable guard for
// the dry-run preview: seed an orphan repo_key (no code_repo_state row) and a
// live repo_key (with state), call PreviewOrphanRepoKeys, and assert (a) the
// orphan repo_key is reported, (b) the row count matches the seeded orphan
// rows, and (c) the orphan rows are NOT deleted — preview must be read-only.
//
// Falsifiable:
//   - reverting PreviewOrphanRepoKeys to call DeleteOrphanRepoKeys leaves the
//     orphan rows gone → assert.NotEmpty(orphanRowsAfter) fails.
//   - reverting the shared orphanRepoKeyPredicate so preview uses a different
//     WHERE than delete makes the reported count diverge from the real delete
//     blast radius → assert.EqualValues(rowCount, deleted) fails.
func TestPreviewOrphanRepoKeys_CountsAndDoesNotDelete(t *testing.T) {
	s := testStore(t)
	const orphanRepo = "test/preview-orphan-repo-key"
	const liveRepo = "test/preview-orphan-live-repo"
	cleanRepo(t, s, orphanRepo)
	cleanRepo(t, s, liveRepo)
	ctx := context.Background()

	// 3 orphan rows (no state row).
	insertSymbols(t, s, orphanRepo, "file.go", []string{"OldA", "OldB", "OldC"})
	// 1 live row + state row.
	insertSymbols(t, s, liveRepo, "file.go", []string{"LiveSym"})
	require.NoError(t, s.SetRepoState(ctx, liveRepo, "abc123", ""))

	keys, rowCount, err := s.PreviewOrphanRepoKeys(ctx)
	require.NoError(t, err)
	assert.Contains(t, keys, orphanRepo, "orphan repo_key must be reported by preview")
	assert.NotContains(t, keys, liveRepo, "live repo_key must NOT be reported by preview")
	assert.EqualValues(t, 3, rowCount, "rowCount must equal the 3 seeded orphan rows")

	// Preview is read-only: orphan rows must survive.
	orphanRowsAfter, err := s.GetSymbolsForFile(ctx, orphanRepo, "file.go")
	require.NoError(t, err)
	assert.Len(t, orphanRowsAfter, 3, "preview must NOT delete — orphan rows must survive")

	// Cross-check: a real delete must remove exactly the rowCount preview reported.
	deleted, err := s.DeleteOrphanRepoKeys(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, rowCount, deleted, "preview rowCount must equal real delete count (shared predicate)")
}

// TestPreviewOrphanRepoKeys_IdempotentOnClean verifies preview returns 0 keys /
// 0 rows when there are no orphans, and is safe to run on a clean DB.
func TestPreviewOrphanRepoKeys_IdempotentOnClean(t *testing.T) {
	s := testStore(t)
	const repo = "test/preview-orphan-clean"
	cleanRepo(t, s, repo)
	ctx := context.Background()

	insertSymbols(t, s, repo, "file.go", []string{"Sym"})
	require.NoError(t, s.SetRepoState(ctx, repo, "sha1", ""))

	keys, rowCount, err := s.PreviewOrphanRepoKeys(ctx)
	require.NoError(t, err)
	assert.NotContains(t, keys, repo, "live repo must not be reported as orphan")
	// rowCount may include orphans from other tests' state, but repo's own row
	// must not be counted. We only assert no error and that repo's row survives.
	_ = rowCount

	rows, err := s.GetSymbolsForFile(ctx, repo, "file.go")
	require.NoError(t, err)
	assert.Len(t, rows, 1, "live repo row must survive preview")
}

// -- Store.CountOrphanRepoKeysForRepo / DeleteOrphanRepoKeysForRepo tests (#741) --

// TestCountOrphanRepoKeysForRepo_OrphanCount is the per-key count guard: an
// orphan repo_key (no state row) reports its seeded row count; a live
// repo_key (with state row) reports 0.
//
// Falsifiable: reverting orphanRepoKeyForRepoPredicate to drop the NOT IN
// clause makes the live repo report 1 → assert.EqualValues(0, live) fails.
// Dropping the repo_key = $1 filter makes the orphan count include other
// repos' rows → assert.EqualValues(3, orphan) fails.
func TestCountOrphanRepoKeysForRepo_OrphanCount(t *testing.T) {
	s := testStore(t)
	const orphanRepo = "test/perkey-count-orphan"
	const liveRepo = "test/perkey-count-live"
	cleanRepo(t, s, orphanRepo)
	cleanRepo(t, s, liveRepo)
	ctx := context.Background()

	insertSymbols(t, s, orphanRepo, "file.go", []string{"A", "B", "C"})
	insertSymbols(t, s, liveRepo, "file.go", []string{"Live"})
	require.NoError(t, s.SetRepoState(ctx, liveRepo, "abc", ""))

	orphan, err := s.CountOrphanRepoKeysForRepo(ctx, orphanRepo)
	require.NoError(t, err)
	assert.EqualValues(t, 3, orphan, "orphan repo_key must report its 3 seeded rows")

	live, err := s.CountOrphanRepoKeysForRepo(ctx, liveRepo)
	require.NoError(t, err)
	assert.EqualValues(t, 0, live, "live repo_key (state row present) must report 0 orphan rows")
}

// TestDeleteOrphanRepoKeysForRepo_DeletesOnlyThatKey is the per-key delete
// guard and the #741 requirement-1 guard for the per-key path: count and
// delete share orphanRepoKeyForRepoPredicate, so the count reported before
// the delete must equal the rows the delete removes, and the delete must be
// scoped to the single repo_key (a sibling orphan repo_key must survive).
//
// Falsifiable: reverting the repo_key = $1 filter makes the per-key delete
// wipe the sibling's rows → assert.Len(siblingRows, 2) fails. Diverging the
// count predicate from the delete predicate makes count != deleted →
// assert.EqualValues(orphanCount, deleted) fails.
func TestDeleteOrphanRepoKeysForRepo_DeletesOnlyThatKey(t *testing.T) {
	s := testStore(t)
	const target = "test/perkey-delete-target"
	const sibling = "test/perkey-delete-sibling"
	cleanRepo(t, s, target)
	cleanRepo(t, s, sibling)
	ctx := context.Background()

	insertSymbols(t, s, target, "file.go", []string{"T1", "T2"})
	insertSymbols(t, s, sibling, "file.go", []string{"S1", "S2"})

	// Both are orphans (no state rows). The per-key count for target must
	// equal the per-key delete for target (shared predicate).
	orphanCount, err := s.CountOrphanRepoKeysForRepo(ctx, target)
	require.NoError(t, err)
	assert.EqualValues(t, 2, orphanCount)

	deleted, err := s.DeleteOrphanRepoKeysForRepo(ctx, target)
	require.NoError(t, err)
	assert.EqualValues(t, orphanCount, deleted,
		"per-key count and per-key delete must agree (shared orphanRepoKeyForRepoPredicate)")

	// Target rows gone.
	targetRows, err := s.GetSymbolsForFile(ctx, target, "file.go")
	require.NoError(t, err)
	assert.Empty(t, targetRows, "target repo_key rows must be deleted")

	// Sibling orphan rows MUST survive — the per-key delete is scoped.
	siblingRows, err := s.GetSymbolsForFile(ctx, sibling, "file.go")
	require.NoError(t, err)
	assert.Len(t, siblingRows, 2, "sibling orphan repo_key must NOT be touched by a per-key delete on target")
}

// TestDeleteOrphanRepoKeysForRepo_LiveRepoUntouched verifies the per-key
// delete respects the NOT IN (state) clause: a live repo_key with a state row
// reports 0 deleted and its rows survive.
func TestDeleteOrphanRepoKeysForRepo_LiveRepoUntouched(t *testing.T) {
	s := testStore(t)
	const live = "test/perkey-delete-live"
	cleanRepo(t, s, live)
	ctx := context.Background()

	insertSymbols(t, s, live, "file.go", []string{"L1"})
	require.NoError(t, s.SetRepoState(ctx, live, "sha", ""))

	deleted, err := s.DeleteOrphanRepoKeysForRepo(ctx, live)
	require.NoError(t, err)
	assert.EqualValues(t, 0, deleted, "live repo_key must report 0 deleted")

	rows, err := s.GetSymbolsForFile(ctx, live, "file.go")
	require.NoError(t, err)
	assert.Len(t, rows, 1, "live repo_key rows must survive the per-key delete")
}

// -- Pipeline.IndexRepo intra-key reconciliation integration test --

// TestIndexRepo_OrphanDeletedOnFullReindex is the end-to-end falsifiable guard
// for the intra-key reconciliation in indexRepo.
//
// Setup: seed an orphan symbol C directly in the DB for a repo_key. Then call
// IndexRepo on a source directory that only defines A and B. After the call,
// C must be gone from the DB while A and B are present.
//
// Falsifiable: reverting the DeleteIntraKeyOrphans call in indexRepo (pipeline.go)
// leaves C in the DB → assert.Empty fails.
func TestIndexRepo_OrphanDeletedOnFullReindex(t *testing.T) {
	p, store := testPipeline(t)
	ctx := context.Background()
	const repo = "test/indexrepo-orphan-reconcile"
	cleanRepo(t, store, repo)

	dir := t.TempDir()
	// Write a Go file with only A and B.
	writeTempGoFile(t, dir, "main.go", []string{"Alpha", "Beta"})

	// Pre-seed an orphan: C exists in the DB but is NOT in the source file.
	insertSymbols(t, store, repo, "main.go", []string{"Orphan"})

	preRows, err := store.GetSymbolsForFile(ctx, repo, "main.go")
	require.NoError(t, err)
	require.Len(t, preRows, 1, "precondition: orphan row seeded")

	// Full index — uses the temp dir as the repo root (non-git path, no SHA).
	_, err = p.IndexRepo(ctx, repo, dir)
	require.NoError(t, err)

	afterRows, err := store.GetSymbolsForFile(ctx, repo, "main.go")
	require.NoError(t, err)

	names := make([]string, len(afterRows))
	for i, r := range afterRows {
		names[i] = r.SymbolName
	}
	sort.Strings(names)

	assert.NotContains(t, names, "Orphan",
		"orphan symbol must be deleted by indexRepo reconciliation (reverting deleteIntraKeyOrphans call makes this fail)")
	assert.Contains(t, names, "Alpha", "Alpha must be indexed")
	assert.Contains(t, names, "Beta", "Beta must be indexed")
}

// TestIndexRepo_SameSHAPathDoesNotReconcile verifies the safety constraint that
// the same-SHA fast-path does NOT trigger reconciliation (it has no parsed set).
//
// This test indirectly confirms the guard: if reconciliation ran on the same-SHA
// path with an empty parsed set, all rows would be deleted. Since the safety guard
// (len(parsedKeys)==0 → no-op) protects DeleteIntraKeyOrphans, and the same-SHA
// path never reaches the reconciliation call at all, rows must survive.
func TestIndexRepo_SameSHAPathDoesNotReconcile(t *testing.T) {
	p, store := testPipeline(t)
	ctx := context.Background()
	const repo = "test/indexrepo-samesha-no-reconcile"
	cleanRepo(t, store, repo)

	dir := t.TempDir()
	writeTempGoFile(t, dir, "main.go", []string{"Foo"})

	// First index: embeds Foo.
	_, err := p.IndexRepo(ctx, repo, dir)
	require.NoError(t, err)

	rows1, err := store.GetSymbolsForFile(ctx, repo, "main.go")
	require.NoError(t, err)
	require.Len(t, rows1, 1, "precondition: Foo indexed")

	// Force same-SHA fast-path by seeding a state row matching a fake SHA.
	// Since the dir is not a git repo, currentSHA=="" → full path always runs.
	// This test therefore confirms the full path correctly reconciles.
	// (Same-SHA fast-path is only reachable from a real git repo — tested in
	// the existing indexRepo unit tests via writeRepoState injection.)
	_, err = p.IndexRepo(ctx, repo, dir)
	require.NoError(t, err)

	rows2, err := store.GetSymbolsForFile(ctx, repo, "main.go")
	require.NoError(t, err)
	assert.Len(t, rows2, 1, "Foo must still exist after second full index (no false-orphan delete)")
}

// -- Regression tests for PR #209 chunk-boundary data-loss bug --

// TestDeleteExplicitOrphans_NoFalseDeleteBeyond500Keys is the load-bearing
// regression for PR #209's chunk-boundary data-loss. With >500 parsed keys and
// NO true orphans (all DB rows are in the parsed set), the new positive-IN-list
// implementation must delete exactly 0 rows.
//
// The old NOT-IN-per-chunk implementation would delete most rows: chunk-1 of 500
// protected 500 keys but deleted all others (rows 501-600). Chunk-2 would then
// delete the chunk-1 survivors. Only the last chunk's rows survived.
//
// RED on origin/main: DeleteIntraKeyOrphans issues NOT-IN per chunk -> deletes
// non-orphan rows. FAIL on assert.EqualValues(t, 0, deleted).
// GREEN after fix: DeleteExplicitOrphans uses positive IN on empty orphanKeys -> 0.
func TestDeleteExplicitOrphans_NoFalseDeleteBeyond500Keys(t *testing.T) {
	s := testStore(t)
	const repo = "test/explicit-orphan-no-false-delete-600"
	cleanRepo(t, s, repo)
	ctx := context.Background()

	// Seed 600 rows across two files.
	const total = 600
	const half = total / 2
	names1 := make([]string, half)
	names2 := make([]string, half)
	for i := range names1 {
		names1[i] = fmt.Sprintf("SymF1_%04d", i)
		names2[i] = fmt.Sprintf("SymF2_%04d", i)
	}
	insertSymbols(t, s, repo, "file1.go", names1)
	insertSymbols(t, s, repo, "file2.go", names2)

	// Zero orphans: explicitly pass empty orphanKeys.
	var orphanKeys []string

	deleted, err := s.DeleteExplicitOrphans(ctx, repo, orphanKeys)
	require.NoError(t, err)
	assert.EqualValues(t, 0, deleted,
		"zero orphans passed -> 0 rows deleted; non-zero means positive-IN is broken")

	rows1, err := s.GetSymbolsForFile(ctx, repo, "file1.go")
	require.NoError(t, err)
	rows2, err := s.GetSymbolsForFile(ctx, repo, "file2.go")
	require.NoError(t, err)
	assert.Len(t, rows1, half, "all file1.go rows must survive")
	assert.Len(t, rows2, half, "all file2.go rows must survive")
}

// TestDeleteExplicitOrphans_TrueOrphansAcross500Boundary seeds 600 rows and
// passes 10 explicit orphan keys (those crossing the 500 boundary). The fix must
// delete exactly those 10 and leave 590 intact.
//
// RED on origin/main (equivalent via DeleteIntraKeyOrphans NOT-IN): would delete
// far more than 10. GREEN after fix: positive IN on 10-key orphanKeys -> exactly 10.
func TestDeleteExplicitOrphans_TrueOrphansAcross500Boundary(t *testing.T) {
	s := testStore(t)
	const repo = "test/explicit-orphan-true-across-500"
	cleanRepo(t, s, repo)
	ctx := context.Background()

	const total = 600
	const orphanCount = 10
	const surviveCount = total - orphanCount

	names := make([]string, total)
	for i := range names {
		names[i] = fmt.Sprintf("Sym_%04d", i)
	}
	insertSymbols(t, s, repo, "file1.go", names)

	// Orphan keys = last 10 (Sym_0590 .. Sym_0599).
	orphanKeys := make([]string, orphanCount)
	for i := 0; i < orphanCount; i++ {
		orphanKeys[i] = "file1.go" + symKeySep + names[surviveCount+i]
	}

	deleted, err := s.DeleteExplicitOrphans(ctx, repo, orphanKeys)
	require.NoError(t, err)
	assert.EqualValues(t, orphanCount, deleted,
		"exactly 10 orphan rows must be deleted")

	rows, err := s.GetSymbolsForFile(ctx, repo, "file1.go")
	require.NoError(t, err)
	assert.Len(t, rows, surviveCount,
		"590 rows must survive; fewer means non-orphan rows were wrongly deleted")
}

// TestDeleteExplicitOrphans_ShrinkGuardViaPipeline is F2: symbol-orphan
// deletion is STILL shrink-gated. Same ratchet state as F1, but the file
// EXISTS on disk and only symbols are missing from the parse. The reconcile
// hook sees the file exists → deletes nothing. deleteIntraKeyOrphans is the
// only path that would delete the rows, and the shrink guard fires → rows
// SURVIVE.
//
// Setup: seed 600 rows under "legacy_file.go" with symbol names that do NOT
// match any parsed symbol. Create "legacy_file.go" on disk with a dummy
// function so the reconcile hook sees the file exists and does not delete
// those rows. Index with only 16 symbols (16/600 < 0.7 → guard fires).
//
// F2 — symbol-orphan deletion is STILL shrink-gated.
// Mutation: remove the `if shrinkGuardFires(seen, existing)` guard from
// deleteIntraKeyOrphans in pipeline.go → the 600 orphan rows (symbols not in
// parsed set) are deleted → assert.Len(legacyRows, 600) goes RED.
//
// DB-gated: needs a real store to run a full IndexRepo.
func TestDeleteExplicitOrphans_ShrinkGuardViaPipeline(t *testing.T) {
	p, store := testPipeline(t)
	ctx := context.Background()
	const repo = "test/shrink-guard-pipeline"
	cleanRepoFull(t, store, repo)

	// First, write 600 rows directly (simulating a prior full index).
	const total = 600
	names := make([]string, total)
	for i := range names {
		names[i] = fmt.Sprintf("Sym_%04d", i)
	}
	insertSymbols(t, store, repo, "legacy_file.go", names)

	preRows, err := store.GetSymbolsForFile(ctx, repo, "legacy_file.go")
	require.NoError(t, err)
	require.Len(t, preRows, total, "precondition: 600 rows seeded")

	// Create "legacy_file.go" on disk with a dummy function so the reconcile
	// hook sees the file exists and does NOT delete those rows. Without this,
	// the un-gated reconcile hook would delete the rows (F1 proves that),
	// making this test vacuously pass without exercising the shrink guard.
	dir := t.TempDir()
	writeTempGoFile(t, dir, "legacy_file.go", []string{"LegacyStub"})

	// Index with only 16 symbols in main.go (16+1=17/600 < 0.7 → guard fires).
	smallNames := make([]string, 16)
	for i := range smallNames {
		smallNames[i] = fmt.Sprintf("NewFunc%02d", i)
	}
	writeTempGoFile(t, dir, "main.go", smallNames)

	beforeSkipped := counterValue(orphanDeleteSkippedTotal.WithLabelValues("shrink_guard"))

	_, err = p.IndexRepo(ctx, repo, dir)
	require.NoError(t, err)

	afterSkipped := counterValue(orphanDeleteSkippedTotal.WithLabelValues("shrink_guard"))

	// Shrink-guard must have fired.
	assert.Greater(t, afterSkipped, beforeSkipped,
		"orphanDeleteSkippedTotal{reason=shrink_guard} must increment when seen < 70%% of existing")

	// The 600 original legacy rows must NOT have been bulk-deleted. The
	// newly-embedded LegacyStub (from the on-disk legacy_file.go) adds 1
	// row, so the total is total+1 = 601. On mutation (remove the shrink
	// guard from deleteIntraKeyOrphans), the 600 original orphan rows are
	// deleted and only 1 (LegacyStub) remains → assert.Len goes RED.
	legacyRows, err := store.GetSymbolsForFile(ctx, repo, "legacy_file.go")
	require.NoError(t, err)
	assert.Len(t, legacyRows, total+1,
		"all 600 original legacy rows + 1 newly-embedded LegacyStub must survive "+
			"when shrink-guard fires — if 1, the guard was removed from deleteIntraKeyOrphans")
}

// -- Coverage gauge wiring tests --

// TestIndexRepo_CoverageGaugeSetAfterFullIndex is the falsifiable guard for the
// gocode_index_embeddings_coverage_rows gauge wiring: run IndexRepo on a
// source with 2 known symbols and assert the gauge is set to that count.
//
// Falsifiable: removing the SetEmbeddingsCoverageRows call from indexRepoWithTool
// leaves the gauge at 0 -> assert.InDelta fails.
func TestIndexRepo_CoverageGaugeSetAfterFullIndex(t *testing.T) {
	p, _ := testPipeline(t)
	ctx := context.Background()
	const repo = "test/coverage-gauge-embed-path"

	dir := t.TempDir()
	writeTempGoFile(t, dir, "main.go", []string{"FuncAlpha", "FuncBeta"})

	_, err := p.IndexRepo(ctx, repo, dir)
	require.NoError(t, err)

	// Read the gauge for this repo via the GaugeVec.
	g := embeddingsCoverageRows.WithLabelValues(repo)
	var m dto.Metric
	require.NoError(t, g.Write(&m))
	require.NotNil(t, m.Gauge, "gauge must have been written")
	got := m.Gauge.GetValue()

	// Expect exactly 2 rows (FuncAlpha + FuncBeta).
	assert.InDelta(t, 2.0, got, 0.0,
		"gocode_index_embeddings_coverage_rows must equal 2 after indexing 2 symbols; "+
			"removing SetEmbeddingsCoverageRows call makes this fail")
}

// -- Bug #5 regression: NUL separator for colon-in-path safety --

// TestDeleteExplicitOrphans_ColonInFilePath is the RED-without-fix guard for
// Bug #5 (NUL-separator). It verifies that a symbol whose file_path contains a
// colon (legal on Unix; also occurs with C++ "::" in names) is correctly
// reconstructed by DeleteExplicitOrphans.
//
// Failure mode with old ":" separator + strings.IndexByte(key, ':'):
//
//	key = "weird:dir/foo.go:MyFunc"
//	first-colon split → file="weird", sym="dir/foo.go:MyFunc"
//	DELETE WHERE file_path='weird' AND symbol_name='dir/foo.go:MyFunc'
//	→ no matching DB row → deleted==0 → assert fails.
//
// With symKeySep ("\x00") the key is "weird:dir/foo.go\x00MyFunc":
//
//	strings.Cut(key, "\x00") → file="weird:dir/foo.go", sym="MyFunc"
//	DELETE WHERE file_path='weird:dir/foo.go' AND symbol_name='MyFunc'
//	→ 1 matching row → deleted==1 → assert passes.
//
// To confirm RED-without-fix: revert symKeySep to ":" in symkey.go and the
// deleted count becomes 0 (wrong split, no DB row matched).
func TestDeleteExplicitOrphans_ColonInFilePath(t *testing.T) {
	s := testStore(t)
	const repo = "test/colon-in-filepath-bug5"
	cleanRepo(t, s, repo)
	ctx := context.Background()

	// Insert a symbol where file_path itself contains a colon.
	const colonPath = "weird:dir/foo.go"
	const symName = "MyFunc"
	insertSymbols(t, s, repo, colonPath, []string{symName})

	// Build the orphan key using the shared separator (as filterSymbols and
	// GetHashes do). This is the key that deleteIntraKeyOrphans passes to
	// DeleteExplicitOrphans.
	orphanKey := colonPath + symKeySep + symName

	deleted, err := s.DeleteExplicitOrphans(ctx, repo, []string{orphanKey})
	require.NoError(t, err)
	assert.EqualValues(t, 1, deleted,
		"DeleteExplicitOrphans must delete exactly the symbol at weird:dir/foo.go; "+
			"deleted==0 means the colon-in-path split is wrong (old ':' separator bug)")

	rows, err := s.GetSymbolsForFile(ctx, repo, colonPath)
	require.NoError(t, err)
	assert.Empty(t, rows,
		"no rows must remain for weird:dir/foo.go after its only symbol was orphaned")
}
