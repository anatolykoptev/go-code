package main

import (
	"context"
	"encoding/xml"
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
