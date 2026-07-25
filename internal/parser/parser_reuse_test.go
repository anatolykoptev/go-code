package parser_test

import (
	"testing"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/anatolykoptev/vaelor/internal/parser"
)

// starvedParser returns a *sitter.Parser whose parse is restricted to a single
// byte at the start of whatever code it is fed. tree-sitter's SetIncludedRanges
// is honored by ParseCtx and is NOT reset by SetLanguage (verified against the
// smacker go-tree-sitter binding: SetLanguage only calls ts_parser_set_language),
// so when a callee reuses this parser the resulting tree is starved — no named
// symbols, no call sites — regardless of which grammar or VirtualSource bytes it
// is pointed at. A callee that IGNORES opts.Parser allocates a fresh parser with
// no range restriction and parses fully.
//
// This is the observable that distinguishes reuse from discard: it is per-parser
// (parallel-safe, no global counter), needs no production test seam, and flows
// through the REAL public API (ParseFile / ExtractCalls). Asserting "parse still
// works" does NOT cover this fix — it passes either way; asserting that a symbol
// known to exist in a full parse is ABSENT under the starved shared parser does.
func starvedParser() *sitter.Parser {
	ps := sitter.NewParser()
	ps.SetIncludedRanges([]sitter.Range{{
		StartPoint: sitter.Point{Row: 0, Column: 0},
		EndPoint:   sitter.Point{Row: 0, Column: 1},
		StartByte:  0,
		EndByte:    1,
	}})
	return ps
}

// hasSymbol reports whether any symbol in result has the given Name.
func hasSymbol(result *parser.ParseResult, name string) bool {
	for _, s := range result.Symbols {
		if s.Name == name {
			return true
		}
	}
	return false
}

// TestParseFileReusesParser_Svelte proves the .svelte Parse path
// (parseSvelteWithRunes) reuses opts.Parser. Under the starved shared parser a
// reused path yields no 'count' rune; a discarding path allocates a fresh parser
// and finds it.
func TestParseFileReusesParser_Svelte(t *testing.T) {
	src := []byte(`<script>
	let count = $state(0);
	let doubled = $derived(count * 2);
</script>
<p>{doubled}</p>
`)

	// Control: a nil parser (full parse) must find the rune — proves the fixture
	// is valid so absence under the starved parser is attributable to reuse, not
	// to a broken fixture.
	ctrl, err := parser.ParseFile("ctrl.svelte", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("control parse: %v", err)
	}
	if !hasSymbol(ctrl, "count") {
		t.Fatalf("control: 'count' rune missing from full parse; fixture invalid; symbols=%v", ctrl.Symbols)
	}

	// Reuse: the starved shared parser must be honored → 'count' absent.
	ps := starvedParser()
	defer ps.Close()
	got, err := parser.ParseFile("shared.svelte", src, parser.ParseOpts{Parser: ps})
	if err != nil {
		t.Fatalf("shared parse: %v", err)
	}
	if hasSymbol(got, "count") {
		t.Fatalf("opts.Parser was NOT reused: full parse happened under the starved parser, found 'count'; symbols=%v", got.Symbols)
	}
}

// TestParseFileReusesParser_SvelteRuneModule proves the .svelte.ts rune-module
// path (typescriptHandler.finalizeSymbols → collectRuneSymbols) reuses
// opts.Parser. collectRuneSymbols is the one site that parses the RAW src (no
// VirtualSource), so the starved range applies directly. A discarding path
// allocates a fresh parser for the rune walk and finds 'count' even though the
// ordinary-symbol parse (parserBase.Parse, already correct) was starved.
func TestParseFileReusesParser_SvelteRuneModule(t *testing.T) {
	src := []byte(`let count = $state(0);
let doubled = $derived(count * 2);
`)

	ctrl, err := parser.ParseFile("ctrl.svelte.ts", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("control parse: %v", err)
	}
	if !hasSymbol(ctrl, "count") {
		t.Fatalf("control: 'count' rune missing from full parse; fixture invalid; symbols=%v", ctrl.Symbols)
	}

	ps := starvedParser()
	defer ps.Close()
	got, err := parser.ParseFile("shared.svelte.ts", src, parser.ParseOpts{Parser: ps})
	if err != nil {
		t.Fatalf("shared parse: %v", err)
	}
	if hasSymbol(got, "count") {
		t.Fatalf("collectRuneSymbols did NOT reuse opts.Parser: fresh parser found 'count' rune; symbols=%v", got.Symbols)
	}
}

// TestExtractCallsReusesParser_Svelte proves the Svelte calls path
// (svelteHandler.ScriptCalls → scriptRegionCalls, svelteHandler.MarkupCalls →
// markupExprReparse) reuses opts.Parser. Under the starved shared parser a
// reused path yields no calls; a discarding path allocates fresh parsers and
// finds the script call 'fetchUser' and the markup call 'greet'.
func TestExtractCallsReusesParser_Svelte(t *testing.T) {
	src := []byte(`<script>
	import { fetchUser } from './api';
	function load() { return fetchUser(1); }
</script>
<p>{user.greet()}</p>
`)

	ctrl, err := parser.ExtractCalls("ctrl.svelte", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("control extract: %v", err)
	}
	if !hasCall(ctrl, "fetchUser", 3, false) {
		t.Fatalf("control: missing script call fetchUser@3; fixture invalid; %s", formatCalls(ctrl))
	}

	ps := starvedParser()
	defer ps.Close()
	got, err := parser.ExtractCalls("shared.svelte", src, parser.ParseOpts{Parser: ps})
	if err != nil {
		t.Fatalf("shared extract: %v", err)
	}
	if hasCall(got, "fetchUser", 3, false) {
		t.Fatalf("Svelte calls path did NOT reuse opts.Parser: fresh parser found fetchUser@3; %s", formatCalls(got))
	}
}

// TestExtractCallsReusesParser_Astro proves the Astro calls path
// (astroHandler.ScriptCalls → scriptRegionCalls, astroHandler.MarkupCalls →
// markupExprReparse) reuses opts.Parser. Under the starved shared parser a
// reused path yields no calls; a discarding path allocates fresh parsers and
// finds the frontmatter call 'fetchUser'.
func TestExtractCallsReusesParser_Astro(t *testing.T) {
	src := []byte(`---
import { fetchUser } from './api';
const data = fetchUser(1);
---
<p>{data.name}</p>
`)

	ctrl, err := parser.ExtractCalls("ctrl.astro", src, parser.ParseOpts{})
	if err != nil {
		t.Fatalf("control extract: %v", err)
	}
	if !hasCall(ctrl, "fetchUser", 3, false) {
		t.Fatalf("control: missing frontmatter call fetchUser@3; fixture invalid; %s", formatCalls(ctrl))
	}

	ps := starvedParser()
	defer ps.Close()
	got, err := parser.ExtractCalls("shared.astro", src, parser.ParseOpts{Parser: ps})
	if err != nil {
		t.Fatalf("shared extract: %v", err)
	}
	if hasCall(got, "fetchUser", 3, false) {
		t.Fatalf("Astro calls path did NOT reuse opts.Parser: fresh parser found fetchUser@3; %s", formatCalls(got))
	}
}
