package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/vaelor/internal/embeddings"
	"github.com/anatolykoptev/vaelor/internal/mcpmeta"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// --- semantic_search ladder tests (#685 part 2) ---
//
// Each test drives the REAL finalResult path (the function that builds the
// ladder) with enough candidates to overflow the budget. The assertions are
// falsifiable: remove the ladder wiring from finalResult and the parse test
// goes RED (xml.Unmarshal errors on the hard-truncated fragment).

// semanticLadderDeps builds a minimal SemanticDeps sufficient to drive
// finalResult end-to-end without a live embed-server, Postgres pool, or AGE
// graph. deps.Store is nil (GetIndexedAt skipped), deps.AnalyzeDeps is zero
// (IndexedSHA returns ""), prSignals is nil (annotateWithPageRank no-op).
func semanticLadderDeps() SemanticDeps {
	return SemanticDeps{}
}

// buildSemanticCandidates builds n SearchResult entries across nFiles unique
// files. The full rendering (rank/distance/source/file/symbol/line/language
// per hit) overflows DefaultBudget at ~80+ results; the counts rendering
// (one <file> per unique file) stays small when nFiles is small.
func buildSemanticCandidates(n, nFiles int) []embeddings.SearchResult {
	results := make([]embeddings.SearchResult, n)
	for i := 0; i < n; i++ {
		results[i] = embeddings.SearchResult{
			FilePath:   fmt.Sprintf("src/pkg/file_%d.go", i%nFiles),
			SymbolName: fmt.Sprintf("HandleRequest%d", i),
			SymbolKind: "function",
			Language:   "go",
			StartLine:  i*10 + 1,
			Distance:   0.1234 + float32(i)*0.001,
			Source:     "semantic",
		}
	}
	return results
}

// semanticResultText extracts the first TextContent block from a CallToolResult.
func semanticResultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if res == nil || res.IsError {
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

// parseSemanticXML tries to parse text as any of the semantic_search ladder
// rung shapes. Returns an error if none parse — the envelope must be valid XML
// regardless of which rung was chosen.
func parseSemanticXML(text string) error {
	var resp semanticRespXML
	if err := xml.Unmarshal([]byte(text), &resp); err == nil {
		return nil
	}
	var counts semanticCountsRespXML
	if err := xml.Unmarshal([]byte(text), &counts); err == nil {
		return nil
	}
	// Last resort: check it's at least a valid <response> root.
	type anyResp struct {
		XMLName xml.Name `xml:"response"`
	}
	var ar anyResp
	return xml.Unmarshal([]byte(text), &ar)
}

// TestSemanticSearch_LargeResultUnderBudget_IsParseableXML is THE bug test for
// semantic_search #685: when the result is too large for the byte budget, the
// agent must still receive a VALID, parseable XML envelope — not a
// hard-truncated mid-document fragment.
//
// 100 results across 10 files: full rendering ~12K (overflows DefaultBudget),
// compact ~9K (overflows), counts ~500 bytes (fits). The ladder chooses rung 3
// (counts) — a complete, parseable document.
//
// RED-on-revert: remove the ladder wiring from finalResult and this test goes
// RED (xml.Unmarshal errors on the truncated fragment).
func TestSemanticSearch_LargeResultUnderBudget_IsParseableXML(t *testing.T) {
	candidates := buildSemanticCandidates(100, 10)
	deps := semanticLadderDeps()
	input := SemanticSearchInput{
		Repo:  "/test/repo",
		Query: "handle request",
		TopK:  100,
	}

	res, err := finalResult(context.Background(), input, deps, "repokey", "/test/repo",
		candidates, nil, 100, 0, "", time.Now())
	if err != nil {
		t.Fatalf("finalResult: %v", err)
	}

	// Simulate the addTool wrapper.
	applyBudgetAndTook(res, 5*time.Millisecond, "", "")

	text := semanticResultText(t, res)

	if err := parseSemanticXML(text); err != nil {
		t.Fatalf("result must be parseable XML under budget, got err=%v\ntext (first 400 chars):\n%s",
			err, truncForLog(text, 400))
	}
}

// TestSemanticSearch_CeilingHolds verifies the ceiling invariant: the returned
// body fits within the budget and Shape is a no-op. For max_bytes > 0, the
// budgetAppliedMarker makes the wrapper skip re-shaping; the ceiling is checked
// on the body after stripping the marker (the marker is a wrapper-internal
// sentinel, not agent-visible content). For max_bytes <= 0, the body fits
// DefaultBudget directly.
func TestSemanticSearch_CeilingHolds(t *testing.T) {
	deps := semanticLadderDeps()

	// Small result: fits rung 1 (no condensation). Ceiling must hold.
	smallCandidates := buildSemanticCandidates(5, 5)
	smallInput := SemanticSearchInput{
		Repo:     "/test/repo",
		Query:    "handle request",
		TopK:     5,
		MaxBytes: 4096,
	}
	smallRes, err := finalResult(context.Background(), smallInput, deps, "repokey", "/test/repo",
		smallCandidates, nil, 5, 0, "", time.Now())
	if err != nil {
		t.Fatalf("finalResult small: %v", err)
	}
	smallText := semanticResultText(t, smallRes)
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
	largeCandidates := buildSemanticCandidates(100, 10)
	largeInput := SemanticSearchInput{
		Repo:     "/test/repo",
		Query:    "handle request",
		TopK:     100,
		MaxBytes: 4096,
	}
	largeRes, err := finalResult(context.Background(), largeInput, deps, "repokey", "/test/repo",
		largeCandidates, nil, 100, 0, "", time.Now())
	if err != nil {
		t.Fatalf("finalResult large: %v", err)
	}
	largeText := semanticResultText(t, largeRes)
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

// TestSemanticSearch_CondensationNote_PresentBelowRung1_AbsentOnRung1 verifies
// that the condensation note is present when the ladder condenses (rung > 1)
// and absent when the full rendering fits (rung 1).
func TestSemanticSearch_CondensationNote_PresentBelowRung1_AbsentOnRung1(t *testing.T) {
	deps := semanticLadderDeps()

	// Small result: fits rung 1 → no condensation note.
	smallRes, err := finalResult(context.Background(),
		SemanticSearchInput{Repo: "/test/repo", Query: "test", TopK: 5},
		deps, "repokey", "/test/repo",
		buildSemanticCandidates(5, 5), nil, 5, 0, "", time.Now())
	if err != nil {
		t.Fatalf("finalResult small: %v", err)
	}
	smallText := semanticResultText(t, smallRes)
	if strings.Contains(smallText, "<!-- condensed:") {
		t.Fatalf("rung 1 must NOT carry a condensation note, got (first 200):\n%s", truncForLog(smallText, 200))
	}

	// Large result: condenses → condensation note present.
	largeRes, err := finalResult(context.Background(),
		SemanticSearchInput{Repo: "/test/repo", Query: "test", TopK: 100},
		deps, "repokey", "/test/repo",
		buildSemanticCandidates(100, 10), nil, 100, 0, "", time.Now())
	if err != nil {
		t.Fatalf("finalResult large: %v", err)
	}
	largeText := semanticResultText(t, largeRes)
	if !strings.Contains(largeText, "<!-- condensed:") {
		t.Fatalf("condensed rung must carry a condensation note, got (first 200):\n%s", truncForLog(largeText, 200))
	}
}

// TestSemanticSearch_MaxBytesSmallerThanDefaultHonoured verifies that an
// explicit max_bytes smaller than DefaultBudget is honoured — the ladder fits
// that budget, not 8192.
func TestSemanticSearch_MaxBytesSmallerThanDefaultHonoured(t *testing.T) {
	deps := semanticLadderDeps()
	// 100 results across 10 files → full ~12K, compact ~9K, counts ~500.
	// max_bytes=2048 → the ladder must fit within 2048, not 8192.
	candidates := buildSemanticCandidates(100, 10)
	input := SemanticSearchInput{
		Repo:     "/test/repo",
		Query:    "handle request",
		TopK:     100,
		MaxBytes: 2048,
	}
	res, err := finalResult(context.Background(), input, deps, "repokey", "/test/repo",
		candidates, nil, 100, 0, "", time.Now())
	if err != nil {
		t.Fatalf("finalResult: %v", err)
	}
	text := semanticResultText(t, res)
	body := mcpmeta.StripBudgetMarker(text)

	// The body must fit within the resolved budget (2048), NOT DefaultBudget.
	budget := mcpmeta.ResolveBudget(2048, mcpmeta.DefaultBudget)
	if len(body) > budget {
		t.Fatalf("max_bytes=2048 must be honoured: len(body)=%d > budget=%d\n(first 200):\n%s",
			len(body), budget, truncForLog(body, 200))
	}
	// And it must be strictly smaller than DefaultBudget (proving it didn't
	// just fit 8192 and happen to be under 2048).
	if len(body) > mcpmeta.DefaultBudget {
		t.Fatalf("max_bytes=2048 must produce body smaller than DefaultBudget: len=%d", len(body))
	}
	// The result must still parse as valid XML.
	if err := parseSemanticXML(body); err != nil {
		t.Fatalf("result must be parseable XML under max_bytes, got err=%v\ntext (first 400):\n%s",
			err, truncForLog(body, 400))
	}
}

// TestSemanticSearch_LargeResultWithOutputDir_FileSaved verifies the file-save
// escape hatch via the HANDLER path (finalResult with outputDir threaded
// through): when outputDir is set and the ladder condenses, the full rendering
// is persisted to a file and the returned text references it.
//
// RED-on-revert: revert finalResult's renderLadder call to pass "" for
// outputDir and this test goes RED (no "saved to:" pointer in the body).
func TestSemanticSearch_LargeResultWithOutputDir_FileSaved(t *testing.T) {
	deps := semanticLadderDeps()
	candidates := buildSemanticCandidates(100, 10)
	input := SemanticSearchInput{
		Repo:  "/test/repo",
		Query: "handle request",
		TopK:  100,
	}

	outputDir := t.TempDir()
	res, err := finalResult(context.Background(), input, deps, "repokey", "/test/repo",
		candidates, nil, 100, 0, outputDir, time.Now())
	if err != nil {
		t.Fatalf("finalResult: %v", err)
	}
	text := semanticResultText(t, res)

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
	var fullResp semanticRespXML
	if err := xml.Unmarshal(fileContent, &fullResp); err != nil {
		t.Fatalf("saved file must be parseable XML: %v\n(first 400 chars):\n%s", err, truncForLog(string(fileContent), 400))
	}
	if fullResp.Results.Count <= 0 {
		t.Fatalf("saved file must contain the full result with hits, got count=%d", fullResp.Results.Count)
	}

	// 4. The inline body must be valid, parseable XML (the condensed rung).
	if err := parseSemanticXML(text); err != nil {
		t.Fatalf("inline body must be parseable XML: %v\n(first 400 chars):\n%s", err, truncForLog(text, 400))
	}

	// 5. Ceiling must hold.
	if len(text) > mcpmeta.DefaultBudget {
		t.Fatalf("ceiling violated: len(text)=%d > DefaultBudget=%d", len(text), mcpmeta.DefaultBudget)
	}
}

// TestSemanticSearch_Rung1Fits_RendersExactlyOne proves laziness at the
// adoption site: when the result fits rung 1 (the common case), EXACTLY ONE
// rung rendering is computed — not all three. The spy (semanticFormatCount)
// is incremented inside each formatSemanticResults* function; if the
// eager-render form comes back (pre-computing all three renderings before
// the ladder runs), the count is 3 and this test goes RED.
//
// RED-on-revert: move the formatSemanticResults* calls back out of the closures
// (eager pre-rendering) and the count jumps to 3 → RED.
func TestSemanticSearch_Rung1Fits_RendersExactlyOne(t *testing.T) {
	deps := semanticLadderDeps()
	candidates := buildSemanticCandidates(5, 5) // small — fits rung 1
	input := SemanticSearchInput{
		Repo:  "/test/repo",
		Query: "handle request",
		TopK:  5,
	}

	var count int64
	semanticFormatCount = &count
	defer func() { semanticFormatCount = nil }()

	_, err := finalResult(context.Background(), input, deps, "repokey", "/test/repo",
		candidates, nil, 5, 0, "", time.Now())
	if err != nil {
		t.Fatalf("finalResult: %v", err)
	}

	if count != 1 {
		t.Fatalf("rung-1-fits case must render EXACTLY ONE rung, got %d (eager-render regression)", count)
	}
}
