package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/anatolykoptev/go-kit/cli"
	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/codegraph"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

// newSearchSubcommand builds the "vaelor search" subcommand. It takes two
// arguments — <repo> (owner/repo or local path) and <query> (natural-language
// search query) — and runs semantic_search directly against the DB + embed
// backend without a running MCP server. Reuses newSemanticDeps (the same
// wiring used by the MCP serve path) and handleSemanticSearch (the same
// handler used by the semantic_search MCP tool), so the result is identical
// to what an MCP client would receive.
func newSearchSubcommand(cfg Config) cli.SubcommandConfig {
	return cli.SubcommandConfig{
		Name:  "search",
		Short: "Semantic code search (no MCP server required)",
		Long:  "Runs semantic_search directly against the configured DB + embed backend. Takes <repo> (owner/repo or local path) and <query> (natural-language query). Works without a running MCP server.",
		Run: func(cmd *cobra.Command, args []string) {
			runSearch(cfg, args)
		},
	}
}

func runSearch(cfg Config, args []string) {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "search: <repo> and <query> arguments required")
		fmt.Fprintln(os.Stderr, "usage: vaelor search <owner/repo|path> <query>")
		os.Exit(2)
	}
	repo := args[0]
	query := strings.Join(args[1:], " ")

	if cfg.EmbedURL == "" {
		fmt.Fprintln(os.Stderr, "search: EMBED_URL is not set — semantic search is unavailable")
		os.Exit(1)
	}
	if cfg.DatabaseURL == "" {
		fmt.Fprintln(os.Stderr, "search: DATABASE_URL is not set — semantic search is unavailable")
		os.Exit(1)
	}

	ctx := context.Background()

	// Open the two pools (agePool + dataPool) exactly as registerTools does,
	// so newSemanticDeps receives the same inputs the MCP serve path uses.
	agePool, err := newGocodePool(ctx, cfg.DatabaseURL, agePoolMaxConns, true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "search: age pool connect failed: %v\n", err)
		os.Exit(1)
	}
	defer agePool.Close()

	dataPool, err := newGocodePool(ctx, cfg.DatabaseURL, dataPoolMaxConns, false)
	if err != nil {
		agePool.Close()
		fmt.Fprintf(os.Stderr, "search: data pool connect failed: %v\n", err)
		os.Exit(1)
	}
	defer dataPool.Close()

	// Ping the data pool so we fail fast with a clear message instead of a
	// per-query timeout when the DB is unreachable.
	if err := pingPool(ctx, dataPool); err != nil {
		fmt.Fprintf(os.Stderr, "search: database unreachable: %v\n", err)
		os.Exit(1)
	}

	// graphStore is optional — the search still works without AGE (graph
	// expansion and hotspot/recency arms are skipped). We skip the preflight
	// that registerTools runs at boot; the CLI path degrades gracefully.
	var graphStore *codegraph.Store
	graphStore = codegraph.NewStore(agePool)

	// Build a minimal analyze.Deps for resolveRoot (handleSemanticSearch calls
	// resolveRoot to turn owner/repo into a local checkout). The CLI path
	// reuses the same forge/workspace machinery as the MCP serve path.
	deps := analyze.Deps{
		GithubToken:    cfg.GithubToken,
		CloneTokenFunc: buildCloneTokenFunc(cfg),
		WorkspaceDir:   cfg.WorkspaceDir,
		PathMappings:   cfg.PathMappings,
		LocalRepoDirs:  autoIndexDirs(cfg),
		Forges:         buildForgeRegistry(cfg, nil),
	}
	deps.Graph, deps.Refs = buildGraphDeps(graphStore, cfg.PathMappings)

	rrfWeights := cfg.RRFWeights()
	semDeps := newSemanticDeps(cfg, deps, dataPool, agePool, graphStore, rrfWeights)

	if semDeps.Client == nil {
		fmt.Fprintln(os.Stderr, "search: embed backend unreachable — semantic search is unavailable")
		os.Exit(1)
	}

	result, err := handleSemanticSearch(ctx, SemanticSearchInput{
		Repo:  repo,
		Query: query,
	}, semDeps, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "search: %v\n", err)
		os.Exit(1)
	}
	if result.IsError {
		// Tool-level error: print to stderr and exit non-zero.
		fmt.Fprintln(os.Stderr, callToolResultText(result))
		os.Exit(1)
	}
	fmt.Print(callToolResultText(result))
}

// callToolResultText pulls the concatenated text content from a
// CallToolResult. Used by the CLI search subcommand to render the MCP result
// to stdout.
func callToolResultText(result *mcp.CallToolResult) string {
	var sb strings.Builder
	for _, c := range result.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

// pingPool verifies the pool can serve a trivial query so the CLI fails fast
// with a clear message instead of a per-operation timeout.
func pingPool(ctx context.Context, pool *pgxpool.Pool) error {
	var one int
	return pool.QueryRow(ctx, "SELECT 1").Scan(&one)
}
