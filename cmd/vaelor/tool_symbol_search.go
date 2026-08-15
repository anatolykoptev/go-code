package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/codegraph"
	"github.com/anatolykoptev/vaelor/internal/mcpmeta"
	"github.com/anatolykoptev/vaelor/internal/parser"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// SymbolSearchInput is the input schema for the symbol_search tool.
type SymbolSearchInput struct {
	// Repo is the GitHub repo slug (owner/repo) or local filesystem path.
	Repo string `json:"repo" jsonschema:"GitHub repo slug (owner/repo), full GitHub URL, or absolute local host path (e.g. /home/user/src/project)"`

	// Query is the symbol name or pattern to search for.
	Query string `json:"query,omitempty" jsonschema:"Symbol name or pattern to search (supports wildcards: Auth* or *Handler)"`

	// Symbol is an alias for Query — matches call_trace/impact_analysis naming.
	Symbol string `json:"symbol,omitempty" jsonschema:"Alias for query — symbol name or pattern (supports wildcards: Auth* or *Handler)"`

	// Kind filters by symbol kind: function, method, type, struct, interface, const, var.
	Kind string `json:"kind,omitempty" jsonschema:"Filter by kind: function | method | type | struct | interface | const | var (default: all)"`

	// Language filters to files of a specific language.
	Language string `json:"language,omitempty" jsonschema:"Limit search to files of this language (e.g. go, python)"`

	// IncludeBody includes the full function/type body in results.
	IncludeBody bool `json:"include_body,omitempty" jsonschema:"Include the full source body in results (default: false, only signatures)"`

	// Limit caps the number of results returned.
	Limit int `json:"limit,omitempty" jsonschema:"Maximum number of results to return. Default 100, max 500."`

	// Pattern is an ast-grep structural pattern for shape-based search.
	// Required to run via ox-codes; mutually exclusive with name-based query
	// (Pattern wins when both are set). Requires Language.
	Pattern string `json:"pattern,omitempty" jsonschema:"ast-grep structural pattern (e.g. 'func $NAME($CTX context.Context, $$$) error' to find error-returning funcs taking context). Requires language. When set, name-based query is ignored."`
}

type xmlSymbolSearchResponse struct {
	XMLName xml.Name         `xml:"response"`
	Symbols xmlSymbolResults `xml:"symbols"`
}

type xmlSymbolResults struct {
	Query string             `xml:"query,attr"`
	Count int                `xml:"count,attr"`
	Items []xmlSymSearchItem `xml:"symbol"`
}

type xmlSymSearchItem struct {
	Kind       string    `xml:"kind,attr"`
	Name       string    `xml:"name,attr"`
	File       string    `xml:"file,attr"`
	Line       uint32    `xml:"line,attr"`
	End        uint32    `xml:"end,attr"`
	Complexity int       `xml:"complexity,attr,omitempty"`
	Signature  *xmlCDATA `xml:"signature,omitempty"`
	Doc        string    `xml:"doc,omitempty"`
	Body       *xmlCDATA `xml:"body,omitempty"`
}

// registerSymbolSearch registers the symbol_search MCP tool.
// Searches for symbols across a repository's AST index.
func registerSymbolSearch(server *mcp.Server, cfg Config, deps analyze.Deps, sem *SemanticDeps) {
	outputDir := cfg.OutputDir

	addTool(server, &mcp.Tool{
		Name: "symbol_search",
		Description: "Search for functions, types, methods, constants, or variables across a repository. " +
			"Uses tree-sitter AST parsing for accurate symbol extraction (no grep heuristics). " +
			"Supports wildcard patterns (Auth*, *Handler), kind filtering, and language filtering. " +
			"Set 'pattern' for shape-based ast-grep search (requires language) — finds symbols by code shape rather than name, e.g. 'func $N($$$) error' or 'class $C extends $B'. " +
			"Optionally returns full source bodies for matched symbols. " +
			"Falls back to semantic search when no AST matches found.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input SymbolSearchInput) (*mcp.CallToolResult, error) {
		// Allow "symbol" as alias for "query" — matches call_trace/impact_analysis naming.
		if input.Symbol != "" && input.Query == "" {
			input.Query = input.Symbol
		}

		if input.Repo == "" {
			return errResult("repo is required"), nil
		}

		// Shape-based search via ast-grep (ox-codes).
		if input.Pattern != "" {
			if input.Language == "" {
				return errResult("pattern requires language (ast-grep is per-language)"), nil
			}
			if deps.OxCodes == nil {
				return errResult("pattern requires ox-codes backend (OX_CODES_URL not configured)"), nil
			}
			return handleStructuralSymbolSearch(ctx, input, deps, outputDir)
		}

		if input.Query == "" && input.Kind != "" {
			input.Query = "*"
		}
		if input.Query == "" {
			return errResult("query (or symbol or pattern) is required"), nil
		}

		root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps)
		if err != nil {
			return errResult(fmt.Sprintf("resolve repo: %s", err)), nil
		}
		defer cleanup()

		t0 := time.Now()

		symbols, err := analyze.SearchSymbols(ctx, analyze.SymbolSearchInput{
			Root:        root,
			Query:       input.Query,
			Kind:        parser.NodeKind(input.Kind),
			Language:    input.Language,
			IncludeBody: input.IncludeBody,
			Limit:       input.Limit,
		})
		if err != nil {
			return errResult(fmt.Sprintf("symbol search: %s", err)), nil
		}

		if len(symbols) == 0 {
			hint := indexedPathsHint()
			if suggestions := semanticSuggest(ctx, sem, root, input.Query, input.Language); suggestions != "" {
				env := mcpmeta.Wrap(time.Since(t0), "")
				body := formatSymbolSearchNoMatch(input.Query, suggestions) + "\n\n" + hint
				return metaResult(body, env), nil
			}
			return textResult(fmt.Sprintf("No symbols found matching %q.\n\n%s", input.Query, hint)), nil
		}

		// Extract hint: exactly 1 result → suggest understand on that symbol.
		var firstSym string
		if len(symbols) == 1 {
			firstSym = symbols[0].Name
		}
		hint := mcpmeta.HintAfterCodeSearch(input.Query, len(symbols), firstSym)
		env := mcpmeta.Wrap(time.Since(t0), hint)
		env = annotateEnv(env, input.Repo, root, deps.IndexedSHA(ctx, codegraph.GraphNameFor(root)))
		return metaLargeTextResult(formatSymbolSearchXML(input.Query, symbols, root), "symbol_search", outputDir, env), nil
	})
}

// formatSymbolSearchXML formats matched symbols as XML output.
func formatSymbolSearchXML(query string, symbols []*parser.Symbol, root string) string {
	resp := xmlSymbolSearchResponse{
		Symbols: xmlSymbolResults{
			Query: query,
			Count: len(symbols),
			Items: make([]xmlSymSearchItem, len(symbols)),
		},
	}
	for i, sym := range symbols {
		file := sym.File
		if rel, err := filepath.Rel(root, file); err == nil && !strings.HasPrefix(rel, "..") {
			file = rel
		}
		item := xmlSymSearchItem{
			Kind:       string(sym.Kind),
			Name:       sym.Name,
			File:       file,
			Line:       sym.StartLine,
			End:        sym.EndLine,
			Complexity: sym.Complexity,
			Doc:        sym.DocComment,
		}
		if sym.Signature != "" {
			item.Signature = &xmlCDATA{Inner: wrapCDATA(sym.Signature)}
		}
		if sym.Body != "" {
			item.Body = &xmlCDATA{Inner: wrapCDATA(sym.Body)}
		}
		resp.Symbols.Items[i] = item
	}
	data, err := xml.Marshal(resp)
	if err != nil {
		return xmlMarshalErrorFragment(err)
	}
	return xml.Header + string(data)
}
