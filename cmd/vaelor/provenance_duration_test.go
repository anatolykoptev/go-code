package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/vaelor/internal/mcpmeta"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// parseMetaFooter decodes the trailing <!-- meta: {...} --> envelope so a test
// can assert on FIELD VALUES rather than on the presence of a substring. A
// substring assertion goes green on a footer whose numbers are wrong, which is
// the failure this file exists to catch.
func parseMetaFooter(t *testing.T, text string) mcpmeta.Envelope {
	t.Helper()
	const open = "<!-- meta: "
	i := strings.LastIndex(text, open)
	if i < 0 {
		t.Fatalf("no meta footer present in:\n%s", text)
	}
	rest := text[i+len(open):]
	j := strings.Index(rest, " -->")
	if j < 0 {
		t.Fatalf("unterminated meta footer: %s", rest)
	}
	var env mcpmeta.Envelope
	if err := json.Unmarshal([]byte(rest[:j]), &env); err != nil {
		t.Fatalf("meta footer is not valid JSON (%v): %s", err, rest[:j])
	}
	return env
}

// The centrally-attached footer must carry the duration the wrapper actually
// measured.
//
// DurationMS deliberately has no omitempty — it is always serialized — so
// building the envelope as a bare mcpmeta.Envelope{} publishes
// `"duration_ms":0` on every centrally-attached footer. That is not a missing
// field an agent can ignore; it is a measured-looking zero inside the one
// record it reads to decide whether the answer can be trusted, sitting beside
// a took_ms line carrying the real number and contradicting it.
//
// The wrapper owns DurationMS: it sets it from its own measurement after
// reading the merged envelope from the slot. A recorded envelope's
// DurationMS is never merged (mergeEnvelope skips it).
//
// Mutation that must turn it RED: in applyBudgetAndTook, remove the
// `env.DurationMS = elapsed.Milliseconds()` line. The footer then carries
// duration_ms=0 — a false measurement.
func TestApplyBudgetAndTook_CentralFooterCarriesMeasuredDuration(t *testing.T) {
	root := mkLaggingRepo(t)

	// Over budget, so the central attachment is the producer of this footer.
	// The envelope carries a checkout_lag signal so HasSignal fires.
	env := mcpmeta.WithCheckoutLag(mcpmeta.Envelope{}, root)
	res := &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: strings.Repeat("line of content\n", 1000)}},
	}
	applyBudgetAndTook(res, 1234*time.Millisecond, env)

	got := textContentOf(t, res)
	parsed := parseMetaFooter(t, got)
	if parsed.CheckoutLag == "" {
		t.Fatalf("fixture is inert: the footer under test carries no provenance, got %+v", parsed)
	}
	if parsed.DurationMS != 1234 {
		t.Fatalf("the central footer must carry the measured duration; got duration_ms=%d, want 1234 — "+
			"a zero here is a false measurement, not an absent field", parsed.DurationMS)
	}
}

// Silence must survive the fix: Wrap populates DurationMS, and DurationMS is
// deliberately excluded from HasSignal, so a response with nothing to report
// must still emit no footer at all. A duration alone is telemetry, not
// provenance, and a footer on every response trains the agent to skip the
// field.
//
// Mutation that must turn it RED: add `|| e.DurationMS != 0` to
// mcpmeta.Envelope.HasSignal.
func TestApplyBudgetAndTook_DurationAloneDoesNotBreakSilence(t *testing.T) {
	// Empty envelope — no signal at all. The wrapper sets DurationMS but
	// HasSignal is still false, so no footer.
	res := &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: "short quiet response"}},
	}
	applyBudgetAndTook(res, 4567*time.Millisecond, mcpmeta.Envelope{})

	got := textContentOf(t, res)
	if strings.Contains(got, "<!-- meta:") {
		t.Fatalf("a duration alone must not raise a footer, got:\n%s", got)
	}
}
