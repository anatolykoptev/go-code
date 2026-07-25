package parser

import (
	"context"
	"fmt"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// Capabilities holds the compiled tree-sitter queries and language reference
// for a LanguageHandler. Fields are nil for handlers that do not support a
// given capability (e.g. no call extraction, no relationship extraction).
type Capabilities struct {
	// SitterLanguage is the tree-sitter grammar. Nil for fallback/regex-only handlers.
	SitterLanguage *sitter.Language

	// TagsQuery is the compiled query for symbol extraction (functions, types, etc.).
	TagsQuery *sitter.Query

	// CallsQuery is the compiled query for call-site extraction. Nil if unsupported.
	CallsQuery *sitter.Query

	// RelationshipsQuery is the compiled query for type relationship extraction. Nil if unsupported.
	RelationshipsQuery *sitter.Query

	// MapCapture converts a single tree-sitter capture into a Symbol.
	// Bound to the handler method so it can access handler-specific logic.
	// Returns nil if the capture should be skipped.
	//
	// IMPORTANT: wire this from a POINTER receiver (e.g. goLang.MapCapture where
	// goLang is *goHandler). A value-receiver bound before init() finishes would
	// capture a zero-value copy and miss any subsequent field writes on the
	// singleton.
	MapCapture func(captureName string, node *sitter.Node, src []byte) *Symbol
}

// parserBase is a composable base for LanguageHandler implementations.
// It stores the language name and its Capabilities, and provides a default
// Parse implementation using tree-sitter. Handlers that need custom parsing
// (e.g. preprocessor languages) override Parse.
type parserBase struct {
	lang string
	caps Capabilities
}

// Language returns the canonical language name (e.g. "go", "python").
func (p *parserBase) Language() string { return p.lang }

// Capabilities returns the handler's tree-sitter capabilities.
func (p *parserBase) Capabilities() Capabilities { return p.caps }

// Parse is the default tree-sitter–based parse implementation.
// If SitterLanguage is nil, it falls back to regex-based tokenization.
// Handlers that need custom preprocessing should embed parserBase and override Parse.
func (p *parserBase) Parse(path string, src []byte, opts ParseOpts) (*ParseResult, error) {
	if p.caps.SitterLanguage == nil {
		return fallbackParse(path, src, p.lang), nil
	}

	root, closeTree, err := parseTree(p.caps.SitterLanguage, src, opts.Parser)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	defer closeTree()

	return p.buildResult(root, src, path, opts), nil
}

// buildResult runs the TagsQuery (and, when requested, the RelationshipsQuery)
// against an already-parsed tree root and returns the populated ParseResult.
// Split out of Parse so ONE parsed tree can drive both the tags query and an
// extra AST walk without re-parsing: the Svelte single-parse path
// (parseSvelteWithRunes) calls this and then walks the SAME root for rune
// symbols before closing the tree, eliminating the former double parse of the
// .svelte <script> bytes (issue #401).
func (p *parserBase) buildResult(root *sitter.Node, src []byte, path string, opts ParseOpts) *ParseResult {
	result := &ParseResult{
		File:     path,
		Language: p.lang,
		Symbols:  make([]*Symbol, 0),
		Imports:  make([]string, 0),
	}
	runQueryWithCaps(result, p.caps, root, src, path, opts)
	if opts.IncludeTypeRels && p.caps.RelationshipsQuery != nil {
		result.TypeRels = runRelQuery(p.caps.RelationshipsQuery, root, src, path, p.lang)
	}
	return result
}

// mustCompileQuery compiles a tree-sitter query or panics with a descriptive message.
// Used in handler init() functions where a query compile failure is a programming error.
func mustCompileQuery(src []byte, lang *sitter.Language, name string) *sitter.Query {
	q, err := sitter.NewQuery(src, lang)
	if err != nil {
		panic(name + " query compile error: " + err.Error())
	}
	return q
}

// parseTree parses code with the given tree-sitter grammar and returns the
// root node plus a cleanup func that closes the tree and (when parseTree
// created the parser) the parser. Factored out of six call sites
// (parserBase.Parse, ExtractCalls, scriptRegionCalls, markupExprReparse,
// ExtractRelationships, collectRuneSymbols) that each repeated the same
// NewParser -> SetLanguage -> ParseCtx -> defer-close sequence — pure plumbing
// consolidation with optional parser reuse. Parse output is unchanged.
//
// If ps is non-nil, it is reused and the caller owns its lifecycle (closeFn
// only closes the tree). If ps is nil, parseTree creates a parser and closeFn
// closes it after the tree (tree before parser under Go's LIFO defer
// semantics). Call closeFn via defer exactly once, and only when err is nil.
//
// lang must be non-nil. Four of the six call sites (parserBase.Parse,
// scriptRegionCalls, markupExprReparse, collectRuneSymbols) guard directly on
// caps.SitterLanguage != nil before calling parseTree. The other two
// (ExtractCalls, ExtractRelationships) guard transitively via
// caps.CallsQuery/caps.RelationshipsQuery != nil — those query fields are
// compiled from the same *sitter.Language at handler init() (mustCompileQuery),
// so a non-nil query implies a non-nil SitterLanguage for every registered
// handler today. This helper does not re-check lang itself.
func parseTree(lang *sitter.Language, code []byte, ps *sitter.Parser) (root *sitter.Node, closeFn func(), err error) {
	ownParser := ps == nil
	if ps == nil {
		ps = sitter.NewParser()
	}
	ps.SetLanguage(lang)

	tree, err := ps.ParseCtx(context.Background(), nil, code)
	if err != nil {
		if ownParser {
			ps.Close()
		}
		return nil, nil, err
	}

	return tree.RootNode(), func() {
		tree.Close()
		if ownParser {
			ps.Close()
		}
	}, nil
}

// runQueryWithCaps executes the TagsQuery from caps against the tree root
// and populates result with symbols and imports.
func runQueryWithCaps(result *ParseResult, caps Capabilities, root *sitter.Node, source []byte, path string, opts ParseOpts) {
	qc := sitter.NewQueryCursor()
	defer qc.Close()
	qc.Exec(caps.TagsQuery, root)

	seen := make(map[string]struct{})
	q := caps.TagsQuery

	for {
		match, ok := qc.NextMatch()
		if !ok {
			break
		}
		for _, capture := range match.Captures {
			captureName := q.CaptureNameForId(capture.Index)
			processCaptureWithCaps(result, caps, captureName, capture.Node, source, path, opts, seen)
		}
	}
}

// processCaptureWithCaps handles a single tree-sitter query capture and updates result.
func processCaptureWithCaps(
	result *ParseResult,
	caps Capabilities,
	captureName string,
	node *sitter.Node,
	source []byte,
	path string,
	opts ParseOpts,
	seen map[string]struct{},
) {
	if captureName == captureImport {
		if opts.IncludeImports {
			importPath := trimQuotes(node.Content(source))
			result.Imports = append(result.Imports, importPath)
		}
		return
	}

	sym := caps.MapCapture(captureName, node, source)
	if sym == nil {
		return
	}

	// #664: when ExpandSymbolKinds is OFF, skip the new low-volume kinds
	// (macro, module) entirely so they never enter pr.Symbols. This is the
	// parse-time gate that makes ALL consumers (codegraph, explore, ingest,
	// embeddings) byte-identical to the pre-#664 parse result when the flag is
	// OFF — not just the embeddings path (which additionally filters via
	// IsEmbeddableKindExpanded as defense-in-depth). Type-alias refinement is
	// gated separately below (the symbol stays, only the kind changes).
	if !opts.ExpandSymbolKinds && (sym.Kind == KindMacro || sym.Kind == KindModule) {
		return
	}

	// #664: when ExpandSymbolKinds is set, refine KindType → KindTypeAlias for
	// type-alias AST nodes (Rust type_item, TS type_alias_declaration, C/C++
	// type_definition/alias_declaration). When the flag is OFF the kind stays
	// KindType — byte-identical to the pre-#664 parse result.
	if opts.ExpandSymbolKinds && sym.Kind == KindType {
		if refined := refineTypeAliasKind(sym, node); refined != sym.Kind {
			sym.Kind = refined
		}
	}

	key := fmt.Sprintf("%s:%s:%d", sym.Kind, sym.Name, sym.StartLine)
	if _, exists := seen[key]; exists {
		return
	}
	seen[key] = struct{}{}

	sym.File = path
	sym.DocComment = extractDocComment(node, source)
	if sym.Kind == KindFunction || sym.Kind == KindMethod {
		sym.Complexity = Complexity(node.Content(source), sym.Language)
		sym.CognitiveComplexity = CognitiveComplexity(node.Content(source), sym.Language)
	}
	if opts.IncludeBody {
		sym.Body = node.Content(source)
	}
	result.Symbols = append(result.Symbols, sym)
}

// trimQuotes strips surrounding single or double quotes from an import path.
// Languages use both `"..."` and `'...'` for import/require strings.
func trimQuotes(s string) string {
	return strings.Trim(s, `"'`)
}
