package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anatolykoptev/vaelor/internal/embeddings"
)

// orphanSweepCategory constants are the named categories the sweep can act on.
// They are the values accepted by OrphanSweepInput.Categories and the labels
// on gocode_orphan_sweep_category_keys.
const (
	// catEmbeddingsNotInState: code_embeddings rows whose repo_key has no
	// matching code_repo_state row. This is the ORIGINAL sweep behaviour —
	// it deletes embeddings only, NEVER state rows. An omitted Categories
	// parameter runs ONLY this category, preserving byte-for-byte backward
	// compatibility.
	catEmbeddingsNotInState = "embeddings_not_in_state"

	// catPathMissing: code_repo_state rows whose source_path is non-empty but
	// the directory is gone from disk. The repo was deleted via
	// ReleaseCloneRef/CleanupCloneDir (a pure filesystem call) without a
	// state-row cleanup. Deleting the state row cascades to its embeddings via
	// the ON DELETE CASCADE trigger. This category DELETES STATE ROWS.
	catPathMissing = "path_missing"

	// catPathless: code_repo_state rows with an empty source_path —
	// unidentifiable tombstones whose original root is gone and cannot be
	// reconstructed (repo_key = "code_" + FNV-32a(root), and the root is
	// neither recorded nor recoverable). Deleting the state row cascades to
	// its embeddings. This category DELETES STATE ROWS.
	catPathless = "pathless"
)

// validOrphanSweepCategories is the accept set for OrphanSweepInput.Categories.
var validOrphanSweepCategories = map[string]bool{
	catEmbeddingsNotInState: true,
	catPathMissing:          true,
	catPathless:             true,
}

// OrphanSweepInput is the input schema for the orphan_sweep tool.
//
// DryRun is a POINTER so that "omitted" is distinguishable from "explicitly
// false": omitted (nil) or true ⇒ dry-run preview (the SAFE DEFAULT — counts
// candidates and the rows that would be deleted, no mutation); false ⇒
// perform the real bulk DELETE. A plain bool would default to false=delete,
// the opposite of the safe default — do not use a plain bool.
//
// Categories selects which classes of orphans the sweep acts on. OMITTED ⇒
// ["embeddings_not_in_state"] only — exactly the original behaviour: deletes
// code_embeddings rows whose repo_key has no code_repo_state row, and NEVER
// deletes a state row. An existing call that omits Categories is
// byte-for-byte unchanged in effect. To delete dead state rows, opt in
// explicitly:
//   - "path_missing": state rows whose source_path is non-empty but the
//     directory is gone. DELETES STATE ROWS (cascades embeddings). Guarded
//     by the mount-blip check — see the tool description.
//   - "pathless": state rows with an empty source_path. DELETES STATE ROWS
//     (cascades embeddings). No filesystem guard (no path to check).
//
// Categories may be combined, e.g. ["path_missing","pathless"]. Each
// category's counts are reported SEPARATELY in the output — never merged.
type OrphanSweepInput struct {
	DryRun     *bool    `json:"dry_run,omitempty" jsonschema:"Defaults to TRUE (preview only: counts candidates and the rows that would be deleted per category, with NO mutation). Set to false explicitly to perform the real DELETE."`
	Categories []string `json:"categories,omitempty" jsonschema:"Which classes of orphans to sweep. OMITTED ⇒ [\"embeddings_not_in_state\"] only (the original behaviour — deletes code_embeddings rows whose repo_key has no code_repo_state row; NEVER deletes state rows). Opt in explicitly to delete dead state rows: \"path_missing\" (state rows whose source_path directory is gone — DELETES STATE ROWS, guarded by the mount-blip check) and/or \"pathless\" (state rows with an empty source_path — DELETES STATE ROWS, no filesystem guard). May be combined. Each category's counts are reported separately."`
}

// orphanSweepStore is the subset of *embeddings.Store the handler needs.
// Defined as an interface so tests can supply a fake without a live Postgres
// pool (and so the dry-run path can assert no delete is called).
// *embeddings.Store satisfies it implicitly.
//
// The per-key methods (CountOrphanRepoKeysForRepo, DeleteOrphanRepoKeysForRepo)
// are the surface the in-flight guard drives for the embeddings_not_in_state
// category: the handler claims a repoKey's index slot, then counts and deletes
// that key's orphan rows through these. They share orphanRepoKeyForRepoPredicate
// so the per-key count and the per-key delete can never diverge (the same
// invariant the bulk orphanRepoKeyPredicate enforces — see store.go:549-554).
//
// The bulk DeleteOrphanRepoKeys is deliberately ABSENT from this narrowed
// view: the guarded handler deletes per-key only (claim → count → delete per
// candidate). A bulk delete cannot be guarded per-key, so keeping it off the
// handler's interface makes the unguarded bulk-delete path uncallable by
// construction — a type-level guard is stronger than a test that asserts
// "nobody calls this". The store method itself stays on *embeddings.Store
// (internal/embeddings/store.go:573) with its own tests; only the handler's
// narrowed view drops it.
//
// ListRepoKeysWithSourcePath / ListRepoKeysWithoutSourcePath / CountEmbeddings
// / WipeRepo are the surface the path_missing and pathless categories drive.
// WipeRepo is the ADR-8 irreversible-data-deletion seam (store_wipe.go): it
// deletes both code_embeddings and code_repo_state for a repoKey atomically in
// a single transaction. The handler claims the index slot BEFORE calling
// WipeRepo, so an in-flight indexer cannot have its rows wiped underneath it
// — the same per-key guard the embeddings_not_in_state category uses.
type orphanSweepStore interface {
	// embeddings_not_in_state category
	CountOrphanRepoKeys(ctx context.Context) (int64, error)
	PreviewOrphanRepoKeys(ctx context.Context) (repoKeys []string, rowCount int64, err error)
	CountOrphanRepoKeysForRepo(ctx context.Context, repoKey string) (int64, error)
	DeleteOrphanRepoKeysForRepo(ctx context.Context, repoKey string) (int64, error)
	// path_missing + pathless categories
	ListRepoKeysWithSourcePath(ctx context.Context) ([]embeddings.RepoKeySourcePath, error)
	ListRepoKeysWithoutSourcePath(ctx context.Context) ([]embeddings.RepoKeySourcePath, error)
	CountEmbeddings(ctx context.Context, repoKey string) (int, error)
	WipeRepo(ctx context.Context, repoKey string) error
}

// indexSlotClaimer is the per-repoKey single-flight gate the sweep consults
// before deleting a candidate. *embeddings.Pipeline satisfies it via the
// exported ClaimIndexSlot wrapper around the same LoadOrStore-based slot the
// index entrypoints use (pipeline.go:276). The sweep claims a candidate's
// slot before deleting it:
//   - claim succeeds → no indexer can start for that key while the sweep holds
//     it; delete, then release.
//   - claim fails → an indexer owns the key; skip it and record it as excluded.
//
// This closes the #741 race in both directions with no schema change and no
// new state: an age filter cannot do that — it narrows a window; this removes
// it. A nil claimer (no Pipeline wired) is REFUSED on the destructive path:
// the handler deletes nothing and returns an error naming the missing guard
// (fail-closed — a guard whose only job is to prevent silent data_loss must
// stop the destruction when it is absent, not proceed without it). The dry
// run is unaffected: a preview mutates nothing and needs no guard. This
// branch never ships that way (registerOrphanSweep passes deps.Pipeline), but
// the fail-closed check is tested so a future wiring change is discovered by
// a test, not by missing rows.
//
// LIMITATION (inherited from the existing sweep, flagged in the audit): the
// claim is process-local — it guards against an indexer in THIS process, not
// a concurrent process that does not share the in-memory slot map. The
// mechanism is kept because it is the best available without a distributed
// lock, but it is NOT a cross-process guarantee. A future operator who runs
// orphan_sweep from a different process than the indexer should be aware of
// this; the dry-run-first discipline and the CASCADE trigger (which makes a
// re-index self-healing) are the compensating controls.
type indexSlotClaimer interface {
	ClaimIndexSlot(repoKey string) (release func(), won bool)
}

// dirLister is the filesystem subset the path_missing mount-blip guard
// needs. Defined as an interface so tests can fake a missing parent
// (mount blip) without touching the real filesystem. *osDirLister is the
// production implementation.
type dirLister interface {
	// ReadDir returns the entries of dir, or an error if dir does not exist
	// or is unreadable.
	ReadDir(dir string) ([]os.DirEntry, error)
	// IsGitRoot reports whether path is a live git root (contains a .git
	// directory or file — the latter for worktree-linked checkouts).
	IsGitRoot(path string) bool
}

// osDirLister is the production dirLister, backed by the real filesystem.
type osDirLister struct{}

func (osDirLister) ReadDir(dir string) ([]os.DirEntry, error) { return os.ReadDir(dir) }

func (osDirLister) IsGitRoot(path string) bool {
	_, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil
}

// registerOrphanSweep registers the orphan_sweep MCP tool.
//
// The tool is only registered when a DB pool is available (DATABASE_URL configured).
// It is a no-op (disabled) otherwise, matching the gating pattern used by
// registerSparseBackfill and registerCodeGraph.
//
// The sweep is intentionally operator-gated (not run automatically at startup)
// because it issues a bulk DELETE across potentially tens of thousands of rows.
// It PREVIEWS BY DEFAULT (dry_run=true when the param is omitted); the operator
// must pass dry_run=false to actually delete. The intra-key orphan
// reconciliation in indexRepo is safe to run automatically (it has the complete
// parsed set); this sweep deletes entire repo_keys and requires an explicit
// operator trigger. Run after removing worktrees, deregistering repos, or after
// a mass migration of checkout paths.
//
// CATEGORIES: the Categories parameter selects which classes of orphans to
// sweep. Omitted ⇒ ["embeddings_not_in_state"] only (the original behaviour —
// deletes embeddings, NEVER state rows). Opt in to "path_missing" and/or
// "pathless" to delete dead state rows. See OrphanSweepInput for the contract.
//
// In-flight guard (#741): the real-delete path claims each candidate repoKey's
// index slot via deps.Pipeline before deleting it, so a first index that is
// committing chunks but has not yet written its code_repo_state row cannot have
// its rows deleted underneath it. Keys whose slot is already held by an
// indexer are excluded and reported in the output (a silent skip is the mirror
// of the bug). Dry run does NOT claim — see handleOrphanSweep's DRY-RUN
// DECISION comment. If deps.Pipeline is nil the destructive path is REFUSED
// (fail-closed): the handler deletes nothing and returns an error naming the
// missing guard — see handleOrphanSweep's NIL-CLAIMER REFUSAL comment.
func registerOrphanSweep(server *mcp.Server, deps SemanticDeps) {
	if deps.Store == nil {
		slog.Info("orphan_sweep: DATABASE_URL not set — tool disabled")
		return
	}

	addTool(server, &mcp.Tool{
		Name: "orphan_sweep",
		Description: "Operator-initiated cleanup of orphaned embedding/state rows. DRY-RUN BY DEFAULT (dry_run omitted or true ⇒ preview only, no mutation). " +
			"CATEGORIES (the key parameter — read this before running): " +
			"Omitted ⇒ [\"embeddings_not_in_state\"] only — the ORIGINAL behaviour: deletes code_embeddings rows whose repo_key has no matching code_repo_state row. NEVER deletes code_repo_state rows. " +
			"\"path_missing\" — deletes code_repo_state rows whose source_path is non-empty but the directory is gone from disk (the repo was removed via ReleaseCloneRef/CleanupCloneDir without state cleanup). DELETES STATE ROWS; embeddings cascade via the ON DELETE CASCADE trigger. Guarded by the mount-blip check: the parent directory of each candidate must exist AND contain at least one other live git root — if any parent is missing, unreadable, or holds no live git root, the WHOLE category is refused and nothing is deleted (a mount blip must not wipe a good index). " +
			"\"pathless\" — deletes code_repo_state rows with an empty source_path (unidentifiable tombstones whose original root is gone). DELETES STATE ROWS; embeddings cascade. No filesystem guard (no path to check). " +
			"Categories may be combined. Each category's counts are reported SEPARATELY in the output. " +
			"IN-FLIGHT GUARD: the real delete claims each candidate's index slot first; a repoKey whose slot is held by an in-flight indexer is EXCLUDED and named in the output. " +
			"Idempotent: re-running when clean returns 0 deleted. " +
			"Progress is observable via gocode_orphan_sweep_category_keys{category} and gocode_orphan_repo_keys on /metrics (port 9897).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in OrphanSweepInput) (*mcp.CallToolResult, error) {
		return handleOrphanSweep(ctx, in, deps.Store, deps.Pipeline, osDirLister{})
	})
}

// categoryResult is the outcome of one category's sweep pass. The handler
// aggregates one per requested category and renders them separately in the
// output — never merged into a single number.
type categoryResult struct {
	name             string
	candidateKeys    int    // distinct keys identified as orphans in this category
	eligibleKeys     int    // keys whose index slot was won (delete path only)
	excludedInFlight int    // keys whose slot was held by an indexer (delete path only)
	rowsDeleted      int64  // rows actually deleted (delete path only)
	rowsWouldDelete  int64  // rows that would be deleted (dry-run path)
	stateRowsDeleted int    // state rows deleted (path_missing/pathless only; 0 for embeddings_not_in_state)
	refused          bool   // category was refused (mount-blip guard)
	refusalReason    string // why the category was refused
	excludedKeys     []string
}

// handleOrphanSweep is the extracted handler, callable from tests.
//
// Dry-run defaulting: dry := in.DryRun == nil || *in.DryRun — nil or true ⇒
// preview (safe default); false ⇒ real delete.
//
// Category defaulting: cats := in.Categories; len==0 ⇒ ["embeddings_not_in_state"]
// only — exactly the original behaviour. An omitted Categories parameter
// NEVER triggers path_missing or pathless, so an existing call is
// byte-for-byte unchanged in effect (no state row is ever deleted).
//
// DRY-RUN DECISION (#741 requirement 4): the dry run does NOT claim index
// slots. Rationale: a dry run must not mutate anything AND must not block
// indexers — claiming a slot blocks that repoKey from indexing for the
// duration of the preview, which is a side effect on a non-mutating path. The
// consequence is that a dry-run preview can name a key that the real delete
// would skip (an indexer that starts between the dry run and the real delete
// owns the slot at delete time). That difference is made VISIBLE in the
// output rather than silent. The per-key SQL predicate is shared between
// count and delete (orphanRepoKeyForRepoPredicate) so the dry-run row counts
// and the real delete row counts cannot diverge for a given key under the
// same in-flight state.
//
// Real-delete flow (claim-then-delete, #741):
//  1. NIL-CLAIMER REFUSAL: if claimer is nil, refuse immediately — delete
//     nothing, return an error naming the missing guard, log at slog.Error.
//  2. For each requested category, run the category-specific sweep.
//  3. Each category claims per-key slots before deleting (the same guard the
//     original embeddings_not_in_state category uses).
//  4. Report every category's counts SEPARATELY.
//
// Error propagation: a hard failure in any category (preview/list error, per-
// key delete error) returns a Go error from the handler — matching the
// original handler's convention where every failure returned nil,
// fmt.Errorf(...). The first error across categories wins. A mount-blip
// guard refusal is NOT a Go error — it is a reported refusal in the category
// result (the operator should see it in the output, not as an MCP error).
func handleOrphanSweep(ctx context.Context, in OrphanSweepInput, store orphanSweepStore, claimer indexSlotClaimer, fs dirLister) (*mcp.CallToolResult, error) {
	slog.Info("orphan_sweep: starting")
	dry := in.DryRun == nil || *in.DryRun

	cats := in.Categories
	if len(cats) == 0 {
		cats = []string{catEmbeddingsNotInState}
	}
	// Validate categories.
	for _, c := range cats {
		if !validOrphanSweepCategories[c] {
			return nil, fmt.Errorf("orphan_sweep: unknown category %q (accepted: embeddings_not_in_state, path_missing, pathless)", c)
		}
	}

	// Real-delete path: nil-claimer refusal (fail-closed) applies to ALL
	// categories — every category deletes something, so every category needs
	// the guard. A dry run mutates nothing and needs no guard.
	if !dry && claimer == nil {
		slog.Error("orphan_sweep: in-flight guard not wired (nil claimer) — refusing destructive dry_run=false to prevent silent data_loss")
		return nil, fmt.Errorf("orphan_sweep: in-flight guard not wired (nil claimer) — refusing destructive dry_run=false to prevent silent data_loss; wire deps.Pipeline or run with dry_run=true for a preview")
	}

	var results []categoryResult
	var firstErr error
	for _, c := range cats {
		var cr categoryResult
		var err error
		switch c {
		case catEmbeddingsNotInState:
			cr, err = sweepEmbeddingsNotInState(ctx, dry, store, claimer)
		case catPathMissing:
			cr, err = sweepPathMissing(ctx, dry, store, claimer, fs)
		case catPathless:
			cr, err = sweepPathless(ctx, dry, store, claimer)
		}
		cr.name = c
		results = append(results, cr)
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	// Update gauges per category.
	for _, cr := range results {
		embeddings.SetOrphanSweepCategoryKeys(cr.name, float64(cr.candidateKeys))
	}
	// Backward-compat: the existing gocode_orphan_repo_keys gauge mirrors the
	// embeddings_not_in_state category. Keep it truthful after any sweep that
	// includes that category.
	for _, cr := range results {
		if cr.name == catEmbeddingsNotInState {
			after, err := store.CountOrphanRepoKeys(ctx)
			if err != nil {
				slog.Warn("orphan_sweep: post-sweep orphan count failed", slog.Any("error", err))
				after = 0
			}
			embeddings.SetOrphanRepoKeysGauge(float64(after))
		}
	}

	if firstErr != nil {
		return nil, firstErr
	}

	return textResult(formatOrphanSweepResults(dry, results)), nil
}

// sweepEmbeddingsNotInState is the ORIGINAL category: deletes code_embeddings
// rows whose repo_key has no matching code_repo_state row. NEVER deletes
// state rows. This is the exact logic the handler ran before categories were
// introduced — preserved verbatim so an omitted Categories parameter is
// byte-for-byte unchanged in effect.
//
// Returns (result, error). error is non-nil on a hard failure (preview error,
// per-key delete error) — the handler propagates it as a Go error, matching
// the original handler's convention.
func sweepEmbeddingsNotInState(ctx context.Context, dry bool, store orphanSweepStore, claimer indexSlotClaimer) (categoryResult, error) {
	cr := categoryResult{name: catEmbeddingsNotInState}

	if dry {
		repoKeys, rowCount, err := store.PreviewOrphanRepoKeys(ctx)
		if err != nil {
			return cr, fmt.Errorf("orphan_sweep: preview: %w", err)
		}
		embeddings.SetOrphanRepoKeysGauge(float64(len(repoKeys)))
		cr.candidateKeys = len(repoKeys)
		cr.rowsWouldDelete = rowCount
		slog.Info("orphan_sweep: embeddings_not_in_state dry-run preview",
			slog.Int("orphan_repo_keys", len(repoKeys)),
			slog.Int64("rows_that_would_be_deleted", rowCount),
		)
		return cr, nil
	}

	// Real delete path.
	before, countErr := store.CountOrphanRepoKeys(ctx)
	if countErr != nil {
		slog.Warn("orphan_sweep: embeddings_not_in_state pre-sweep count failed (continuing)", slog.Any("error", countErr))
		before = -1
	}

	candidates, _, err := store.PreviewOrphanRepoKeys(ctx)
	if err != nil {
		return cr, fmt.Errorf("orphan_sweep: preview candidates: %w", err)
	}
	cr.candidateKeys = len(candidates)

	var excluded []string
	var deletedTotal int64
	var firstErr error

	for _, repoKey := range candidates {
		release, won := claimer.ClaimIndexSlot(repoKey)
		if !won {
			excluded = append(excluded, repoKey)
			slog.Info("orphan_sweep: embeddings_not_in_state excluded in-flight repoKey",
				slog.String("repo_key", repoKey))
			continue
		}
		cr.eligibleKeys++
		func() {
			defer release()
			if _, err := store.CountOrphanRepoKeysForRepo(ctx, repoKey); err != nil {
				slog.Warn("orphan_sweep: embeddings_not_in_state per-key count failed (continuing to delete)",
					slog.String("repo_key", repoKey),
					slog.Any("error", err))
			}
			n, err := store.DeleteOrphanRepoKeysForRepo(ctx, repoKey)
			if err != nil {
				slog.Error("orphan_sweep: embeddings_not_in_state per-key delete failed",
					slog.String("repo_key", repoKey),
					slog.Any("error", err))
				if firstErr == nil {
					firstErr = fmt.Errorf("orphan_sweep: delete %s: %w", repoKey, err)
				}
				return
			}
			deletedTotal += n
		}()
	}

	cr.excludedInFlight = len(excluded)
	cr.excludedKeys = excluded
	cr.rowsDeleted = deletedTotal

	slog.Info("orphan_sweep: embeddings_not_in_state complete",
		slog.Int64("orphan_keys_before", before),
		slog.Int("eligible_keys", cr.eligibleKeys),
		slog.Int("excluded_in_flight", cr.excludedInFlight),
		slog.Int64("rows_deleted", deletedTotal),
	)

	if firstErr != nil {
		return cr, firstErr
	}
	return cr, nil
}

// sweepPathMissing is the category that deletes code_repo_state rows whose
// source_path is non-empty but the directory is gone from disk. Deleting the
// state row cascades to its embeddings via the ON DELETE CASCADE trigger.
//
// MOUNT-BLIP GUARD (the load-bearing part): before deleting any candidate,
// verify that the PARENT directory of each candidate's source_path exists AND
// contains at least one other entry that is a live git root. If any parent is
// missing, unreadable, or holds no live git root, refuse the WHOLE category —
// delete nothing, report why. This is the one way this tool can destroy
// something real: a mount blip makes every source_path look "gone" but the
// repos are still alive and will return. The sibling check proves the parent
// mount is alive and the candidate's absence is a genuine deletion, not a
// transient unmount. The check is derived from the filesystem — never from a
// hardcoded list of expected parents.
//
// Safety argument: a missing code_repo_state row means "never indexed". If a
// checkout returns after its state row is deleted, the next query finds no
// state, indexes from scratch, and gets correct fresh vectors. Verified live
// (/host/src/ox-whisper was purged and rebuilt to 308 rows with verifiably
// different vectors). The guard makes "deleted" distinguishable from
// "temporarily unmounted", which is the one gap in that argument.
//
// Returns (result, error). error is non-nil on a hard failure (list error,
// per-key wipe error). A mount-blip guard refusal is NOT an error — it is a
// reported refusal in the result (refused=true) so the operator sees it in
// the output, not as an MCP error.
func sweepPathMissing(ctx context.Context, dry bool, store orphanSweepStore, claimer indexSlotClaimer, fs dirLister) (categoryResult, error) {
	cr := categoryResult{name: catPathMissing}

	repos, err := store.ListRepoKeysWithSourcePath(ctx)
	if err != nil {
		return cr, fmt.Errorf("orphan_sweep: path_missing list: %w", err)
	}

	// Filter to candidates whose source_path does not exist on disk.
	// This is the "directory does not exist" check — the candidate filter.
	// The mount-blip guard below is the ADDITIONAL check on top of this.
	var candidates []embeddings.RepoKeySourcePath
	for _, r := range repos {
		if r.SourcePath == "" {
			continue // pathless — handled by the pathless category
		}
		if !pathExistsDir(fs, r.SourcePath) {
			candidates = append(candidates, r)
		}
	}
	cr.candidateKeys = len(candidates)

	if len(candidates) == 0 {
		return cr, nil
	}

	// MOUNT-BLIP GUARD: verify every candidate's parent directory exists and
	// contains at least one live git root sibling. If any parent fails, refuse
	// the whole category — delete nothing. This runs in BOTH dry-run and
	// delete paths so the dry run can report whether the guard would refuse.
	ok, reason := mountBlipGuard(candidates, fs)
	if !ok {
		cr.refused = true
		cr.refusalReason = reason
		slog.Warn("orphan_sweep: path_missing refused (mount-blip guard)",
			slog.String("reason", reason),
			slog.Int("candidate_keys", len(candidates)))
		// Still count rows that would be deleted for the report.
		for _, c := range candidates {
			n, _ := store.CountEmbeddings(ctx, c.RepoKey)
			cr.rowsWouldDelete += int64(n)
		}
		return cr, nil
	}

	if dry {
		for _, c := range candidates {
			n, _ := store.CountEmbeddings(ctx, c.RepoKey)
			cr.rowsWouldDelete += int64(n)
		}
		slog.Info("orphan_sweep: path_missing dry-run preview",
			slog.Int("dead_path_keys", len(candidates)),
			slog.Int64("rows_that_would_be_deleted", cr.rowsWouldDelete),
		)
		return cr, nil
	}

	// Real delete: claim-then-wipe per key.
	var excluded []string
	var deletedRows int64
	var stateDeleted int
	var firstErr error

	for _, c := range candidates {
		release, won := claimer.ClaimIndexSlot(c.RepoKey)
		if !won {
			excluded = append(excluded, c.RepoKey)
			slog.Info("orphan_sweep: path_missing excluded in-flight repoKey",
				slog.String("repo_key", c.RepoKey))
			continue
		}
		cr.eligibleKeys++
		func() {
			defer release()
			// Count before wipe so the report is accurate (WipeRepo does not
			// return a row count — the CASCADE trigger deletes embeddings
			// server-side).
			n, _ := store.CountEmbeddings(ctx, c.RepoKey)
			if err := store.WipeRepo(ctx, c.RepoKey); err != nil {
				slog.Error("orphan_sweep: path_missing wipe failed",
					slog.String("repo_key", c.RepoKey),
					slog.Any("error", err))
				if firstErr == nil {
					firstErr = fmt.Errorf("orphan_sweep: wipe %s: %w", c.RepoKey, err)
				}
				return
			}
			deletedRows += int64(n)
			stateDeleted++
		}()
	}

	cr.excludedInFlight = len(excluded)
	cr.excludedKeys = excluded
	cr.rowsDeleted = deletedRows
	cr.stateRowsDeleted = stateDeleted

	slog.Info("orphan_sweep: path_missing complete",
		slog.Int("eligible_keys", cr.eligibleKeys),
		slog.Int("excluded_in_flight", cr.excludedInFlight),
		slog.Int64("rows_deleted", deletedRows),
		slog.Int("state_rows_deleted", stateDeleted),
	)

	if firstErr != nil {
		return cr, firstErr
	}
	return cr, nil
}

// sweepPathless is the category that deletes code_repo_state rows with an
// empty source_path — unidentifiable tombstones whose original root is gone
// and cannot be reconstructed. Deleting the state row cascades to its
// embeddings via the ON DELETE CASCADE trigger.
//
// No filesystem guard: there is no source_path to check. The safety argument
// is that the roots are genuinely gone (verified by hashing all git roots on
// the box against the repo_keys — only one of 43 resolved). A pathless key
// can never be re-indexed to the same repo_key because the root that hashed
// to it is neither recorded nor recoverable, so deleting it cannot destroy a
// live index — the next index of the same repo would mint a different
// repo_key.
//
// Returns (result, error). error is non-nil on a hard failure (list error,
// per-key wipe error).
func sweepPathless(ctx context.Context, dry bool, store orphanSweepStore, claimer indexSlotClaimer) (categoryResult, error) {
	cr := categoryResult{name: catPathless}

	repos, err := store.ListRepoKeysWithoutSourcePath(ctx)
	if err != nil {
		return cr, fmt.Errorf("orphan_sweep: pathless list: %w", err)
	}
	cr.candidateKeys = len(repos)

	if len(repos) == 0 {
		return cr, nil
	}

	if dry {
		for _, r := range repos {
			n, _ := store.CountEmbeddings(ctx, r.RepoKey)
			cr.rowsWouldDelete += int64(n)
		}
		slog.Info("orphan_sweep: pathless dry-run preview",
			slog.Int("pathless_keys", len(repos)),
			slog.Int64("rows_that_would_be_deleted", cr.rowsWouldDelete),
		)
		return cr, nil
	}

	// Real delete: claim-then-wipe per key.
	var excluded []string
	var deletedRows int64
	var stateDeleted int
	var firstErr error

	for _, r := range repos {
		release, won := claimer.ClaimIndexSlot(r.RepoKey)
		if !won {
			excluded = append(excluded, r.RepoKey)
			slog.Info("orphan_sweep: pathless excluded in-flight repoKey",
				slog.String("repo_key", r.RepoKey))
			continue
		}
		cr.eligibleKeys++
		func() {
			defer release()
			n, _ := store.CountEmbeddings(ctx, r.RepoKey)
			if err := store.WipeRepo(ctx, r.RepoKey); err != nil {
				slog.Error("orphan_sweep: pathless wipe failed",
					slog.String("repo_key", r.RepoKey),
					slog.Any("error", err))
				if firstErr == nil {
					firstErr = fmt.Errorf("orphan_sweep: wipe %s: %w", r.RepoKey, err)
				}
				return
			}
			deletedRows += int64(n)
			stateDeleted++
		}()
	}

	cr.excludedInFlight = len(excluded)
	cr.excludedKeys = excluded
	cr.rowsDeleted = deletedRows
	cr.stateRowsDeleted = stateDeleted

	slog.Info("orphan_sweep: pathless complete",
		slog.Int("eligible_keys", cr.eligibleKeys),
		slog.Int("excluded_in_flight", cr.excludedInFlight),
		slog.Int64("rows_deleted", deletedRows),
		slog.Int("state_rows_deleted", stateDeleted),
	)

	if firstErr != nil {
		return cr, firstErr
	}
	return cr, nil
}

// mountBlipGuard verifies that the parent directories of the path_missing
// candidates are live: each parent must exist and contain at least one entry
// that is a live git root. If any parent is missing, unreadable, or holds no
// live git root, the guard fails and the whole category is refused — a mount
// blip must not be distinguishable from a genuine deletion, so the safe
// default is to delete nothing.
//
// Returns (ok, reason). ok=true → safe to proceed. ok=false → refuse, reason
// explains which parent failed and why.
//
// The check is derived entirely from the filesystem — never from a hardcoded
// list of expected parents. Each distinct parent is checked once.
func mountBlipGuard(candidates []embeddings.RepoKeySourcePath, fs dirLister) (ok bool, reason string) {
	seen := make(map[string]bool)
	for _, c := range candidates {
		parent := filepath.Dir(c.SourcePath)
		if seen[parent] {
			continue
		}
		seen[parent] = true
		entries, err := fs.ReadDir(parent)
		if err != nil {
			return false, fmt.Sprintf("parent %s missing or unreadable: %v", parent, err)
		}
		hasLiveSibling := false
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			if fs.IsGitRoot(filepath.Join(parent, e.Name())) {
				hasLiveSibling = true
				break
			}
		}
		if !hasLiveSibling {
			return false, fmt.Sprintf("parent %s holds no live git root sibling", parent)
		}
	}
	return true, ""
}

// pathExistsDir reports whether path exists and is a directory, using the
// dirLister's ReadDir (a failed ReadDir means the path does not exist or is
// not a directory). Used by sweepPathMissing to filter candidates whose
// source_path is gone.
func pathExistsDir(fs dirLister, path string) bool {
	_, err := fs.ReadDir(path)
	return err == nil
}

// formatOrphanSweepResults renders the per-category results into a single
// text response. Each category is reported SEPARATELY — never merged into
// one number — so the caller can act on one without the other.
func formatOrphanSweepResults(dry bool, results []categoryResult) string {
	mode := "DRY RUN"
	if !dry {
		mode = "DELETED"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "orphan_sweep %s:", mode)
	for _, cr := range results {
		b.WriteByte('\n')
		fmt.Fprintf(&b, "  %s: candidate_keys=%d", cr.name, cr.candidateKeys)
		if cr.refused {
			fmt.Fprintf(&b, " REFUSED (%s)", cr.refusalReason)
			if cr.rowsWouldDelete > 0 {
				fmt.Fprintf(&b, " rows_that_would_be_deleted=%d", cr.rowsWouldDelete)
			}
			continue
		}
		if dry {
			fmt.Fprintf(&b, " rows_that_would_be_deleted=%d", cr.rowsWouldDelete)
		} else {
			fmt.Fprintf(&b, " eligible_keys=%d excluded_in_flight=%d rows_deleted=%d",
				cr.eligibleKeys, cr.excludedInFlight, cr.rowsDeleted)
			if cr.stateRowsDeleted > 0 {
				fmt.Fprintf(&b, " state_rows_deleted=%d", cr.stateRowsDeleted)
			}
			if len(cr.excludedKeys) > 0 {
				b.WriteString(formatExcluded(cr.excludedKeys))
			}
		}
	}
	if dry {
		b.WriteString("\npass dry_run=false to delete (in-flight indexes are excluded at delete time and named in the output)")
	}
	return b.String()
}

// formatExcluded renders the excluded key list for the response so a skip is
// never silent (#741 requirement 2). Empty → "".
func formatExcluded(excluded []string) string {
	if len(excluded) == 0 {
		return ""
	}
	return " excluded_keys=" + strings.Join(excluded, ",")
}
