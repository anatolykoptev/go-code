package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"log/slog"
	"time"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/codegraph"
	"github.com/anatolykoptev/vaelor/internal/codesearch"
	"github.com/anatolykoptev/vaelor/internal/mcpmeta"
	"github.com/anatolykoptev/vaelor/internal/oxcodes"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// CodeSearchInput is the input schema for the code_search tool.
type CodeSearchInput struct {
	Repo          string `json:"repo,omitempty" jsonschema:"Repository to search: GitHub slug (owner/repo), full GitHub URL, or absolute local host path (e.g. /host/src/<name>). Not schema-required: when omitted, the handler infers it from the path argument if it lies inside a known indexed checkout — otherwise the call fails with a short error naming recent repos."`
	Pattern       string `json:"pattern,omitempty" jsonschema:"Primary search term — a literal string or regex (controlled by is_regex). This is what grep matches against; it is NOT a semantic query. If both pattern and query are supplied, pattern wins and query is silently ignored. At least one of pattern/query must be non-empty after normalization or the call errors with 'pattern is required'."`
	Query         string `json:"query,omitempty" jsonschema:"Alias for pattern — used only when pattern is empty (normalizeCodeSearchInput copies query into pattern). Neither pattern nor query is semantic; semantic search is a FALLBACK that triggers automatically when the literal/regex grep returns zero matches, not a mode you select via this parameter."`
	IsRegex       bool   `json:"is_regex,omitempty" jsonschema:"Treat pattern as regular expression (default: literal)"`
	FileGlob      string `json:"file_glob,omitempty" jsonschema:"File glob filter (e.g. '*.go', '*.py')"`
	Path          string `json:"path,omitempty" jsonschema:"Directory path filter — alias for file_glob (e.g. 'internal/query'). Converted to file_glob automatically."`
	Language      string `json:"language,omitempty" jsonschema:"Limit search to files of this language (e.g. go, python, typescript)"`
	ContextLines  int    `json:"context_lines,omitempty" jsonschema:"Number of context lines before/after each match (default: 2)"`
	MaxResults    int    `json:"max_results,omitempty" jsonschema:"Maximum number of matches to return (default: 50, max: 200)"`
	CaseSensitive *bool  `json:"case_sensitive,omitempty" jsonschema:"Case-sensitive matching (default: true). Set false for case-insensitive."`
	ExcludeGlob   string `json:"exclude_glob,omitempty" jsonschema:"Comma-separated glob patterns to exclude files (e.g. 'docs/*,vendor/*'). Matches against relative paths."`
	Scope         string `json:"scope,omitempty" jsonschema:"AST scope filter: function_bodies, comments, strings, type_definitions, imports. Requires language."`
	Structural    bool   `json:"structural,omitempty" jsonschema:"Treat pattern as structural AST pattern with $WILDCARDS (e.g. 'if $ERR != nil { return $ERR }'). Requires language."`
	Expand        string `json:"expand,omitempty" jsonschema:"Expand matches to enclosing AST symbol: 'function' (enclosing function/method) or 'block' (function/struct/class/impl). Returns full symbol body."`
	MaxTokens     int    `json:"max_tokens,omitempty" jsonschema:"Maximum token budget for expanded bodies. Matches exceeding this are skipped. Estimate: 1 token ≈ 4 chars."`
	IncludeBody   bool   `json:"include_body,omitempty" jsonschema:"Return the enclosing declaration body for each match (≈80 line cap per match). Convenience alias for expand=\"function\" with a bounded body budget."`
}

type xmlSearchResponse struct {
	XMLName xml.Name  `xml:"response"`
	Search  xmlSearch `xml:"search"`
}

type xmlSearch struct {
	Pattern string           `xml:"pattern,attr"`
	IsRegex bool             `xml:"isRegex,attr"`
	Matches int              `xml:"matches,attr"`
	Items   []xmlSearchMatch `xml:"match"`
}

type xmlSearchMatch struct {
	File     string            `xml:"file,attr"`
	Line     int               `xml:"line,attr"`
	Text     xmlCDATA          `xml:"text"`
	Context  []xmlCDATA        `xml:"ctx,omitempty"`
	Expanded *xmlExpandedBlock `xml:"expanded,omitempty"`
}

type xmlExpandedBlock struct {
	SymbolName string    `xml:"symbol,attr"`
	SymbolKind string    `xml:"kind,attr"`
	LineStart  int       `xml:"lineStart,attr"`
	LineEnd    int       `xml:"lineEnd,attr"`
	Body       *xmlCDATA `xml:"body,omitempty"`
}

func registerCodeSearch(server *mcp.Server, cfg Config, deps analyze.Deps, sem *SemanticDeps) {
	outputDir := cfg.OutputDir

	addTool(server, &mcp.Tool{
		Name: "code_search",
		Description: "Search for code patterns within a repository. " +
			"\"repo\" (owner/repo or /host/src/<name>) is inferred from the path argument when omitted — the call fails only if inference fails. " +
			"Supports literal strings and regular expressions. " +
			"Returns matching lines with file paths, line numbers, and surrounding context. " +
			"Use for finding: TODO comments, error messages, function calls, string literals, " +
			"API endpoints, configuration patterns, or any text pattern in source code. " +
			"Falls back to semantic search when no matches found.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CodeSearchInput) (*mcp.CallToolResult, error) {
		return handleCodeSearch(ctx, input, deps, sem, outputDir)
	})
}

func handleCodeSearch(ctx context.Context, input CodeSearchInput, deps analyze.Deps, sem *SemanticDeps, outputDir string) (*mcp.CallToolResult, error) {
	// Repo inference (issue #569): when repo is omitted but an absolute path
	// lies inside a known indexed checkout, infer repo and note it. Otherwise
	// emit a short, first-line-actionable error naming recent repos.
	repo, inferNote, ok := resolveOrInferRepo(input.Repo, input.Path, "", deps)
	if !ok {
		return errResult(shortMissingRepoMsg(ctx, semStore(sem), deps.LocalRepoDirs)), nil
	}
	input.Repo = repo
	res, err := handleCodeSearchInner(ctx, input, deps, sem, outputDir)
	if err != nil {
		return res, err
	}
	return appendInferNote(res, inferNote), nil
}

// appendInferNote adds the repo-inference note as an extra TextContent block on
// a non-error CallToolResult. Error results and empty notes pass through so
// tool-error messages stay clean.
func appendInferNote(result *mcp.CallToolResult, note string) *mcp.CallToolResult {
	if note == "" || result == nil || result.IsError {
		return result
	}
	out := *result
	out.Content = append([]mcp.Content{}, result.Content...)
	out.Content = append(out.Content, &mcp.TextContent{Text: note})
	return &out
}

func handleCodeSearchInner(ctx context.Context, input CodeSearchInput, deps analyze.Deps, sem *SemanticDeps, outputDir string) (*mcp.CallToolResult, error) {
	normalizeCodeSearchInput(&input)
	if input.Pattern == "" {
		return errResult("pattern is required"), nil
	}

	root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps)
	if err != nil {
		return errResult(fmt.Sprintf("resolve repo: %s", err)), nil
	}
	defer cleanup()

	t0 := time.Now()

	// Route to ox-codes for scoped or structural search (no Go fallback for new features).
	if input.Scope != "" && deps.OxCodes != nil {
		return handleScopedSearch(ctx, input, root, deps.OxCodes, outputDir, deps.PathMappings)
	}
	if input.Structural && deps.OxCodes != nil {
		return handleStructuralSearch(ctx, input, root, deps.OxCodes, outputDir, deps.PathMappings)
	}

	// When expand is requested, use ox-codes directly and return expanded format.
	if input.Expand != "" && deps.OxCodes != nil {
		oxMatches, err := grepSearchOx(ctx, input, root, deps.OxCodes)
		if err != nil {
			return errResult(fmt.Sprintf("search: %s", err)), nil
		}
		env := mcpmeta.Wrap(time.Since(t0), "")
		return metaXMLMarshalResult(formatExpandedSearchXML(input, oxMatches), "code_search", outputDir, env), nil
	}

	matches, err := grepSearch(ctx, input, root, deps.OxCodes)
	if err != nil {
		return errResult(fmt.Sprintf("search: %s", err)), nil
	}

	// Semantic fallback when grep finds nothing.
	if len(matches) == 0 {
		if suggestions := semanticSuggest(ctx, sem, root, input.Pattern, input.Language); suggestions != "" {
			env := mcpmeta.Wrap(time.Since(t0), "")
			return metaResult(formatCodeSearchNoMatch(input.Pattern, suggestions), env), nil
		}
	}

	// Extract hint from first hit (if exactly one match exists).
	var firstSym string
	if len(matches) == 1 {
		firstLine := matches[0].File + ":" + fmt.Sprintf("%d", matches[0].Line) + ":" + matches[0].Text
		firstSym = mcpmeta.ExtractSymbolFromHit(firstLine)
	}
	query := input.Pattern
	if query == "" {
		query = input.Query
	}
	hint := mcpmeta.HintAfterCodeSearch(query, len(matches), firstSym)
	env := mcpmeta.Wrap(time.Since(t0), hint)
	if sha := deps.IndexedSHA(ctx, codegraph.GraphNameFor(root)); sha != "" {
		env = mcpmeta.WithFreshness(env, root, sha)
	}
	return metaXMLMarshalResult(formatCodeSearchXML(input, matches, deps.PathMappings), "code_search", outputDir, env), nil
}

// grepSearch runs grep via ox-codes with fallback to Go codesearch.
func grepSearch(ctx context.Context, input CodeSearchInput, root string, client *oxcodes.Client) ([]codesearch.SearchMatch, error) {
	searchInput := buildCodeSearchInput(input, root)

	if client != nil {
		oxResult, err := client.Search(ctx, oxcodes.SearchInput{
			Root:          searchInput.Root,
			Pattern:       searchInput.Pattern,
			IsRegex:       searchInput.IsRegex,
			FileGlob:      searchInput.FileGlob,
			ExcludeGlob:   searchInput.ExcludeGlob,
			ContextLines:  searchInput.ContextLines,
			MaxResults:    searchInput.MaxResults,
			CaseSensitive: searchInput.CaseSensitive,
			Language:      searchInput.Language,
		})
		if err == nil {
			return convertOxMatches(oxResult.Matches), nil
		}
		slog.Warn("ox-codes search failed, falling back to Go codesearch", "err", err)
	}

	return codesearch.Search(ctx, searchInput)
}

// grepSearchOx runs grep via ox-codes with expand support, returning raw ox matches.
func grepSearchOx(ctx context.Context, input CodeSearchInput, root string, client *oxcodes.Client) ([]oxcodes.SearchMatch, error) {
	searchInput := buildCodeSearchInput(input, root)
	// Only request markdown when expand is active — otherwise body is empty.
	format := ""
	if input.Expand != "" {
		format = "markdown"
	}
	oxResult, err := client.Search(ctx, oxcodes.SearchInput{
		Root:          searchInput.Root,
		Pattern:       searchInput.Pattern,
		IsRegex:       searchInput.IsRegex,
		FileGlob:      searchInput.FileGlob,
		ExcludeGlob:   searchInput.ExcludeGlob,
		ContextLines:  searchInput.ContextLines,
		MaxResults:    searchInput.MaxResults,
		CaseSensitive: searchInput.CaseSensitive,
		Language:      searchInput.Language,
		Expand:        input.Expand,
		MaxTokens:     input.MaxTokens,
		Format:        format,
	})
	if err != nil {
		return nil, err
	}
	return oxResult.Matches, nil
}

// normalizeCodeSearchInput resolves aliases and sets defaults.
func normalizeCodeSearchInput(input *CodeSearchInput) {
	if input.Pattern == "" && input.Query != "" {
		input.Pattern = input.Query
	}
	if input.Path != "" && input.FileGlob == "" {
		input.FileGlob = input.Path + "/**"
	}
	// include_body (issue #568, 4x demand): convenience alias for expand="function"
	// with a bounded body budget (~80 lines ≈ 800 tokens). Does not override an
	// explicit expand/max_tokens. Falls back to plain matches when ox-codes is
	// unavailable (the expand path degrades gracefully — see handleCodeSearch).
	if input.IncludeBody {
		if input.Expand == "" {
			input.Expand = "function"
		}
		if input.MaxTokens == 0 {
			input.MaxTokens = includeBodyDefaultMaxTokens
		}
	}
}

// includeBodyDefaultMaxTokens bounds the enclosing-decl body returned per match
// when include_body=true is used without an explicit max_tokens. ≈80 lines at
// ~10 tokens/line (1 token ≈ 4 chars, ~40 chars/line) → 800 tokens.
const includeBodyDefaultMaxTokens = 800

const (
	defaultContextLines = 2
	maxContextLines     = 10
	defaultMaxResults   = 50
	maxResultsCap       = 200
)

// buildCodeSearchInput converts MCP input to internal codesearch.SearchInput.
func buildCodeSearchInput(input CodeSearchInput, root string) codesearch.SearchInput {
	contextLines := input.ContextLines
	if contextLines <= 0 {
		contextLines = defaultContextLines
	}
	if contextLines > maxContextLines {
		contextLines = maxContextLines
	}

	maxResults := input.MaxResults
	if maxResults <= 0 {
		maxResults = defaultMaxResults
	}
	if maxResults > maxResultsCap {
		maxResults = maxResultsCap
	}

	caseSensitive := true
	if input.CaseSensitive != nil {
		caseSensitive = *input.CaseSensitive
	}

	return codesearch.SearchInput{
		Root:          root,
		Pattern:       input.Pattern,
		IsRegex:       input.IsRegex,
		FileGlob:      input.FileGlob,
		ExcludeGlob:   input.ExcludeGlob,
		Language:      input.Language,
		ContextLines:  contextLines,
		MaxResults:    maxResults,
		CaseSensitive: caseSensitive,
	}
}

// formatCodeSearchXML builds the XML response for code_search results.
// mappings is used to reverse-translate container-internal paths to host paths.
func formatCodeSearchXML(input CodeSearchInput, matches []codesearch.SearchMatch, mappings []analyze.PathMapping) xmlSearchResponse {
	resp := xmlSearchResponse{
		Search: xmlSearch{
			Pattern: input.Pattern,
			IsRegex: input.IsRegex,
			Matches: len(matches),
			Items:   make([]xmlSearchMatch, len(matches)),
		},
	}
	for i, m := range matches {
		item := xmlSearchMatch{
			File: reverseToHost(m.File, mappings),
			Line: m.Line,
			Text: xmlCDATA{Inner: wrapCDATA(m.Text)},
		}
		for _, c := range m.Context {
			if c != "" {
				item.Context = append(item.Context, xmlCDATA{Inner: wrapCDATA(c)})
			}
		}
		resp.Search.Items[i] = item
	}
	return resp
}
