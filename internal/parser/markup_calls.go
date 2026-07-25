package parser

import (
	_ "embed"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/anatolykoptev/vaelor/internal/parser/preproc"
)

//go:embed queries/markup_refs.scm
var markupRefsQueryBytes []byte

// markupRefsQuery is the bare-top-level-identifier query used ONLY by the markup
// {expr} reparse path. It is compiled once at startup by buildMarkupRefsQuery,
// which handler_tsx.go's init() calls immediately after wiring tsxLang's
// capabilities — so a malformed query fails fast at program start (like every
// sibling handler's mustCompileQuery in init), not lazily on the first .astro
// file, and the compile is guaranteed to see a non-nil TSX grammar without
// depending on cross-file init ordering.
var markupRefsQuery *sitter.Query

// buildMarkupRefsQuery compiles markupRefsQuery against the TSX grammar. Called
// from handler_tsx.go's init() as its last statement (tsxLang.caps is set by
// then, within the same init, so ordering is guaranteed).
func buildMarkupRefsQuery() {
	markupRefsQuery = mustCompileQuery(markupRefsQueryBytes, tsxLang.Capabilities().SitterLanguage, "markup_refs.scm")
}

// remapCallLines rewrites each call site's Line from virtual to original-file
// coordinates via the shared virtualToOriginal helper, dropping sites that map to
// padding (origin 0). Shared by the two VirtualSource call producers:
// markupExprReparse (template region, TSX grammar) and scriptRegionCalls (script
// region, TS grammar).
func remapCallLines(calls []CallSite, lineMap []uint32) []CallSite {
	remapped := calls[:0]
	for _, c := range calls {
		orig := virtualToOriginal(lineMap, c.Line)
		if orig == 0 {
			continue
		}
		c.Line = orig
		remapped = append(remapped, c)
	}
	return remapped
}

// scriptRegionCalls runs the plain-TypeScript CallsQuery over a preprocessor
// handler's extracted <script>/frontmatter VirtualSource and remaps call-site
// line numbers from virtual to original-file coordinates. It is the calls-path
// analogue of parseVirtualWithRemap (the Symbol path): the preprocessor handlers
// (Astro, Svelte) run the delegated grammar over the SCRIPT region ONLY, so the
// template body is served solely by MarkupCalls — one producer per region, no
// duplicate/garbled edges from parsing the raw non-TS file.
func scriptRegionCalls(path string, vs *preproc.VirtualSource, ps *sitter.Parser) []CallSite {
	if vs == nil || len(vs.Code) == 0 {
		return nil
	}
	caps := tsLang.Capabilities()
	if caps.CallsQuery == nil || caps.SitterLanguage == nil {
		return nil
	}

	root, closeTree, err := parseTree(caps.SitterLanguage, vs.Code, ps)
	if err != nil {
		return nil
	}
	defer closeTree()

	calls := runCallQuery(caps.CallsQuery, root, vs.Code, path)
	return remapCallLines(calls, vs.LineMap)
}

// markupExprReparse extracts the function/method/argref call sites embedded in a
// preprocessor-language file's template expressions. The caller supplies the
// batched virtual source produced by the language's expr scanner
// (preproc.ExtractMarkupExprs for Astro plain {expr}; preproc.ExtractSvelteMarkupExprs
// for Svelte {expr} + sigil-aware block-header EXPR) — this is the SINGLE reparse
// path both languages share, differing only in which scanner assembled vs.
//
// It reparses vs.Code with the TSX grammar (tsxLang) rather than plain
// TypeScript: template expressions legally embed JSX (e.g. {list.map(i => <Card/>)}),
// which a plain-TS reparse would reject as ERROR nodes, dropping the calls. Under
// the TSX grammar tsx_calls.scm fires for free (calls / member-calls / argrefs);
// markup_refs.scm additionally captures bare top-level identifiers ({count}) for
// React parity.
//
// Call-site line numbers are remapped from virtual to original-file coordinates
// via remapCallLines; padding lines are dropped. This mirrors the
// collectRuneSymbols / parseSvelteWithRunes post-parse-classifier precedent (operate
// on the original src via a VirtualSource, remap afterwards).
func markupExprReparse(path string, vs *preproc.VirtualSource, ps *sitter.Parser) []CallSite {
	if vs == nil || len(vs.Code) == 0 {
		return nil
	}
	lang := tsxLang.Capabilities().SitterLanguage
	if lang == nil {
		return nil
	}

	root, closeTree, err := parseTree(lang, vs.Code, ps)
	if err != nil {
		return nil
	}
	defer closeTree()

	// tsx_calls.scm: calls, member-calls, argrefs (incl. JSX-expression argrefs).
	// markup_refs.scm: bare top-level identifiers ({count}) for React parity.
	calls := runCallQuery(tsxLang.Capabilities().CallsQuery, root, vs.Code, path)
	calls = append(calls, runCallQuery(markupRefsQuery, root, vs.Code, path)...)

	return remapCallLines(calls, vs.LineMap)
}

// ScriptCalls satisfies scriptCallSource (see calls.go) for Astro: script-region
// calls come from the extracted frontmatter + <script> VirtualSource, clean and
// line-remapped — never a raw CallsQuery over the .astro file.
func (h *astroHandler) ScriptCalls(path string, src []byte, opts ParseOpts) []CallSite {
	return scriptRegionCalls(path, preproc.ExtractAstro(src), opts.Parser)
}

// MarkupCalls satisfies markupCallSource (see calls.go): the Astro handler's
// template body carries {expr} call sites, surfaced by reparsing the batched
// template expressions with the TSX grammar. This is the SOLE producer of Astro's
// template-region calls (the raw CallsQuery is not run for scriptCallSource
// handlers). opts.Parser is honored when non-nil so callers can reuse a parser
// across files; the markup reparse is still language-fixed to TSX.
func (h *astroHandler) MarkupCalls(path string, src []byte, opts ParseOpts) []CallSite {
	return markupExprReparse(path, preproc.ExtractMarkupExprs(src), opts.Parser)
}

// ScriptCalls satisfies scriptCallSource (see calls.go) for Svelte: script-region
// calls come from the extracted <script>/<script module> VirtualSource, clean and
// line-remapped — never a raw CallsQuery over the .svelte file.
func (h *svelteHandler) ScriptCalls(path string, src []byte, opts ParseOpts) []CallSite {
	return scriptRegionCalls(path, preproc.ExtractSvelte(src), opts.Parser)
}

// MarkupCalls satisfies markupCallSource (see calls.go) for Svelte: the template
// body carries plain {expr} mustaches AND sigil-aware block-header expressions
// ({#if EXPR}, {#each EXPR as x}, {#await EXPR then v}, {#key EXPR}, {:else if EXPR},
// {@const NAME = EXPR}, {@html EXPR}, {@render EXPR}). preproc.ExtractSvelteMarkupExprs
// is the sigil-aware scanner; the reparse path is shared with Astro
// (markupExprReparse). This is the SOLE producer of Svelte's template-region calls
// (the raw CallsQuery is not run for scriptCallSource handlers). opts.Parser is
// honored when non-nil so callers can reuse a parser across files.
func (h *svelteHandler) MarkupCalls(path string, src []byte, opts ParseOpts) []CallSite {
	return markupExprReparse(path, preproc.ExtractSvelteMarkupExprs(src), opts.Parser)
}

// ScriptCalls satisfies scriptCallSource (see calls.go) for Vue (issue #409):
// script-region calls come from the extracted <script setup>/<script>
// VirtualSource, clean and line-remapped — never a raw CallsQuery over the whole
// .vue file. It reuses scriptRegionCalls + preproc.ExtractVue, the same seam Astro
// and Svelte use.
//
// The pre-#409 whole-file path leaned on the plain-TS grammar error-recovering
// <template>…</template> into junk nodes (no clean calls), so on a well-formed
// SFC it happened to emit only the script calls. That accident is fragile: when a
// template expression instead reaches the TS parser (markup not enclosed in a
// recognised <template> tag, or a grammar version change) error-recovery captures a
// GARBLED cross-region call — a template identifier with a receiver spanning
// </script> into the markup. Extracting from the isolated <script> region makes the
// leak impossible, while producing byte-identical calls to the old path on every
// valid Vue SFC.
func (h *vueHandler) ScriptCalls(path string, src []byte, opts ParseOpts) []CallSite {
	return scriptRegionCalls(path, preproc.ExtractVue(src), opts.Parser)
}

// MarkupCalls satisfies markupCallSource (see calls.go) for Vue. It returns nil:
// Vue's <template> uses {{ mustache }} interpolation plus v-bind/v-on/v-directive
// attribute expressions — a different surface from the JSX-style {expr} that
// markupExprReparse (Astro/Svelte) reparses — so template-expression call
// extraction is intentionally deferred (matches vueHandler's documented scope:
// <template> expressions are silently ignored; issue #409 leaves it "if desired").
//
// Implementing this method is nonetheless REQUIRED, not optional: ScriptCalls makes
// Vue a scriptCallSource, which routes ExtractCalls away from the raw whole-file
// CallsQuery fallback, so a scriptCallSource that were NOT also a markupCallSource
// would leave the template region with no producer at all — the exact half-wiring
// TestScriptCallSourceImpliesMarkupCallSource forecloses. nil is the deliberate "no
// template calls yet" producer: identical call-graph output to the historical
// whole-file path on every valid Vue SFC (where <template> error-recovered to
// zero clean calls), minus the garbled-edge hazard.
func (h *vueHandler) MarkupCalls(_ string, _ []byte, _ ParseOpts) []CallSite {
	return nil
}
