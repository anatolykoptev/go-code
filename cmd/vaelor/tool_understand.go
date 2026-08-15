package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/codegraph"
	"github.com/anatolykoptev/vaelor/internal/compound"
	"github.com/anatolykoptev/vaelor/internal/ingest"
	"github.com/anatolykoptev/vaelor/internal/mcpmeta"
	"github.com/anatolykoptev/vaelor/internal/parser"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// UnderstandInput is the input schema for the understand tool.
type UnderstandInput struct {
	Repo           string `json:"repo" jsonschema:"Repository: GitHub slug (owner/repo), full GitHub URL, or absolute local host path"`
	Symbol         string `json:"symbol" jsonschema:"Function or method name to analyze in depth"`
	Focus          string `json:"focus,omitempty" jsonschema:"Subdirectory path to limit scope"`
	Language       string `json:"language,omitempty" jsonschema:"Limit to files of this language"`
	IncludeCallers bool   `json:"include_callers,omitempty" jsonschema:"Include who calls this symbol (default: false)"`
	FieldAccess    bool   `json:"field_access,omitempty" jsonschema:"When true, include heuristic argument-reference call sites (struct field accesses, identifier args) as callees even when they don't resolve to a known function — legacy permissive behaviour. Default false: only true call expressions and resolved function references are reported."`
	MaxBytes       int    `json:"max_bytes,omitempty" jsonschema:"Response budget in bytes (default 8192). When the response exceeds this, a progressively condensed rendering is returned with a note saying how it was shortened."`
}

// understandBuildFromRepo is the production seam for callgraph.BuildFromRepo;
// handler-level tests can override it to avoid heavy parsing.
var understandBuildFromRepo = callgraph.BuildFromRepo

// understandCompound is the production seam for compound.Understand; tests
// can override it to inject a pre-built UnderstandResult (e.g. one with
// prior_learnings/graph_signals/tested_by populated) without wiring a live
// learnings store or AGE graph. The seam is on the result-assembly step, not
// on the ladder — the ladder still runs through renderLadder/formatUnderstand*.
var understandCompound = compound.Understand

func registerUnderstand(server *mcp.Server, cfg Config, deps analyze.Deps, sem *SemanticDeps, graphStore *codegraph.Store) {
	outputDir := cfg.OutputDir
	addTool(server, &mcp.Tool{
		Name: "understand",
		Description: "Deep-dive into a single symbol. Aggregates: symbol info + callees + callers + complexity. " +
			"Returns type-aware results for Go repos (interface dispatch resolution). " +
			"Use instead of separate call_trace + symbol_search + code_graph calls. " +
			"Suggests similar symbols when the target is not found. " +
			"When a code_graph snapshot exists: shows tested_by (test functions covering this symbol) " +
			"and dead_code_score (CE reranker confidence that this function is unused, if applicable).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input UnderstandInput) (*mcp.CallToolResult, error) {
		return handleUnderstand(ctx, input, deps, sem, graphStore, outputDir)
	})
}

func handleUnderstand(ctx context.Context, input UnderstandInput, deps analyze.Deps, sem *SemanticDeps, graphStore *codegraph.Store, outputDir string) (*mcp.CallToolResult, error) {
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

	t0 := time.Now()

	// Remote repos only: avoid a synchronous full repo parse when the AGE call
	// graph is not yet built. Start a background build and return a building
	// status; the caller can retry once the graph is fresh. Local repos keep
	// their pre-#490 inline BuildFromRepo behavior (no gate).
	isRemote := ingest.IsRemote(input.Repo)
	if graphStore != nil && isRemote {
		if fresh, status := ensureAgeGraphOrStatus(ctx, "understand", graphStore, root, codegraph.GraphNameFor(root), isRemote, codegraph.IndexConfig{}, func(status, message string) *mcp.CallToolResult {
			return buildUnderstandStatusResponse(input, status, message)
		}); !fresh {
			return status, nil
		}
	}

	cg, err := understandBuildFromRepo(ctx, callgraph.TraceRepoInput{
		Root:               root,
		Language:           input.Language,
		IncludeFieldAccess: input.FieldAccess,
	})
	if err != nil {
		return errResult(fmt.Sprintf("build call graph: %s", err)), nil
	}

	matches := filterByFocus(compound.FindSymbol(cg.Symbols, input.Symbol), input.Focus)

	if len(matches) == 0 {
		msg := fmt.Sprintf("symbol %q not found in repository", input.Symbol)
		if suggestions := semanticSuggest(ctx, sem, root, input.Symbol, input.Language); suggestions != "" {
			return textResult(formatToolErrorWithSuggestions("understand", msg, suggestions)), nil
		}
		return errResult(msg), nil
	}

	if len(matches) > 1 {
		return understandAmbiguousResult(input.Symbol, matches, deps.PathMappings)
	}

	opts := compound.UnderstandOpts{
		IncludeCallers: input.IncludeCallers,
		OxCodes:        deps.OxCodes,
		Root:           root,
		Repo:           input.Repo,
	}
	// Avoid the typed-nil-interface trap: only assign Learnings when the
	// store is actually configured, so opts.Learnings == nil behaves correctly.
	if deps.Learnings != nil {
		opts.Learnings = deps.Learnings
	}
	if deps.Graph != nil {
		opts.Graph = deps.Graph
	}
	if deps.Refs != nil {
		opts.Refs = deps.Refs
	}
	if graphStore != nil {
		opts.DeadCodeScores = graphStore
		opts.SymbolRanker = graphStore
	}
	result := understandCompound(ctx, matches[0], cg, opts)

	// Surface the warming note when go/types enrichment was skipped on a cold
	// cache (issue #735). The background warm is running; a retry will return
	// the enhanced tier with type-aware call resolution.
	if cg.Warming {
		result.Warnings = append(result.Warnings,
			"type-aware enrichment is warming in the background; retry for the enhanced tier (go/types interface dispatch resolution)")
	}

	// Reverse-map container-internal paths back to host-side paths so callers
	// see clickable file locations matching their local checkout.
	if len(deps.PathMappings) > 0 {
		result.Symbol.File = reverseToHost(result.Symbol.File, deps.PathMappings)
		for i := range result.Callees {
			result.Callees[i].File = reverseToHost(result.Callees[i].File, deps.PathMappings)
		}
		for i := range result.Callers {
			result.Callers[i].File = reverseToHost(result.Callers[i].File, deps.PathMappings)
		}
	}

	// understand is a terminal call — no chaining hint.
	env := mcpmeta.Wrap(time.Since(t0), "")
	env = annotateEnv(env, input.Repo, root, deps.IndexedSHA(ctx, codegraph.GraphNameFor(root)))
	// Progressive result-shortening ladder (#685 part 3): full →
	// no-learnings (drop prior_learnings/graph_signals/tested_by — the
	// enrichment from external stores, the "surrounding context" around the
	// core call topology) → counts (drop per-ref callee/caller lists, keep
	// counts + symbol identity + tier + dead_code_score + structural_rank).
	// renderLadder owns the five invariants so this tool cannot forget one.
	//
	// Rung choice reasoning: understand's primary value is the call topology
	// (callees/callers). The enrichment (prior_learnings, graph_signals,
	// tested_by) is auxiliary context — dropped first. The per-ref lists are
	// the core payload — dropped last, leaving counts so the agent still
	// knows "this symbol has N callees, M callers, dead_code_score X".
	//
	// Laziness: each rung's rendering work (struct construction +
	// json.Marshal + appendMetaFooter) is INSIDE the closure, so an
	// unreached rung is never rendered. The common case (full result fits
	// rung 1) does exactly one render, not three.
	//
	// Budget ownership: the LADDER owns the budget, not the addTool wrapper.
	// When max_bytes > 0, MarkBudgetApplied appends the budget-applied
	// sentinel so the wrapper skips re-shaping at DefaultBudget (matches
	// semantic_search's behaviour).
	ladder := mcpmeta.Ladder{
		{Name: "full", Render: func() string { return formatUnderstandFull(result, env) }},
		{Name: "no-learnings", Render: func() string { return formatUnderstandNoLearnings(result, env) }},
		{Name: "counts", Render: func() string { return formatUnderstandCounts(result, env) }},
	}
	budget := mcpmeta.ResolveBudget(input.MaxBytes, mcpmeta.DefaultBudget)
	body := renderLadder(ladder, "understand", outputDir, budget)
	if input.MaxBytes > 0 {
		body = mcpmeta.MarkBudgetApplied(body)
	}
	return textResult(body), nil
}

// filterByFocus narrows a symbol list to those whose file path matches focus.
// Strategy: exact → suffix → substring. Empty focus returns all symbols unchanged.
func filterByFocus(symbols []*parser.Symbol, focus string) []*parser.Symbol {
	if focus == "" {
		return symbols
	}
	var exact []*parser.Symbol
	for _, sym := range symbols {
		if sym.File == focus {
			exact = append(exact, sym)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	var suffix []*parser.Symbol
	for _, sym := range symbols {
		if strings.HasSuffix(sym.File, focus) {
			suffix = append(suffix, sym)
		}
	}
	if len(suffix) > 0 {
		return suffix
	}
	var sub []*parser.Symbol
	for _, sym := range symbols {
		if strings.Contains(sym.File, focus) {
			sub = append(sub, sym)
		}
	}
	return sub
}

// understandStatusResponse is the JSON short-circuit envelope returned when the
// AGE graph is not yet fresh. It preserves the tool's JSON response format and
// carries a retry hint.
type understandStatusResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Repo    string `json:"repo"`
	Symbol  string `json:"symbol"`
}

// buildUnderstandStatusResponse builds a JSON status response for understand.
func buildUnderstandStatusResponse(input UnderstandInput, status, message string) *mcp.CallToolResult {
	resp := understandStatusResponse{
		Status:  status,
		Message: message,
		Repo:    input.Repo,
		Symbol:  input.Symbol,
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return errResult(fmt.Sprintf("marshal: %s", err))
	}
	return textResult(string(data))
}

// understandAmbiguousResult returns a JSON response listing ambiguous symbol matches.
// mappings is used to reverse-translate container-internal paths to host paths.
func understandAmbiguousResult(name string, symbols []*parser.Symbol, mappings []analyze.PathMapping) (*mcp.CallToolResult, error) {
	refs := make([]*compound.MatchRef, 0, len(symbols))
	for _, sym := range symbols {
		refs = append(refs, &compound.MatchRef{
			Name:     sym.Name,
			Kind:     string(sym.Kind),
			File:     reverseToHost(sym.File, mappings),
			Line:     sym.StartLine,
			Receiver: sym.Receiver,
		})
	}

	type ambiguousResponse struct {
		Error   string               `json:"error"`
		Matches []*compound.MatchRef `json:"matches"`
	}
	resp := ambiguousResponse{
		Error:   fmt.Sprintf("symbol %q is ambiguous (%d matches) — provide more context via focus= or use a qualified name", name, len(symbols)),
		Matches: refs,
	}
	return jsonMarshalResult(resp), nil
}

// understandFormatCount is a test-only seam for the render-count laziness
// assertion. Nil in production (zero overhead); tests set it to an int64
// counter that the three formatUnderstand* functions increment via
// atomic.AddInt64. The test then asserts EXACTLY ONE increment when rung 1
// fits — proving the unreached rungs were never rendered. The spy is on the
// RENDERING FUNCTION, not on the closure: PickFitting invokes only the
// closure it reaches, so a closure-level spy cannot distinguish lazy from
// eager and will pass either way.
var understandFormatCount *int64

// formatUnderstandFull is ladder rung 1: the complete UnderstandResult JSON
// with the meta envelope footer. This is the fullest rendering — every
// callee, caller, body analysis, prior learning, graph signal, etc.
func formatUnderstandFull(result *compound.UnderstandResult, env mcpmeta.Envelope) string {
	if understandFormatCount != nil {
		atomic.AddInt64(understandFormatCount, 1)
	}
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Sprintf(`{"error":"marshal: %s"}`, err.Error())
	}
	return appendMetaFooter(string(data), env)
}

// formatUnderstandNoLearnings is ladder rung 2: the UnderstandResult JSON
// with the enrichment fields dropped (prior_learnings, graph_signals,
// tested_by) — the "surrounding context" around the core call topology.
// The callees/callers lists (the primary payload) are preserved.
func formatUnderstandNoLearnings(result *compound.UnderstandResult, env mcpmeta.Envelope) string {
	if understandFormatCount != nil {
		atomic.AddInt64(understandFormatCount, 1)
	}
	// Shallow-copy the result and nil out the enrichment fields. The slices
	// share backing arrays with the original, but we only read them for
	// JSON marshaling — no mutation.
	condensed := *result
	condensed.PriorLearnings = nil
	condensed.GraphSignals = nil
	condensed.TestedBy = nil
	data, err := json.Marshal(&condensed)
	if err != nil {
		return fmt.Sprintf(`{"error":"marshal: %s"}`, err.Error())
	}
	return appendMetaFooter(string(data), env)
}

// understandCountsResult is ladder rung 3 for understand: the symbol
// identity + per-symbol counts + tier + dead_code_score + structural_rank,
// with the per-ref callee/caller lists, body_analysis, prior_learnings,
// graph_signals, and tested_by all dropped. The cheapest rendering —
// the agent gets "this symbol has N callees, M callers, dead_code_score X"
// instead of a hard-truncated JSON fragment when even the no-learnings
// rendering overflows the budget. (prior_learnings/graph_signals/tested_by
// are already absent at rung 2; body_analysis and the per-ref lists are the
// fields rung 3 drops relative to rung 2.)
type understandCountsResult struct {
	Symbol                compound.SymbolInfo `json:"symbol"`
	Tier                  string              `json:"tier"`
	CalleesCount          int                 `json:"callees_count"`
	CallersCount          int                 `json:"callers_count"`
	ProductionCallerCount int                 `json:"production_caller_count,omitempty"`
	DeadCodeScore         *float32            `json:"dead_code_score,omitempty"`
	DeadCodeNote          string              `json:"dead_code_note,omitempty"`
	StructuralRank        string              `json:"structural_rank,omitempty"`
	Warnings              []string            `json:"warnings,omitempty"`
}

// formatUnderstandCounts is ladder rung 3: per-symbol counts with the
// per-ref lists dropped.
func formatUnderstandCounts(result *compound.UnderstandResult, env mcpmeta.Envelope) string {
	if understandFormatCount != nil {
		atomic.AddInt64(understandFormatCount, 1)
	}
	counts := understandCountsResult{
		Symbol:                result.Symbol,
		Tier:                  result.Tier,
		CalleesCount:          len(result.Callees),
		CallersCount:          len(result.Callers),
		ProductionCallerCount: result.ProductionCallerCount,
		DeadCodeScore:         result.DeadCodeScore,
		DeadCodeNote:          result.DeadCodeNote,
		StructuralRank:        result.StructuralRank,
		Warnings:              result.Warnings,
	}
	data, err := json.Marshal(&counts)
	if err != nil {
		return fmt.Sprintf(`{"error":"marshal: %s"}`, err.Error())
	}
	return appendMetaFooter(string(data), env)
}
