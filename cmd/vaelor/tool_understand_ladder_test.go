package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/compound"
	"github.com/anatolykoptev/vaelor/internal/graphx"
	"github.com/anatolykoptev/vaelor/internal/learnings"
	"github.com/anatolykoptev/vaelor/internal/mcpmeta"
	"github.com/anatolykoptev/vaelor/internal/parser"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// --- understand ladder tests (#685 part 3) ---
//
// Each test drives the REAL handleUnderstand path via the
// understandBuildFromRepo test seam (returns a large fake CallGraph without
// a live repo parse). The assertions are falsifiable: remove the ladder
// wiring from handleUnderstand and the parse test goes RED (json.Unmarshal
// errors on the hard-truncated fragment).

// longPath builds a long file path string of the form
// "src/pkg/very/deeply/nested/path/.../file_N.go" that inflates the JSON
// rendering past DefaultBudget when many callees/callers carry it.
func longPath(prefix string, i int) string {
	return fmt.Sprintf("src/pkg/very/deeply/nested/%s/path/that/is/long/enough/to/overflow/the/budget/when/many/refs/carry/it/file_%d.go", prefix, i)
}

// buildLargeUnderstandCallGraph builds a CallGraph with 1 target symbol
// "Foo" that has 20 callees and 20 callers (IncludeCallers=true), each with
// a long file path. The full UnderstandResult JSON overflows DefaultBudget
// (~12K); the counts rendering is tiny (~200 bytes).
func buildLargeUnderstandCallGraph(root string) *callgraph.CallGraph {
	target := &parser.Symbol{
		Name:      "Foo",
		Kind:      parser.KindFunction,
		File:      root + "/src/pkg/handler.go",
		StartLine: 1,
		EndLine:   100,
	}

	symbols := []*parser.Symbol{target}
	var edges []callgraph.CallEdge

	// 20 callees: Foo calls 20 different functions with long file paths.
	for i := 0; i < 20; i++ {
		callee := &parser.Symbol{
			Name:      fmt.Sprintf("helper_%02d_with_a_long_name_to_inflate_json", i),
			Kind:      parser.KindFunction,
			File:      longPath("callees", i),
			StartLine: uint32(i*10 + 1),
		}
		symbols = append(symbols, callee)
		edges = append(edges, callgraph.CallEdge{
			Caller:     target,
			Callee:     callee,
			CalleeName: callee.Name,
			Line:       uint32(i + 10),
		})
	}

	// 20 callers: 20 different functions call Foo, with long file paths.
	for i := 0; i < 20; i++ {
		caller := &parser.Symbol{
			Name:      fmt.Sprintf("caller_%02d_with_a_long_name_to_inflate_json", i),
			Kind:      parser.KindFunction,
			File:      longPath("callers", i),
			StartLine: uint32(i*10 + 1),
		}
		symbols = append(symbols, caller)
		edges = append(edges, callgraph.CallEdge{
			Caller:     caller,
			Callee:     target,
			CalleeName: target.Name,
			Line:       uint32(i + 5),
		})
	}

	return &callgraph.CallGraph{
		Edges:   edges,
		Symbols: symbols,
		Tier:    "basic",
	}
}

// setupUnderstandBuildSeam overrides the understandBuildFromRepo seam to
// return the given CallGraph, so handleUnderstand skips the real repo parse.
// Returns a cleanup func.
func setupUnderstandBuildSeam(t *testing.T, cg *callgraph.CallGraph) func() {
	t.Helper()
	orig := understandBuildFromRepo
	understandBuildFromRepo = func(_ context.Context, _ callgraph.TraceRepoInput) (*callgraph.CallGraph, error) {
		return cg, nil
	}
	return func() { understandBuildFromRepo = orig }
}

// understandResultText extracts the first TextContent block.
func understandResultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if res == nil {
		t.Fatal("nil result")
	}
	if res.IsError {
		if len(res.Content) > 0 {
			if tc, ok := res.Content[0].(*mcp.TextContent); ok {
				t.Fatalf("unexpected error result: %s", tc.Text)
			}
		}
		t.Fatalf("unexpected error result: %+v", res)
	}
	if len(res.Content) == 0 {
		t.Fatal("result has no content")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("first content is not TextContent: %T", res.Content[0])
	}
	return tc.Text
}

// parseUnderstandJSON tries to parse text as any of the understand ladder
// rung shapes. Returns an error if none parse — the envelope must be valid
// JSON regardless of which rung was chosen. The condensation note and
// file-save pointer are XML-style comments appended AFTER the JSON body
// (matching the XML tools' pattern and appendMetaFooter's precedent); they
// are not part of the JSON structure, so we extract the JSON part before
// the first "<!--" marker before parsing.
func parseUnderstandJSON(text string) error {
	jsonPart := text
	if idx := strings.Index(text, "<!--"); idx >= 0 {
		jsonPart = text[:idx]
	}
	// Rung 1 / 2: UnderstandResult (has "callees" or "callers" array).
	type fullResp struct {
		Symbol  json.RawMessage `json:"symbol"`
		Callees json.RawMessage `json:"callees,omitempty"`
		Callers json.RawMessage `json:"callers,omitempty"`
		Tier    string          `json:"tier"`
	}
	if err := json.Unmarshal([]byte(jsonPart), &fullResp{}); err == nil {
		return nil
	}
	// Rung 3: understandCountsResult (has "callees_count").
	type countsResp struct {
		Symbol       json.RawMessage `json:"symbol"`
		CalleesCount int             `json:"callees_count"`
		CallersCount int             `json:"callers_count"`
		Tier         string          `json:"tier"`
	}
	if err := json.Unmarshal([]byte(jsonPart), &countsResp{}); err == nil {
		return nil
	}
	// Last resort: check it's at least a valid JSON object.
	var raw map[string]json.RawMessage
	return json.Unmarshal([]byte(jsonPart), &raw)
}

// TestUnderstand_LargeResultUnderBudget_IsParseableJSON is THE bug test for
// understand #685: when the result is too large for the byte budget, the
// agent must still receive a VALID, parseable JSON envelope — not a
// hard-truncated mid-document fragment.
//
// 20 callees + 20 callers with long paths → full rendering ~12K (overflows
// DefaultBudget), counts rendering ~200 bytes (fits). The ladder chooses
// rung 3 (counts) — a complete, parseable document.
//
// RED-on-revert: remove the ladder wiring from handleUnderstand and this
// test goes RED (json.Unmarshal errors on the truncated fragment).
func TestUnderstand_LargeResultUnderBudget_IsParseableJSON(t *testing.T) {
	root := t.TempDir()
	cg := buildLargeUnderstandCallGraph(root)
	cleanup := setupUnderstandBuildSeam(t, cg)
	defer cleanup()

	input := UnderstandInput{
		Repo:           root,
		Symbol:         "Foo",
		IncludeCallers: true,
	}
	deps := analyze.Deps{}

	res, err := handleUnderstand(context.Background(), input, deps, nil, nil, "")
	if err != nil {
		t.Fatalf("handleUnderstand: %v", err)
	}

	// Simulate the addTool wrapper.
	applyBudgetAndTook(res, 5*time.Millisecond)

	text := understandResultText(t, res)

	if err := parseUnderstandJSON(text); err != nil {
		t.Fatalf("result must be parseable JSON under budget, got err=%v\ntext (first 400 chars):\n%s",
			err, truncForLog(text, 400))
	}
}

// TestUnderstand_CeilingHolds verifies the ceiling invariant: the returned
// body fits within the budget and Shape is a no-op.
func TestUnderstand_CeilingHolds(t *testing.T) {
	deps := analyze.Deps{}

	// Small result: fits rung 1 (no condensation). Ceiling must hold.
	smallRoot := t.TempDir()
	smallTarget := &parser.Symbol{
		Name: "Bar", Kind: parser.KindFunction, File: smallRoot + "/main.go", StartLine: 1, EndLine: 10,
	}
	smallCG := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{smallTarget},
		Tier:    "basic",
	}
	smallCleanup := setupUnderstandBuildSeam(t, smallCG)
	smallInput := UnderstandInput{
		Repo:           smallRoot,
		Symbol:         "Bar",
		IncludeCallers: true,
		MaxBytes:       4096,
	}
	smallRes, err := handleUnderstand(context.Background(), smallInput, deps, nil, nil, "")
	smallCleanup()
	if err != nil {
		t.Fatalf("handleUnderstand small: %v", err)
	}
	smallText := understandResultText(t, smallRes)
	smallBudget := mcpmeta.ResolveBudget(4096, mcpmeta.DefaultBudget)
	smallBody := mcpmeta.StripBudgetMarker(smallText)
	if len(smallBody) > smallBudget {
		t.Fatalf("small case: ceiling violated len(body)=%d > budget=%d", len(smallBody), smallBudget)
	}
	if shaped := mcpmeta.Shape(smallBody, smallBudget, ""); shaped != smallBody {
		t.Fatalf("small case: Shape must be a no-op, len=%d shaped=%d", len(smallBody), len(shaped))
	}
	if !mcpmeta.IsShaped(smallText) {
		t.Fatal("small case: max_bytes>0 must produce IsShaped text (marker present)")
	}

	// Large result: condenses. Ceiling must hold against the per-call budget.
	largeRoot := t.TempDir()
	largeCG := buildLargeUnderstandCallGraph(largeRoot)
	largeCleanup := setupUnderstandBuildSeam(t, largeCG)
	defer largeCleanup()
	largeInput := UnderstandInput{
		Repo:           largeRoot,
		Symbol:         "Foo",
		IncludeCallers: true,
		MaxBytes:       4096,
	}
	largeRes, err := handleUnderstand(context.Background(), largeInput, deps, nil, nil, "")
	if err != nil {
		t.Fatalf("handleUnderstand large: %v", err)
	}
	largeText := understandResultText(t, largeRes)
	largeBudget := mcpmeta.ResolveBudget(4096, mcpmeta.DefaultBudget)
	largeBody := mcpmeta.StripBudgetMarker(largeText)
	if len(largeBody) > largeBudget {
		t.Fatalf("large case: ceiling violated len(body)=%d > budget=%d", len(largeBody), largeBudget)
	}
	if shaped := mcpmeta.Shape(largeBody, largeBudget, ""); shaped != largeBody {
		t.Fatalf("large case: Shape must be a no-op, len=%d shaped=%d", len(largeBody), len(shaped))
	}
	if !mcpmeta.IsShaped(largeText) {
		t.Fatal("large case: max_bytes>0 must produce IsShaped text (marker present)")
	}
}

// TestUnderstand_CondensationNote_PresentBelowRung1_AbsentOnRung1 verifies
// that the condensation note is present when the ladder condenses (rung > 1)
// and absent when the full rendering fits (rung 1).
func TestUnderstand_CondensationNote_PresentBelowRung1_AbsentOnRung1(t *testing.T) {
	deps := analyze.Deps{}

	// Small result: fits rung 1 → no condensation note.
	smallRoot := t.TempDir()
	smallTarget := &parser.Symbol{
		Name: "Bar", Kind: parser.KindFunction, File: smallRoot + "/main.go", StartLine: 1, EndLine: 10,
	}
	smallCG := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{smallTarget},
		Tier:    "basic",
	}
	smallCleanup := setupUnderstandBuildSeam(t, smallCG)
	defer smallCleanup()

	smallRes, err := handleUnderstand(context.Background(),
		UnderstandInput{Repo: smallRoot, Symbol: "Bar"}, deps, nil, nil, "")
	if err != nil {
		t.Fatalf("handleUnderstand small: %v", err)
	}
	smallText := understandResultText(t, smallRes)
	if strings.Contains(smallText, "<!-- condensed:") {
		t.Fatalf("rung 1 must NOT carry a condensation note, got (first 200):\n%s", truncForLog(smallText, 200))
	}

	// Large result: condenses → condensation note present.
	largeRoot := t.TempDir()
	largeCG := buildLargeUnderstandCallGraph(largeRoot)
	largeCleanup := setupUnderstandBuildSeam(t, largeCG)
	defer largeCleanup()

	largeRes, err := handleUnderstand(context.Background(),
		UnderstandInput{Repo: largeRoot, Symbol: "Foo", IncludeCallers: true}, deps, nil, nil, "")
	if err != nil {
		t.Fatalf("handleUnderstand large: %v", err)
	}
	largeText := understandResultText(t, largeRes)
	if !strings.Contains(largeText, "<!-- condensed:") {
		t.Fatalf("condensed rung must carry a condensation note, got (first 200):\n%s", truncForLog(largeText, 200))
	}
}

// TestUnderstand_MaxBytesSmallerThanDefaultHonoured verifies that an
// explicit max_bytes smaller than DefaultBudget is honoured — the ladder
// fits that budget, not 8192.
func TestUnderstand_MaxBytesSmallerThanDefaultHonoured(t *testing.T) {
	root := t.TempDir()
	cg := buildLargeUnderstandCallGraph(root)
	cleanup := setupUnderstandBuildSeam(t, cg)
	defer cleanup()

	input := UnderstandInput{
		Repo:           root,
		Symbol:         "Foo",
		IncludeCallers: true,
		MaxBytes:       2048,
	}
	deps := analyze.Deps{}
	res, err := handleUnderstand(context.Background(), input, deps, nil, nil, "")
	if err != nil {
		t.Fatalf("handleUnderstand: %v", err)
	}
	text := understandResultText(t, res)
	body := mcpmeta.StripBudgetMarker(text)

	// The body must fit within the resolved budget (2048), NOT DefaultBudget.
	budget := mcpmeta.ResolveBudget(2048, mcpmeta.DefaultBudget)
	if len(body) > budget {
		t.Fatalf("max_bytes=2048 must be honoured: len(body)=%d > budget=%d\n(first 200):\n%s",
			len(body), budget, truncForLog(body, 200))
	}
	// And it must still parse as valid JSON.
	if err := parseUnderstandJSON(body); err != nil {
		t.Fatalf("result must be parseable JSON under max_bytes, got err=%v\ntext (first 400):\n%s",
			err, truncForLog(body, 400))
	}
}

// TestUnderstand_LargeResultWithOutputDir_FileSaved verifies the file-save
// escape hatch via the HANDLER path (handleUnderstand with outputDir
// threaded through): when outputDir is set and the ladder condenses, the
// full rendering is persisted to a file and the returned text references it.
//
// RED-on-revert: revert handleUnderstand's renderLadder call to pass "" for
// outputDir and this test goes RED (no "saved to:" pointer in the body).
func TestUnderstand_LargeResultWithOutputDir_FileSaved(t *testing.T) {
	root := t.TempDir()
	cg := buildLargeUnderstandCallGraph(root)
	cleanup := setupUnderstandBuildSeam(t, cg)
	defer cleanup()

	outputDir := t.TempDir()
	input := UnderstandInput{
		Repo:           root,
		Symbol:         "Foo",
		IncludeCallers: true,
	}
	deps := analyze.Deps{}

	res, err := handleUnderstand(context.Background(), input, deps, nil, nil, outputDir)
	if err != nil {
		t.Fatalf("handleUnderstand: %v", err)
	}
	text := understandResultText(t, res)

	// 1. The returned text must reference the saved file.
	if !strings.Contains(text, "full-result:") || !strings.Contains(text, "saved to:") {
		t.Fatalf("returned text must reference the saved file, got (first 400 chars):\n%s", truncForLog(text, 400))
	}

	// 2. Extract the file path.
	pathStart := strings.Index(text, "saved to: ")
	if pathStart < 0 {
		t.Fatal("cannot find 'saved to:' in pointer")
	}
	pathStart += len("saved to: ")
	pathEnd := strings.Index(text[pathStart:], " —")
	if pathEnd < 0 {
		t.Fatal("cannot find end of path in pointer")
	}
	savedPath := text[pathStart : pathStart+pathEnd]

	// 3. The file MUST exist with the full rendering.
	fileContent, err := os.ReadFile(savedPath)
	if err != nil {
		t.Fatalf("saved file must exist and be readable: %s", err)
	}
	// The saved file is the full UnderstandResult JSON. Parse it and verify
	// it has the callees array (the full rung, not a condensed rung).
	var fullResp struct {
		Callees []json.RawMessage `json:"callees"`
	}
	if err := json.Unmarshal(fileContent, &fullResp); err != nil {
		t.Fatalf("saved file must be parseable JSON: %v\n(first 400 chars):\n%s", err, truncForLog(string(fileContent), 400))
	}
	if len(fullResp.Callees) <= 0 {
		t.Fatalf("saved file must contain the full result with callees, got %d", len(fullResp.Callees))
	}

	// 4. The inline body must be valid, parseable JSON (the condensed rung).
	if err := parseUnderstandJSON(text); err != nil {
		t.Fatalf("inline body must be parseable JSON: %v\n(first 400 chars):\n%s", err, truncForLog(text, 400))
	}

	// 5. Ceiling must hold.
	if len(text) > mcpmeta.DefaultBudget {
		t.Fatalf("ceiling violated: len(text)=%d > DefaultBudget=%d", len(text), mcpmeta.DefaultBudget)
	}
}

// TestUnderstand_Rung1Fits_RendersExactlyOne proves laziness at the adoption
// site: when the result fits rung 1 (the common case), EXACTLY ONE rung
// rendering is computed — not all three. The spy (understandFormatCount)
// is incremented inside each formatUnderstand* function; if the eager-render
// form comes back (pre-computing all three renderings before the ladder
// runs), the count is 3 and this test goes RED.
//
// RED-on-revert: move the formatUnderstand* calls back out of the closures
// (eager pre-rendering) and the count jumps to 3 → RED.
func TestUnderstand_Rung1Fits_RendersExactlyOne(t *testing.T) {
	root := t.TempDir()
	target := &parser.Symbol{
		Name: "Bar", Kind: parser.KindFunction, File: root + "/main.go", StartLine: 1, EndLine: 10,
	}
	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{target},
		Tier:    "basic",
	}
	cleanup := setupUnderstandBuildSeam(t, cg)
	defer cleanup()

	input := UnderstandInput{
		Repo:   root,
		Symbol: "Bar",
	}
	deps := analyze.Deps{}

	var count int64
	understandFormatCount = &count
	defer func() { understandFormatCount = nil }()

	_, err := handleUnderstand(context.Background(), input, deps, nil, nil, "")
	if err != nil {
		t.Fatalf("handleUnderstand: %v", err)
	}

	// Spy-wiring check: count == 0 means the spy was never incremented,
	// proving nothing (a broken spy passes the laziness check vacuously).
	if count == 0 {
		t.Fatal("spy never incremented — understandFormatCount not wired to formatUnderstand* functions")
	}
	// Laziness check: exactly one render for a rung-1-fits result.
	if count != 1 {
		t.Fatalf("rung-1-fits case must render EXACTLY ONE rung, got %d (eager-render regression)", count)
	}
}

// jsonBodyOf returns the JSON portion of a ladder rendering — everything
// before the first "<!--" marker (condensation note or meta footer). The
// ladder appends XML-comment trailers after the JSON body, so the JSON part
// is what must be valid/parseable on its own.
func jsonBodyOf(text string) string {
	if idx := strings.Index(text, "<!--"); idx >= 0 {
		return text[:idx]
	}
	return text
}

// buildSmallUnderstandCallGraph builds a CallGraph with 1 target "Foo" and a
// FEW callees/callers with SHORT paths — small enough that rung 2 (no
// enrichment) fits DefaultBudget. The enrichment (prior_learnings /
// graph_signals / tested_by), injected via the understandCompound seam, is
// what overflows rung 1 — so rung 2 is the rung that fits, not rung 3.
func buildSmallUnderstandCallGraph(root string) *callgraph.CallGraph {
	target := &parser.Symbol{
		Name:      "Foo",
		Kind:      parser.KindFunction,
		File:      root + "/src/handler.go",
		StartLine: 1,
		EndLine:   40,
	}
	symbols := []*parser.Symbol{target}
	var edges []callgraph.CallEdge
	for i := 0; i < 3; i++ {
		callee := &parser.Symbol{
			Name:      fmt.Sprintf("helper_%d", i),
			Kind:      parser.KindFunction,
			File:      fmt.Sprintf("%s/src/callee_%d.go", root, i),
			StartLine: uint32(i*10 + 1),
		}
		symbols = append(symbols, callee)
		edges = append(edges, callgraph.CallEdge{
			Caller: target, Callee: callee, CalleeName: callee.Name, Line: uint32(i + 10),
		})
	}
	for i := 0; i < 3; i++ {
		caller := &parser.Symbol{
			Name:      fmt.Sprintf("caller_%d", i),
			Kind:      parser.KindFunction,
			File:      fmt.Sprintf("%s/src/caller_%d.go", root, i),
			StartLine: uint32(i*10 + 1),
		}
		symbols = append(symbols, caller)
		edges = append(edges, callgraph.CallEdge{
			Caller: caller, Callee: target, CalleeName: target.Name, Line: uint32(i + 5),
		})
	}
	return &callgraph.CallGraph{Edges: edges, Symbols: symbols, Tier: "basic"}
}

// buildRung2UnderstandResult builds an UnderstandResult with a SMALL call
// topology (so rung 2 fits) but LARGE enrichment (prior_learnings /
// graph_signals / tested_by) — exactly the fields rung 2 drops. Sized so
// rung 1 (full) overflows DefaultBudget and rung 2 (no-learnings) fits.
//
// Fixture populates:
//   - Callees/Callers: 3 each, short paths (the primary payload, preserved
//     by rung 2).
//   - PriorLearnings: 25 records with ~300-char Note fields (~11K bytes) —
//     the bulk of the overflow.
//   - GraphSignals: Found=true with PageRank/Community/Surprise — the
//     persistent-graph enrichment.
//   - TestedBy: 30 SymbolRefs with ~100-char File paths (~4K bytes).
func buildRung2UnderstandResult(root string) *compound.UnderstandResult {
	result := &compound.UnderstandResult{
		Symbol: compound.SymbolInfo{
			Name: "Foo", Kind: "function", File: root + "/src/handler.go",
			StartLine: 1, EndLine: 40,
		},
		Tier: "basic",
	}
	for i := 0; i < 3; i++ {
		result.Callees = append(result.Callees, compound.CallRef{
			Name: fmt.Sprintf("helper_%d", i), File: fmt.Sprintf("%s/src/callee_%d.go", root, i), Line: uint32(i + 10),
		})
		result.Callers = append(result.Callers, compound.CallRef{
			Name: fmt.Sprintf("caller_%d", i), File: fmt.Sprintf("%s/src/caller_%d.go", root, i), Line: uint32(i + 5),
		})
	}
	// Large prior_learnings — the fields rung 2 drops, sized to overflow rung 1.
	longNote := strings.Repeat("prior-learning-note-content-that-is-long-enough-to-inflate-rung1-", 5)
	for i := 0; i < 25; i++ {
		result.PriorLearnings = append(result.PriorLearnings, learnings.Record{
			Repo:          "owner/repo",
			Symbol:        "Foo",
			RiskLevel:     "high",
			ReviewOutcome: "bad",
			Flag:          "critical",
			Note:          fmt.Sprintf("%s#%d", longNote, i),
			PRURL:         fmt.Sprintf("https://github.com/owner/repo/pull/%d", i+1),
		})
	}
	// Real graph_signals (Found=true so it is NOT omitted by omitempty).
	result.GraphSignals = &graphx.Signals{
		PageRank:  0.42,
		Community: "core",
		Surprise:  0.7,
		Found:     true,
	}
	// Large tested_by — more rung-2-dropped content.
	for i := 0; i < 30; i++ {
		result.TestedBy = append(result.TestedBy, graphx.SymbolRef{
			Name: fmt.Sprintf("TestFoo_case_%d", i),
			File: fmt.Sprintf("src/pkg/very/deeply/nested/test/path/that/is/long/enough/to/inflate/file_%d_test.go", i),
		})
	}
	return result
}

// TestUnderstand_Rung2DropsEnrichment_IsRung2ValidSmaller pins the MIDDLE
// rung of the understand ladder (#685 part 3, round 2). The pre-existing
// fixtures leave prior_learnings/graph_signals/tested_by nil (all omitempty),
// so rung 2 rendered byte-for-byte identical to rung 1, overflowed again,
// and the ladder fell through to rung 3 — nothing asserted rung 2 was ever
// reached, valid in isolation, or saved any bytes.
//
// This test populates exactly the fields rung 2 drops, sized so rung 1
// overflows DefaultBudget and rung 2 fits, then asserts:
//  1. the rendering is rung 2 (callees present, prior_learnings/graph_signals/
//     tested_by absent, callees_count absent) — NOT rung 1 and NOT rung 3;
//  2. it is valid, parseable JSON on its own;
//  3. it carries the condensation note and the note names the rung (HOW);
//  4. it is strictly smaller than rung 1's rendering for the same input.
//
// RED-on-revert: make rung 2 stop dropping its fields (render byte-identical
// to rung 1) and assertion 4 goes RED — a rung that saves nothing is not a
// rung. Force the ladder to a different rung and assertion 1 goes RED.
func TestUnderstand_Rung2DropsEnrichment_IsRung2ValidSmaller(t *testing.T) {
	root := t.TempDir()
	cg := buildSmallUnderstandCallGraph(root)
	cgCleanup := setupUnderstandBuildSeam(t, cg)
	defer cgCleanup()

	richResult := buildRung2UnderstandResult(root)
	origCompound := understandCompound
	understandCompound = func(_ context.Context, _ *parser.Symbol, _ *callgraph.CallGraph, _ compound.UnderstandOpts) *compound.UnderstandResult {
		return richResult
	}
	defer func() { understandCompound = origCompound }()

	deps := analyze.Deps{}

	// Rung 2: default budget. Rung 1 (with enrichment) overflows; rung 2 fits.
	rung2Res, err := handleUnderstand(context.Background(),
		UnderstandInput{Repo: root, Symbol: "Foo", IncludeCallers: true}, deps, nil, nil, "")
	if err != nil {
		t.Fatalf("handleUnderstand rung2: %v", err)
	}
	rung2Text := understandResultText(t, rung2Res)
	rung2JSON := strings.TrimSpace(jsonBodyOf(rung2Text))

	// 1. Rung 2 reached: has "callees" (rung 1 or 2), no enrichment keys
	//    (not rung 1), no "callees_count" (not rung 3).
	var keys map[string]json.RawMessage
	if err := json.Unmarshal([]byte(rung2JSON), &keys); err != nil {
		t.Fatalf("rung 2 must be valid JSON: %v\n(first 400):\n%s", err, truncForLog(rung2JSON, 400))
	}
	if _, ok := keys["callees"]; !ok {
		t.Fatalf("rung 2 must carry the callees array (rung 1 or 2 shape), got keys present: %v", mapKeys(keys))
	}
	if _, ok := keys["prior_learnings"]; ok {
		t.Fatal("rung 2 must NOT carry prior_learnings — that is rung 1's enrichment, dropped at rung 2")
	}
	if _, ok := keys["graph_signals"]; ok {
		t.Fatal("rung 2 must NOT carry graph_signals — that is rung 1's enrichment, dropped at rung 2")
	}
	if _, ok := keys["tested_by"]; ok {
		t.Fatal("rung 2 must NOT carry tested_by — that is rung 1's enrichment, dropped at rung 2")
	}
	if _, ok := keys["callees_count"]; ok {
		t.Fatal("rung 2 must NOT carry callees_count — that is the rung 3 shape")
	}

	// 2. Valid, parseable JSON on its own (ladder-shape agnostic).
	if err := parseUnderstandJSON(rung2Text); err != nil {
		t.Fatalf("rung 2 must be parseable JSON: %v\n(first 400):\n%s", err, truncForLog(rung2Text, 400))
	}

	// 3. Condensation note present and names the rung (HOW it was shortened).
	if !strings.Contains(rung2Text, "<!-- condensed:") {
		t.Fatalf("rung 2 must carry a condensation note, got (first 200):\n%s", truncForLog(rung2Text, 200))
	}
	if !strings.Contains(rung2Text, "no-learnings") {
		t.Fatalf("condensation note must name the rung (no-learnings = HOW), got (first 300):\n%s", truncForLog(rung2Text, 300))
	}

	// 4. Strictly smaller than rung 1's rendering for the same input. Rung 1
	//    is the exact function the ladder calls (formatUnderstandFull); we
	//    invoke it directly because MaxBudget (9000) caps the per-call budget
	//    above DefaultBudget, so no MaxBytes override can make the ladder
	//    return rung 1 for a result this large — the comparison is about the
	//    byte savings of rung 2 vs rung 1, a property of the rung design.
	rung1Text := formatUnderstandFull(richResult, mcpmeta.Wrap(0, ""))
	rung1JSON := jsonBodyOf(rung1Text)
	if len(rung2JSON) >= len(rung1JSON) {
		t.Fatalf("rung 2 must be STRICTLY smaller than rung 1: len(rung2)=%d >= len(rung1)=%d\nrung2 (first 200): %s\nrung1 (first 200): %s",
			len(rung2JSON), len(rung1JSON), truncForLog(rung2JSON, 200), truncForLog(rung1JSON, 200))
	}
}

// mapKeys returns the sorted keys of m for deterministic fatal messages.
func mapKeys(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}
