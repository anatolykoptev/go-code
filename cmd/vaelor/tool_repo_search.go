package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	kitcache "github.com/anatolykoptev/go-kit/cache"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/cache"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	maxReposToEnrich = 12
	maxReadmeRunes   = 3000
)

const systemPromptRepoSearch = `You are recommending GitHub repositories based on search results.
For each relevant repository, explain:
1. What it does and why it's relevant to the query
2. Key stats (stars, language, last activity)
3. Notable features from the README

Rank by relevance to the query. Be concise. Include GitHub URLs.`

// RepoSearchInput is the input schema for the repo_search tool.
type RepoSearchInput struct {
	Query    string `json:"query" jsonschema:"What repositories to find. Supports GitHub syntax: 'language:go topic:ai', 'stars:>100'"`
	Language string `json:"language,omitempty" jsonschema:"Filter by programming language"`
	Sort     string `json:"sort,omitempty" jsonschema:"Sort by: stars, forks, updated"`
}

// repoHit holds a search result before enrichment.
type repoHit struct {
	Owner string
	Repo  string
	URL   string
}

// enrichedRepo holds enriched repo data.
type enrichedRepo struct {
	Owner       string
	Repo        string
	Description string
	Stars       int
	Language    string
	Topics      []string
	LastPush    string
	Archived    bool
	Readme      string
}

// registerRepoSearch registers the repo_search MCP tool.
func registerRepoSearch(server *mcp.Server, _ Config, deps analyze.Deps) {
	addTool(server, &mcp.Tool{
		Name: "repo_search",
		Description: "Discover repositories for a task or technology. " +
			"Searches web + GitHub/GitLab APIs, enriches with metadata (stars, language, topics), " +
			"fetches READMEs, and returns LLM-summarized recommendations.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input RepoSearchInput) (*mcp.CallToolResult, error) {
		return handleRepoSearch(ctx, input, deps)
	})
}

// handleRepoSearch is the extracted handler for repo_search, callable from tests.
func handleRepoSearch(ctx context.Context, input RepoSearchInput, deps analyze.Deps) (*mcp.CallToolResult, error) {
	if input.Query == "" {
		return errResult("query is required"), nil
	}

	// Gate: repo_search requires LLM to summarize results.
	if !deps.LLMHasKey {
		return errResult("repo_search: requires LLM_API_KEY to be set"), nil
	}

	// Apply language filter to query if provided.
	query := input.Query
	if input.Language != "" {
		query = fmt.Sprintf("%s language:%s", query, input.Language)
	}

	// Check cache.
	cacheKey := cache.Key("repo_search", query, input.Sort)
	if cached, ok, _ := kitcache.GetJSON[string](deps.ToolCache, ctx, cacheKey); ok {
		return textResult(cached), nil
	}

	// Step 1: Parallel search with query relaxation on empty results.
	repos := parallelRepoSearch(ctx, query, input.Sort, deps)
	if len(repos) == 0 {
		for _, relaxed := range relaxQuery(query) {
			repos = parallelRepoSearch(ctx, relaxed, input.Sort, deps)
			if len(repos) > 0 {
				slog.Info("repo_search: relaxed query found results", "original", query, "relaxed", relaxed)
				break
			}
		}
	}
	if len(repos) == 0 {
		return textResult("No repositories found for: " + input.Query), nil
	}

	// Step 2: Dedup by owner/repo.
	unique := deduplicateRepoResults(repos)

	// Step 3: Parallel enrich.
	enriched := enrichRepoResults(ctx, unique, deps)

	// Step 4: Build text for LLM.
	repoText := buildRepoSearchContext(enriched)

	// Step 5: LLM summarize.
	prompt := fmt.Sprintf("Query: %s\n\nRepositories found:\n%s", input.Query, repoText)
	summary, err := deps.LLM.Complete(ctx, systemPromptRepoSearch, prompt)
	if err != nil {
		slog.Warn("repo_search: LLM summarization failed, returning raw data", "err", err)
		result := fmt.Sprintf("# Repository Search: %s\n\n%s", input.Query, repoText)
		return textResult(result), nil
	}

	result := fmt.Sprintf("# Repository Search: %s\n\n%s", input.Query, summary)
	if err := kitcache.SetJSONWithTTL(deps.ToolCache, ctx, cacheKey, result, 24*time.Hour); err != nil {
		slog.Warn("repo_search: failed to cache result", "key", cacheKey, "err", err)
	}
	return textResult(result), nil
}

// parallelRepoSearch runs three searches concurrently and merges results.
func parallelRepoSearch(ctx context.Context, query, sort string, deps analyze.Deps) []repoHit {
	type searchFunc func() []repoHit

	searches := []searchFunc{
		func() []repoHit { return forgeAPIRepoHits(ctx, query, sort, deps.Forges) },
		func() []repoHit { return webSearchMultiQuery(ctx, query, deps.WebSearch) },
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	var all []repoHit

	wg.Add(len(searches))
	for _, fn := range searches {
		fn := fn
		go func() {
			defer wg.Done()
			hits := fn()
			mu.Lock()
			all = append(all, hits...)
			mu.Unlock()
		}()
	}
	wg.Wait()

	return all
}
