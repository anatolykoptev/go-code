package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anatolykoptev/vaelor/internal/embeddings"
)

// ReconcilePathsInput is the input schema for the reconcile_paths tool.
//
// DryRun is a POINTER so that "omitted" is distinguishable from "explicitly
// false": omitted (nil) or true ⇒ dry-run preview (the SAFE DEFAULT — counts
// stale paths and the rows that would be deleted, with NO mutation); false ⇒
// perform the real DELETE. This mirrors OrphanSweepInput exactly — an
// operator should not have to learn two idioms for two cleanup tools.
type ReconcilePathsInput struct {
	DryRun *bool `json:"dry_run,omitempty" jsonschema:"Defaults to TRUE (preview only: for each repo with a source_path, counts file_paths that do not resolve under the repo root and the rows that would be deleted, with NO mutation). Set to false explicitly to perform the real DELETE of code_embeddings rows whose file_path does not resolve under the repo's root."`
}

// reconcilePathsStore is the subset of *embeddings.Store the handler needs.
// Defined as an interface so tests can supply a fake without a live Postgres
// pool (and so the dry-run path can assert no deletions occur). The interface
// carries ReconcileRepoPaths so the handler delegates to the SINGLE
// implementation in internal/embeddings — no parallel adapter (#708 round 2
// finding 2). *embeddings.Store satisfies it implicitly. Mirrors
// orphanSweepStore.
type reconcilePathsStore interface {
	ListRepoKeysWithSourcePath(ctx context.Context) ([]embeddings.RepoKeySourcePath, error)
	ReconcileRepoPaths(ctx context.Context, repoKey, sourcePath string, dryRun bool) (*embeddings.ReconcileResult, error)
}

// registerReconcilePaths registers the reconcile_paths MCP tool.
//
// The tool is only registered when a DB pool is available (DATABASE_URL
// configured), matching the gating pattern used by registerOrphanSweep.
//
// It is the intra-key counterpart to orphan_sweep: orphan_sweep deletes rows
// whose repo_key has no state row (whole-dead-key cleanup); reconcile_paths
// deletes rows whose file_path does not resolve under the repo's root
// (live-key stale-path cleanup, the #708 rename-collision class). Both are
// DRY-RUN BY DEFAULT and require an explicit dry_run=false to mutate.
func registerReconcilePaths(server *mcp.Server, deps SemanticDeps) {
	if deps.Store == nil {
		slog.Info("reconcile_paths: DATABASE_URL not set — tool disabled")
		return
	}

	addTool(server, &mcp.Tool{
		Name: "reconcile_paths",
		Description: "Operator-initiated: delete code_embeddings rows whose file_path does not resolve under the repo's root (source_path). " +
			"These stale rows accumulate when a checkout path is reused by a different project after a rename (#708: the code service went go-code → vaelor " +
			"and the Telegram agent went vaelor → vaelor-agent, so /host/src/vaelor's repo_key inherited the agent's rows). " +
			"Also runs automatically at index time; this tool is for operator-triggered cleanup without a shell on the host. " +
			"SAFETY: deletes embeddings rows only; never deletes code_repo_state rows. " +
			"If a repo's source_path does not exist on disk (mount blip), deletes NOTHING for that repo — a mount blip must not wipe a good index. " +
			"Repos with an empty source_path (pathless tombstones, 13.1% of the corpus) are skipped — they are a separate dispatch. " +
			"DRY-RUN BY DEFAULT: when dry_run is omitted (or true) the tool COUNTS the stale paths and the rows that would be deleted WITHOUT mutating anything — " +
			"pass dry_run=false to perform the real delete. " +
			"Idempotent: re-running when clean returns 0 deleted. " +
			"Progress is observable via gocode_index_stale_path_ratio{repo} on /metrics (port 9897).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ReconcilePathsInput) (*mcp.CallToolResult, error) {
		return handleReconcilePaths(ctx, in, deps.Store)
	})
}

// handleReconcilePaths is the extracted handler, callable from tests.
//
// Dry-run defaulting: dry := in.DryRun == nil || *in.DryRun — nil or true ⇒
// preview (safe default); false ⇒ real delete. Mirrors handleOrphanSweep.
//
// Iterates every repo with a non-empty source_path. For each, calls
// ReconcileRepoPaths (which contains the root-missing guard). Aggregates the
// results into a single text report.
func handleReconcilePaths(ctx context.Context, in ReconcilePathsInput, store reconcilePathsStore) (*mcp.CallToolResult, error) {
	slog.Info("reconcile_paths: starting")
	dry := in.DryRun == nil || *in.DryRun

	repos, err := store.ListRepoKeysWithSourcePath(ctx)
	if err != nil {
		return nil, fmt.Errorf("reconcile_paths: list repos: %w", err)
	}

	var (
		b          strings.Builder
		totalStale int64
		totalDel   int64
		skipped    int
		reconciled int
	)
	for _, r := range repos {
		res, rErr := store.ReconcileRepoPaths(ctx, r.RepoKey, r.SourcePath, dry)
		if rErr != nil {
			return nil, fmt.Errorf("reconcile_paths: repo %s: %w", r.RepoKey, rErr)
		}
		if res.Skipped {
			skipped++
			fmt.Fprintf(&b, "repo=%s SKIPPED (%s) total_rows=%d\n", res.RepoKey, res.SkipReason, res.TotalRows)
			continue
		}
		reconciled++
		totalStale += res.StaleRows
		if dry {
			fmt.Fprintf(&b, "repo=%s DRY RUN stale_paths=%d stale_rows=%d total_rows=%d\n",
				res.RepoKey, res.StalePaths, res.StaleRows, res.TotalRows)
		} else {
			totalDel += res.Deleted
			fmt.Fprintf(&b, "repo=%s DELETED stale_paths=%d rows_deleted=%d total_rows=%d\n",
				res.RepoKey, res.StalePaths, res.Deleted, res.TotalRows)
		}
	}

	mode := "DRY RUN"
	if !dry {
		mode = "DELETED"
	}
	summary := fmt.Sprintf("reconcile_paths %s: repos_reconciled=%d repos_skipped=%d stale_rows=%d rows_deleted=%d — %s",
		mode, reconciled, skipped, totalStale, totalDel, dryRunHint(dry))
	b.WriteString(summary)

	slog.Info("reconcile_paths: complete",
		slog.Int("repos_reconciled", reconciled),
		slog.Int("repos_skipped", skipped),
		slog.Int64("stale_rows", totalStale),
		slog.Int64("rows_deleted", totalDel))

	return textResult(b.String()), nil
}

// dryRunHint returns the operator instruction for forcing a real delete, or
// empty when already deleting.
func dryRunHint(dry bool) string {
	if dry {
		return "pass dry_run=false to delete"
	}
	return "done"
}
