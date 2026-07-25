package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"time"

	"github.com/anatolykoptev/go-kit/embed"
	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/codegraph"
	"github.com/anatolykoptev/vaelor/internal/compare"
	"github.com/anatolykoptev/vaelor/internal/mcpmeta"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// CodeCompareInput is the input schema for the code_compare tool.
type CodeCompareInput struct {
	RepoA    string `json:"repo_a" jsonschema:"First repository: GitHub slug (owner/repo), full GitHub URL, or absolute local host path (e.g. /home/user/src/project)"`
	RepoB    string `json:"repo_b" jsonschema:"Second repository: GitHub slug (owner/repo), full GitHub URL, or absolute local host path (e.g. /home/user/src/project)"`
	Query    string `json:"query,omitempty" jsonschema:"What to compare — quality aspects, architectural patterns, specific concerns (default: general comparison)"`
	Focus    string `json:"focus,omitempty" jsonschema:"Subdirectory path to limit comparison scope (e.g. internal/auth, pkg/api), or space-separated keywords (e.g. 'auth handler'). Use query for topic focus."`
	Language string `json:"language,omitempty" jsonschema:"Limit comparison to files of this language (e.g. go, python, rust)"`
	MaxBytes int    `json:"max_bytes,omitempty" jsonschema:"Response budget in bytes (default 8192). When the response exceeds this, the ranked head is returned with a continuation footer."`
}

// registerCodeCompare registers the code_compare MCP tool.
func registerCodeCompare(server *mcp.Server, cfg Config, deps analyze.Deps, semDeps *SemanticDeps, graphStore *codegraph.Store) {
	outputDir := cfg.OutputDir

	addTool(server, &mcp.Tool{
		Name: "code_compare",
		Description: "Compare two code repositories to find the better implementation. " +
			"Analyzes architecture, code quality, patterns, and identifies missing features. " +
			"Returns XML with quality verdicts, coverage gaps, architecture insights, " +
			"metrics, and actionable recommendations. Works cross-language.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CodeCompareInput) (*mcp.CallToolResult, error) {
		t0 := time.Now()
		if input.RepoA == "" || input.RepoB == "" {
			return errResult("repo_a and repo_b are required"), nil
		}
		if input.Query == "" {
			input.Query = "Compare architecture, code quality, patterns, and identify missing features"
		}

		// Soft deadline: 25s default, strictly below the ~100s external MCP
		// proxy timeout. Applied BEFORE resolveRoot so the entire tool
		// (clone + snapshot + match + enrichment) is bounded — the #566 bug
		// was the deadline applied AFTER clone, so clone ate the proxy
		// budget and the 25s compute window fired at ~95s = the proxy kill.
		softCtx, softCancel := mcpmeta.SoftDeadlineWith(ctx, mcpmeta.SlowToolSoftDeadline)
		defer softCancel()

		rootA, cleanupA, err := resolveRoot(softCtx, input.RepoA, "", deps)
		if err != nil {
			if softCtx.Err() != nil {
				return softDeadlineResult(
					fmt.Sprintf("code_compare: timed out resolving repo_a after %s — retry with a local path or narrower focus.", time.Since(t0).Round(time.Second)),
					"repo_a resolution (soft deadline)",
					time.Since(t0),
				), nil
			}
			return errResult(fmt.Sprintf("resolve repo_a: %s", err)), nil
		}
		defer cleanupA()

		rootB, cleanupB, err := resolveRoot(softCtx, input.RepoB, "", deps)
		if err != nil {
			if softCtx.Err() != nil {
				return softDeadlineResult(
					fmt.Sprintf("code_compare: timed out resolving repo_b after %s — retry with a local path or narrower focus.", time.Since(t0).Round(time.Second)),
					"repo_b resolution (soft deadline)",
					time.Since(t0),
				), nil
			}
			return errResult(fmt.Sprintf("resolve repo_b: %s", err)), nil
		}
		defer cleanupB()

		var embedClient *embed.Client
		if semDeps != nil {
			embedClient = semDeps.Client
		}

		result, err := compare.CompareRepos(softCtx, compare.CompareInput{
			RootA:       rootA,
			RootB:       rootB,
			Query:       input.Query,
			OxCodes:     deps.OxCodes,
			EmbedClient: embedClient,
			GraphStore:  graphStore,
			ParseCache:  deps.ParseCache,
			Opts: compare.SnapshotOpts{
				Focus:    input.Focus,
				Language: input.Language,
			},
		}, deps.LLM)

		elapsed := time.Since(t0)
		// If the soft deadline fired, compare returns either a partial
		// result (err == nil but incomplete) or a context-canceled error.
		// In the error case, we have no result to return — surface a
		// clear partial message instead of nothing.
		if softCtx.Err() != nil {
			logSoftDeadlineHit("code_compare", elapsed)
			if err != nil {
				return softDeadlineResult(
					fmt.Sprintf("code_compare: timed out after %s — snapshots/metrics computed partially, LLM analysis skipped. Retry with a narrower focus or language filter.", elapsed.Round(time.Second)),
					"LLM analysis, route diff, enrichment (soft deadline)",
					elapsed,
				), nil
			}
			// Partial result available — render it with a partial footer.
			xmlOut := buildCompareXML(result)
			data, mErr := xml.Marshal(xmlOut)
			if mErr != nil {
				return softDeadlineResult(
					fmt.Sprintf("code_compare: timed out after %s — partial result marshal failed: %s", elapsed.Round(time.Second), mErr),
					"XML marshal of partial result",
					elapsed,
				), nil
			}
			text := xml.Header + string(data)
			return shapedPartialResult(text, budgetOverride(input.MaxBytes),
				"narrow with focus= or language=",
				"some enrichment/LLM stages skipped (soft deadline)", elapsed), nil
		}

		if err != nil {
			return errResult(fmt.Sprintf("compare: %s", err)), nil
		}

		return xmlMarshalFileResult(buildCompareXML(result), "code_compare", outputDir), nil
	})
}
