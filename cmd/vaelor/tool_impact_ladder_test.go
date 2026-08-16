package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/go-kit/llm"
	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/impact"
	"github.com/anatolykoptev/vaelor/internal/mcpmeta"
	"github.com/anatolykoptev/vaelor/internal/parser"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// --- impact_analysis ladder tests (#685 part 3) ---
//
// Each test drives the REAL handleImpact path via the impactBuildFromRepo
// test seam (returns a large fake CallGraph without a live repo parse). The
// assertions are falsifiable: remove the ladder wiring from handleImpact
// and the parse test goes RED (json.Unmarshal errors on the hard-truncated
// fragment).

// longImpactPath builds a long file path string that inflates the JSON
// rendering past DefaultBudget when many callers carry it.
func longImpactPath(prefix string, i int) string {
	return fmt.Sprintf("src/pkg/very/deeply/nested/%s/path/that/is/long/enough/to/overflow/the/budget/when/many/callers/carry/it/file_%d.go", prefix, i)
}

// buildLargeImpactCallGraph builds a CallGraph with 1 target symbol "Foo"
// that has 30 direct callers, each with a long file path. The full
// impactOutput JSON overflows DefaultBudget (~12K); the counts rendering is
// tiny (~200 bytes).
func buildLargeImpactCallGraph(root string) *callgraph.CallGraph {
	target := &parser.Symbol{
		Name:      "Foo",
		Kind:      parser.KindFunction,
		File:      root + "/src/pkg/handler.go",
		StartLine: 1,
		EndLine:   100,
	}

	symbols := []*parser.Symbol{target}
	var edges []callgraph.CallEdge

	// 30 direct callers: each calls Foo directly.
	for i := 0; i < 30; i++ {
		caller := &parser.Symbol{
			Name:      fmt.Sprintf("caller_%02d_with_a_long_name_to_inflate_json_output", i),
			Kind:      parser.KindFunction,
			File:      longImpactPath("callers", i),
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

// setupImpactBuildSeam overrides the impactBuildFromRepo seam to return the
// given CallGraph, so handleImpact skips the real repo parse.
// Returns a cleanup func.
func setupImpactBuildSeam(t *testing.T, cg *callgraph.CallGraph) func() {
	t.Helper()
	orig := impactBuildFromRepo
	impactBuildFromRepo = func(_ context.Context, _ callgraph.TraceRepoInput) (*callgraph.CallGraph, error) {
		return cg, nil
	}
	return func() { impactBuildFromRepo = orig }
}

// impactResultText extracts the first TextContent block.
func impactResultText(t *testing.T, res *mcp.CallToolResult) string {
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

// parseImpactJSON tries to parse text as any of the impact_analysis ladder
// rung shapes. Returns an error if none parse — the envelope must be valid
// JSON regardless of which rung was chosen. The condensation note and
// file-save pointer are XML-style comments appended AFTER the JSON body
// (matching the XML tools' pattern and appendMetaFooter's precedent); they
// are not part of the JSON structure, so we extract the JSON part before
// the first "<!--" marker before parsing.
func parseImpactJSON(text string) error {
	jsonPart := text
	if idx := strings.Index(text, "<!--"); idx >= 0 {
		jsonPart = text[:idx]
	}
	// Rung 1 / 2: impactOutput (has "direct_callers" array).
	type fullResp struct {
		Symbol        string          `json:"symbol"`
		Found         bool            `json:"found"`
		TotalAffected int             `json:"total_affected"`
		DirectCallers json.RawMessage `json:"direct_callers"`
		BlastRadius   string          `json:"blast_radius"`
	}
	if err := json.Unmarshal([]byte(jsonPart), &fullResp{}); err == nil {
		return nil
	}
	// Rung 3: impactCountsOutput (has "direct_callers_count").
	type countsResp struct {
		Symbol             string `json:"symbol"`
		TotalAffected      int    `json:"total_affected"`
		DirectCallersCount int    `json:"direct_callers_count"`
		BlastRadius        string `json:"blast_radius"`
	}
	if err := json.Unmarshal([]byte(jsonPart), &countsResp{}); err == nil {
		return nil
	}
	// Last resort: check it's at least a valid JSON object.
	var raw map[string]json.RawMessage
	return json.Unmarshal([]byte(jsonPart), &raw)
}

// impactLadderDeps builds analyze.Deps with NoOp LLM (no narrative, no panic).
func impactLadderDeps() analyze.Deps {
	return analyze.Deps{
		LLM:       llm.NoOp{},
		LLMHasKey: false,
	}
}

// TestImpact_LargeResultUnderBudget_IsParseableJSON is THE bug test for
// impact_analysis #685: when the result is too large for the byte budget,
// the agent must still receive a VALID, parseable JSON envelope — not a
// hard-truncated mid-document fragment.
//
// 30 direct callers with long paths → full rendering ~12K (overflows
// DefaultBudget), counts rendering ~200 bytes (fits). The ladder chooses
// rung 3 (counts) — a complete, parseable document.
//
// RED-on-revert: remove the ladder wiring from handleImpact and this test
// goes RED (json.Unmarshal errors on the truncated fragment).
func TestImpact_LargeResultUnderBudget_IsParseableJSON(t *testing.T) {
	root := t.TempDir()
	cg := buildLargeImpactCallGraph(root)
	cleanup := setupImpactBuildSeam(t, cg)
	defer cleanup()

	input := ImpactInput{Repo: root, Symbol: "Foo"}
	deps := impactLadderDeps()

	res, err := handleImpact(context.Background(), input, deps, nil, "")
	if err != nil {
		t.Fatalf("handleImpact: %v", err)
	}

	// Simulate the addTool wrapper.
	applyBudgetAndTook(res, 5*time.Millisecond, mcpmeta.Envelope{})

	text := impactResultText(t, res)

	if err := parseImpactJSON(text); err != nil {
		t.Fatalf("result must be parseable JSON under budget, got err=%v\ntext (first 400 chars):\n%s",
			err, truncForLog(text, 400))
	}
}

// TestImpact_CeilingHolds verifies the ceiling invariant: the returned body
// fits within the budget and Shape is a no-op.
func TestImpact_CeilingHolds(t *testing.T) {
	deps := impactLadderDeps()

	// Small result: fits rung 1 (no condensation). Ceiling must hold.
	smallRoot := t.TempDir()
	smallTarget := &parser.Symbol{
		Name: "Bar", Kind: parser.KindFunction, File: smallRoot + "/main.go", StartLine: 1, EndLine: 10,
	}
	smallCG := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{smallTarget},
		Tier:    "basic",
	}
	smallCleanup := setupImpactBuildSeam(t, smallCG)
	smallInput := ImpactInput{
		Repo:     smallRoot,
		Symbol:   "Bar",
		MaxBytes: 4096,
	}
	smallRes, err := handleImpact(context.Background(), smallInput, deps, nil, "")
	smallCleanup()
	if err != nil {
		t.Fatalf("handleImpact small: %v", err)
	}
	smallText := impactResultText(t, smallRes)
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
	largeCG := buildLargeImpactCallGraph(largeRoot)
	largeCleanup := setupImpactBuildSeam(t, largeCG)
	defer largeCleanup()
	largeInput := ImpactInput{
		Repo:     largeRoot,
		Symbol:   "Foo",
		MaxBytes: 4096,
	}
	largeRes, err := handleImpact(context.Background(), largeInput, deps, nil, "")
	if err != nil {
		t.Fatalf("handleImpact large: %v", err)
	}
	largeText := impactResultText(t, largeRes)
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

// TestImpact_CondensationNote_PresentBelowRung1_AbsentOnRung1 verifies that
// the condensation note is present when the ladder condenses (rung > 1) and
// absent when the full rendering fits (rung 1).
func TestImpact_CondensationNote_PresentBelowRung1_AbsentOnRung1(t *testing.T) {
	deps := impactLadderDeps()

	// Small result: fits rung 1 → no condensation note.
	smallRoot := t.TempDir()
	smallTarget := &parser.Symbol{
		Name: "Bar", Kind: parser.KindFunction, File: smallRoot + "/main.go", StartLine: 1, EndLine: 10,
	}
	smallCG := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{smallTarget},
		Tier:    "basic",
	}
	smallCleanup := setupImpactBuildSeam(t, smallCG)
	defer smallCleanup()

	smallRes, err := handleImpact(context.Background(),
		ImpactInput{Repo: smallRoot, Symbol: "Bar"}, deps, nil, "")
	if err != nil {
		t.Fatalf("handleImpact small: %v", err)
	}
	smallText := impactResultText(t, smallRes)
	if strings.Contains(smallText, "<!-- condensed:") {
		t.Fatalf("rung 1 must NOT carry a condensation note, got (first 200):\n%s", truncForLog(smallText, 200))
	}

	// Large result: condenses → condensation note present.
	largeRoot := t.TempDir()
	largeCG := buildLargeImpactCallGraph(largeRoot)
	largeCleanup := setupImpactBuildSeam(t, largeCG)
	defer largeCleanup()

	largeRes, err := handleImpact(context.Background(),
		ImpactInput{Repo: largeRoot, Symbol: "Foo"}, deps, nil, "")
	if err != nil {
		t.Fatalf("handleImpact large: %v", err)
	}
	largeText := impactResultText(t, largeRes)
	if !strings.Contains(largeText, "<!-- condensed:") {
		t.Fatalf("condensed rung must carry a condensation note, got (first 200):\n%s", truncForLog(largeText, 200))
	}
}

// TestImpact_MaxBytesSmallerThanDefaultHonoured verifies that an explicit
// max_bytes smaller than DefaultBudget is honoured — the ladder fits that
// budget, not 8192.
func TestImpact_MaxBytesSmallerThanDefaultHonoured(t *testing.T) {
	root := t.TempDir()
	cg := buildLargeImpactCallGraph(root)
	cleanup := setupImpactBuildSeam(t, cg)
	defer cleanup()

	input := ImpactInput{
		Repo:     root,
		Symbol:   "Foo",
		MaxBytes: 2048,
	}
	deps := impactLadderDeps()
	res, err := handleImpact(context.Background(), input, deps, nil, "")
	if err != nil {
		t.Fatalf("handleImpact: %v", err)
	}
	text := impactResultText(t, res)
	body := mcpmeta.StripBudgetMarker(text)

	// The body must fit within the resolved budget (2048), NOT DefaultBudget.
	budget := mcpmeta.ResolveBudget(2048, mcpmeta.DefaultBudget)
	if len(body) > budget {
		t.Fatalf("max_bytes=2048 must be honoured: len(body)=%d > budget=%d\n(first 200):\n%s",
			len(body), budget, truncForLog(body, 200))
	}
	// And it must still parse as valid JSON.
	if err := parseImpactJSON(body); err != nil {
		t.Fatalf("result must be parseable JSON under max_bytes, got err=%v\ntext (first 400):\n%s",
			err, truncForLog(body, 400))
	}
}

// TestImpact_LargeResultWithOutputDir_FileSaved verifies the file-save
// escape hatch via the HANDLER path (handleImpact with outputDir threaded
// through): when outputDir is set and the ladder condenses, the full
// rendering is persisted to a file and the returned text references it.
//
// RED-on-revert: revert handleImpact's renderLadder call to pass "" for
// outputDir and this test goes RED (no "saved to:" pointer in the body).
func TestImpact_LargeResultWithOutputDir_FileSaved(t *testing.T) {
	root := t.TempDir()
	cg := buildLargeImpactCallGraph(root)
	cleanup := setupImpactBuildSeam(t, cg)
	defer cleanup()

	outputDir := t.TempDir()
	input := ImpactInput{Repo: root, Symbol: "Foo"}
	deps := impactLadderDeps()

	res, err := handleImpact(context.Background(), input, deps, nil, outputDir)
	if err != nil {
		t.Fatalf("handleImpact: %v", err)
	}
	text := impactResultText(t, res)

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
	// The saved file is the full impactOutput JSON. Parse it and verify
	// it has the direct_callers array (the full rung, not a condensed rung).
	var fullResp struct {
		DirectCallers []json.RawMessage `json:"direct_callers"`
	}
	if err := json.Unmarshal(fileContent, &fullResp); err != nil {
		t.Fatalf("saved file must be parseable JSON: %v\n(first 400 chars):\n%s", err, truncForLog(string(fileContent), 400))
	}
	if len(fullResp.DirectCallers) <= 0 {
		t.Fatalf("saved file must contain the full result with direct_callers, got %d", len(fullResp.DirectCallers))
	}

	// 4. The inline body must be valid, parseable JSON (the condensed rung).
	if err := parseImpactJSON(text); err != nil {
		t.Fatalf("inline body must be parseable JSON: %v\n(first 400 chars):\n%s", err, truncForLog(text, 400))
	}

	// 5. Ceiling must hold.
	if len(text) > mcpmeta.DefaultBudget {
		t.Fatalf("ceiling violated: len(text)=%d > DefaultBudget=%d", len(text), mcpmeta.DefaultBudget)
	}
}

// TestImpact_Rung1Fits_RendersExactlyOne proves laziness at the adoption
// site: when the result fits rung 1 (the common case), EXACTLY ONE rung
// rendering is computed — not all three. The spy (impactFormatCount) is
// incremented inside each formatImpact* function; if the eager-render form
// comes back (pre-computing all three renderings before the ladder runs),
// the count is 3 and this test goes RED.
//
// RED-on-revert: move the formatImpact* calls back out of the closures
// (eager pre-rendering) and the count jumps to 3 → RED.
func TestImpact_Rung1Fits_RendersExactlyOne(t *testing.T) {
	root := t.TempDir()
	target := &parser.Symbol{
		Name: "Bar", Kind: parser.KindFunction, File: root + "/main.go", StartLine: 1, EndLine: 10,
	}
	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{target},
		Tier:    "basic",
	}
	cleanup := setupImpactBuildSeam(t, cg)
	defer cleanup()

	input := ImpactInput{Repo: root, Symbol: "Bar"}
	deps := impactLadderDeps()

	var count int64
	impactFormatCount = &count
	defer func() { impactFormatCount = nil }()

	_, err := handleImpact(context.Background(), input, deps, nil, "")
	if err != nil {
		t.Fatalf("handleImpact: %v", err)
	}

	// Spy-wiring check: count == 0 means the spy was never incremented,
	// proving nothing (a broken spy passes the laziness check vacuously).
	if count == 0 {
		t.Fatal("spy never incremented — impactFormatCount not wired to formatImpact* functions")
	}
	// Laziness check: exactly one render for a rung-1-fits result.
	if count != 1 {
		t.Fatalf("rung-1-fits case must render EXACTLY ONE rung, got %d (eager-render regression)", count)
	}
}

// stubNarrativeLLM is a stub llm.Completer that returns a fixed narrative
// string. It lets the impact rung-2 test populate the Narrative field (which
// NoOp leaves empty) without a live LLM, driving the real generateNarrative
// → handleImpact path.
type stubNarrativeLLM struct{ narrative string }

func (s stubNarrativeLLM) Complete(_ context.Context, _, _ string, _ ...llm.ChatOption) (string, error) {
	return s.narrative, nil
}

// buildSmallImpactCallGraph builds a CallGraph with 1 target "Foo" and a FEW
// direct callers with SHORT paths — small enough that rung 2 (no narrative)
// fits DefaultBudget. The narrative (injected via the stub LLM) is what
// overflows rung 1 — so rung 2 is the rung that fits, not rung 3.
func buildSmallImpactCallGraph(root string) *callgraph.CallGraph {
	target := &parser.Symbol{
		Name: "Foo", Kind: parser.KindFunction, File: root + "/src/handler.go",
		StartLine: 1, EndLine: 40,
	}
	symbols := []*parser.Symbol{target}
	var edges []callgraph.CallEdge
	for i := 0; i < 3; i++ {
		caller := &parser.Symbol{
			Name: fmt.Sprintf("caller_%d", i), Kind: parser.KindFunction,
			File: fmt.Sprintf("%s/src/caller_%d.go", root, i), StartLine: uint32(i*10 + 1),
		}
		symbols = append(symbols, caller)
		edges = append(edges, callgraph.CallEdge{
			Caller: caller, Callee: target, CalleeName: target.Name, Line: uint32(i + 5),
		})
	}
	return &callgraph.CallGraph{Edges: edges, Symbols: symbols, Tier: "basic"}
}

// TestImpact_Rung2DropsNarrative_IsRung2ValidSmaller pins the MIDDLE rung of
// the impact_analysis ladder (#685 part 3, round 2). The pre-existing
// fixture (impactLadderDeps) wires a NoOp LLM, so generateNarrative returns
// "" and rung 2 (no-narrative) renders byte-for-byte identical to rung 1,
// overflowed again, and the ladder fell through to rung 3 — nothing asserted
// rung 2 was ever reached, valid in isolation, or saved any bytes.
//
// This test wires a stub LLM that returns a ~10K-char narrative, sized so
// rung 1 (topology + narrative) overflows DefaultBudget (8192) and rung 2
// (topology only) fits. Then asserts:
//  1. the rendering is rung 2 (direct_callers present, narrative absent,
//     direct_callers_count absent) — NOT rung 1 and NOT rung 3;
//  2. it is valid, parseable JSON on its own;
//  3. it carries the condensation note and the note names the rung (HOW);
//  4. it is strictly smaller than rung 1's rendering for the same input.
//
// RED-on-revert: make rung 2 stop dropping the narrative (render
// byte-identical to rung 1) and assertion 4 goes RED — a rung that saves
// nothing is not a rung. Force the ladder to a different rung and assertion
// 1 goes RED.
func TestImpact_Rung2DropsNarrative_IsRung2ValidSmaller(t *testing.T) {
	root := t.TempDir()
	cg := buildSmallImpactCallGraph(root)
	cleanup := setupImpactBuildSeam(t, cg)
	defer cleanup()

	// ~10K-char narrative: rung 1 (topology ~1K + narrative ~10K) ≈ 11K,
	// well over DefaultBudget (8192); rung 2 (topology ~1K) fits easily.
	// The narrative is large (not tuned to fit MaxBudget=9000) because rung 1
	// is rendered directly via formatImpactFull for the size comparison —
	// MaxBudget caps the per-call budget above DefaultBudget, so no MaxBytes
	// override can make the ladder return rung 1 for a result this large.
	narrative := strings.Repeat("impact-narrative-prose-", 435)
	stubLLM := stubNarrativeLLM{narrative: narrative}
	deps := analyze.Deps{LLM: stubLLM, LLMHasKey: true}

	// Rung 2: default budget → rung 1 (with narrative) overflows, rung 2 fits.
	rung2Res, err := handleImpact(context.Background(),
		ImpactInput{Repo: root, Symbol: "Foo"}, deps, nil, "")
	if err != nil {
		t.Fatalf("handleImpact rung2: %v", err)
	}
	rung2Text := impactResultText(t, rung2Res)
	rung2JSON := strings.TrimSpace(jsonBodyOf(rung2Text))

	// 1. Rung 2 reached: has "direct_callers" (rung 1 or 2), no "narrative"
	//    (not rung 1), no "direct_callers_count" (not rung 3).
	var keys map[string]json.RawMessage
	if err := json.Unmarshal([]byte(rung2JSON), &keys); err != nil {
		t.Fatalf("rung 2 must be valid JSON: %v\n(first 400):\n%s", err, truncForLog(rung2JSON, 400))
	}
	if _, ok := keys["direct_callers"]; !ok {
		t.Fatalf("rung 2 must carry the direct_callers array (rung 1 or 2 shape), got keys present: %v", mapKeys(keys))
	}
	if _, ok := keys["narrative"]; ok {
		t.Fatal("rung 2 must NOT carry narrative — that is rung 1's LLM prose, dropped at rung 2")
	}
	if _, ok := keys["direct_callers_count"]; ok {
		t.Fatal("rung 2 must NOT carry direct_callers_count — that is the rung 3 shape")
	}

	// 2. Valid, parseable JSON on its own (ladder-shape agnostic).
	if err := parseImpactJSON(rung2Text); err != nil {
		t.Fatalf("rung 2 must be parseable JSON: %v\n(first 400):\n%s", err, truncForLog(rung2Text, 400))
	}

	// 3. Condensation note present and names the rung (HOW it was shortened).
	if !strings.Contains(rung2Text, "<!-- condensed:") {
		t.Fatalf("rung 2 must carry a condensation note, got (first 200):\n%s", truncForLog(rung2Text, 200))
	}
	if !strings.Contains(rung2Text, "no-narrative") {
		t.Fatalf("condensation note must name the rung (no-narrative = HOW), got (first 300):\n%s", truncForLog(rung2Text, 300))
	}

	// 4. Strictly smaller than rung 1's rendering for the same input. Rung 1
	//    is the exact function the ladder calls (formatImpactFull); we
	//    reconstruct the output handleImpact would build (impact.Analyze +
	//    the stub narrative) and invoke it directly, because MaxBudget (9000)
	//    caps the per-call budget above DefaultBudget — no MaxBytes override
	//    can make the ladder return rung 1 for an 11K result. The comparison
	//    is about the byte savings of rung 2 vs rung 1, a property of the
	//    rung design.
	impactResult := impact.Analyze(context.Background(), cg, "Foo", impact.Options{
		MaxDepth: defaultImpactDepth, Root: root, Repo: root,
	})
	rung1Output := impactOutput{Result: impactResult, Tier: cg.Tier, Narrative: narrative}
	rung1Text := formatImpactFull(rung1Output)
	rung1JSON := jsonBodyOf(rung1Text)
	if len(rung2JSON) >= len(rung1JSON) {
		t.Fatalf("rung 2 must be STRICTLY smaller than rung 1: len(rung2)=%d >= len(rung1)=%d\nrung2 (first 200): %s\nrung1 (first 200): %s",
			len(rung2JSON), len(rung1JSON), truncForLog(rung2JSON, 200), truncForLog(rung1JSON, 200))
	}
}
