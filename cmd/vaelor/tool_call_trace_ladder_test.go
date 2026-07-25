package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/go-kit/llm"
	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/codegraph"
	"github.com/anatolykoptev/vaelor/internal/mcpmeta"
	"github.com/anatolykoptev/vaelor/internal/parser"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// --- call_trace ladder tests (#685 part 2) ---
//
// Each test drives the REAL handleCallTrace path via the callTraceTraceFromAGE
// test seam (returns a large fake TraceResult without a live AGE graph). The
// freshness gate (ensureAgeGraphOrStatus) is bypassed because the AGE fast path
// succeeds (result != nil, Root != nil) — the gate only runs when the fast path
// misses. The ladder is placed after the gate, so it never runs on the
// building-status short-circuit path.

// buildLargeTraceResult builds a TraceResult with a tree large enough to
// overflow DefaultBudget (8192) at full depth but fit at depth-1.
//
// Structure: 1 root → 50 children (depth 1) → 5 grandchildren each (depth 2).
// Total: 1 + 50 + 250 = 301 nodes. Full rendering ~24K (overflows), depth-1
// rendering ~4K (fits), counts rendering tiny (fits).
func buildLargeTraceResult() *callgraph.TraceResult {
	root := &parser.Symbol{
		Name:      "HandleRequest",
		Kind:      "function",
		File:      "internal/handler/handler.go",
		StartLine: 1,
		EndLine:   100,
	}

	children := make([]callgraph.CallChainNode, 50)
	for i := 0; i < 50; i++ {
		child := callgraph.CallChainNode{
			Symbol: &parser.Symbol{
				Name:      fmt.Sprintf("helper_%d", i),
				Kind:      "function",
				File:      fmt.Sprintf("internal/pkg/worker_%d.go", i),
				StartLine: uint32(i*10 + 1),
				EndLine:   uint32(i*10 + 5),
			},
		}
		// Each child has 5 grandchildren (depth 2) — elided by the depth-1 rung.
		grandchildren := make([]callgraph.CallChainNode, 5)
		for j := 0; j < 5; j++ {
			grandchildren[j] = callgraph.CallChainNode{
				Symbol: &parser.Symbol{
					Name:      fmt.Sprintf("deep_%d_%d", i, j),
					Kind:      "function",
					File:      fmt.Sprintf("internal/pkg/sub_%d_%d.go", i, j),
					StartLine: uint32(j*10 + 1),
					EndLine:   uint32(j*10 + 3),
				},
			}
		}
		child.Children = grandchildren
		children[i] = child
	}

	return &callgraph.TraceResult{
		Root:       root,
		Tree:       []callgraph.CallChainNode{{Symbol: root, Children: children}},
		MaxDepth:   2,
		TotalNodes: 301,
		Resolved:   301,
		Unresolved: 0,
	}
}

// callTraceLadderDeps builds analyze.Deps with NoOp LLM (no narrative).
func callTraceLadderDeps() analyze.Deps {
	return analyze.Deps{
		LLM:       llm.NoOp{},
		LLMHasKey: false,
	}
}

// setupCallTraceAgeSeam overrides the AGE fast-path seam to return the given
// TraceResult, so handleCallTrace skips the freshness gate and the tree-sitter
// fallback. Returns a cleanup func.
func setupCallTraceAgeSeam(t *testing.T, result *callgraph.TraceResult) func() {
	t.Helper()
	orig := callTraceTraceFromAGE
	callTraceTraceFromAGE = func(context.Context, *codegraph.Store, string, string, string, int) (*callgraph.TraceResult, error) {
		return result, nil
	}
	return func() { callTraceTraceFromAGE = orig }
}

// callTraceResultText extracts the first TextContent block.
func callTraceResultText(t *testing.T, res *mcp.CallToolResult) string {
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

// parseTraceXML tries to parse text as any of the call_trace ladder rung
// shapes. Returns an error if none parse.
func parseTraceXML(text string) error {
	var resp xmlTraceResponse
	if err := xml.Unmarshal([]byte(text), &resp); err == nil {
		return nil
	}
	var counts xmlTraceCountsResponse
	if err := xml.Unmarshal([]byte(text), &counts); err == nil {
		return nil
	}
	type anyResp struct {
		XMLName xml.Name `xml:"response"`
	}
	var ar anyResp
	return xml.Unmarshal([]byte(text), &ar)
}

// TestCallTrace_LargeResultUnderBudget_IsParseableXML is THE bug test for
// call_trace #685: when the call tree is too large for the byte budget, the
// agent must still receive a VALID, parseable XML envelope — not a
// hard-truncated mid-document fragment.
//
// RED-on-revert: remove the ladder wiring from handleCallTrace and this test
// goes RED (xml.Unmarshal errors on the truncated fragment).
func TestCallTrace_LargeResultUnderBudget_IsParseableXML(t *testing.T) {
	cleanup := setupCallTraceAgeSeam(t, buildLargeTraceResult())
	defer cleanup()

	input := CallTraceInput{
		Repo:    t.TempDir(),
		Symbol:  "HandleRequest",
		Compact: true,
	}
	deps := callTraceLadderDeps()
	store := &codegraph.Store{}

	res, err := handleCallTrace(context.Background(), input, deps, nil, "", store)
	if err != nil {
		t.Fatalf("handleCallTrace: %v", err)
	}

	// Simulate the addTool wrapper.
	applyBudgetAndTook(res, 5*time.Millisecond)

	text := callTraceResultText(t, res)

	if err := parseTraceXML(text); err != nil {
		t.Fatalf("result must be parseable XML under budget, got err=%v\ntext (first 400 chars):\n%s",
			err, truncForLog(text, 400))
	}
}

// TestCallTrace_CeilingHolds verifies the ceiling invariant: the returned body
// fits within DefaultBudget and Shape is a no-op.
func TestCallTrace_CeilingHolds(t *testing.T) {
	cleanup := setupCallTraceAgeSeam(t, buildLargeTraceResult())
	defer cleanup()

	input := CallTraceInput{
		Repo:    t.TempDir(),
		Symbol:  "HandleRequest",
		Compact: true,
	}
	deps := callTraceLadderDeps()
	store := &codegraph.Store{}

	res, err := handleCallTrace(context.Background(), input, deps, nil, "", store)
	if err != nil {
		t.Fatalf("handleCallTrace: %v", err)
	}
	text := callTraceResultText(t, res)

	if len(text) > mcpmeta.DefaultBudget {
		t.Fatalf("ceiling violated: len(text)=%d > DefaultBudget=%d\n(first 200):\n%s",
			len(text), mcpmeta.DefaultBudget, truncForLog(text, 200))
	}
	if shaped := mcpmeta.Shape(text, mcpmeta.DefaultBudget, ""); shaped != text {
		t.Fatalf("Shape must be a no-op: len=%d shaped=%d", len(text), len(shaped))
	}
}

// TestCallTrace_CondensationNote_PresentBelowRung1_AbsentOnRung1 verifies that
// the condensation note is present when the ladder condenses (rung > 1) and
// absent when the full rendering fits (rung 1).
func TestCallTrace_CondensationNote_PresentBelowRung1_AbsentOnRung1(t *testing.T) {
	// Large tree: condenses → condensation note present.
	cleanup := setupCallTraceAgeSeam(t, buildLargeTraceResult())
	defer cleanup()

	input := CallTraceInput{
		Repo:    t.TempDir(),
		Symbol:  "HandleRequest",
		Compact: true,
	}
	deps := callTraceLadderDeps()
	store := &codegraph.Store{}

	largeRes, err := handleCallTrace(context.Background(), input, deps, nil, "", store)
	if err != nil {
		t.Fatalf("handleCallTrace large: %v", err)
	}
	largeText := callTraceResultText(t, largeRes)
	if !strings.Contains(largeText, "<!-- condensed:") {
		t.Fatalf("condensed rung must carry a condensation note, got (first 200):\n%s", truncForLog(largeText, 200))
	}

	// Small tree: fits rung 1 → no condensation note.
	smallRoot := &parser.Symbol{Name: "Foo", Kind: "function", File: "main.go", StartLine: 1, EndLine: 10}
	smallResult := &callgraph.TraceResult{
		Root:       smallRoot,
		Tree:       []callgraph.CallChainNode{{Symbol: smallRoot, Children: []callgraph.CallChainNode{{Symbol: &parser.Symbol{Name: "Bar", Kind: "function", File: "main.go", StartLine: 12, EndLine: 20}}}}},
		TotalNodes: 2,
		MaxDepth:   1,
		Resolved:   2,
	}
	cleanup2 := setupCallTraceAgeSeam(t, smallResult)
	defer cleanup2()

	smallRes, err := handleCallTrace(context.Background(), input, deps, nil, "", store)
	if err != nil {
		t.Fatalf("handleCallTrace small: %v", err)
	}
	smallText := callTraceResultText(t, smallRes)
	if strings.Contains(smallText, "<!-- condensed:") {
		t.Fatalf("rung 1 must NOT carry a condensation note, got (first 200):\n%s", truncForLog(smallText, 200))
	}
}

// TestCallTrace_LargeResultWithOutputDir_FileSaved verifies the file-save
// escape hatch: when outputDir is set and the ladder condenses, the full
// rendering is persisted to a file and the returned text references it.
func TestCallTrace_LargeResultWithOutputDir_FileSaved(t *testing.T) {
	cleanup := setupCallTraceAgeSeam(t, buildLargeTraceResult())
	defer cleanup()

	outputDir := t.TempDir()
	input := CallTraceInput{
		Repo:    t.TempDir(),
		Symbol:  "HandleRequest",
		Compact: true,
	}
	deps := callTraceLadderDeps()
	store := &codegraph.Store{}

	res, err := handleCallTrace(context.Background(), input, deps, nil, outputDir, store)
	if err != nil {
		t.Fatalf("handleCallTrace: %v", err)
	}
	text := callTraceResultText(t, res)

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
	var fullResp xmlTraceResponse
	if err := xml.Unmarshal(fileContent, &fullResp); err != nil {
		t.Fatalf("saved file must be parseable XML: %v\n(first 400 chars):\n%s", err, truncForLog(string(fileContent), 400))
	}
	if fullResp.Trace.TotalNodes <= 0 {
		t.Fatalf("saved file must contain the full result, got totalNodes=%d", fullResp.Trace.TotalNodes)
	}

	// 4. The inline body must be valid, parseable XML (the condensed rung).
	if err := parseTraceXML(text); err != nil {
		t.Fatalf("inline body must be parseable XML: %v\n(first 400 chars):\n%s", err, truncForLog(text, 400))
	}

	// 5. Ceiling must hold.
	if len(text) > mcpmeta.DefaultBudget {
		t.Fatalf("ceiling violated: len(text)=%d > DefaultBudget=%d", len(text), mcpmeta.DefaultBudget)
	}
}

// TestCallTrace_Depth1Rung_StatesDeeperLevelsDropped verifies that the depth-1
// rung explicitly states that deeper levels were dropped and how many were
// elided. A silently shallow tree is a wrong answer, not a condensed one.
func TestCallTrace_Depth1Rung_StatesDeeperLevelsDropped(t *testing.T) {
	cleanup := setupCallTraceAgeSeam(t, buildLargeTraceResult())
	defer cleanup()

	input := CallTraceInput{
		Repo:    t.TempDir(),
		Symbol:  "HandleRequest",
		Compact: true,
	}
	deps := callTraceLadderDeps()
	store := &codegraph.Store{}

	res, err := handleCallTrace(context.Background(), input, deps, nil, "", store)
	if err != nil {
		t.Fatalf("handleCallTrace: %v", err)
	}
	text := callTraceResultText(t, res)

	// Parse as the trace response (the depth-1 rung uses xmlTraceResponse with
	// Condensed/Elided attrs).
	var resp xmlTraceResponse
	if err := xml.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("result must be parseable as xmlTraceResponse: %v\ntext (first 400):\n%s",
			err, truncForLog(text, 400))
	}

	// The depth-1 rung MUST state that deeper levels were dropped.
	if resp.Trace.Condensed != "depth-1" {
		t.Fatalf("depth-1 rung must state condensed=\"depth-1\", got %q", resp.Trace.Condensed)
	}
	// The elided count MUST be > 0 (the tree has 250 grandchildren at depth 2).
	if resp.Trace.Elided <= 0 {
		t.Fatalf("depth-1 rung must state how many nodes were elided, got %d", resp.Trace.Elided)
	}
}
