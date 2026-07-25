package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/callgraph"
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

	// Ensure the counter was actually wired — a broken spy that never
	// increments would pass with count==0, proving nothing.
	if !atomic.CompareAndSwapInt64(&count, 1, 1) && count == 0 {
		t.Fatal("spy never incremented — understandFormatCount not wired to formatUnderstand* functions")
	}
	if count != 1 {
		t.Fatalf("rung-1-fits case must render EXACTLY ONE rung, got %d (eager-render regression)", count)
	}
}
