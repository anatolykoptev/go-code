package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anatolykoptev/vaelor/internal/embeddings"
)

// OrphanSweepInput is the input schema for the orphan_sweep tool.
//
// DryRun is a POINTER so that "omitted" is distinguishable from "explicitly
// false": omitted (nil) or true ⇒ dry-run preview (the SAFE DEFAULT — counts
// orphan repo_keys and the rows that would be deleted, no mutation); false ⇒
// perform the real bulk DELETE. A plain bool would default to false=delete,
// the opposite of the safe default — do not use a plain bool.
type OrphanSweepInput struct {
	DryRun *bool `json:"dry_run,omitempty" jsonschema:"Defaults to TRUE (preview only: counts orphan repo_keys and the rows that would be deleted, with NO mutation). Set to false explicitly to perform the real bulk DELETE of code_embeddings rows whose repo_key has no matching code_repo_state row."`
}

// orphanSweepStore is the subset of *embeddings.Store the handler needs.
// Defined as an interface so tests can supply a fake without a live Postgres
// pool (and so the dry-run path can assert no delete is called).
// *embeddings.Store satisfies it implicitly.
//
// The per-key methods (CountOrphanRepoKeysForRepo, DeleteOrphanRepoKeysForRepo)
// are the surface the in-flight guard drives: the handler claims a repoKey's
// index slot, then counts and deletes that key's orphan rows through these.
// They share orphanRepoKeyForRepoPredicate so the per-key count and the
// per-key delete can never diverge (the same invariant the bulk
// orphanRepoKeyPredicate enforces — see store.go:549-554).
type orphanSweepStore interface {
	CountOrphanRepoKeys(ctx context.Context) (int64, error)
	PreviewOrphanRepoKeys(ctx context.Context) (repoKeys []string, rowCount int64, err error)
	DeleteOrphanRepoKeys(ctx context.Context) (int64, error)
	CountOrphanRepoKeysForRepo(ctx context.Context, repoKey string) (int64, error)
	DeleteOrphanRepoKeysForRepo(ctx context.Context, repoKey string) (int64, error)
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
// it. A nil claimer (no Pipeline wired) disables the guard and the sweep
// falls back to the legacy per-key delete without exclusion reporting — this
// branch never ships that way (registerOrphanSweep passes deps.Pipeline), but
// the nil-handling keeps the handler safe if the wiring is ever loosened.
type indexSlotClaimer interface {
	ClaimIndexSlot(repoKey string) (release func(), won bool)
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
// In-flight guard (#741): the real-delete path claims each candidate repoKey's
// index slot via deps.Pipeline before deleting it, so a first index that is
// committing chunks but has not yet written its code_repo_state row cannot have
// its rows deleted underneath it. Keys whose slot is already held by an
// indexer are excluded and reported in the output (a silent skip is the mirror
// of the bug). Dry run does NOT claim — see handleOrphanSweep's DRY-RUN
// DECISION comment.
func registerOrphanSweep(server *mcp.Server, deps SemanticDeps) {
	if deps.Store == nil {
		slog.Info("orphan_sweep: DATABASE_URL not set — tool disabled")
		return
	}

	addTool(server, &mcp.Tool{
		Name: "orphan_sweep",
		Description: "Operator-initiated: delete code_embeddings rows whose repo_key has no matching code_repo_state row. " +
			"These orphans accumulate when worktrees are removed without cleanup, or when a repo's checkout path " +
			"changes (GraphNameFor hashes the root path, so a new path mints a new repo_key and the old snapshot " +
			"is never cleaned up). " +
			"SAFETY: deletes embeddings-keys-not-in-state only; never deletes code_repo_state rows. " +
			"The intra-key orphan reconciliation that runs automatically in indexRepo handles per-symbol cleanup; " +
			"this tool handles entire-repo_key cleanup. " +
			"DRY-RUN BY DEFAULT: when dry_run is omitted (or true) the tool COUNTS the orphan repo_keys and the " +
			"rows that would be deleted WITHOUT mutating anything — pass dry_run=false to perform the real delete. " +
			"IN-FLIGHT GUARD: the real delete claims each candidate's index slot first; a repoKey whose slot is " +
			"held by an in-flight indexer is EXCLUDED and named in the output (a silent skip is the mirror of the " +
			"bug). Idempotent: re-running when clean returns 0 deleted. " +
			"Progress is observable via gocode_orphan_repo_keys on /metrics (port 9897).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in OrphanSweepInput) (*mcp.CallToolResult, error) {
		return handleOrphanSweep(ctx, in, deps.Store, deps.Pipeline)
	})
}

// handleOrphanSweep is the extracted handler, callable from tests.
//
// Dry-run defaulting: dry := in.DryRun == nil || *in.DryRun — nil or true ⇒
// preview (safe default); false ⇒ real delete.
//
// DRY-RUN DECISION (#741 requirement 4): the dry run does NOT claim index
// slots. Rationale: a dry run must not mutate anything AND must not block
// indexers — claiming a slot blocks that repoKey from indexing for the
// duration of the preview, which is a side effect on a non-mutating path. The
// consequence is that a dry-run preview can name a key that the real delete
// would skip (an indexer that starts between the dry run and the real delete
// owns the slot at delete time). That difference is made VISIBLE in the
// output rather than silent: the dry-run response explicitly notes that
// in-flight keys are excluded only at delete time, and the real-delete
// response names every excluded key and reports excluded_in_flight=N. The
// per-key SQL predicate is shared between count and delete
// (orphanRepoKeyForRepoPredicate) so the dry-run row counts and the real
// delete row counts cannot diverge for a given key under the same in-flight
// state — the divergence that wiped 15076 rows (store.go:549-554) was a SQL
// predicate divergence, not a runtime-guard divergence, and that class is
// still closed.
//
// Real-delete flow (claim-then-delete, #741):
//  1. Preview the candidate repoKeys (all orphans, no guard).
//  2. For each candidate: claim its index slot. won → eligible; lost →
//     excluded (recorded). Release is deferred per-key so a delete error or a
//     panic cannot leak a claim (a leaked claim blocks that repoKey from ever
//     indexing again for the process lifetime — requirement 3).
//  3. For each eligible key: count its orphan rows, delete them, accumulate.
//  4. Report deleted rows, eligible keys, and excluded keys (names + count).
//
// Exclusion reporting (#741 requirement 2): excluded keys appear in the
// response text AND in the excluded_in_flight counter. A skip nobody can see
// is the same class of defect as the delete nobody can see.
func handleOrphanSweep(ctx context.Context, in OrphanSweepInput, store orphanSweepStore, claimer indexSlotClaimer) (*mcp.CallToolResult, error) {
	slog.Info("orphan_sweep: starting")
	dry := in.DryRun == nil || *in.DryRun

	if dry {
		repoKeys, rowCount, err := store.PreviewOrphanRepoKeys(ctx)
		if err != nil {
			return nil, fmt.Errorf("orphan_sweep: preview: %w", err)
		}
		// Keep the gauge truthful: a dry-run still observes the real orphan count.
		embeddings.SetOrphanRepoKeysGauge(float64(len(repoKeys)))
		slog.Info("orphan_sweep: dry-run preview",
			slog.Int("orphan_repo_keys", len(repoKeys)),
			slog.Int64("rows_that_would_be_deleted", rowCount),
		)
		// The dry run does NOT claim, so it reports the unguarded candidate set.
		// The output flags that in-flight keys are excluded only at delete time
		// so the operator can see the difference between the preview and a real
		// delete that lands inside an index window.
		return textResult(fmt.Sprintf(
			"orphan_sweep DRY RUN: orphan_repo_keys=%d rows_that_would_be_deleted=%d — "+
				"pass dry_run=false to delete (in-flight indexes are excluded at delete time and named in the output) — "+
				"dry_run=false",
			len(repoKeys), rowCount,
		)), nil
	}

	// Real delete path (dry_run=false).

	// Snapshot the orphan count before the delete so the result is informative.
	before, countErr := store.CountOrphanRepoKeys(ctx)
	if countErr != nil {
		slog.Warn("orphan_sweep: pre-sweep count failed (continuing)", slog.Any("error", countErr))
		before = -1 // unknown
	}

	// Discover the candidate set via the shared orphan predicate.
	candidates, _, err := store.PreviewOrphanRepoKeys(ctx)
	if err != nil {
		return nil, fmt.Errorf("orphan_sweep: preview candidates: %w", err)
	}

	// Claim-then-delete per key. eligible = keys whose slot we won and will
	// delete; excluded = keys whose slot an in-flight indexer holds and we skip.
	var eligible []string
	var excluded []string
	var deletedTotal int64
	var firstErr error

	for _, repoKey := range candidates {
		release, won := claimOrSkip(claimer, repoKey)
		if !won {
			excluded = append(excluded, repoKey)
			slog.Info("orphan_sweep: excluded in-flight repoKey",
				slog.String("repo_key", repoKey))
			continue
		}
		// Release is deferred per-key so a delete error, a panic, or a
		// continue below cannot leak the claim. A leaked claim blocks this
		// repoKey from ever indexing again for the process lifetime (#741
		// requirement 3) — defer per key, not one defer for the loop.
		eligible = append(eligible, repoKey)
		func() {
			defer release()
			// Count then delete through the shared per-key predicate
			// (orphanRepoKeyForRepoPredicate) so the per-key count and the
			// per-key delete cannot disagree for this key — the guard for
			// requirement 1 applied to the per-key path. A future change that
			// updates one predicate and not the other reddens
			// TestHandleOrphanSweep_PreviewCountDeleteAgree.
			if _, err := store.CountOrphanRepoKeysForRepo(ctx, repoKey); err != nil {
				slog.Warn("orphan_sweep: per-key count failed (continuing to delete)",
					slog.String("repo_key", repoKey),
					slog.Any("error", err))
			}
			n, err := store.DeleteOrphanRepoKeysForRepo(ctx, repoKey)
			if err != nil {
				slog.Error("orphan_sweep: per-key delete failed",
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

	if firstErr != nil {
		// All claims for already-processed keys were released by their defers;
		// remaining candidates were not claimed. Surface the first failure so
		// the operator sees the delete did not complete cleanly.
		return nil, firstErr
	}

	// Update the gauge: after the sweep, orphan count should reflect survivors.
	after, afterErr := store.CountOrphanRepoKeys(ctx)
	if afterErr != nil {
		slog.Warn("orphan_sweep: post-sweep count failed", slog.Any("error", afterErr))
		after = 0 // assume clean
	}
	embeddings.SetOrphanRepoKeysGauge(float64(after))

	slog.Info("orphan_sweep: complete",
		slog.Int64("orphan_keys_before", before),
		slog.Int("eligible_keys", len(eligible)),
		slog.Int("excluded_in_flight", len(excluded)),
		slog.Int64("rows_deleted", deletedTotal),
		slog.Int64("orphan_keys_after", after),
	)

	return textResult(fmt.Sprintf(
		"orphan_sweep DELETED: orphan_repo_keys_before=%d eligible_keys=%d excluded_in_flight=%d rows_deleted=%d orphan_repo_keys_after=%d%s",
		before, len(eligible), len(excluded), deletedTotal, after, formatExcluded(excluded),
	)), nil
}

// claimOrSkip claims repoKey's index slot via the claimer. When claimer is nil
// (no Pipeline wired — not the shipped configuration) the guard is disabled
// and the key is treated as eligible with a no-op release. This keeps the
// handler safe if the wiring is ever loosened, but registerOrphanSweep always
// passes deps.Pipeline so the guard is active in production.
func claimOrSkip(claimer indexSlotClaimer, repoKey string) (release func(), won bool) {
	if claimer == nil {
		return func() {}, true
	}
	return claimer.ClaimIndexSlot(repoKey)
}

// formatExcluded renders the excluded key list for the response so a skip is
// never silent (#741 requirement 2). Empty → "".
func formatExcluded(excluded []string) string {
	if len(excluded) == 0 {
		return ""
	}
	return " excluded_keys=" + strings.Join(excluded, ",")
}
