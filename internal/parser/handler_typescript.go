package parser

import (
	_ "embed"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
)

//go:embed queries/typescript.scm
var typescriptQueryBytes []byte

//go:embed queries/typescript_calls.scm
var tsCallsQueryBytes []byte

//go:embed queries/typescript_rels.scm
var tsRelsQueryBytes []byte

// typescriptHandler implements LanguageHandler for TypeScript and JavaScript source files.
type typescriptHandler struct {
	parserBase
}

// tsLang is the singleton TypeScript language handler, registered on package init.
var tsLang = &typescriptHandler{}

func init() {
	lang := typescript.GetLanguage()
	tsLang.parserBase = parserBase{
		lang: "typescript",
		caps: Capabilities{
			SitterLanguage:     lang,
			TagsQuery:          mustCompileQuery(typescriptQueryBytes, lang, "typescript.scm"),
			CallsQuery:         mustCompileQuery(tsCallsQueryBytes, lang, "typescript_calls.scm"),
			RelationshipsQuery: mustCompileQuery(tsRelsQueryBytes, lang, "typescript_rels.scm"),
			MapCapture:         tsLang.MapCapture,
		},
	}
	registerHandler(tsLang)
}

func (h *typescriptHandler) Extensions() []string {
	return []string{".ts", ".js", ".mjs", ".cjs", ".cts", ".mts"}
}

// Parse parses the source file. For .svelte.ts and .svelte.js files, rune symbols
// are appended after normal TypeScript parsing — these files are Svelte 5 rune modules
// (shared state outside .svelte components) and use the same rune syntax.
func (h *typescriptHandler) Parse(path string, src []byte, opts ParseOpts) (*ParseResult, error) {
	result, err := h.parserBase.Parse(path, src, opts)
	if err != nil {
		return nil, err
	}
	h.finalizeSymbols(result, path, src, opts)
	return result, nil
}

// ParseWithCalls shares ONE tree-sitter parse for symbols+calls (issue #400) then
// applies the same TS/JS-family symbol post-processing as Parse. Because the shared
// parse runs the identical grammar over the identical raw src as Parse does, the
// symbols returned here equal Parse's and the calls equal ExtractCalls's.
func (h *typescriptHandler) ParseWithCalls(path string, src []byte, opts ParseOpts) (*ParseResult, []CallSite, bool, error) {
	result, calls, shared, err := h.parserBase.ParseWithCalls(path, src, opts)
	if err != nil || !shared {
		return result, calls, shared, err
	}
	h.finalizeSymbols(result, path, src, opts)
	return result, calls, true, nil
}

// finalizeSymbols applies the TS/JS-family symbol post-processing shared by Parse
// and ParseWithCalls: append Svelte 5 rune symbols for .svelte.ts/.svelte.js rune
// modules, then correct every symbol's Language (the shared MapCapture and the rune
// collector both hardcode "typescript") to agree with DetectLanguageFromPath —
// override-first for backfill hash reproduction. The rune append runs first so
// ordinary + rune symbols are relabeled uniformly. Kept in one place so the two
// entry points can never diverge.
func (h *typescriptHandler) finalizeSymbols(result *ParseResult, path string, src []byte, opts ParseOpts) {
	if strings.HasSuffix(path, ".svelte.ts") || strings.HasSuffix(path, ".svelte.js") {
		result.Symbols = append(result.Symbols, collectRuneSymbols(src, path, opts.Parser)...)
	}
	applyDetectedSymbolLanguage(result, path, opts)
}

// MapCapture converts a tree-sitter capture to a Symbol for TypeScript/JavaScript.
func (h *typescriptHandler) MapCapture(captureName string, node *sitter.Node, source []byte) *Symbol {
	switch captureName {
	case captureFunction:
		return h.mapFunction(node, source)
	case captureClass:
		return h.mapClass(node, source)
	case captureInterface:
		return h.mapInterface(node, source)
	case captureType:
		return h.mapTypeAlias(node, source)
	case captureMethod:
		return h.mapMethod(node, source)
	}
	return nil
}

func (h *typescriptHandler) mapFunction(node *sitter.Node, source []byte) *Symbol {
	name := h.resolveFunctionName(node, source)
	if name == "" {
		return nil
	}
	return &Symbol{
		Name:      name,
		Kind:      KindFunction,
		Language:  "typescript",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

// resolveFunctionName extracts the function name from a function_declaration,
// lexical_declaration (arrow function), or export_statement wrapping one of those.
func (h *typescriptHandler) resolveFunctionName(node *sitter.Node, source []byte) string {
	switch node.Type() {
	case "function_declaration":
		nameNode := node.ChildByFieldName("name")
		if nameNode == nil {
			return ""
		}
		return nameNode.Content(source)

	case "lexical_declaration", "export_statement":
		// Walk descendants to find the variable_declarator with an arrow_function value.
		return findArrowFunctionName(node, source)
	}
	return ""
}

// findArrowFunctionName walks an AST subtree looking for a variable_declarator
// whose value is an arrow_function, returning the identifier name.
func findArrowFunctionName(node *sitter.Node, source []byte) string {
	if node.Type() == "variable_declarator" {
		valueNode := node.ChildByFieldName("value")
		if valueNode != nil && valueNode.Type() == "arrow_function" {
			nameNode := node.ChildByFieldName("name")
			if nameNode != nil {
				return nameNode.Content(source)
			}
		}
	}
	for i := range int(node.ChildCount()) {
		if name := findArrowFunctionName(node.Child(i), source); name != "" {
			return name
		}
	}
	return ""
}

func (h *typescriptHandler) mapClass(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindClass,
		Language:  "typescript",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

func (h *typescriptHandler) mapInterface(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindInterface,
		Language:  "typescript",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

func (h *typescriptHandler) mapTypeAlias(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindType,
		Language:  "typescript",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}

func (h *typescriptHandler) mapMethod(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:      nameNode.Content(source),
		Kind:      KindMethod,
		Language:  "typescript",
		StartLine: node.StartPoint().Row + 1,
		EndLine:   node.EndPoint().Row + 1,
		Signature: extractSignature(node, source),
	}
}
