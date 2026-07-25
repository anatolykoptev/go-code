package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"sort"
	"time"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/codegraph"
	"github.com/anatolykoptev/vaelor/internal/compare"
	"github.com/anatolykoptev/vaelor/internal/deadcode"
	"github.com/anatolykoptev/vaelor/internal/mcpmeta"
	"github.com/anatolykoptev/vaelor/internal/prompts"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type xmlDeadCodeResponse struct {
	XMLName  xml.Name    `xml:"response"`
	DeadCode xmlDeadCode `xml:"deadCode"`
}

type xmlDeadCode struct {
	Total      int             `xml:"total,attr"`
	Dead       int             `xml:"dead,attr"`
	Ratio      float64         `xml:"ratio,attr"`
	Tier       string          `xml:"tier,attr,omitempty"`
	Warming    string          `xml:"warming,attr,omitempty"`
	DeadStores int             `xml:"dataflowDeadStores,attr,omitempty"`
	UnusedVars int             `xml:"dataflowUnusedVars,attr,omitempty"`
	Symbols    []xmlDeadSymbol `xml:"symbol"`
	Narrative  *xmlCDATA       `xml:"narrative,omitempty"`
}

type xmlDeadSymbol struct {
	Kind       string  `xml:"kind,attr"`
	Name       string  `xml:"name,attr"`
	File       string  `xml:"file,attr"`
	Package    string  `xml:"package,attr"`
	Line       int     `xml:"line,attr"`
	Lines      int     `xml:"lines,attr"`
	Exported   bool    `xml:"exported,attr,omitempty"`
	Confidence string  `xml:"confidence,attr"`
	CEScore    float32 `xml:"ceScore,attr,omitempty"` // CE dead-code probability [0..1]
}

// deadCodeBuildFromRepo is the production seam for callgraph.BuildFromRepo;
// handler-level tests can override it to avoid heavy parsing.
var deadCodeBuildFromRepo = callgraph.BuildFromRepo

// DeadCodeInput is the input schema for the dead_code tool.
type DeadCodeInput struct {
	Repo            string `json:"repo" jsonschema:"Repository: GitHub slug (owner/repo), full GitHub URL, or absolute local host path"`
	Language        string `json:"language,omitempty" jsonschema:"Limit analysis to files of this language (e.g. go, python)"`
	IncludeExported bool   `json:"include_exported,omitempty" jsonschema:"Include exported/public functions (usually false positives, default: false)"`
	Focus           string `json:"focus,omitempty" jsonschema:"Optional focus area for the LLM narrative"`
}

func registerDeadCode(server *mcp.Server, cfg Config, deps analyze.Deps, store *codegraph.Store) {
	outputDir := cfg.OutputDir

	addTool(server, &mcp.Tool{
		Name: "dead_code",
		Description: "Detect functions and methods with zero incoming calls. " +
			"Filters out entry points (main, init), test functions, and exported symbols " +
			"to reduce false positives. Shows confidence levels: high (unexported), " +
			"medium (methods, may satisfy interfaces), low (exported). " +
			"When the repository has a code_graph snapshot, results are enriched with " +
			"CE dead-code probability scores (ceScore attribute, ceScore 0..1: higher = more likely genuine dead code).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DeadCodeInput) (*mcp.CallToolResult, error) {
		return handleDeadCode(ctx, input, deps, outputDir, store)
	})
}

func handleDeadCode(ctx context.Context, input DeadCodeInput, deps analyze.Deps, outputDir string, store *codegraph.Store) (*mcp.CallToolResult, error) {
	if input.Repo == "" {
		return errResult("repo is required"), nil
	}

	root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps)
	if err != nil {
		return errResult(fmt.Sprintf("resolve repo: %s", err)), nil
	}
	defer cleanup()

	cg, err := deadCodeBuildFromRepo(ctx, callgraph.TraceRepoInput{
		Root:     root,
		Focus:    input.Focus,
		Language: input.Language,
	})
	if err != nil {
		return errResult(fmt.Sprintf("build call graph: %s", err)), nil
	}

	t0 := time.Now()

	result := deadcode.Analyze(cg, deadcode.Options{
		IncludeExported: input.IncludeExported,
		HookCallbacks:   cg.HookCallbacks,
		Relationships:   cg.TypeRels,
		OxCodes:         deps.OxCodes,
		Root:            root,
		Language:        input.Language,
		Ctx:             ctx,
	})

	// Convert dead symbols to XML structs.
	symbols := make([]xmlDeadSymbol, len(result.DeadSymbols))
	for i, s := range result.DeadSymbols {
		symbols[i] = xmlDeadSymbol{
			Kind:       s.Kind,
			Name:       s.Name,
			File:       s.File,
			Package:    s.Package,
			Line:       s.StartLine,
			Lines:      s.Lines,
			Exported:   s.Exported,
			Confidence: s.Confidence,
		}
	}

	// Enrich with pre-computed CE scores from AGE graph.
	if store != nil {
		repoKey := codegraph.GraphNameFor(root)
		for i := range symbols {
			if score, ok := store.LoadDeadCodeScore(ctx, repoKey, symbols[i].Name, symbols[i].File); ok {
				symbols[i].CEScore = score
			}
		}
		// Sort: CE-scored symbols first (by score DESC = most likely dead code first),
		// then unscored symbols by confidence.
		sort.SliceStable(symbols, func(i, j int) bool {
			si, sj := symbols[i].CEScore, symbols[j].CEScore
			hasI, hasJ := si != 0, sj != 0
			if hasI != hasJ {
				return hasI // CE-scored first
			}
			if hasI && hasJ {
				return si > sj // higher CE score = more dead
			}
			return false
		})
	}

	// Dataflow analysis via ox-codes (non-fatal, 10s timeout, language-gated).
	var dfStats *compare.DataflowStats
	if deps.OxCodes != nil && input.Language != "" {
		dctx, dcancel := context.WithTimeout(ctx, 10*time.Second)
		defer dcancel()
		dfStats = compare.GatherDataflow(dctx, deps.OxCodes, root, input.Language)
	}

	resp := xmlDeadCodeResponse{
		DeadCode: xmlDeadCode{
			Total:   result.TotalFunctions,
			Dead:    result.DeadCount,
			Ratio:   result.DeadRatio,
			Tier:    cg.Tier,
			Warming: warmingAttr(cg.Warming),
			Symbols: symbols,
		},
	}
	if dfStats != nil {
		resp.DeadCode.DeadStores = dfStats.DeadStores
		resp.DeadCode.UnusedVars = dfStats.UnusedVars
	}

	// LLM narrative (optional, non-fatal).
	if result.DeadCount > 0 {
		prefix := "Repository dead code analysis:\n"
		if input.Focus != "" {
			prefix = fmt.Sprintf("Focus area: %s\n\n%s", input.Focus, prefix)
		}
		if n := generateNarrative(ctx, deps.LLM, prompts.SystemPromptDeadCode, result, prefix); n != "" {
			resp.DeadCode.Narrative = &xmlCDATA{Inner: wrapCDATA(n)}
		}
	}

	// Build hint: find the worst-offender file by dead-symbol count.
	worstFile, worstCount := deadCodeWorstFile(symbols)
	hint := mcpmeta.HintAfterDeadCode(worstFile, worstCount)
	env := mcpmeta.Wrap(time.Since(t0), hint)
	if sha := deps.IndexedSHA(ctx, codegraph.GraphNameFor(root)); sha != "" {
		env = mcpmeta.WithFreshness(env, root, sha)
	}
	return metaXMLMarshalResult(resp, "dead_code", outputDir, env), nil
}

// deadCodeWorstFile returns the file with the most dead symbols and its count.
// Returns ("", 0) when the symbols list is empty.
func deadCodeWorstFile(symbols []xmlDeadSymbol) (string, int) {
	counts := make(map[string]int, len(symbols))
	for _, s := range symbols {
		counts[s.File]++
	}
	var worst string
	var worstN int
	for f, n := range counts {
		if n > worstN || (n == worstN && f < worst) {
			worst = f
			worstN = n
		}
	}
	return worst, worstN
}
