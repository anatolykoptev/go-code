package main

import (
	"context"
	"fmt"
	"log/slog"

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
// pool (and so the dry-run path can assert DeleteOrphanRepoKeys is NOT called).
// *embeddings.Store satisfies it implicitly.
type orphanSweepStore interface {
	CountOrphanRepoKeys(ctx context.Context) (int64, error)
	PreviewOrphanRepoKeys(ctx context.Context) (repoKeys []string, rowCount int64, err error)
	DeleteOrphanRepoKeys(ctx context.Context) (int64, error)
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
			"Idempotent: re-running when clean returns 0 deleted. " +
			"Progress is observable via gocode_orphan_repo_keys on /metrics (port 9897).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in OrphanSweepInput) (*mcp.CallToolResult, error) {
		return handleOrphanSweep(ctx, in, deps.Store)
	})
}

// handleOrphanSweep is the extracted handler, callable from tests.
//
// Dry-run defaulting: dry := in.DryRun == nil || *in.DryRun — nil or true ⇒
// preview (safe default); false ⇒ real delete. The preview path counts the
// orphan repo_keys and the rows that would be deleted via the shared
// orphanRepoKeyPredicate in internal/embeddings, so preview and delete can
// never diverge. The real-delete path is the pre-existing behavior, unchanged
// except the response is now explicitly labelled DELETED.
func handleOrphanSweep(ctx context.Context, in OrphanSweepInput, store orphanSweepStore) (*mcp.CallToolResult, error) {
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
		return textResult(fmt.Sprintf(
			"orphan_sweep DRY RUN: orphan_repo_keys=%d rows_that_would_be_deleted=%d — pass dry_run=false to delete",
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

	deleted, err := store.DeleteOrphanRepoKeys(ctx)
	if err != nil {
		return nil, fmt.Errorf("orphan_sweep: %w", err)
	}

	// Update the gauge: after the sweep, orphan count should be 0.
	// Re-count to confirm (accounts for concurrent indexRepo adding new state rows).
	after, afterErr := store.CountOrphanRepoKeys(ctx)
	if afterErr != nil {
		slog.Warn("orphan_sweep: post-sweep count failed", slog.Any("error", afterErr))
		after = 0 // assume clean
	}
	embeddings.SetOrphanRepoKeysGauge(float64(after))

	slog.Info("orphan_sweep: complete",
		slog.Int64("orphan_keys_before", before),
		slog.Int64("rows_deleted", deleted),
		slog.Int64("orphan_keys_after", after),
	)

	return textResult(fmt.Sprintf(
		"orphan_sweep DELETED: orphan_repo_keys_before=%d rows_deleted=%d orphan_repo_keys_after=%d",
		before, deleted, after,
	)), nil
}
