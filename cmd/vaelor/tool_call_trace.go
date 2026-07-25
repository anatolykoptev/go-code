package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"log/slog"
	"strconv"
	"sync/atomic"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/codegraph"
	"github.com/anatolykoptev/vaelor/internal/ingest"
	"github.com/anatolykoptev/vaelor/internal/langutil"
	"github.com/anatolykoptev/vaelor/internal/mcpmeta"
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
	Condensed             string         `xml:"condensed,attr,omitempty"`
	Elided                int            `xml:"elided,attr,omitempty"`
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
	if callTraceConvertCount != nil {
		atomic.AddInt64(callTraceConvertCount, 1)
	}
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

// xmlTraceCountsResponse is ladder rung 3 for call_trace: N callers/callees
// across M files plus the immediate caller/callee list. The cheapest rendering
// — the agent gets the blast radius summary instead of a hard-truncated tree
// when even the depth-1 rendering overflows the budget.
type xmlTraceCountsResponse struct {
	XMLName xml.Name       `xml:"response"`
	Trace   xmlTraceCounts `xml:"trace"`
}

type xmlTraceCounts struct {
	Symbol    string         `xml:"symbol,attr"`
	Direction string         `xml:"direction,attr"`
	Total     int            `xml:"total,attr"`
	Files     int            `xml:"files,attr"`
	Nodes     []xmlTraceNode `xml:"node"`
}

// countTraceNodes counts all nodes in the tree recursively (including the
// roots).
func countTraceNodes(nodes []callgraph.CallChainNode) int {
	c := 0
	for _, n := range nodes {
		c += 1 + countTraceNodes(n.Children)
	}
	return c
}

// countTraceFiles collects unique file paths from all nodes in the tree.
func countTraceFiles(nodes []callgraph.CallChainNode, seen map[string]struct{}) {
	for _, n := range nodes {
		if n.Symbol != nil && n.Symbol.File != "" {
			seen[n.Symbol.File] = struct{}{}
		}
		countTraceFiles(n.Children, seen)
	}
}

// pruneTraceToDepth1 returns a copy of the tree pruned to depth 1 (root +
// immediate children only, deeper levels dropped) and the count of elided
// nodes (those at depth >= 2). A silently shallow tree is a wrong answer, not
// a condensed one — the caller sets Condensed="depth-1" and Elided=N so the
// agent knows deeper levels were dropped and how many.
func pruneTraceToDepth1(nodes []callgraph.CallChainNode) ([]callgraph.CallChainNode, int) {
	var elided int
	pruned := make([]callgraph.CallChainNode, len(nodes))
	for i, n := range nodes {
		// Count all nodes at depth >= 2 (grandchildren and deeper).
		for _, child := range n.Children {
			elided += countTraceNodes(child.Children)
		}
		// Shallow-copy the root, keep immediate children but nil their Children.
		rootCopy := n
		rootCopy.Children = make([]callgraph.CallChainNode, len(n.Children))
		for j, child := range n.Children {
			childCopy := child
			childCopy.Children = nil
			rootCopy.Children[j] = childCopy
		}
		pruned[i] = rootCopy
	}
	return pruned, elided
}

// buildTraceCounts builds the counts rung data: total nodes (excluding root),
// unique files, and the immediate children as XML nodes (depth-1 list).
func buildTraceCounts(nodes []callgraph.CallChainNode) (total int, files int, immediate []xmlTraceNode) {
	filesSet := make(map[string]struct{})
	for _, root := range nodes {
		// Immediate children become the depth-1 list.
		for _, child := range root.Children {
			// Count this child + all its descendants.
			total += countTraceNodes([]callgraph.CallChainNode{child})
		}
		// Collect files from all nodes except the root.
		countTraceFiles(root.Children, filesSet)
	}
	// Build the immediate list as shallow XML nodes (no children).
	immediate = convertTraceNodes(pruneChildrenShallow(nodes))
	files = len(filesSet)
	return total, files, immediate
}

// pruneChildrenShallow returns a copy of the tree where each root's children
// have their Children set to nil (depth-1 flat list).
func pruneChildrenShallow(nodes []callgraph.CallChainNode) []callgraph.CallChainNode {
	result := make([]callgraph.CallChainNode, len(nodes))
	for i, n := range nodes {
		rootCopy := n
		rootCopy.Children = make([]callgraph.CallChainNode, len(n.Children))
		for j, child := range n.Children {
			childCopy := child
			childCopy.Children = nil
			rootCopy.Children[j] = childCopy
		}
		result[i] = rootCopy
	}
	return result
}

// marshalTraceXML renders a trace XML response struct as a complete XML
// document string with the xml.Header prolog. Each rung closure in the
// ladder uses this so every rendering is a self-consistent, parseable
// document.
func marshalTraceXML(v any) string {
	data, err := xml.Marshal(v)
	if err != nil {
		return xmlMarshalErrorFragment(err)
	}
	return xml.Header + string(data)
}

// callTraceTraceFromAGE is the test seam for codegraph.TraceFromAGE. It is a
// package-level variable so handler-level tests can simulate an AGE miss
// without requiring a live AGE graph.
var callTraceTraceFromAGE = codegraph.TraceFromAGE

// callTraceConvertCount is a test-only seam for the render-count laziness
// assertion. Nil in production (zero overhead); tests set it to an int64
// counter that convertTraceNodes increments via atomic.AddInt64 on each
// (recursive) call. The test then asserts the count matches exactly one
// rung's worth of convertTraceNodes calls when rung 1 fits — proving the
// unreached rungs were never rendered. Without this, the eager-render form
// (building all three response structs before the ladder runs) comes
// straight back with a green suite, because TestPickFitting_UnreachedRung-
// ClosureNeverCalled tests PickFitting in isolation and cannot see what
// the caller does before calling it.
var callTraceConvertCount *int64

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

	// Progressive result-shortening ladder (#685 part 2): full tree →
	// depth-1 (direct callers/callees only, states deeper levels dropped) →
	// counts (N across M files + immediate list). renderLadder owns the
	// five invariants. The ladder is placed AFTER the freshness gate
	// (ensureAgeGraphOrStatus above returns a <status> envelope on a stale
	// graph before any result exists — the ladder must not run on that
	// path, and it doesn't: the gate returns at line ~218, well before
	// here).
	//
	// Laziness: each rung's rendering work (struct construction +
	// convertTraceNodes + marshalTraceXML) is INSIDE the closure, so an
	// unreached rung is never rendered. The common case (full tree fits
	// rung 1) does exactly one render, not three.
	ladder := mcpmeta.Ladder{
		{Name: "full", Render: func() string {
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
			return marshalTraceXML(resp)
		}},
		{Name: "depth-1", Render: func() string {
			// Rung 2: depth-1 — direct callers/callees only, deeper levels
			// dropped. MUST state that deeper levels were dropped and how
			// many were elided (a silently shallow tree is a wrong answer,
			// not a condensed one).
			prunedTree, elided := pruneTraceToDepth1(output.CallTree)
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
					Condensed:             "depth-1",
					Elided:                elided,
					Nodes:                 convertTraceNodes(prunedTree),
				},
			}
			return marshalTraceXML(resp)
		}},
		{Name: "counts", Render: func() string {
			// Rung 3: counts — N callers/callees across M files + immediate list.
			total, fileCount, immediateNodes := buildTraceCounts(output.CallTree)
			resp := xmlTraceCountsResponse{
				Trace: xmlTraceCounts{
					Symbol:    output.Symbol,
					Direction: output.Direction,
					Total:     total,
					Files:     fileCount,
					Nodes:     immediateNodes,
				},
			}
			return marshalTraceXML(resp)
		}},
	}
	body := renderLadder(ladder, "call_trace", outputDir, mcpmeta.DefaultBudget)
	return textResult(body), nil
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
