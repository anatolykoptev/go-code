package main

import (
	"context"
	"fmt"

	"github.com/anatolykoptev/vaelor/internal/webanalyze"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// SiteCrawlInput is the input schema for the site_crawl tool.
type SiteCrawlInput struct {
	URL             string `json:"url" jsonschema:"Seed URL to start crawling from (e.g. https://example.com)"`
	MaxDepth        int    `json:"max_depth,omitempty" jsonschema:"Maximum crawl depth (default: 3)"`
	MaxPages        int    `json:"max_pages,omitempty" jsonschema:"Maximum number of pages to crawl (default: 50)"`
	Scope           string `json:"scope,omitempty" jsonschema:"URL scope: same_domain (default) or same_host"`
	IncludeMarkdown *bool  `json:"include_markdown,omitempty" jsonschema:"Include markdown content for each page (default: true)"`
}

const (
	defaultCrawlMaxDepth = 3
	defaultCrawlMaxPages = 50
)

func registerSiteCrawl(server *mcp.Server, cfg Config) {
	if cfg.OxBrowserURL == "" {
		return
	}
	client := webanalyze.NewClient(cfg.OxBrowserURL)

	addTool(server, &mcp.Tool{
		Name: "site_crawl",
		Description: "BFS site crawler via ox-browser. Starts from a seed URL, " +
			"discovers pages up to max_depth, respects robots.txt, deduplicates URLs. " +
			"Returns all crawled pages with titles, markdown content, and link counts. " +
			"Use for site-wide content extraction, documentation crawling, or site mapping.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input SiteCrawlInput) (*mcp.CallToolResult, error) {
		return handleSiteCrawl(ctx, input, client, cfg.OutputDir)
	})
}

func handleSiteCrawl(
	ctx context.Context, input SiteCrawlInput,
	client *webanalyze.Client, outputDir string,
) (*mcp.CallToolResult, error) {
	if input.URL == "" {
		return errResult("url is required"), nil
	}

	maxDepth := input.MaxDepth
	if maxDepth <= 0 {
		maxDepth = defaultCrawlMaxDepth
	}
	maxPages := input.MaxPages
	if maxPages <= 0 {
		maxPages = defaultCrawlMaxPages
	}

	includeMarkdown := input.IncludeMarkdown == nil || *input.IncludeMarkdown

	resp, err := client.Crawl(ctx, webanalyze.CrawlInput{
		URL:             input.URL,
		MaxDepth:        maxDepth,
		MaxPages:        maxPages,
		Scope:           input.Scope,
		IncludeMarkdown: includeMarkdown,
	})
	if err != nil {
		return errResult(fmt.Sprintf("crawl: %s", err)), nil
	}

	return largeTextResult(formatCrawlResponse(resp), "site_crawl", outputDir), nil
}

// formatCrawlResponse renders the crawl result as XML via typed structs +
// encoding/xml.Marshal (escaping correct by construction).
func formatCrawlResponse(resp *webanalyze.CrawlResponse) string {
	return xmlMarshalFragment(buildCrawlResponseXML(resp))
}

func buildCrawlResponseXML(resp *webanalyze.CrawlResponse) siteCrawlRespXML {
	out := siteCrawlRespXML{
		Tool: "site_crawl",
		Summary: crawlSummaryXML{
			Pages:     resp.Summary.PagesCrawled,
			Errors:    resp.Summary.Errors,
			ElapsedMs: resp.Summary.ElapsedMs,
		},
	}
	for _, p := range resp.Pages {
		if p.Error != nil {
			out.Pages = append(out.Pages, crawlPageXML{URL: p.URL, Depth: p.Depth, Error: p.Error})
			continue
		}
		// Bind loop-invariant copies for the pointer attrs so a success page
		// keeps its zero-valued title/links/bytes (non-nil pointer renders "0"),
		// exactly as the prior formatter always emitted them.
		status := p.Status
		title := p.Title
		links := p.LinksFound
		bytes := p.ContentLength
		page := crawlPageXML{
			URL:    p.URL,
			Status: &status,
			Depth:  p.Depth,
			Title:  &title,
			Links:  &links,
			Bytes:  &bytes,
		}
		if p.Markdown != "" {
			page.Content = &xmlCDATA{Inner: wrapCDATA(p.Markdown)}
		}
		out.Pages = append(out.Pages, page)
	}
	return out
}
