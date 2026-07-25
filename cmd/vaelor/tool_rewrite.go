package main

import (
	"context"
	"encoding/xml"
	"fmt"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/oxcodes"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RewriteInput is the input schema for the rewrite tool.
type RewriteInput struct {
	Repo        string `json:"repo" jsonschema:"Repository: GitHub slug, URL, or absolute local path"`
	Pattern     string `json:"pattern" jsonschema:"Structural AST pattern with $WILDCARDS (e.g. 'if $ERR != nil { return $ERR }')"`
	Rewrite     string `json:"rewrite" jsonschema:"Replacement template using same $WILDCARDS (e.g. 'if $ERR != nil { return fmt.Errorf(\"%w\", $ERR) }')"`
	Language    string `json:"language" jsonschema:"Target language (go, rust, python, typescript, java, etc.)"`
	MaxResults  int    `json:"max_results,omitempty" jsonschema:"Max matches (default: 50)"`
	FileGlob    string `json:"file_glob,omitempty" jsonschema:"File glob filter (e.g. '*.go')"`
	ExcludeGlob string `json:"exclude_glob,omitempty" jsonschema:"Exclude glob filter (e.g. 'vendor/*,testdata/*')"`
	Apply       bool   `json:"apply,omitempty" jsonschema:"If true, write changes to disk (default: dry-run diff only)"`
}

type xmlRewriteResponse struct {
	XMLName xml.Name          `xml:"response"`
	Rewrite xmlRewriteSummary `xml:"rewrite"`
}

type xmlRewriteSummary struct {
	Pattern      string             `xml:"pattern,attr"`
	Replacement  string             `xml:"replacement,attr"`
	TotalMatches int                `xml:"matches,attr"`
	TotalSkipped int                `xml:"skipped,attr,omitempty"`
	TotalFiles   int                `xml:"files,attr"`
	DurationMS   int64              `xml:"duration_ms,attr"`
	Files        []xmlRewriteFile   `xml:"file"`
	Rejected     []xmlRewriteReject `xml:"rejected,omitempty"`
}

type xmlRewriteReject struct {
	File   string `xml:"file,attr"`
	Reason string `xml:",chardata"`
}

type xmlRewriteFile struct {
	Path    string    `xml:"path,attr"`
	Matches int       `xml:"matches,attr"`
	Diff    *xmlCDATA `xml:"diff,omitempty"`
}

func registerRewrite(server *mcp.Server, cfg Config, deps analyze.Deps) {
	outputDir := cfg.OutputDir

	addTool(server, &mcp.Tool{
		Name: "rewrite",
		Description: "Structural search-and-replace using AST patterns with $WILDCARDS. " +
			"Finds code matching the pattern and generates unified diffs showing the transformation. " +
			"Useful for codebase-wide refactoring: wrapping errors, updating API calls, " +
			"migrating patterns, or enforcing coding conventions. " +
			"Requires ox-codes backend and a language specification.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input RewriteInput) (*mcp.CallToolResult, error) {
		return handleRewrite(ctx, input, deps, outputDir)
	})
}

func handleRewrite(ctx context.Context, input RewriteInput, deps analyze.Deps, outputDir string) (*mcp.CallToolResult, error) {
	if deps.OxCodes == nil {
		return errResult("rewrite requires ox-codes backend (OX_CODES_URL not configured)"), nil
	}
	if input.Repo == "" {
		return errResult("repo is required"), nil
	}
	if input.Pattern == "" {
		return errResult("pattern is required"), nil
	}
	if input.Rewrite == "" {
		return errResult("rewrite is required"), nil
	}
	if input.Language == "" {
		return errResult("language is required"), nil
	}

	root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps)
	if err != nil {
		return errResult(fmt.Sprintf("resolve repo: %s", err)), nil
	}
	defer cleanup()

	maxResults := input.MaxResults
	if maxResults <= 0 {
		maxResults = defaultMaxResults
	}

	result, err := deps.OxCodes.Rewrite(ctx, oxcodes.RewriteInput{
		Root:        root,
		Pattern:     input.Pattern,
		Rewrite:     input.Rewrite,
		Language:    input.Language,
		MaxResults:  maxResults,
		FileGlob:    input.FileGlob,
		ExcludeGlob: input.ExcludeGlob,
		Apply:       input.Apply,
	})
	if err != nil {
		return errResult(fmt.Sprintf("rewrite: %s", err)), nil
	}

	return xmlMarshalResult(formatRewriteXML(input, result), "rewrite", outputDir), nil
}

func formatRewriteXML(input RewriteInput, result *oxcodes.RewriteResponse) xmlRewriteResponse {
	files := make([]xmlRewriteFile, len(result.Files))
	for i, f := range result.Files {
		files[i] = xmlRewriteFile{
			Path:    f.File,
			Matches: f.Matches,
		}
		if f.Diff != "" {
			files[i].Diff = &xmlCDATA{Inner: wrapCDATA(f.Diff)}
		}
	}
	rejected := make([]xmlRewriteReject, len(result.Rejected))
	for i, r := range result.Rejected {
		rejected[i] = xmlRewriteReject{File: r.File, Reason: r.Reason}
	}

	return xmlRewriteResponse{
		Rewrite: xmlRewriteSummary{
			Pattern:      input.Pattern,
			Replacement:  input.Rewrite,
			TotalMatches: result.TotalMatches,
			TotalSkipped: result.TotalSkipped,
			TotalFiles:   result.TotalFiles,
			DurationMS:   result.DurationMS,
			Files:        files,
			Rejected:     rejected,
		},
	}
}
