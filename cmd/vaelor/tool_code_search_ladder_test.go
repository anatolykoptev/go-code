package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/mcpmeta"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestCodeSearch_LargeResultUnderBudget_IsParseableXML is THE bug test for
// vaelor#685: when code_search returns a result too large for the byte
// budget, the agent must still receive a VALID, parseable XML envelope —
// not a hard-truncated mid-document fragment.
//
// It drives the REAL code_search result path (handleCodeSearchInner →
// grepSearch → codesearch.Search → formatCodeSearchXML → metaXMLMarshalResult)
// and then simulates the addTool wrapper (applyBudgetAndTook), which applies
// mcpmeta.Shape at DefaultBudget. On main, Shape hard-truncates the XML
// mid-document and xml.Unmarshal FAILS. With the progressive-shortening
// ladder, code_search renders a cheaper (but complete, parseable) rung that
// fits the budget, so xml.Unmarshal SUCCEEDS.
//
// RED-on-revert: remove the ladder wiring from handleCodeSearchInner and this
// test goes RED (xml.Unmarshal errors on the truncated fragment).
func TestCodeSearch_LargeResultUnderBudget_IsParseableXML(t *testing.T) {
	// Build a local repo whose code_search XML exceeds DefaultBudget (8192).
	// 60 matches across 6 files, each with 2 context lines → ~15 KB of XML.
	repoDir := t.TempDir()
	for f := 0; f < 6; f++ {
		name := string(rune('a'+f)) + ".go"
		var sb strings.Builder
		sb.WriteString("package main\n")
		for i := 0; i < 10; i++ {
			// surrounding lines become context for each match
			sb.WriteString("// padding line number ")
			sb.WriteString(string(rune('A' + (i % 26))))
			sb.WriteString("\n// MARKER hit here\n// more padding\n")
		}
		if err := os.WriteFile(filepath.Join(repoDir, name), []byte(sb.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	deps := analyze.Deps{}
	sem := &SemanticDeps{}

	input := CodeSearchInput{
		Repo:         repoDir,
		Pattern:      "MARKER",
		ContextLines: 2,
		MaxResults:   200,
	}

	res, err := handleCodeSearchInner(context.Background(), input, deps, sem, "")
	if err != nil {
		t.Fatalf("handleCodeSearchInner: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("unexpected error result: %+v", res)
	}

	// Simulate the addTool wrapper — this is where DefaultBudget shaping is
	// applied to the first TextContent block.
	applyBudgetAndTook(res, 5*time.Millisecond, mcpmeta.Envelope{})

	if len(res.Content) == 0 {
		t.Fatal("result has no content")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("first content is not TextContent: %T", res.Content[0])
	}
	text := tc.Text

	// The result must parse as a valid XML envelope. This is the assertion
	// that fails on main (hard-truncated XML) and passes with the ladder.
	var resp xmlSearchResponse
	if err := xml.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("result must be parseable XML under budget, got err=%v\ntext (first 400 chars):\n%s", err, truncForLog(text, 400))
	}
	// The envelope must report the matches (condensed rung still carries the
	// total count — that is the whole point of a valid summary).
	if resp.Search.Matches <= 0 {
		t.Fatalf("parsed envelope reports %d matches — condensation must preserve the count", resp.Search.Matches)
	}
}

func truncForLog(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// TestCodeSearch_LargeResult_FileSaveEscapeHatch is THE regression test for
// the file-save escape hatch (finding 1): when code_search returns a result
// whose FULL rendering exceeds maxInlineCharsDefault AND outputDir is set,
// the full rendering MUST be persisted to a file and the returned text MUST
// reference it. The agent receives a budget-fitting condensed rung inline
// AND can read the complete result via the Read tool.
//
// RED-on-revert: if the file-save wiring is dropped from
// handleCodeSearchInner, no file is created and the pointer assertion fails.
func TestCodeSearch_LargeResult_FileSaveEscapeHatch(t *testing.T) {
	// Build a local repo whose code_search XML exceeds maxInlineCharsDefault
	// (50000). 200 matches across 10 files, each with 2 context lines and
	// long text → ~60 KB of XML.
	repoDir := t.TempDir()
	for f := 0; f < 10; f++ {
		name := string(rune('a'+f)) + ".go"
		var sb strings.Builder
		sb.WriteString("package main\n")
		for i := 0; i < 20; i++ {
			// Long padding lines inflate the full rendering past 50K.
			sb.WriteString("// padding-padding-padding-padding-padding-padding-padding ")
			sb.WriteString(fmt.Sprintf("%d", i))
			sb.WriteString("\n// MARKER hit here with long text padding padding padding\n")
			sb.WriteString("// more-padding-padding-padding-padding-padding-padding-padding\n")
		}
		if err := os.WriteFile(filepath.Join(repoDir, name), []byte(sb.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	outputDir := t.TempDir()
	deps := analyze.Deps{}
	sem := &SemanticDeps{}

	input := CodeSearchInput{
		Repo:         repoDir,
		Pattern:      "MARKER",
		ContextLines: 2,
		MaxResults:   200,
	}

	res, err := handleCodeSearchInner(context.Background(), input, deps, sem, outputDir)
	if err != nil {
		t.Fatalf("handleCodeSearchInner: %v", err)
	}
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
	text := tc.Text

	// 1. The returned text must reference the saved file.
	if !strings.Contains(text, "full-result:") || !strings.Contains(text, "saved to:") {
		t.Fatalf("returned text must reference the saved file, got (first 400 chars):\n%s", truncForLog(text, 400))
	}

	// 2. Extract the file path from the pointer comment.
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

	// 3. The file MUST exist on disk.
	fileContent, err := os.ReadFile(savedPath)
	if err != nil {
		t.Fatalf("saved file must exist and be readable: %s", err)
	}

	// 4. The file's content must be the COMPLETE full rendering — parse it
	//    as XML and verify it has all the matches. This is the anti-vacuity
	//    core: not merely "some file was created" but "the full result is
	//    in the file".
	var fullResp xmlSearchResponse
	if err := xml.Unmarshal(fileContent, &fullResp); err != nil {
		t.Fatalf("saved file must be parseable XML: %v\n(first 400 chars):\n%s", err, truncForLog(string(fileContent), 400))
	}
	if fullResp.Search.Matches <= 0 {
		t.Fatalf("saved file must contain the full result with matches, got %d", fullResp.Search.Matches)
	}
	// The full rendering must have context lines (the condensed inline body
	// may not — but the file is the FULL rung with context).
	if len(fullResp.Search.Items) == 0 {
		t.Fatal("saved file must contain match items")
	}
	hasContext := false
	for _, item := range fullResp.Search.Items {
		if len(item.Context) > 0 {
			hasContext = true
			break
		}
	}
	if !hasContext {
		t.Fatal("saved file (full rung) must contain context lines — the file is the full rendering, not a condensed rung")
	}

	// 5. The inline body must be a valid, parseable XML envelope (the
	//    budget-fitting condensed rung), distinct from the file.
	var inlineResp xmlSearchResponse
	if err := xml.Unmarshal([]byte(text), &inlineResp); err != nil {
		t.Fatalf("inline body must be parseable XML: %v\n(first 400 chars):\n%s", err, truncForLog(text, 400))
	}
	if inlineResp.Search.Matches != fullResp.Search.Matches {
		t.Fatalf("inline body must report same match count as full file: inline=%d file=%d",
			inlineResp.Search.Matches, fullResp.Search.Matches)
	}
}

// TestCodeSearch_LargeResultWithOutputDir_CeilingHolds is THE handler-level
// ceiling test for finding 4: when code_search returns a large result AND
// outputDir is set (so the file-save pointer is appended to the inline body),
// the returned text MUST satisfy len(text) <= DefaultBudget so the addTool
// wrapper's Shape is a provable no-op. Without the reserve mechanism, the
// pointer pushes the body over budget and Shape hard-truncates the tail
// (pointer, condensation note, meta footer) — reintroducing the exact #685
// bug at the margin.
//
// The near-budget window (chosen rung lands within ~pointer-len of the
// ceiling) is data-dependent and not reliably reproducible at the handler
// level — it is tested directly at the PickFitting level in
// TestPickFitting_ReserveKeepsBodyPlusPointerUnderBudget. This test asserts
// the ceiling on a real large-result handler path: the body+pointer must
// stay <= DefaultBudget and Shape must be a no-op.
func TestCodeSearch_LargeResultWithOutputDir_CeilingHolds(t *testing.T) {
	// Reuse the large-result setup (200 matches, 10 files, long padding →
	// full rendering ~60K, well past the budget; the ladder condenses).
	repoDir := t.TempDir()
	for f := 0; f < 10; f++ {
		name := string(rune('a'+f)) + ".go"
		var sb strings.Builder
		sb.WriteString("package main\n")
		for i := 0; i < 20; i++ {
			sb.WriteString("// padding-padding-padding-padding-padding-padding-padding ")
			sb.WriteString(fmt.Sprintf("%d", i))
			sb.WriteString("\n// MARKER hit here with long text padding padding padding\n")
			sb.WriteString("// more-padding-padding-padding-padding-padding-padding-padding\n")
		}
		if err := os.WriteFile(filepath.Join(repoDir, name), []byte(sb.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	outputDir := t.TempDir()
	deps := analyze.Deps{}
	sem := &SemanticDeps{}

	input := CodeSearchInput{
		Repo:         repoDir,
		Pattern:      "MARKER",
		ContextLines: 2,
		MaxResults:   200,
	}

	res, err := handleCodeSearchInner(context.Background(), input, deps, sem, outputDir)
	if err != nil {
		t.Fatalf("handleCodeSearchInner: %v", err)
	}
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
	text := tc.Text

	// 1. The returned text MUST fit within DefaultBudget — the ceiling
	//    invariant. The wrapper's Shape is then a provable no-op.
	if len(text) > mcpmeta.DefaultBudget {
		t.Fatalf("ceiling violated: len(text)=%d > DefaultBudget=%d\n(first 200 chars):\n%s",
			len(text), mcpmeta.DefaultBudget, truncForLog(text, 200))
	}

	// 2. Shape must be a no-op (the defining property of the ceiling).
	if shaped := mcpmeta.Shape(text, mcpmeta.DefaultBudget, ""); shaped != text {
		t.Fatalf("Shape must be a no-op on ceiling-conformant text: len=%d, shaped len=%d\noriginal tail (last 200):\n%s\nshaped tail (last 200):\n%s",
			len(text), len(shaped), truncForLog(text[len(text)-200:], 200), truncForLog(shaped[len(shaped)-200:], 200))
	}

	// 3. The file-save pointer MUST survive (the whole point — the pointer
	//    must not be what gets dropped).
	if !strings.Contains(text, "full-result:") || !strings.Contains(text, "saved to:") {
		t.Fatalf("file-save pointer must survive in the inline body, got (first 400 chars):\n%s", truncForLog(text, 400))
	}
}

// TestCodeSearch_MidSizeResult_BetweenBudgetAndInlineThreshold_FileSaved is
// THE regression test for finding 5: when the full rendering is between
// DefaultBudget (8192) and maxInlineCharsDefault (50000), the ladder
// condenses (rung > 1) but the OLD gate (len(full) > 50000) would NOT save
// the full result to a file — leaving the agent with a condensed inline body
// and the full result reachable NOWHERE. The new gate (rung > 1) saves the
// full rendering whenever the ladder actually condensed, so the agent can
// always reach the complete result via the Read tool.
func TestCodeSearch_MidSizeResult_BetweenBudgetAndInlineThreshold_FileSaved(t *testing.T) {
	// Build a repo whose full rendering lands between DefaultBudget (8192)
	// and maxInlineCharsDefault (50000): 60 matches across 6 files with 2
	// context lines → ~15K of XML.
	repoDir := t.TempDir()
	for f := 0; f < 6; f++ {
		name := string(rune('a'+f)) + ".go"
		var sb strings.Builder
		sb.WriteString("package main\n")
		for i := 0; i < 10; i++ {
			sb.WriteString("// padding line number ")
			sb.WriteString(string(rune('A' + (i % 26))))
			sb.WriteString("\n// MARKER hit here\n// more padding\n")
		}
		if err := os.WriteFile(filepath.Join(repoDir, name), []byte(sb.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	outputDir := t.TempDir()
	deps := analyze.Deps{}
	sem := &SemanticDeps{}

	input := CodeSearchInput{
		Repo:         repoDir,
		Pattern:      "MARKER",
		ContextLines: 2,
		MaxResults:   200,
	}

	res, err := handleCodeSearchInner(context.Background(), input, deps, sem, outputDir)
	if err != nil {
		t.Fatalf("handleCodeSearchInner: %v", err)
	}
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
	text := tc.Text

	// 1. The returned text must reference the saved file — the full result
	//    is reachable on disk even though it's under the old 50000 threshold.
	if !strings.Contains(text, "full-result:") || !strings.Contains(text, "saved to:") {
		t.Fatalf("mid-size result (between budget and 50000) must save full rendering to file and reference it, got (first 400 chars):\n%s", truncForLog(text, 400))
	}

	// 2. Extract the file path and verify the file exists with the full result.
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

	fileContent, err := os.ReadFile(savedPath)
	if err != nil {
		t.Fatalf("saved file must exist and be readable: %s", err)
	}
	var fullResp xmlSearchResponse
	if err := xml.Unmarshal(fileContent, &fullResp); err != nil {
		t.Fatalf("saved file must be parseable XML: %v\n(first 400 chars):\n%s", err, truncForLog(string(fileContent), 400))
	}
	if fullResp.Search.Matches <= 0 {
		t.Fatalf("saved file must contain the full result with matches, got %d", fullResp.Search.Matches)
	}

	// 3. The inline body must still be a valid, parseable XML envelope
	//    (the budget-fitting condensed rung).
	var inlineResp xmlSearchResponse
	if err := xml.Unmarshal([]byte(text), &inlineResp); err != nil {
		t.Fatalf("inline body must be parseable XML: %v\n(first 400 chars):\n%s", err, truncForLog(text, 400))
	}
	if inlineResp.Search.Matches != fullResp.Search.Matches {
		t.Fatalf("inline body must report same match count as full file: inline=%d file=%d",
			inlineResp.Search.Matches, fullResp.Search.Matches)
	}

	// 4. The ceiling must still hold (finding 4 invariant).
	if len(text) > mcpmeta.DefaultBudget {
		t.Fatalf("ceiling violated: len(text)=%d > DefaultBudget=%d", len(text), mcpmeta.DefaultBudget)
	}
}

// TestCodeSearch_NearBudgetCeiling_PointerSurvives is the handler-level
// near-budget ceiling test for finding 4. It dynamically finds the match
// count where the no-context rung (rung 2) lands within the pointer-reserve
// window — i.e. rung 2 fits the FULL budget but NOT the effective budget
// (budget - reserve). At that boundary:
//
//   - WITH the reserve: the ladder steps down to rung 3 (counts), the body
//     is tiny, body+pointer <= budget, the pointer survives, Shape is a
//     no-op.
//   - WITHOUT the reserve (anti-vacuity): rung 2 is chosen, body+pointer
//     overflows the budget, Shape hard-truncates the tail — the exact #685
//     bug reintroduced at the margin.
//
// The boundary N depends on the temp-dir path length (which affects both the
// rendering size and the pointer reserve), so the test scans a range of N
// to find it dynamically rather than hardcoding.
func TestCodeSearch_NearBudgetCeiling_PointerSurvives(t *testing.T) {
	outputDir := t.TempDir()

	// buildRepo writes a single file with nMarks MARKER hits, each with 2
	// context lines, into a fresh temp dir.
	buildRepo := func(nMarks int) string {
		repoDir := t.TempDir()
		var sb strings.Builder
		sb.WriteString("package main\n")
		for i := 0; i < nMarks; i++ {
			sb.WriteString("// ctx before line\n")
			sb.WriteString("MARKER hit here with some text\n")
			sb.WriteString("// ctx after line\n")
		}
		if err := os.WriteFile(filepath.Join(repoDir, "a.go"), []byte(sb.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return repoDir
	}

	runHandler := func(nMarks int) string {
		input := CodeSearchInput{
			Repo:         buildRepo(nMarks),
			Pattern:      "MARKER",
			ContextLines: 2,
			MaxResults:   200,
		}
		deps := analyze.Deps{}
		sem := &SemanticDeps{}
		res, err := handleCodeSearchInner(context.Background(), input, deps, sem, outputDir)
		if err != nil || res == nil || res.IsError {
			t.Fatalf("handler failed at n=%d: err=%v res=%+v", nMarks, err, res)
		}
		tc, ok := res.Content[0].(*mcp.TextContent)
		if !ok {
			t.Fatalf("n=%d: content is not TextContent: %T", nMarks, res.Content[0])
		}
		return tc.Text
	}

	// Scan N to find the boundary: the first N where the body drops from
	// rung 2 (large, >2000 bytes) to rung 3 (small, <2000 bytes). Just
	// below the boundary, rung 2 is chosen and body+pointer is near the
	// budget — the near-budget case. At the boundary, rung 3 is chosen
	// because rung 2 no longer fits the effective budget.
	var nearBudgetText string // largest rung-2 result (closest to ceiling)
	var boundaryText string   // first rung-3 result (reserve forced step-down)
	for n := 78; n <= 95; n++ {
		text := runHandler(n)
		if len(text) > 2000 {
			// Rung 2 chosen — track the largest (closest to ceiling).
			nearBudgetText = text
		} else if boundaryText == "" {
			// First rung-3 result — the boundary.
			boundaryText = text
		}
	}

	if nearBudgetText == "" {
		t.Fatal("could not find a rung-2 (near-budget) result in the scan range")
	}
	if boundaryText == "" {
		t.Fatal("could not find the rung-2→rung-3 boundary in the scan range")
	}

	// Assert the ceiling on the near-budget rung-2 case: body+pointer must
	// stay <= DefaultBudget and Shape must be a no-op.
	if len(nearBudgetText) > mcpmeta.DefaultBudget {
		t.Fatalf("near-budget rung-2 case: ceiling violated len=%d > budget=%d",
			len(nearBudgetText), mcpmeta.DefaultBudget)
	}
	if shaped := mcpmeta.Shape(nearBudgetText, mcpmeta.DefaultBudget, ""); shaped != nearBudgetText {
		t.Fatalf("near-budget rung-2 case: Shape must be a no-op, len=%d shaped=%d",
			len(nearBudgetText), len(shaped))
	}
	if !strings.Contains(nearBudgetText, "full-result:") {
		t.Fatal("near-budget rung-2 case: file-save pointer must survive")
	}

	// Assert the ceiling on the boundary rung-3 case (reserve forced the
	// step-down). The pointer must survive here too.
	if len(boundaryText) > mcpmeta.DefaultBudget {
		t.Fatalf("boundary rung-3 case: ceiling violated len=%d > budget=%d",
			len(boundaryText), mcpmeta.DefaultBudget)
	}
	if shaped := mcpmeta.Shape(boundaryText, mcpmeta.DefaultBudget, ""); shaped != boundaryText {
		t.Fatalf("boundary rung-3 case: Shape must be a no-op, len=%d shaped=%d",
			len(boundaryText), len(shaped))
	}
	if !strings.Contains(boundaryText, "full-result:") {
		t.Fatal("boundary rung-3 case: file-save pointer must survive")
	}
}
