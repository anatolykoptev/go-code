package main

import (
	"context"
	"fmt"

	"github.com/anatolykoptev/go-kit/embed"
	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/designmd"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// DesignSearchInput is the input schema for the design_search tool.
type DesignSearchInput struct {
	Query string `json:"query" jsonschema:"Natural language description of the UI you want (e.g. 'dark minimal SaaS dashboard with green accent', 'art deco gold and black luxury')"`
	TopK  int    `json:"top_k,omitempty" jsonschema:"Number of results (default 5, max 20)"`
}

// DesignDeps holds dependencies for design_search tool.
type DesignDeps struct {
	Client *embed.Client
	Store  *designmd.Store
}

const (
	defaultDesignTopK = 5
	maxDesignTopK     = 20
)

func registerDesignSearch(server *mcp.Server, cfg Config, deps DesignDeps) {
	metaIndex, dirs := loadDesignMeta(cfg.DesignMDDir)
	desc := fmt.Sprintf("Find the best DESIGN.md for your UI by describing the look and feel. "+
		"Searches %d design systems: brand-inspired (Stripe, Linear) and style-based (Cyberpunk, Art Deco).", len(metaIndex))

	addTool(server, &mcp.Tool{Name: "design_search", Description: desc},
		func(ctx context.Context, _ *mcp.CallToolRequest, input DesignSearchInput) (*mcp.CallToolResult, error) {
			return handleDesignSearch(ctx, input, deps, metaIndex, dirs, cfg.PathMappings)
		})
}

func handleDesignSearch(
	ctx context.Context, input DesignSearchInput, deps DesignDeps,
	metaIndex map[string]designmd.BrandMeta, dirs []string, mappings []analyze.PathMapping,
) (*mcp.CallToolResult, error) {
	if input.Query == "" {
		return errResult("query is required"), nil
	}
	if deps.Client == nil || deps.Store == nil {
		return errResult("design_search requires DESIGN_EMBED_URL and DATABASE_URL"), nil
	}

	topK := max(min(input.TopK, maxDesignTopK), 1)
	if input.TopK <= 0 {
		topK = defaultDesignTopK
	}

	vector, err := deps.Client.EmbedQuery(ctx, input.Query)
	if err != nil {
		return errResult(fmt.Sprintf("embed query: %s", err)), nil
	}

	results, err := deps.Store.Search(ctx, vector, topK*3)
	if err != nil {
		return errResult(fmt.Sprintf("search: %s", err)), nil
	}

	if len(results) == 0 {
		return textResult(formatDesignStatus("not_indexed",
			"No design embeddings. Run: go-code index-designs /path/to/dir/")), nil
	}

	hits := groupByBrand(results, dirs, topK)
	return textResult(formatDesignResults(input.Query, hits, metaIndex, mappings)), nil
}
