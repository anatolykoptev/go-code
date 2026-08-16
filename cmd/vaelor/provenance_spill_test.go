package main

import (
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/vaelor/internal/mcpmeta"
)

// When a response body is too large to inline, largeTextResult writes it to a
// file and returns a short summary. Provenance must land in that SUMMARY — the
// text the agent actually reads — and not travel into the file with the body.
//
// This property was pinned by TestMetaXMLMarshalResult_EnvelopeAppearsInSummary‐
// WhenSavedToFile, which was deleted together with the helper it exercised when
// envelope assembly moved into the wrapper. The helper is gone; the property is
// not. Eight tools reach largeTextResult, and the spill path is live in prod —
// a real code_search response was observed carrying
// "full-result: 11542 chars saved to: /tmp/go-code-output/...".
//
// Mutation that must turn it RED, and that no other test catches: make
// applyBudgetAndTook skip a body that was spilled, e.g. insert
// `if strings.Contains(text, "saved to:") { return }` immediately after the
// StripBudgetMarker line in cmd/vaelor/addtool.go.
func TestApplyBudgetAndTook_FileSpill_FooterLandsInTheVisibleSummary(t *testing.T) {
	root := mkLaggingRepo(t)

	res := largeTextResult(strings.Repeat("X", maxInlineCharsDefault*2), "test_tool", t.TempDir())

	summary := textContentOf(t, res)
	if !strings.Contains(summary, "saved to:") {
		t.Fatalf("fixture is inert: the file-save path was not taken, so this test "+
			"cannot tell the summary from the body; got %q", truncForLog(summary, 200))
	}

	env := mcpmeta.WithCheckoutLag(mcpmeta.Envelope{}, root)
	if env.CheckoutLag == "" {
		t.Fatal("fixture is inert: no provenance signal to place")
	}

	applyBudgetAndTook(res, 5*time.Millisecond, env)

	got := textContentOf(t, res)
	if !strings.Contains(got, "saved to:") {
		t.Fatalf("the summary must survive the wrapper; got %q", truncForLog(got, 300))
	}
	if !strings.Contains(got, "checkout_lag") {
		t.Fatalf("provenance must land in the summary the agent READS, not in the "+
			"file the body was written to; got %q", truncForLog(got, 400))
	}
}
