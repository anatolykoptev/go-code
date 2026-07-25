package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/codegraph"
	"github.com/anatolykoptev/vaelor/internal/ingest"
	"github.com/anatolykoptev/vaelor/internal/langutil"
	"github.com/anatolykoptev/vaelor/internal/prompts"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type xmlTraceResponse struct {
	XMLName xml.Name `xml:"response"`
	Trace   xmlTrace `xml:"trace"`
}

type xmlTrace struct {
	Symbol                string         `xml:"symbol,attr"`
	Direction             string         `xml:"direction,attr"`
	TotalNodes            int            `xml:"totalNodes,attr"`
	MaxDepth              int            `xml:"maxDepth,attr"`
	Resolved              int            `xml:"resolved,attr"`
	Unresolved            int            `xml:"unresolved,attr"`
	ResolvedRatio         float64        `xml:"resolvedRatio,attr"`
	Tier                  string         `xml:"tier,attr,omitempty"`
	ProductionCallerCount int            `xml:"production_caller_count,attr,omitempty"`
	Nodes                 []xmlTraceNode `xml:"node"`
	Narrative             *xmlCDATA      `xml:"narrative,omitempty"`
}

type xmlTraceNode struct {
	Kind       string         `xml:"kind,attr,omitempty"`
	CallerKind string         `xml:"caller_kind,attr,omitempty"`
	Name       string         `xml:"name,attr"`
	File       string         `xml:"file,attr"`
	Line       uint32         `xml:"line,attr"`
	End        uint32         `xml:"end,attr,omitempty"`
	CallLine   uint32         `xml:"callLine,attr,omitempty"`
	Cycle      bool           `xml:"cycle,attr,omitempty"`
	Signature  *xmlCDATA      `xml:"signature,omitempty"`
	Children   []xmlTraceNode `xml:"node,omitempty"`
}

func convertTraceNodes(nodes []callgraph.CallChainNode) []xmlTraceNode {
	result := make([]xmlTraceNode, len(nodes))
	for i, n := range nodes {
		xn := xmlTraceNode{
			CallLine: n.CallLine,
			Cycle:    n.Cycle,
		}
		if n.Symbol != nil {
			xn.Kind = string(n.Symbol.Kind)
			xn.Name = n.Symbol.Name
			xn.File = n.Symbol.File
			xn.Line = n.Symbol.StartLine
			xn.End = n.Symbol.EndLine
			if n.Symbol.Signature != "" {
				xn.Signature = &xmlCDATA{Inner: wrapCDATA(n.Symbol.Signature)}
			}
		}
		xn.CallerKind = n.CallerKind
		if len(n.Children) > 0 {
			xn.Children = convertTraceNodes(n.Children)
		}
		result[i] = xn
	}
	return result
}

// callTraceTraceFromAGE is the test seam for codegraph.TraceFromAGE. It is a
// package-level variable so handler-level tests can simulate an AGE miss
// without requiring a live AGE graph.
var callTraceTraceFromAGE = codegraph.TraceFromAGE

// callTraceStatusXML is the building-status short-circuit response for call_trace.
// It mirrors the normal <response><trace.../> shape with status/message attrs.
type callTraceStatusXML struct {
	XMLName xml.Name             `xml:"response"`
	Trace   callTraceStatusTrace `xml:"trace"`
}

type callTraceStatusTrace struct {
	Symbol  string `xml:"symbol,attr"`
	Status  string `xml:"status,attr"`
	Message string `xml:"message,attr"`
}

// buildCallTraceStatusResponse builds an XML status response for call_trace.
func buildCallTraceStatusResponse(input CallTraceInput, status, message string) *mcp.CallToolResult {
	return textResult(xmlMarshalFragment(callTraceStatusXML{
		Trace: callTraceStatusTrace{
			Symbol:  input.Symbol,
			Status:  status,
			Message: message,
		},
	}))
}

// CallTraceInput is the input schema for the call_trace tool.
type CallTraceInput struct {
	Repo      string `json:"repo" jsonschema:"Repository: GitHub slug (owner/repo), full GitHub URL, or absolute local host path (e.g. /home/user/src/project)"`
	Symbol    string `json:"symbol" jsonschema:"Function or method name to trace (e.g. CompareRepos, Server.Serve)"`
	Depth     int    `json:"depth,omitempty" jsonschema:"Max trace depth (default 5, max 10)"`
	Direction string `json:"direction,omitempty" jsonschema:"Trace direction: callees (what does X call?) or callers (who calls X?). Default: callees"`
	Focus     string `json:"focus,omitempty" jsonschema:"Subdirectory path to limit scope (e.g. internal/auth), or space-separated keywords (e.g. 'auth handler')"`
	Language  string `json:"language,omitempty" jsonschema:"Limit to files of this language (e.g. go, python)"`
	Compact   bool   `json:"compact,omitempty" jsonschema:"When true, return only the call tree without LLM narrative (faster, fewer tokens)"`

	FieldAccess bool `json:"field_access,omitempty" jsonschema:"When true, include heuristic argument-reference call sites (struct field accesses, identifier args) as callees even when they don't resolve to a known function — legacy permissive behaviour. Default false: only true call expressions and resolved function references are reported."`
	Refresh     bool `json:"refresh,omitempty" jsonschema:"When true, bypass the in-memory call graph cache and force a full re-parse with SCIP/go/types enrichment. Use after git checkout or new commits when the cache is stale. Slower but fresh."`
}

type callTraceOutput struct {
	Symbol                string                    `json:"symbol"`
	Direction             string                    `json:"direction"`
	CallTree              []callgraph.CallChainNode `json:"call_tree"`
	Stats                 traceStats                `json:"stats"`
	Tier                  string                    `json:"tier,omitempty"`
	Narrative             string                    `json:"narrative,omitempty"`
	ProductionCallerCount int                       `json:"production_caller_count,omitempty"`
}

type traceStats struct {
	TotalNodes    int     `json:"total_nodes"`
	MaxDepth      int     `json:"max_depth"`
	Resolved      int     `json:"resolved"`
	Unresolved    int     `json:"unresolved"`
	ResolvedRatio float64 `json:"resolved_ratio"`
}

const defaultTraceDepth = 5

// normalizeCallTraceDirection maps the tool's documented direction values
// (forward/reverse/callees/callers) to the canonical values expected by
// internal/callgraph.Trace ("callers" for reverse, "callees" otherwise).
func normalizeCallTraceDirection(direction string) string {
	switch direction {
	case "reverse", "callers":
		return "callers"
	case "forward", "callees":
		return "callees"
	default:
		return "callees"
	}
}

// registerCallTrace registers the call_trace MCP tool.
func registerCallTrace(server *mcp.Server, cfg Config, deps analyze.Deps, sem *SemanticDeps, store *codegraph.Store) {
	outputDir := cfg.OutputDir

	addTool(server, &mcp.Tool{
		Name: "call_trace",
		Description: "Trace the execution path of a function through a codebase. " +
			"Shows what happens when a function is called (callees) or who calls it (callers). " +
			"Returns a call tree with resolved cross-file references and an LLM-generated " +
			"narrative explanation of the execution flow. " +
			"Type-aware for Go repos: resolves interface calls to concrete implementations via go/types. " +
			"Suggests semantically similar symbols when the target is not found.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CallTraceInput) (*mcp.CallToolResult, error) {
		return handleCallTrace(ctx, input, deps, sem, outputDir, store)
	})
}

func handleCallTrace(ctx context.Context, input CallTraceInput, deps analyze.Deps, sem *SemanticDeps, outputDir string, store *codegraph.Store) (*mcp.CallToolResult, error) {
	if input.Repo == "" {
		return errResult("repo is required"), nil
	}
	if input.Symbol == "" {
		return errResult("symbol is required"), nil
	}

	root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps)
	if err != nil {
		return errResult(fmt.Sprintf("resolve repo: %s", err)), nil
	}
	defer cleanup()

	depth := input.Depth
	if depth <= 0 {
		depth = defaultTraceDepth
	}

	direction := normalizeCallTraceDirection(input.Direction)

	// Fast path: try AGE graph first (avoids 2-60s repo reparse on cache miss).
	// The AGE graph already contains CALLS edges from the last IndexRepo build.
	// Falls back to BuildFromRepo (tree-sitter parse) if the graph is absent,
	// the symbol is not found, or the AGE query fails for any reason.
	var result *callgraph.TraceResult
	if store != nil && !input.Refresh {
		graphName := codegraph.GraphNameFor(root)
		if ageResult, ageErr := callTraceTraceFromAGE(ctx, store, graphName, input.Symbol, direction, depth); ageErr == nil && ageResult != nil && ageResult.Root != nil {
			slog.Debug("call_trace: using AGE graph (fast path)",
				slog.String("symbol", input.Symbol),
				slog.String("direction", direction),
				slog.Int("nodes", ageResult.TotalNodes))
			result = ageResult
		}
	}

	// Fallback: if the AGE graph is not fresh, start a background build and
	// return a building status instead of blocking on the full tree-sitter parse.
	// Explicit refresh bypasses this gate and forces the synchronous reparse.
	if (result == nil || result.Root == nil) && store != nil && !input.Refresh {
		fresh, status := ensureAgeGraphOrStatus(ctx, "call_trace", store, root, codegraph.GraphNameFor(root), ingest.IsRemote(input.Repo), codegraph.IndexConfig{}, func(status, message string) *mcp.CallToolResult {
			return buildCallTraceStatusResponse(input, status, message)
		})
		if !fresh {
			return status, nil
		}
	}

	// Fallback: full repo parse via tree-sitter (slower but more accurate —
	// includes go/types interface resolution, SCIP, cross-language refs).
	if result == nil || result.Root == nil {
		result, err = callgraph.TraceRepo(ctx, callgraph.TraceRepoInput{
			Root:               root,
			Symbol:             input.Symbol,
			Focus:              input.Focus,
			Language:           input.Language,
			IncludeFieldAccess: input.FieldAccess,
			Refresh:            input.Refresh,
			Opts: callgraph.TraceOpts{
				Direction: direction,
				MaxDepth:  depth,
				CrossRefs: deps.Refs,
				Repo:      root,
			},
		})
		if err != nil {
			return errResult(fmt.Sprintf("trace: %s", err)), nil
		}
	}

	if result.Root == nil {
		msg := fmt.Sprintf("symbol %q not found in repository", input.Symbol)
		if suggestions := semanticSuggest(ctx, sem, root, input.Symbol, input.Language); suggestions != "" {
			return textResult(formatToolErrorWithSuggestions("call_trace", msg, suggestions)), nil
		}
		return errResult(msg), nil
	}

	// Speculative resolution: enrich unresolved call sites via ox-codes text search.
	if deps.OxCodes != nil && result.Unresolved > 0 {
		callgraph.ResolveSpeculative(ctx, deps.OxCodes, root, input.Language, result.Tree)
	}

	output := buildCallTraceOutput(ctx, input.Symbol, direction, result, deps, input.Compact)

	resp := xmlTraceResponse{
		Trace: xmlTrace{
			Symbol:                output.Symbol,
			Direction:             output.Direction,
			TotalNodes:            output.Stats.TotalNodes,
			MaxDepth:              output.Stats.MaxDepth,
			Resolved:              output.Stats.Resolved,
			Unresolved:            output.Stats.Unresolved,
			ResolvedRatio:         output.Stats.ResolvedRatio,
			Tier:                  output.Tier,
			ProductionCallerCount: output.ProductionCallerCount,
			Nodes:                 convertTraceNodes(output.CallTree),
		},
	}
	if output.Narrative != "" {
		resp.Trace.Narrative = &xmlCDATA{Inner: wrapCDATA(output.Narrative)}
	}

	return xmlMarshalResult(resp, "call_trace", outputDir), nil
}

// countProductionCallers returns the number of distinct DIRECT callers whose
// CallerKind is "production". When skipRoot is true, the root node (depth 0,
// the queried symbol) is excluded; only its immediate children (depth 1) are
// considered. Duplicate direct callers reached via multiple branches, cycles,
// or multiple call sites are counted once using symbol identity.
func countProductionCallers(nodes []callgraph.CallChainNode, skipRoot bool) int {
	seen := make(map[string]struct{})
	for _, root := range nodes {
		candidates := root.Children
		if !skipRoot {
			candidates = append([]callgraph.CallChainNode{root}, candidates...)
		}
		for _, n := range candidates {
			if n.CallerKind != langutil.CallerKindProduction {
				continue
			}
			key := productionCallerKey(n)
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
		}
	}
	return len(seen)
}

func productionCallerKey(n callgraph.CallChainNode) string {
	if n.Symbol == nil {
		return "\x00\x00\x00\x00"
	}
	return n.Symbol.Name + "\x00" + n.Symbol.File + "\x00" + strconv.Itoa(int(n.Symbol.StartLine)) + "\x00" + n.Symbol.Receiver
}

func buildCallTraceOutput(ctx context.Context, symbol, direction string, result *callgraph.TraceResult, deps analyze.Deps, compact bool) callTraceOutput {
	total := result.Resolved + result.Unresolved
	var ratio float64
	if total > 0 {
		ratio = float64(result.Resolved) / float64(total)
	}

	output := callTraceOutput{
		Symbol:    symbol,
		Direction: direction,
		CallTree:  result.Tree,
		Stats: traceStats{
			TotalNodes:    result.TotalNodes,
			MaxDepth:      result.MaxDepth,
			Resolved:      result.Resolved,
			Unresolved:    result.Unresolved,
			ResolvedRatio: ratio,
		},
		Tier: result.Tier,
	}

	if direction == "callers" {
		output.ProductionCallerCount = countProductionCallers(result.Tree, true)
	}

	// LLM narrative (optional, non-fatal). Skipped in compact mode.
	if !compact && result.TotalNodes > 1 {
		prefix := fmt.Sprintf("Entry function: %s\nDirection: %s\n\nCall tree:\n", symbol, direction)
		output.Narrative = generateNarrative(ctx, deps.LLM, prompts.SystemPromptCallTrace, result.Tree, prefix)
	}

	return output
}
