package parser

import (
	"fmt"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/anatolykoptev/vaelor/internal/parser/preproc"
)

// collectRuneSymbols parses src with the TypeScript grammar and returns KindRune
// symbols at their raw (1-based) source line numbers — no coordinate remapping.
// Used for .svelte.ts/.svelte.js files where the source is the whole file (see
// typescriptHandler.Parse). The in-component .svelte path parses vs.Code only
// ONCE (parseSvelteWithRunes) and does not go through this helper.
func collectRuneSymbols(src []byte, path string, ps *sitter.Parser) []*Symbol {
	if len(src) == 0 {
		return nil
	}
	caps := tsLang.Capabilities()
	if caps.SitterLanguage == nil {
		return nil
	}
	root, closeTree, err := parseTree(caps.SitterLanguage, src, ps)
	if err != nil {
		return nil
	}
	defer closeTree()

	var syms []*Symbol
	walkRuneNodes(root, src, &syms, path)
	return syms
}

// parseSvelteWithRunes parses vs.Code ONCE with the TypeScript grammar and, from
// that single tree, extracts BOTH the ordinary tags-query symbols AND the Svelte
// 5 rune symbols, then remaps every symbol from virtual to original .svelte
// coordinates.
//
// It replaces the former two-parse Svelte path (parseWithTSAndRemap followed by
// appendRuneSymbols), which parsed the identical bytes with the identical grammar
// twice (issue #401). The tree walked here is byte-identical to either former
// parse, so the emitted tags and rune symbols — and their remapped coordinates —
// are unchanged; only the redundant second parse is gone.
//
// path is the ORIGINAL .svelte file path (propagated into Symbol.File); lang is
// the preprocessor-language label ("svelte").
func parseSvelteWithRunes(path string, vs *preproc.VirtualSource, lang string, opts ParseOpts) (*ParseResult, error) {
	base := &tsLang.parserBase
	if vs == nil || len(vs.Code) == 0 {
		return &ParseResult{File: path, Language: lang, Symbols: []*Symbol{}, Imports: []string{}}, nil
	}
	if base.caps.SitterLanguage == nil {
		// TypeScript grammar unwired (not expected in practice): fall back exactly
		// as base.Parse would and remap. Runes need the AST, so none are emitted —
		// this matches the former path, whose fallbackParse produced no rune nodes.
		result := fallbackParse(path, vs.Code, base.lang)
		RemapSymbolLines(result, vs)
		return result, nil
	}

	root, closeTree, err := parseTree(base.caps.SitterLanguage, vs.Code, opts.Parser)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	defer closeTree()

	// Ordinary tags-query symbols (+ optional type relationships) from the tree.
	result := base.buildResult(root, vs.Code, path, opts)

	// Rune symbols from the SAME tree, still in virtual coordinates.
	var runeSyms []*Symbol
	walkRuneNodes(root, vs.Code, &runeSyms, path)

	// Remap the tags symbols in place (drops padding rows, stamps Language), then
	// remap and append the rune symbols after them — preserving the historical
	// [tags…]+[runes…] ordering and the rune remap semantics exactly.
	RemapSymbolLines(result, vs)
	remapAndAppendRunes(result, vs, runeSyms)
	return result, nil
}

// remapAndAppendRunes remaps rune symbols (collected in virtual coordinates) to
// original .svelte coordinates and appends them to result.Symbols. This is the
// exact remap loop the former appendRuneSymbols used: a symbol whose StartLine
// maps onto padding (origStart == 0) is dropped; an EndLine that maps to 0 keeps
// StartLine as its EndLine; Language is stamped to vs.Lang.
func remapAndAppendRunes(result *ParseResult, vs *preproc.VirtualSource, syms []*Symbol) {
	for _, sym := range syms {
		origStart := virtualToOriginal(vs.LineMap, sym.StartLine)
		if origStart == 0 {
			continue // on padding — drop
		}
		origEnd := virtualToOriginal(vs.LineMap, sym.EndLine)
		if origEnd == 0 {
			origEnd = origStart
		}
		sym.StartLine = origStart
		sym.EndLine = origEnd
		sym.Language = vs.Lang
		result.Symbols = append(result.Symbols, sym)
	}
}
