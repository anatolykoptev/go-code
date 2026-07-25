package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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
}

// understandBuildFromRepo is the production seam for callgraph.BuildFromRepo;
// handler-level tests can override it to avoid heavy parsing.
var understandBuildFromRepo = callgraph.BuildFromRepo

func registerUnderstand(server *mcp.Server, _ Config, deps analyze.Deps, sem *SemanticDeps, graphStore *codegraph.Store) {
	addTool(server, &mcp.Tool{
		Name: "understand",
		Description: "Deep-dive into a single symbol. Aggregates: symbol info + callees + callers + complexity. " +
			"Returns type-aware results for Go repos (interface dispatch resolution). " +
			"Use instead of separate call_trace + symbol_search + code_graph calls. " +
			"Suggests similar symbols when the target is not found. " +
			"When a code_graph snapshot exists: shows tested_by (test functions covering this symbol) " +
			"and dead_code_score (CE reranker confidence that this function is unused, if applicable).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input UnderstandInput) (*mcp.CallToolResult, error) {
		return handleUnderstand(ctx, input, deps, sem, graphStore)
	})
}

func handleUnderstand(ctx context.Context, input UnderstandInput, deps analyze.Deps, sem *SemanticDeps, graphStore *codegraph.Store) (*mcp.CallToolResult, error) {
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
	result := compound.Understand(ctx, matches[0], cg, opts)

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

	data, err := json.Marshal(result)
	if err != nil {
		return errResult(fmt.Sprintf("marshal: %s", err)), nil
	}
	// understand is a terminal call — no chaining hint.
	env := mcpmeta.Wrap(time.Since(t0), "")
	if sha := deps.IndexedSHA(ctx, codegraph.GraphNameFor(root)); sha != "" {
		env = mcpmeta.WithFreshness(env, root, sha)
	}
	return metaResult(string(data), env), nil
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
