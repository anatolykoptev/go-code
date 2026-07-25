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
	applyBudgetAndTook(res, 5*time.Millisecond)

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
