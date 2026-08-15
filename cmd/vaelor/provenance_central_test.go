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
// This is the bug the task fixes: a tool folds the provenance footer into its
// body BEFORE the wrapper runs; applyBudgetAndTook runs mcpmeta.Shape (which
// truncates the TAIL) and only then appends took_ms. The footer sat in the
// truncated tail, so checkout_lag was silently lost while took_ms survived.
//
// The test builds a body that exceeds mcpmeta.DefaultBudget, carries a
// provenance-bearing envelope folded into the body (simulating the per-tool
// annotateEnv path), and drives it through the REAL wrapper
// (applyBudgetAndTook) with a (requested, resolved) pair that produces a
// checkout_lag signal. It asserts the footer is present in the final text.
//
// Mutation that must turn it RED: move the provenance-append call in
// applyBudgetAndTook from AFTER the mcpmeta.Shape(...) line to immediately
// BEFORE it. That reproduces today's bug exactly — the footer is folded into
// the body, Shape cuts the tail, the footer is gone.
func TestApplyBudgetAndTook_OverBudgetRetainsProvenanceFooter_F1(t *testing.T) {
	// A checkout whose main branch is BEHIND origin — WithCheckoutLag will
	// produce a checkout_lag signal.
	root := mkLaggingRepo(t)

	// A body comfortably over the default budget, with a provenance footer
	// folded into the tail — exactly what the per-tool annotateEnv path
	// produces before the wrapper runs.
	env := mcpmeta.Envelope{}
	env = mcpmeta.WithSourcePath(env, "/Users/dev/Developer/acme", root)
	env = mcpmeta.WithCheckoutLag(env, root)
	if !env.HasSignal() {
		t.Fatalf("fixture must carry a provenance signal, got %+v", env)
	}
	body := strings.Repeat("line of content\n", 1000) // ~16KB, over budget
	bodyWithFooter := appendMetaFooter(body, env)
	if !strings.Contains(bodyWithFooter, "<!-- meta:") {
		t.Fatal("fixture body must carry the meta footer before shaping")
	}

	res := &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: bodyWithFooter}},
	}

	// Drive the REAL wrapper with the (requested, resolved) pair the slot
	// would record. requested is an absolute path that differs from root so
	// WithSourcePath fires; root lags origin so WithCheckoutLag fires.
	applyBudgetAndTook(res, 5*time.Millisecond, "/Users/dev/Developer/acme", root)

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

// F2 — nothing to report produces no provenance footer.
//
// Silence is load-bearing: a footer on every response trains the agent to
// ignore the field. A response from a repo with nothing to report (no
// checkout lag, requested == resolved so no source_path alias) must come out
// with NO provenance footer at all.
//
// Mutation that must turn it RED: force the central attachment to fire
// unconditionally — e.g. remove the HasMetaFooter/HasSignal gate, or always
// set env.SourcePath = resolved. Then a footer appears on a response that
// should be silent.
func TestApplyBudgetAndTook_NothingToReport_NoFooter_F2(t *testing.T) {
	// A plain tempdir with no .git — WithCheckoutLag is silent (no main-branch
	// refs). requested == resolved so WithSourcePath is silent. The envelope
	// has no signal, so appendMetaFooter returns the body unchanged.
	root := t.TempDir()

	// A body under budget so shaping does not truncate — the only thing that
	// could add a footer is the central attachment, and it must not.
	body := "short response with nothing to report"

	res := &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: body}},
	}

	applyBudgetAndTook(res, 5*time.Millisecond, root, root)

	got := textContentOf(t, res)
	if strings.Contains(got, "<!-- meta:") {
		t.Fatalf("a response with nothing to report must NOT carry a provenance "+
			"footer, got:\n%s", got)
	}
	if strings.Contains(got, "source_path") {
		t.Fatalf("no source_path alias (requested==resolved) must produce no "+
			"source_path field, got:\n%s", got)
	}
	if strings.Contains(got, "checkout_lag") {
		t.Fatalf("a repo with no lag must produce no checkout_lag field, got:\n%s", got)
	}
	// took_ms is still expected — it is observability, not provenance.
	if !strings.Contains(got, "took_ms=") {
		t.Fatal("took_ms footer must still be present on the quiet path")
	}
}

// F1-supplement — the central attachment is idempotent: when the per-tool
// footer SURVIVES shaping (under-budget body), the central step must not add
// a second one. This guards the happy consequence noted in the spec: the
// eight existing per-tool annotateEnv calls stay in place, and the central
// step must not double-tag when they survive.
func TestApplyBudgetAndTook_IdempotentWhenFooterSurvives_F1supplement(t *testing.T) {
	root := mkLaggingRepo(t)

	env := mcpmeta.Envelope{}
	env = mcpmeta.WithSourcePath(env, "/Users/dev/Developer/acme", root)
	env = mcpmeta.WithCheckoutLag(env, root)

	// Under-budget body — the per-tool footer survives shaping.
	body := "short response\n"
	bodyWithFooter := appendMetaFooter(body, env)

	res := &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: bodyWithFooter}},
	}

	applyBudgetAndTook(res, 5*time.Millisecond, "/Users/dev/Developer/acme", root)

	got := textContentOf(t, res)
	count := strings.Count(got, "<!-- meta:")
	if count != 1 {
		t.Fatalf("exactly one meta footer must be present when the per-tool one "+
			"survives shaping, got %d:\n%s", count, got)
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
	recordProvenance(ctx, "requested", "resolved") // must not panic

	if _, _, ok := provenanceSnapshot(ctx); ok {
		t.Fatal("snapshot must report ok=false when no slot is seeded")
	}
}
