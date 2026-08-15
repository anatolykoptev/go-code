package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/mcpmeta"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mkLaggingSourceRepo builds a directory that is both a searchable source tree
// and a checkout whose main branch is behind origin.
func mkLaggingSourceRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeLaggingGitDir(t, dir)
	body := "package main\n\n// MARKER lives here\nfunc marked() {}\n"
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func firstText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if res == nil || len(res.Content) == 0 {
		t.Fatal("result has no content")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("first content is not TextContent: %T", res.Content[0])
	}
	return tc.Text
}

// The unit tests drive annotateEnv directly, so deleting the call from a tool
// leaves them all green while every real response loses the signal — a review
// probe amputated all three code_search/symbol_search call sites and the suite
// never noticed. This drives the REAL handler and asserts the signal reaches
// the text an agent reads.
func TestCodeSearch_LaggingCheckout_SurfacesCheckoutLag(t *testing.T) {
	repoDir := mkLaggingSourceRepo(t)

	res, err := handleCodeSearchInner(context.Background(), CodeSearchInput{
		Repo:       repoDir,
		Pattern:    "MARKER",
		MaxResults: 10,
	}, analyze.Deps{}, &SemanticDeps{}, "")
	if err != nil {
		t.Fatalf("handleCodeSearchInner: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("unexpected error result: %+v", res)
	}

	text := firstText(t, res)
	if !strings.Contains(text, "checkout_lag") {
		t.Fatalf("a checkout behind origin must surface checkout_lag in the response the agent reads, got:\n%s",
			truncForLog(text, 600))
	}
}

// The same signal must survive the file-save path. metaXMLMarshalResult used to
// fold the footer into the body BEFORE largeTextResult decided to spill that
// body to a file, so the summary the agent actually read carried no footer --
// the staleness warning went into the file nobody re-reads for it.
func TestMetaXMLMarshalResult_EnvelopeAppearsInSummaryWhenSavedToFile(t *testing.T) {
	// A payload comfortably over the inline ceiling so largeTextResult spills.
	big := struct {
		XMLName struct{} `xml:"big"`
		Blob    string   `xml:"blob"`
	}{Blob: strings.Repeat("X", maxInlineCharsDefault*2)}

	env := mcpmeta.WithSourcePath(mcpmeta.Wrap(50*time.Millisecond, ""), "/Users/dev/Developer/acme", "/host/src/acme")
	if !env.HasSignal() {
		t.Fatal("fixture must carry a signal, else this asserts nothing")
	}

	text := firstText(t, metaXMLMarshalResult(big, "test_tool", t.TempDir(), env))

	if !strings.Contains(text, "saved to:") {
		t.Fatalf("fixture must exercise the file-save path, got: %q", truncForLog(text, 200))
	}
	if !strings.Contains(text, "<!-- meta:") {
		t.Fatalf("footer must appear in the visible summary when the body is saved to file, got: %q",
			truncForLog(text, 400))
	}
	if !strings.Contains(text, "source_path") {
		t.Fatalf("the signal itself must survive, got: %q", truncForLog(text, 400))
	}
}
