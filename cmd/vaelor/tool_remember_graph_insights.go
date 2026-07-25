package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/codegraph"
	"github.com/anatolykoptev/vaelor/internal/ingest"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RememberGraphInsightsInput is the input schema for remember_graph_insights.
// This tool is human-invoked only — never called by agents or routine automation.
type RememberGraphInsightsInput struct {
	Repo           string `json:"repo" jsonschema:"Repository: absolute local path or GitHub slug (owner/repo)"`
	MaxPerTemplate int    `json:"max_per_template,omitempty" jsonschema:"Max findings to persist per template (default 20, max 100)"`
}

const (
	defaultRememberMaxPerTemplate = 20
	maxRememberMaxPerTemplate     = 100
)

// rememberTemplates is the set of template IDs whose results are both
// queryable via ExecCypher and accepted by PersistInsights.
// "community_changes" is intentionally absent — it has no stable column shape
// and is not in PersistInsights's dispatch table (see persist_insights.go comment).
var rememberTemplates = []string{
	codegraph.TemplateInsightSurprises,
	codegraph.TemplateInsightDeadCode,
}

// registerRememberGraphInsights registers the remember_graph_insights MCP tool.
// If store is nil (DATABASE_URL not set) the tool is not registered.
func registerRememberGraphInsights(server *mcp.Server, cfg Config, deps analyze.Deps, store *codegraph.Store) {
	if store == nil {
		slog.Info("remember_graph_insights: DATABASE_URL not set, tool disabled")
		return
	}
	addTool(server, &mcp.Tool{
		Name: "remember_graph_insights",
		Description: "Human-invoked persistence: runs structural graph queries " +
			"(surprises, dead_code) on the repo and writes findings to the learnings " +
			"store so future understand calls surface them as prior_learnings. " +
			"Call ONLY when a human explicitly asks to 'remember' or 'bank' current " +
			"findings — NOT for routine inspection. Permanent dedupe by " +
			"(repo, symbol, flag): calling twice on unchanged findings is a no-op.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input RememberGraphInsightsInput) (*mcp.CallToolResult, error) {
		return handleRememberGraphInsights(ctx, input, cfg, deps, store)
	})
}

// rememberResult is the JSON response shape returned by the tool.
type rememberResult struct {
	Persisted map[string]int `json:"persisted"`
	Total     int            `json:"total"`
}

// handleRememberGraphInsights executes structural queries and persists findings.
func handleRememberGraphInsights(
	ctx context.Context,
	input RememberGraphInsightsInput,
	cfg Config,
	deps analyze.Deps,
	store *codegraph.Store,
) (*mcp.CallToolResult, error) {
	if input.Repo == "" {
		return errResult("repo is required"), nil
	}
	if deps.Learnings == nil {
		return errResult("learnings store not configured (LEARNINGS_DATABASE_URL unset)"), nil
	}

	limit := input.MaxPerTemplate
	if limit <= 0 {
		limit = defaultRememberMaxPerTemplate
	}
	if limit > maxRememberMaxPerTemplate {
		limit = maxRememberMaxPerTemplate
	}

	root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps)
	if err != nil {
		return errResult(fmt.Errorf("resolve repo: %w", err).Error()), nil
	}
	defer cleanup()

	isRemote := ingest.IsRemote(input.Repo)
	meta, err := codegraph.IndexRepo(ctx, store, root, isRemote, codegraph.IndexConfig{
		TTLLocal:            cfg.GraphTTLLocal,
		TTLRemote:           cfg.GraphTTLRemote,
		BatchSize:           cfg.GraphBatchSize,
		EnableSurpriseIndex: cfg.CodegraphSurpriseIndex,
		FlowsMax:            cfg.FlowsMax,
		FlowsDFSDepth:       cfg.FlowsDFSDepth,
	})
	if err != nil {
		return errResult(fmt.Errorf("index repo: %w", err).Error()), nil
	}

	counts := make(map[string]int, len(rememberTemplates))
	total := 0

	for _, templateID := range rememberTemplates {
		tmpl := codegraph.GetTemplate(templateID)
		if tmpl == nil {
			slog.Warn("remember_graph_insights: unknown template, skipping",
				slog.String("template", templateID))
			continue
		}

		cypher := tmpl.Render(map[string]string{"limit": fmt.Sprintf("%d", limit*2)})
		rows, execErr := store.ExecCypher(ctx, meta.GraphName, cypher, tmpl.Cols)
		if execErr != nil {
			slog.Warn("remember_graph_insights: exec failed, skipping template",
				slog.String("template", templateID),
				slog.Any("error", execErr))
			continue
		}

		// surprises rows from ExecCypher are raw (8-col); PersistInsights
		// expects the scored 6-col shape produced by PostProcessSurprises.
		if templateID == codegraph.TemplateInsightSurprises {
			rows, _ = codegraph.PostProcessSurprises(rows, limit)
		}

		if len(rows) > limit {
			rows = rows[:limit]
		}

		n := codegraph.PersistInsights(ctx, deps.Learnings, input.Repo, templateID, rows)
		counts[templateID] = n
		total += n
	}

	// Route through the shared jsonMarshalResult helper (helpers.go); the success
	// path is byte-identical (compact JSON) and the marshal-error branch is
	// unreachable for this all-serialisable result struct.
	return jsonMarshalResult(rememberResult{Persisted: counts, Total: total}), nil
}
