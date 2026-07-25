package main

import (
	"context"
	"fmt"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/forge"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// GithubCodeSearchInput is the input schema for the github_code_search tool.
type GithubCodeSearchInput struct {
	Query          string   `json:"query" jsonschema:"Code search query. Supports GitHub syntax: 'func resize language:python', 'className path:src/', 'TODO language:go'. Without repo qualifier searches all public repos."`
	Repo           string   `json:"repo,omitempty" jsonschema:"Repository to search (owner/repo or full GitHub URL). If empty, searches all public GitHub."`
	ExcludeRepos   []string `json:"exclude_repos,omitempty" jsonschema:"Repositories to exclude (owner/repo or full URL). Added as -repo: qualifiers."`
	Language       string   `json:"language,omitempty" jsonschema:"Filter results by language (e.g. go, python). Appended as language: qualifier if not already in query."`
	FileExtensions []string `json:"file_extensions,omitempty" jsonschema:"Filter results by file extension (e.g. go, ts). Added as extension: qualifiers. Leading dots are stripped."`
	Sort           string   `json:"sort,omitempty" jsonschema:"Sort field for code search. Only 'indexed' is supported by the GitHub API (default: best match)."`
	Order          string   `json:"order,omitempty" jsonschema:"Sort order: asc or desc (default: desc)."`
	MinStars       int      `json:"min_stars,omitempty" jsonschema:"Minimum stargazers count for the result's repository. Requires extra repo metadata calls; set per_page to 100 to get enough candidates."`
	PerPage        int      `json:"per_page,omitempty" jsonschema:"Results per page (default: 10, max: 100)"`
	Page           int      `json:"page,omitempty" jsonschema:"Page number for pagination (default: 1)"`
	MaxResults     int      `json:"max_results,omitempty" jsonschema:"Maximum results to return after server-side filtering. Capped at 1000 (100 when min_stars is used). May override per_page for efficiency."`
	// MaxFragmentChars limits each text-match fragment. 0 means no limit.
	MaxFragmentChars int `json:"max_fragment_chars,omitempty" jsonschema:"Max characters per code fragment. 0 or omitted means no limit."`
	// MaxTotalChars limits the total joined content per result. 0 means no limit.
	MaxTotalChars int `json:"max_total_chars,omitempty" jsonschema:"Max total characters of joined fragments per result. 0 or omitted means no limit."`
}

// githubCodeSearchResult is a single search result.
type githubCodeSearchResult struct {
	Path      string `json:"path"`
	Repo      string `json:"repo"`
	URL       string `json:"url"`
	Fragments string `json:"fragments,omitempty"`
}

// githubCodeSearchOutput is the tool output.
type githubCodeSearchOutput struct {
	Query             string                   `json:"query"`
	Count             int                      `json:"count"`
	Total             int                      `json:"total"`
	IncompleteResults bool                     `json:"incomplete_results"`
	Results           []githubCodeSearchResult `json:"results"`
}

func registerGithubCodeSearch(server *mcp.Server, _ Config, deps analyze.Deps) {
	addTool(server, &mcp.Tool{
		Name: "github_code_search",
		Description: "Search code on GitHub using the Code Search API. Returns file paths with matching code fragments. " +
			"Use this instead of web_url_read for GitHub search URLs. " +
			"Supports GitHub search syntax: 'func resize language:python', 'className path:src/'. " +
			"Requires GITHUB_TOKEN for higher rate limits.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input GithubCodeSearchInput) (*mcp.CallToolResult, error) {
		return handleGithubCodeSearch(ctx, input, deps)
	})
}

func handleGithubCodeSearch(ctx context.Context, input GithubCodeSearchInput, deps analyze.Deps) (*mcp.CallToolResult, error) {
	if input.Query == "" {
		return errResult("query is required"), nil
	}
	if deps.Forges == nil {
		return errResult("github_code_search: no forge configured"), nil
	}
	gh := deps.Forges.Get(forge.GitHub)
	if gh == nil {
		return errResult("github_code_search: GitHub forge not configured"), nil
	}

	var repos []string
	if input.Repo != "" {
		normalized, err := forge.NormalizeGitHubRepo(input.Repo)
		if err != nil {
			return errResult(fmt.Sprintf("invalid repo: %q", input.Repo)), nil
		}
		repos = []string{normalized}
	}

	opts := forge.SearchCodeOptions{
		ExcludeRepos:     input.ExcludeRepos,
		FileExtensions:   input.FileExtensions,
		Language:         input.Language,
		Sort:             input.Sort,
		Order:            input.Order,
		MinStars:         input.MinStars,
		MaxResults:       input.MaxResults,
		PerPage:          input.PerPage,
		Page:             input.Page,
		MaxFragmentChars: input.MaxFragmentChars,
		MaxTotalChars:    input.MaxTotalChars,
	}

	result, err := gh.SearchCode(ctx, input.Query, repos, opts)
	if err != nil {
		msg := fmt.Sprintf("github code search: %s", err)
		// #567: GitHub Code Search times out (HTTP 408) or 5xx on complex
		// queries — the forge layer already retries once; surface a hint to
		// simplify the query (fewer OR operators) so the agent pivots instead
		// of retrying the same shape.
		if forge.IsTransientAPIError(err) {
			msg += " — tip: simplify the query (drop OR operators / narrow terms) and retry"
		}
		return errResult(msg), nil
	}

	out := githubCodeSearchOutput{
		Query:             result.Query,
		Count:             len(result.Results),
		Total:             result.Total,
		IncompleteResults: result.Incomplete,
		Results:           make([]githubCodeSearchResult, 0, len(result.Results)),
	}

	for _, r := range result.Results {
		out.Results = append(out.Results, githubCodeSearchResult{
			Path:      r.Path,
			Repo:      r.Repo,
			URL:       r.URL,
			Fragments: r.Content,
		})
	}

	return jsonMarshalResult(out), nil
}
