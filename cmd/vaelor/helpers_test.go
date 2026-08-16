package main

import (
	"testing"
	"time"

	"github.com/anatolykoptev/vaelor/internal/mcpmeta"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func textContentOf(t *testing.T, r *mcp.CallToolResult) string {
	t.Helper()
	if len(r.Content) == 0 {
		return ""
	}
	tc, ok := r.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("Content[0] is %T, want *mcp.TextContent", r.Content[0])
	}
	return tc.Text
}

// TestAppendMetaFooter_EmptyEnvelopeNoOp verifies the helper returns the
// body unchanged when the envelope carries no signal.
func TestAppendMetaFooter_EmptyEnvelopeNoOp(t *testing.T) {
	got := appendMetaFooter("hello", mcpmeta.Envelope{})
	if got != "hello" {
		t.Fatalf("empty envelope must be no-op, got %q", got)
	}
}

// TestAppendMetaFooter_NonEmptyAppends verifies the helper appends the
// HTML-comment footer with a leading blank-line separator.
func TestAppendMetaFooter_NonEmptyAppends(t *testing.T) {
	env := mcpmeta.Wrap(50*time.Millisecond, "next call X")
	got := appendMetaFooter("body", env)
	want := "body\n\n<!-- meta: " + `{"duration_ms":50,"hint":"next call X"}` + " -->"
	if got != want {
		t.Fatalf("appendMetaFooter:\n got %q\nwant %q", got, want)
	}
}
