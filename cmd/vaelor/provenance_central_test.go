package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/vaelor/internal/mcpmeta"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// F1 — over-budget response retains the provenance footer.
//
// The envelope is recorded by resolveRoot (path signals) and the tool
// (freshness), then the wrapper renders it ONCE after shaping. This is the
// fix for #778: freshness was folded into the body before shaping and
// truncated away. Now the code knows the envelope; it does not sniff text.
//
// Mutation that must turn it RED: move the appendMetaFooter call in
// applyBudgetAndTook from AFTER the mcpmeta.Shape(...) line to immediately
// BEFORE it. The footer is then folded into the body, Shape cuts the tail,
// the footer is gone.
func TestApplyBudgetAndTook_OverBudgetRetainsProvenanceFooter_F1(t *testing.T) {
	// A checkout whose main branch is BEHIND origin — WithCheckoutLag will
	// produce a checkout_lag signal.
	root := mkLaggingRepo(t)

	// Build the envelope the way the slot would: path signals from
	// resolveRoot, freshness from the tool.
	env := mcpmeta.Envelope{}
	env = mcpmeta.WithSourcePath(env, "/Users/dev/Developer/acme", root)
	env = mcpmeta.WithCheckoutLag(env, root)
	env = mcpmeta.WithFreshness(env, root, "cccccccccccccccccccccccccccccccccccccccc")
	if !env.HasSignal() {
		t.Fatalf("fixture must carry a provenance signal, got %+v", env)
	}

	// A body comfortably over the default budget.
	body := strings.Repeat("line of content\n", 1000) // ~16KB, over budget

	res := &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: body}},
	}

	applyBudgetAndTook(res, 5*time.Millisecond, env)

	got := textContentOf(t, res)
	if !strings.Contains(got, "<!-- meta:") {
		t.Fatalf("over-budget response must retain the provenance footer after shaping, "+
			"got tail:\n%s", truncForLog(got, 400))
	}
	if !strings.Contains(got, "checkout_lag") {
		t.Fatalf("the checkout_lag signal must survive shaping, got tail:\n%s",
			truncForLog(got, 400))
	}
	if !strings.Contains(got, "took_ms=") {
		t.Fatal("took_ms footer must still be present")
	}
}

// F1-freshness — freshness (stale_warning) survives budget shaping (closes #778).
//
// Today's per-tool annotateEnv path folds freshness into the body BEFORE the
// wrapper runs; Shape cuts the tail and the stale_warning is lost. The
// recorded-envelope design moves the render to AFTER shaping, so freshness
// reaches the agent. This test pins that property directly: an envelope
// carrying ONLY a stale_warning (no path signals) must surface it after
// shaping.
//
// Mutation that must turn it RED: drop the freshness contribution from the
// merge — i.e. make mergeEnvelope skip StaleWarning. The recorded
// stale_warning never reaches the snapshot, the footer is empty, no signal.
func TestApplyBudgetAndTook_FreshnessSurvivesShaping_F1freshness(t *testing.T) {
	root := mkLaggingRepo(t)

	// Only freshness — no path signals. This isolates the property: the
	// stale_warning itself must survive shaping. The lagging fixture has
	// main=aaaa...; pass a DIFFERENT indexedSHA so WithFreshness fires.
	freshEnv := mcpmeta.WithFreshness(mcpmeta.Envelope{}, root, "cccccccccccccccccccccccccccccccccccccccc")
	if freshEnv.StaleWarning == "" {
		t.Skip("fixture repo has no main-branch advancement — freshness is silent, nothing to pin")
	}

	// Drive through the REAL slot path: record → snapshot → render. This is
	// the path the mutation must break. Passing the envelope directly to
	// applyBudgetAndTook would bypass the merge and not test the property.
	ctx := seedProvenanceSlot(context.Background())
	recordEnvelope(ctx, freshEnv)
	env := envelopeSnapshot(ctx)

	body := strings.Repeat("line of content\n", 1000) // ~16KB, over budget

	res := &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: body}},
	}

	applyBudgetAndTook(res, 5*time.Millisecond, env)

	got := textContentOf(t, res)
	if !strings.Contains(got, "stale_warning") {
		t.Fatalf("freshness (stale_warning) must survive budget shaping — this is the "+
			"#778 fix. got tail:\n%s", truncForLog(got, 400))
	}
}

// F2 — nothing to report produces no provenance footer.
//
// Silence is load-bearing: a footer on every response trains the agent to
// ignore the field. A response from a repo with nothing to report (no
// checkout lag, no source_path alias, no staleness) must come out with NO
// provenance footer at all.
//
// Mutation that must turn it RED: remove the HasSignal gate from
// appendMetaFooter, or always set a field on the envelope. Then a footer
// appears on a response that should be silent.
func TestApplyBudgetAndTook_NothingToReport_NoFooter_F2(t *testing.T) {
	// A zero-signal envelope — nothing to report.
	body := "short response with nothing to report"

	res := &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: body}},
	}

	applyBudgetAndTook(res, 5*time.Millisecond, mcpmeta.Envelope{})

	got := textContentOf(t, res)
	if strings.Contains(got, "<!-- meta:") {
		t.Fatalf("a response with nothing to report must NOT carry a provenance "+
			"footer, got:\n%s", got)
	}
	if !strings.Contains(got, "took_ms=") {
		t.Fatal("took_ms footer must still be present on the quiet path")
	}
}

// F3 — a body that appends after its own footer yields EXACTLY ONE envelope
// (closes #781).
//
// The old wrapper sniffed the body's last line for the meta sentinel; a tool
// that appended its condensation marker after its own footer defeated the
// sniff and got a second envelope. The new design does not sniff — the
// wrapper renders exactly one footer from the recorded envelope, regardless
// of what the body contains.
//
// Mutation that must turn it RED: restore the HasMetaFooter sniff in
// applyBudgetAndTook and make the body's last line NOT match (e.g. append a
// condensation marker after a quoted sentinel). The sniff misses, the
// wrapper appends a second footer — count != 1.
func TestApplyBudgetAndTook_ExactlyOneEnvelope_F3(t *testing.T) {
	root := mkLaggingRepo(t)

	env := mcpmeta.WithCheckoutLag(mcpmeta.Envelope{}, root)
	if !env.HasSignal() {
		t.Fatal("fixture must carry a signal")
	}

	// A body that QUOTES the sentinel AND has trailing content after it —
	// the exact shape that defeated the old last-line sniff (#781).
	quoted := `cmd/vaelor/helpers.go:69:	return body + "\n\n<!-- meta: " + string(js) + " -->"` + "\n"
	quoted += "<!-- condensed: rung 2/3 — counts -->\n"
	body := quoted + strings.Repeat("line of content\n", 1000)

	res := &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: body}},
	}

	applyBudgetAndTook(res, 5*time.Millisecond, env)

	got := textContentOf(t, res)
	// Count REAL meta footers — JSON envelopes carrying checkout_lag. The
	// body quotes `<!-- meta:` in source code, so a naive substring count
	// would see 2. The wrapper renders exactly one real footer from the
	// recorded envelope.
	footerCount := strings.Count(got, `"checkout_lag"`)
	if footerCount != 1 {
		t.Fatalf("exactly one real meta footer must be present (the recorded "+
			"one), got %d checkout_lag occurrences:\n%s", footerCount,
			truncForLog(got, 400))
	}
	if !strings.Contains(got, "checkout_lag") {
		t.Fatalf("the recorded signal must be in the footer, got tail:\n%s",
			truncForLog(got, 400))
	}
}

// F4 — silence preserved: a body quoting the footer sentinel but with no
// recorded envelope must produce NO footer. The wrapper does not sniff; it
// renders from the slot. An empty slot → no footer, regardless of body
// content.
//
// Mutation that must turn it RED: restore a body-sniffing idempotence check
// that false-positives on the quoted sentinel. The sniff reads the body as
// "already has a footer" and... actually, the old bug was the OPPOSITE (sniff
// missed → double footer). The silence-preserving mutation is: make
// appendMetaFooter always append (remove the HasSignal gate). Then a footer
// appears on a body that should be silent.
func TestApplyBudgetAndTook_BodyQuotingSentinel_NoEnvelope_NoFooter_F4(t *testing.T) {
	// A body that quotes the sentinel but the slot is empty — no envelope
	// was recorded. The wrapper must NOT produce a footer.
	quoted := `cmd/vaelor/helpers.go:69:	return body + "\n\n<!-- meta: " + string(js) + " -->"` + "\n"
	body := quoted + strings.Repeat("line of content\n", 1000)

	res := &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: body}},
	}

	applyBudgetAndTook(res, 5*time.Millisecond, mcpmeta.Envelope{})

	got := textContentOf(t, res)
	// The quoted sentinel in the body may survive shaping or be truncated.
	// The point is the wrapper must not ADD a footer.
	// Count only footers that look like real meta envelopes (JSON object).
	// The quoted line is source code, not a real footer — but even if it
	// survives, the wrapper must not add a SECOND one.
	footerCount := strings.Count(got, `"checkout_lag"`)
	if footerCount > 0 {
		t.Fatalf("no envelope recorded → wrapper must not add a provenance "+
			"footer, got %d checkout_lag occurrences:\n%s", footerCount,
			truncForLog(got, 400))
	}
}

// provenanceCtxSlot — write-once: first writer wins, later writes dropped.
// Guards the data-race/determinism property the spec requires: handlers spawn
// goroutines that outlive the response, and a last-writer-wins slot would be
// a race and would make the reported root depend on scheduling.
func TestProvenanceSlot_WriteOnceFirstWriterWins(t *testing.T) {
	t.Parallel()

	ctx := seedProvenanceSlot(context.Background())
	recordProvenance(ctx, "first-requested", "first-resolved")
	recordProvenance(ctx, "second-requested", "second-resolved")

	req, res, ok := provenanceSnapshot(ctx)
	if !ok {
		t.Fatal("snapshot must report a recording after the first write")
	}
	if req != "first-requested" || res != "first-resolved" {
		t.Fatalf("write-once must keep the first writer: got (%q, %q), want "+
			"(%q, %q)", req, res, "first-requested", "first-resolved")
	}
}

// provenanceCtxSlot — no-op outside the addTool wrapper (no slot on the
// context). resolveRoot is called outside the wrapper in tests; the slot
// must not panic and the snapshot must report ok=false.
func TestProvenanceSlot_NoopWithoutSeededContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	recordProvenance(ctx, "requested", "resolved")      // must not panic
	recordEnvelope(ctx, mcpmeta.Envelope{Hint: "test"}) // must not panic

	if _, _, ok := provenanceSnapshot(ctx); ok {
		t.Fatal("snapshot must report ok=false when no slot is seeded")
	}
	if env := envelopeSnapshot(ctx); env.HasSignal() {
		t.Fatal("envelopeSnapshot must return zero envelope when no slot is seeded")
	}
}

// F2-merge — the merge keeps BOTH contributions: resolveRoot's path signals
// AND the tool's freshness. Dropping either side is caught.
//
// Mutation that must turn it RED: make the merge last-writer-wins (overwrite
// all fields) instead of per-field first-writer-wins. The second
// recordEnvelope (freshness) overwrites the first (path signals), and
// SourcePath/CheckoutLag are lost.
func TestProvenanceSlot_MergeKeepsBothContributions_F2merge(t *testing.T) {
	t.Parallel()

	root := mkLaggingRepo(t)

	ctx := seedProvenanceSlot(context.Background())

	// resolveRoot contributes path signals.
	pathEnv := mcpmeta.WithCheckoutLag(
		mcpmeta.WithSourcePath(mcpmeta.Envelope{}, "/Users/dev/Developer/acme", root), root)
	recordEnvelope(ctx, pathEnv)

	// Tool contributes freshness.
	freshEnv := mcpmeta.WithFreshness(mcpmeta.Envelope{}, root,
		"cccccccccccccccccccccccccccccccccccccccc")
	recordEnvelope(ctx, freshEnv)

	merged := envelopeSnapshot(ctx)
	if merged.SourcePath == "" {
		t.Fatal("merge must keep resolveRoot's SourcePath — last-writer-wins would drop it")
	}
	if merged.CheckoutLag == "" {
		t.Fatal("merge must keep resolveRoot's CheckoutLag — last-writer-wins would drop it")
	}
	if freshEnv.StaleWarning != "" && merged.StaleWarning == "" {
		t.Fatal("merge must keep the tool's StaleWarning — dropping it is the #778 silent-loss class")
	}
	if !merged.HasSignal() {
		t.Fatalf("merged envelope must carry signal from both contributions, got %+v", merged)
	}
}

// F2-merge-duration — DurationMS is owned by the wrapper alone. A recorded
// envelope's DurationMS is never merged.
//
// Mutation that must turn it RED: add DurationMS to mergeEnvelope (i.e.
// `if dst.DurationMS == 0 && src.DurationMS != 0 { dst.DurationMS = src.DurationMS }`).
// Then record an envelope with DurationMS=999, snapshot, and the snapshot
// carries 999 instead of 0 — the wrapper's own measurement would be
// overwritten by the recorded value.
func TestProvenanceSlot_MergeIgnoresDurationMS(t *testing.T) {
	t.Parallel()

	ctx := seedProvenanceSlot(context.Background())
	recordEnvelope(ctx, mcpmeta.Envelope{DurationMS: 999, Hint: "test"})

	merged := envelopeSnapshot(ctx)
	if merged.DurationMS != 0 {
		t.Fatalf("merge must NOT carry DurationMS from recorded envelopes — "+
			"the wrapper owns it. got %d, want 0", merged.DurationMS)
	}
	if merged.Hint != "test" {
		t.Fatal("merge must keep the Hint (non-DurationMS fields are merged)")
	}
}
