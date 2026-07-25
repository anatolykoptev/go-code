package main

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/anatolykoptev/vaelor/internal/webanalyze"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// SiteAnalyzeInput is the input schema for the site_analyze tool.
type SiteAnalyzeInput struct {
	URL  string `json:"url" jsonschema:"Website URL to analyze (e.g. https://example.com)"`
	Mode string `json:"mode,omitempty" jsonschema:"detect (tech stack only, default) or full (detect + download source maps)"`
}

func registerSiteAnalyze(server *mcp.Server, cfg Config) {
	if cfg.OxBrowserURL == "" {
		return
	}
	client := webanalyze.NewClient(cfg.OxBrowserURL)
	workDir := cfg.WorkspaceDir

	addTool(server, &mcp.Tool{
		Name: "site_analyze",
		Description: "Analyze a website: technology stack (7000+ techs), SEO/OG tags, " +
			"performance hints, accessibility audit, content/media analysis, fonts, PWA, API discovery. " +
			"In full mode, also downloads JS bundles and extracts source maps.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input SiteAnalyzeInput) (*mcp.CallToolResult, error) {
		return handleSiteAnalyze(ctx, input, client, workDir)
	})
}

func handleSiteAnalyze(
	ctx context.Context, input SiteAnalyzeInput,
	client *webanalyze.Client, workDir string,
) (*mcp.CallToolResult, error) {
	if input.URL == "" {
		return errResult("url is required"), nil
	}

	resp, err := client.Analyze(ctx, input.URL)
	if err != nil {
		return errResult(fmt.Sprintf("analyze: %s", err)), nil
	}
	if resp.Error != "" {
		return errResult(fmt.Sprintf("ox-browser: %s", resp.Error)), nil
	}

	mode := input.Mode
	if mode == "" {
		mode = "detect"
	}

	if mode == "detect" {
		return textResult(formatDetectResponse(resp)), nil
	}

	// Full mode: download assets and extract source maps.
	domain := extractDomain(input.URL)
	outDir := filepath.Join(workDir, "sites", domain)

	stats, extractErr := extractSources(ctx, client, resp, outDir)
	return textResult(formatFullResponse(resp, outDir, stats, extractErr)), nil
}

func extractSources(
	ctx context.Context, client *webanalyze.Client,
	resp *webanalyze.AnalyzeResponse, outDir string,
) (*webanalyze.SourceStats, error) {
	totalStats := &webanalyze.SourceStats{Languages: make(map[string]int)}

	for _, scriptURL := range resp.Assets.Scripts {
		absURL := resolveURL(resp.URL, scriptURL)
		fetchResp, err := client.Fetch(ctx, absURL)
		if err != nil || fetchResp.Status != 200 {
			continue
		}

		mapURL := webanalyze.FindSourceMapURL(fetchResp.Body)
		if mapURL == "" {
			continue
		}

		absMapURL := resolveURL(absURL, mapURL)
		mapResp, err := client.Fetch(ctx, absMapURL)
		if err != nil || mapResp.Status != 200 {
			continue
		}

		sm, err := webanalyze.ParseSourceMap([]byte(mapResp.Body))
		if err != nil {
			continue
		}

		stats, err := webanalyze.WriteSourceTree(outDir, sm)
		if err != nil {
			continue
		}
		totalStats.Files += stats.Files
		for ext, count := range stats.Languages {
			totalStats.Languages[ext] += count
		}
	}
	return totalStats, nil
}

// formatDetectResponse renders the tech-stack-only response as XML via
// typed structs + encoding/xml.Marshal (escaping correct by construction).
func formatDetectResponse(resp *webanalyze.AnalyzeResponse) string {
	site := buildSiteHead(resp)
	site.Meta = &metaXML{
		Generator: resp.Meta.Generator,
		Server:    resp.Meta.Server,
		Title:     resp.Meta.Title,
	}
	site.Assets = &assetsXML{
		Scripts:     len(resp.Assets.Scripts),
		Stylesheets: len(resp.Assets.Stylesheets),
	}
	return marshalSiteAnalyze(site)
}

// formatFullResponse renders the full-mode response (detect + extracted source
// map stats) as XML via typed structs + encoding/xml.Marshal.
func formatFullResponse(
	resp *webanalyze.AnalyzeResponse, outDir string,
	stats *webanalyze.SourceStats, extractErr error,
) string {
	site := buildSiteHead(resp)
	if stats != nil && stats.Files > 0 {
		src := &sourcesXML{Path: outDir, Files: stats.Files}
		for ext, count := range stats.Languages {
			src.Languages = append(src.Languages, languageXML{Name: ext, Files: count})
		}
		site.Sources = src
		site.Hint = fmt.Sprintf("Use explore, symbol_search, or dep_graph with repo=%q", outDir)
	} else {
		msg := "No source maps found"
		if extractErr != nil {
			msg = extractErr.Error()
		}
		site.Sources = &sourcesXML{Files: 0, Reason: msg}
	}
	return marshalSiteAnalyze(site)
}

// marshalSiteAnalyze wraps a built <site> in the <response tool="site_analyze">
// envelope and marshals it. No xml.Header prolog is emitted (the response is an
// XML fragment consumed by the MCP caller, matching the prior formatter).
func marshalSiteAnalyze(site siteXML) string {
	return xmlMarshalFragment(siteAnalyzeRespXML{Tool: "site_analyze", Site: site})
}

func extractDomain(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "unknown"
	}
	return u.Hostname()
}

func resolveURL(base, ref string) string {
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref
	}
	u, err := url.Parse(base)
	if err != nil {
		return ref
	}
	r, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return u.ResolveReference(r).String()
}
