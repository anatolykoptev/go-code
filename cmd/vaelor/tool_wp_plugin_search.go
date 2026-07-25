package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/ingest"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// WPPluginSearchInput is the input schema for the wp_plugin_search tool.
type WPPluginSearchInput struct {
	Query   string `json:"query" jsonschema:"Search query (e.g. 'seo', 'woocommerce payment', 'cache')"`
	PerPage int    `json:"per_page,omitempty" jsonschema:"Results per page (1-20, default 10)"`
	Page    int    `json:"page,omitempty" jsonschema:"Page number (default 1)"`
}

// registerWPPluginSearch registers the wp_plugin_search MCP tool.
func registerWPPluginSearch(server *mcp.Server, _ Config, _ analyze.Deps) {
	addTool(server, &mcp.Tool{
		Name: "wp_plugin_search",
		Description: "Search the WordPress.org plugin directory. " +
			"Returns plugin name, slug, version, active installs, rating, and description. " +
			"Use the slug with any other tool (e.g. explore repo=\"wp:slug\") to analyze the plugin.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input WPPluginSearchInput) (*mcp.CallToolResult, error) {
		if input.Query == "" {
			return errResult("query is required"), nil
		}

		result, err := ingest.SearchWPPlugins(ctx, input.Query, input.PerPage, input.Page)
		if err != nil {
			return errResult(fmt.Sprintf("search failed: %v", err)), nil
		}

		if len(result.Plugins) == 0 {
			return textResult("No plugins found for: " + input.Query), nil
		}

		return textResult(formatWPSearchResults(input.Query, result)), nil
	})
}

func formatWPSearchResults(query string, resp *ingest.WPSearchResponse) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# WordPress Plugin Search: %s\n", query)
	fmt.Fprintf(&sb, "Found %d plugins (page %d/%d)\n\n", len(resp.Plugins), resp.Page, resp.Pages)

	for _, p := range resp.Plugins {
		stars := p.Rating / 20 //nolint:mnd // rating is 0-100, convert to 0-5 stars
		fmt.Fprintf(&sb, "## %s\n", p.Name)
		fmt.Fprintf(&sb, "- **Slug:** `wp:%s`\n", p.Slug)
		fmt.Fprintf(&sb, "- **Version:** %s\n", p.Version)
		fmt.Fprintf(&sb, "- **Active installs:** %s\n", formatInstalls(p.ActiveInstalls))
		fmt.Fprintf(&sb, "- **Rating:** %d/5 (%d%%)\n", stars, p.Rating)
		if p.ShortDescription != "" {
			fmt.Fprintf(&sb, "- %s\n", p.ShortDescription)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func formatInstalls(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%dM+", n/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%dK+", n/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}
