package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/compare"
	"github.com/anatolykoptev/vaelor/internal/compound"
	"github.com/anatolykoptev/vaelor/internal/oxcodes"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// PrepareChangeInput is the input schema for the prepare_change tool.
type PrepareChangeInput struct {
	Repo     string `json:"repo" jsonschema:"Repository: GitHub slug (owner/repo), full GitHub URL, or absolute local host path"`
	Symbol   string `json:"symbol" jsonschema:"Function or method name to assess change risk for"`
	Focus    string `json:"focus,omitempty" jsonschema:"Subdirectory path to limit scope"`
	Language string `json:"language,omitempty" jsonschema:"Limit to files of this language (e.g. go, python)"`
	Depth    int    `json:"depth,omitempty" jsonschema:"Max impact traversal depth (default 5, max 10)"`
}

const maxPrepareChangeDepth = 10

// couplingMinCoChanges is the minimum number of shared git commits required
// to consider two files "coupled" — matches compare.defaultMinCoChanges so
// the signal is comparable across tools.
const couplingMinCoChanges = 3

func registerPrepareChange(server *mcp.Server, _ Config, deps analyze.Deps, sem *SemanticDeps) {
	addTool(server, &mcp.Tool{
		Name: "prepare_change",
		Description: "Pre-change risk assessment for a function or method. Aggregates: impact_analysis (blast radius, affected callers) + dead_code (is it even used?). " +
			"Returns risk level, affected packages, and dead code status. " +
			"Use before refactoring to understand what might break. " +
			"Suggests similar symbols when the target is not found.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input PrepareChangeInput) (*mcp.CallToolResult, error) {
		return handlePrepareChange(ctx, input, deps, sem)
	})
}

func handlePrepareChange(ctx context.Context, input PrepareChangeInput, deps analyze.Deps, sem *SemanticDeps) (*mcp.CallToolResult, error) {
	if input.Repo == "" {
		return errResult("repo is required"), nil
	}
	if input.Symbol == "" {
		return errResult("symbol is required"), nil
	}

	depth := input.Depth
	if depth <= 0 || depth > maxPrepareChangeDepth {
		depth = maxPrepareChangeDepth
	}

	root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps)
	if err != nil {
		return errResult(fmt.Sprintf("resolve repo: %s", err)), nil
	}
	defer cleanup()

	cg, err := callgraph.BuildFromRepo(ctx, callgraph.TraceRepoInput{
		Root:     root,
		Focus:    input.Focus,
		Language: input.Language,
	})
	if err != nil {
		return errResult(fmt.Sprintf("build call graph: %s", err)), nil
	}

	opts := compound.PrepareChangeOpts{
		MaxDepth: depth,
		Repo:     input.Repo,
		Root:     root,
	}
	if deps.Graph != nil {
		opts.Graph = deps.Graph
	}
	result := compound.PrepareChange(ctx, cg, input.Symbol, opts)

	if !result.Found {
		msg := fmt.Sprintf("symbol %q not found in repository", input.Symbol)
		if suggestions := semanticSuggest(ctx, sem, root, input.Symbol, input.Language); suggestions != "" {
			return textResult(formatToolErrorWithSuggestions("prepare_change", msg, suggestions)), nil
		}
		return errResult(msg), nil
	}

	// Collect git-coupled files that frequently change together with the target.
	var relatedFiles []string
	targetFile := result.Symbol.File
	if rel, err := filepath.Rel(root, targetFile); err == nil {
		targetFile = rel
	}
	cctx, ccancel := context.WithTimeout(ctx, 10*time.Second)
	defer ccancel()
	pairs := compare.CollectCoupling(cctx, root, couplingMinCoChanges)
	seen := make(map[string]bool)
	for _, p := range pairs {
		var other string
		switch {
		case p.FileA == targetFile:
			other = p.FileB
		case p.FileB == targetFile:
			other = p.FileA
		}
		if other != "" && !seen[other] {
			seen[other] = true
			relatedFiles = append(relatedFiles, other)
		}
	}
	// Call site count via ox-codes structural search — AST-precise, no false positives.
	// Methods: $RECV.Name($$$), functions: Name($$$). ox-codes v0.2.2 fixes Go method call parsing.
	var callSiteCount int
	if deps.OxCodes != nil && result.Found && result.Symbol.Name != "" {
		lang := detectLangFromFile(result.Symbol.File)
		if lang != "" {
			sctx, scancel := context.WithTimeout(ctx, 8*time.Second)
			defer scancel()
			// Method: $RECV.Name($$$) matches any receiver; Function: Name($$$)
			var pattern string
			if result.Symbol.Kind == "method" {
				pattern = "$RECV." + result.Symbol.Name + "($$$)"
			} else {
				pattern = result.Symbol.Name + "($$$)"
			}
			if sresp, serr := deps.OxCodes.SearchStructural(sctx, oxcodes.StructuralSearchInput{
				Root:        root,
				Pattern:     pattern,
				Language:    lang,
				MaxResults:  200,
				ExcludeGlob: "vendor/**,*_test.*",
			}); serr == nil && sresp != nil {
				callSiteCount = sresp.TotalMatches
			}
		}
	}
	response := prepareChangeResponse{
		PrepareChangeResult: *result,
		RelatedFiles:        relatedFiles,
		CallSiteCount:       callSiteCount,
	}
	return jsonMarshalResult(response), nil
}

// prepareChangeResponse wraps PrepareChangeResult with additional git coupling data.
type prepareChangeResponse struct {
	compound.PrepareChangeResult
	RelatedFiles  []string `json:"related_files,omitempty"`
	CallSiteCount int      `json:"call_site_count,omitempty"` // structural call sites via ox-codes
}

// detectLangFromFile maps a file extension to the ox-codes language identifier.
func detectLangFromFile(filePath string) string {
	switch strings.ToLower(filepath.Ext(filePath)) {
	case ".go":
		return "go"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx":
		return "javascript"
	case ".rs":
		return "rust"
	case ".py":
		return "python"
	default:
		return ""
	}
}
