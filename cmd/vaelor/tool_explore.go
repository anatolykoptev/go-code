package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/compare"
	"github.com/anatolykoptev/vaelor/internal/envdetect"
	"github.com/anatolykoptev/vaelor/internal/explore"
	"github.com/anatolykoptev/vaelor/internal/mcpmeta"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ExploreInput is the input schema for the explore tool.
type ExploreInput struct {
	Repo     string `json:"repo" jsonschema:"Repository: GitHub slug (owner/repo), full GitHub URL, or absolute local host path"`
	Language string `json:"language,omitempty" jsonschema:"Limit analysis to files of this language (e.g. go, python)"`
	Focus    string `json:"focus,omitempty" jsonschema:"Subdirectory path to limit scope (e.g. internal/auth), or space-separated keywords to filter by path components (e.g. 'auth middleware')"`
	MaxBytes int    `json:"max_bytes,omitempty" jsonschema:"Response budget in bytes (default 8192). When the response exceeds this, the ranked head is returned with a continuation footer."`
}

// exploreFreshnessSummary is the trimmed freshness view surfaced on explore
// output.
type exploreFreshnessSummary struct {
	DepFreshnessRatio float64 `json:"dep_freshness_ratio"`
	VulnDeps          int     `json:"vuln_deps"`
	TotalDeps         int     `json:"total_deps"`
}

// exploreOutput is explore's JSON shape: the base explore.Result plus two
// additive, omitempty blocks. Both are computed independently of
// explore.Run and of each other — neither can change the other's fields.
type exploreOutput struct {
	*explore.Result
	Freshness   *exploreFreshnessSummary `json:"freshness,omitempty"`
	Environment *envdetect.Environment   `json:"environment,omitempty"`
}

// freshnessTimeout bounds the best-effort dependency-freshness lookup so a
// slow registry call can never hold up explore's overall response.
const freshnessTimeout = 5 * time.Second

// buildExploreOutput runs explore plus its two additive enrichments
// (dependency freshness, ADR-0002-Phase-0 environment detection) against an
// already-resolved root and composes the final result. Extracted from the
// tool handler so both can be exercised directly in tests without going
// through MCP request/response marshaling or repo resolution.
func buildExploreOutput(ctx context.Context, root string, input ExploreInput) (*exploreOutput, error) {
	result, err := explore.Run(ctx, explore.Input{
		Root:     root,
		Language: input.Language,
		Focus:    input.Focus,
	})
	if err != nil {
		return nil, fmt.Errorf("explore: %w", err)
	}

	var fresh *compare.FreshnessStats
	{
		fctx, fcancel := context.WithTimeout(ctx, freshnessTimeout)
		defer fcancel()
		fresh, _, _ = compare.CollectFreshness(fctx, root)
	}

	// Phase 0 (ADR 0002): pure-static, unconditional — a file walk over the
	// clone already in hand, no flag/cold-path guard needed.
	env, _ := envdetect.Detect(ctx, root)

	output := &exploreOutput{Result: result}
	if fresh != nil {
		output.Freshness = &exploreFreshnessSummary{
			DepFreshnessRatio: fresh.DepFreshnessRatio,
			VulnDeps:          fresh.VulnDeps,
			TotalDeps:         fresh.TotalDeps,
		}
	}
	if env != nil && len(env.Toolchains) > 0 {
		output.Environment = env
	}

	return output, nil
}

func registerExplore(server *mcp.Server, _ Config, deps analyze.Deps) {
	addTool(server, &mcp.Tool{
		Name: "explore",
		Description: "Quick structured overview of a repository. " +
			"Returns file/symbol counts, language breakdown, top symbols by call frequency, " +
			"dead code summary, package list, health score (A-F), dependency freshness, and vulnerability count. " +
			"Use as a first step when encountering an unfamiliar codebase. " +
			"Fast (no LLM calls) — purely static analysis.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ExploreInput) (*mcp.CallToolResult, error) {
		return handleExplore(ctx, input, deps)
	})
}

// handleExplore is the extracted explore tool handler body, testable without
// MCP request/response marshaling. Applies the soft deadline, resolves the
// repo, runs the analysis, and renders a partial result + footer when the
// deadline fires (#534).
func handleExplore(ctx context.Context, input ExploreInput, deps analyze.Deps) (*mcp.CallToolResult, error) {
	if input.Repo == "" {
		return errResult("repo is required"), nil
	}

	t0 := time.Now()

	// Soft deadline: 25s default, strictly below the ~100s external MCP
	// proxy timeout. Applied before resolveRoot so the entire tool
	// (clone + analysis) is bounded — without it a large repo's Louvain
	// community detection runs unbounded past the proxy kill (#534).
	softCtx, softCancel := mcpmeta.SoftDeadlineWith(ctx, mcpmeta.SlowToolSoftDeadline)
	defer softCancel()

	root, cleanup, err := resolveRoot(softCtx, input.Repo, "", deps)
	if err != nil {
		if softCtx.Err() != nil {
			return softDeadlineResult(
				fmt.Sprintf("explore: timed out during repo resolution after %s — retry with a local path or narrower focus.", time.Since(t0).Round(time.Second)),
				"repo resolution (soft deadline)",
				time.Since(t0),
			), nil
		}
		return errResult(fmt.Sprintf("resolve repo: %s", err)), nil
	}
	defer cleanup()

	output, err := buildExploreOutput(softCtx, root, input)
	if err != nil {
		if softCtx.Err() != nil {
			return softDeadlineResult(
				fmt.Sprintf("explore: timed out after %s — partial overview, some stages skipped.", time.Since(t0).Round(time.Second)),
				"community detection, recent commits, coupled files (soft deadline)",
				time.Since(t0),
			), nil
		}
		return errResult(err.Error()), nil
	}

	// Fast path (no deadline hit): byte-identical JSON output.
	if softCtx.Err() == nil && (output.Result == nil || !output.Partial) {
		return jsonMarshalResult(output), nil
	}

	// Partial path: append a partial footer to the JSON text so the agent
	// sees both the partial data and what was truncated.
	data, mErr := json.Marshal(output)
	if mErr != nil {
		return errResult(fmt.Sprintf("marshal: %s", mErr)), nil
	}
	what := "community detection, recent commits, coupled files"
	if output.Result != nil && output.PartialReason != "" {
		what = output.PartialReason
	}
	text := string(data) + mcpmeta.PartialFooter(what+" (soft deadline)")
	return textResult(text), nil
}
