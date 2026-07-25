package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/codegraph"
)

// ListFlowsInput is the input schema for the list_flows MCP tool.
type ListFlowsInput struct {
	Repo string `json:"repo" jsonschema:"Repository: GitHub slug (owner/repo), full GitHub URL, or absolute local host path (e.g. /home/user/src/project)"`
}

// registerListFlows registers the list_flows MCP tool.
//
// The tool is only registered when a codegraph Store is available (DATABASE_URL configured).
// It is a no-op (disabled) otherwise, matching the operator-gated pattern used by
// registerOrphanSweep and registerSparseBackfill.
func registerListFlows(server *mcp.Server, graphStore *codegraph.Store, deps SemanticDeps) {
	if graphStore == nil {
		slog.Info("list_flows: DATABASE_URL not set — tool disabled")
		return
	}

	addTool(server, &mcp.Tool{
		Name: "list_flows",
		Description: "List precomputed named execution flows for a repository, ordered by priority (highest-PageRank chains first). " +
			"Flows are community-clustered, entry-to-leaf call chains extracted at index time — providing a birds-eye view " +
			"of the main execution pipelines without running a live call trace. " +
			"Each flow has an entry-point (route handler or top-level exported function), a dominant leaf " +
			"(highest-PageRank callee), and a list of member symbols. " +
			"Requires DATABASE_URL and a prior code_graph/semantic_search index pass to populate code_flows. " +
			"Returns an empty result if the repo has not been indexed or has no flows.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ListFlowsInput) (*mcp.CallToolResult, error) {
		return handleListFlows(ctx, in, graphStore, deps.AnalyzeDeps)
	})
}

// handleListFlows is the extracted handler, callable from tests.
func handleListFlows(ctx context.Context, in ListFlowsInput, graphStore *codegraph.Store, deps analyze.Deps) (*mcp.CallToolResult, error) {
	root, cleanup, err := resolveRoot(ctx, in.Repo, "", deps)
	if err != nil {
		return errResult(fmt.Sprintf("list_flows: resolve repo: %s", err)), nil
	}
	if cleanup != nil {
		defer cleanup()
	}

	repoKey := codegraph.GraphNameFor(root)

	flows, err := graphStore.ListFlows(ctx, repoKey)
	if err != nil {
		return nil, fmt.Errorf("list_flows: %w", err)
	}

	return textResult(formatFlows(flows, root, repoKey)), nil
}

// flowChainMaxDisplay is the maximum number of intermediate chain members shown
// before truncating with a "… (N more)" suffix.
const flowChainMaxDisplay = 6

// formatFlows renders the flow list as a human-readable text report.
func formatFlows(flows []codegraph.Flow, root, repoKey string) string {
	if len(flows) == 0 {
		return fmt.Sprintf("list_flows: no flows found for %s (repo_key=%s).\n"+
			"Run code_graph or semantic_search on this repo first to trigger indexing.", root, repoKey)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Named execution flows for %s (%d flows, ordered by priority)\n", root, len(flows))
	sb.WriteString(strings.Repeat("─", 72) + "\n")

	for i, f := range flows {
		fmt.Fprintf(&sb, "%d. %s\n", i+1, f.Name)
		fmt.Fprintf(&sb, "   priority=%.4f  community=%s  members=%d\n",
			f.Priority, f.Community, len(f.MemberSyms))
		fmt.Fprintf(&sb, "   entry: %s  (%s)\n", f.EntrySym, f.EntryFile)
		fmt.Fprintf(&sb, "   leaf:  %s\n", f.LeafSym)
		if len(f.MemberSyms) > 2 {
			// Show intermediate reachable members (the full set is a community-bounded
			// reachable subtree, not a single linear call path — see ADR-001 §DFS semantics).
			intermediate := f.MemberSyms[1 : len(f.MemberSyms)-1]
			if len(intermediate) > 0 && len(intermediate) <= flowChainMaxDisplay {
				fmt.Fprintf(&sb, "   reaches: %s\n", strings.Join(intermediate, ", "))
			} else if len(intermediate) > flowChainMaxDisplay {
				fmt.Fprintf(&sb, "   reaches: %s … (%d more)\n",
					strings.Join(intermediate[:flowChainMaxDisplay], ", "), len(intermediate)-flowChainMaxDisplay)
			}
		}
		sb.WriteByte('\n')
	}

	return sb.String()
}
