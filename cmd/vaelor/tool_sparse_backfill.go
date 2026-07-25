package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anatolykoptev/vaelor/internal/codegraph"
	"github.com/anatolykoptev/vaelor/internal/embeddings"
)

// SparseBackfillInput is the input schema for the sparse_backfill tool.
type SparseBackfillInput struct {
	RepoKey string `json:"repo_key,omitempty" jsonschema:"Optional: scope backfill to a single repo key (e.g. 'code_a3f2b1c0'). Leave empty to backfill all repos with NULL sparse_embedding rows."`
}

// registerSparseBackfill registers the sparse_backfill MCP tool.
// The tool is only registered when SPARSE_EMBED_URL is configured and the DB
// pool is available — it is a no-op (disabled) otherwise, matching the gating
// pattern used by registerCodeGraph and registerSemanticSearch.
func registerSparseBackfill(server *mcp.Server, cfg Config, deps SemanticDeps) {
	if deps.SparseClient == nil {
		slog.Info("sparse_backfill: SPARSE_EMBED_URL not set — tool disabled")
		return
	}
	if deps.Store == nil {
		slog.Info("sparse_backfill: DATABASE_URL not set — tool disabled")
		return
	}

	addTool(server, &mcp.Tool{
		Name: "sparse_backfill",
		Description: "Operator-initiated: populate sparse_embedding for existing code_embeddings rows where it is NULL. " +
			"Reads each symbol's source from disk, recomputes the body hash, skips rows where the hash " +
			"has drifted from the indexed value (those rows self-heal on the next incremental index). " +
			"Idempotent and resumable — re-running picks up exactly the rows still NULL. " +
			"Requires SPARSE_EMBED_URL to be configured. " +
			"Progress is observable via gocode_sparse_backfill_total{outcome} and " +
			"gocode_sparse_backfill_remaining on /metrics (port 9897). " +
			"Outcomes: backfilled | skipped_drift | skipped_missing | embed_failed.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input SparseBackfillInput) (*mcp.CallToolResult, error) {
		return handleSparseBackfill(ctx, input, cfg, deps)
	})
}

// handleSparseBackfill is the extracted handler, callable from tests.
func handleSparseBackfill(
	ctx context.Context,
	input SparseBackfillInput,
	cfg Config,
	deps SemanticDeps,
) (*mcp.CallToolResult, error) {
	slog.Info("sparse_backfill: starting",
		slog.String("repo_key", input.RepoKey),
		slog.Any("auto_index_dirs", cfg.AutoIndexDirs),
	)

	rootLookup := buildRepoRootLookup(autoIndexDirs(cfg))

	result, err := deps.Store.BackfillSparse(ctx, deps.SparseClient, embeddings.BackfillOpts{
		RepoKey:           input.RepoKey,
		RepoRootLookup:    rootLookup,
		WriteSparsesBatch: deps.Store.UpdateSparseEmbeddingsBatch,
	})
	if err != nil {
		return nil, fmt.Errorf("sparse_backfill: %w", err)
	}

	return textResult(fmt.Sprintf(
		"sparse_backfill complete: backfilled=%d skipped_drift=%d skipped_missing=%d embed_failed=%d total_examined=%d",
		result.Backfilled, result.SkippedDrift, result.SkippedMiss, result.EmbedFailed, result.Total,
	)), nil
}

// buildRepoRootLookup scans localRepoDirs and builds a closure that maps a
// repo_key (as produced by codegraph.GraphNameFor) to its absolute disk root.
// Only immediate subdirectories containing a .git folder are considered — same
// filter as repofind.Discover.
//
// This function lives in cmd/ (not internal/embeddings) because it depends on
// codegraph.GraphNameFor. Injecting the lookup as a closure avoids an import
// cycle between internal/embeddings and internal/codegraph.
func buildRepoRootLookup(localRepoDirs []string) func(repoKey string) (string, bool) {
	m := make(map[string]string)
	for _, dir := range localRepoDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			slog.Debug("sparse_backfill: skip dir", slog.String("dir", dir), slog.Any("error", err))
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			root := filepath.Join(dir, e.Name())
			if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
				continue // not a git repo
			}
			key := codegraph.GraphNameFor(root)
			m[key] = root
		}
	}
	slog.Info("sparse_backfill: repo root map built",
		slog.Int("repos_found", len(m)),
		slog.Any("dirs", localRepoDirs),
	)
	return func(repoKey string) (string, bool) {
		root, ok := m[repoKey]
		return root, ok
	}
}
