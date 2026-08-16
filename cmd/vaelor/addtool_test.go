package main

import (
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/vaelor/internal/mcpmeta"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestAddTookFooter_PresentOnEveryResponse verifies that the addTool wrapper
// appends a took_ms footer to every non-error tool response. This is the
// observability contract from #572.
func TestAddTookFooter_PresentOnEveryResponse(t *testing.T) {
	// Simulate what the addTool wrapper does after a handler returns.
	res := textResult("hello world")
	applyBudgetAndTook(res, 42*time.Millisecond, mcpmeta.Envelope{})

	got := textContentOf(t, res)
	if !strings.Contains(got, "took_ms=") {
		t.Fatalf("response must contain took_ms footer, got:\n%s", got)
	}
	if !strings.Contains(got, "took_ms=42") {
		t.Fatalf("footer must show 42ms, got:\n%s", got)
	}
}

// TestAddTookFooter_NotOnErrors verifies that error results are returned
// unchanged (no budget shaping, no took_ms).
func TestAddTookFooter_NotOnErrors(t *testing.T) {
	res := errResult("something went wrong")
	applyBudgetAndTook(res, 10*time.Millisecond, mcpmeta.Envelope{})

	got := textContentOf(t, res)
	if strings.Contains(got, "took_ms=") {
		t.Fatalf("error result must not get took_ms footer, got:\n%s", got)
	}
}

// TestAddBudgetShaping_OverBudget verifies that the wrapper applies the
// default budget shaping to over-budget responses.
func TestAddBudgetShaping_OverBudget(t *testing.T) {
	long := strings.Repeat("line of content\n", 1000)
	res := textResult(long)
	applyBudgetAndTook(res, 5*time.Millisecond, mcpmeta.Envelope{})

	got := textContentOf(t, res)
	if len(got) >= len(long) {
		t.Fatalf("over-budget response must be shaped: got %d, orig %d", len(got), len(long))
	}
	if !strings.Contains(got, "[truncated:") {
		t.Fatalf("shaped response must have truncation footer")
	}
	if !strings.Contains(got, "took_ms=") {
		t.Fatalf("shaped response must still have took_ms footer")
	}
}

// TestAddBudgetShaping_SkipsAlreadyShaped verifies that a tool that already
// shaped its output (with a custom budget) is not double-shaped.
func TestAddBudgetShaping_SkipsAlreadyShaped(t *testing.T) {
	// Simulate a tool that shaped its own output.
	alreadyShaped := "head content\n[truncated: 500 more chars — pass offset=10]"
	res := textResult(alreadyShaped)
	applyBudgetAndTook(res, 1*time.Millisecond, mcpmeta.Envelope{})

	got := textContentOf(t, res)
	// Should not have a second truncation footer.
	count := strings.Count(got, "[truncated:")
	if count != 1 {
		t.Fatalf("already-shaped output must not be double-shaped: found %d truncation footers", count)
	}
}

// TestSoftDeadlineResult_Format verifies the partial-result helper produces
// the correct footer structure.
func TestSoftDeadlineResult_Format(t *testing.T) {
	res := softDeadlineResult("partial data here", "LLM analysis skipped", 30*time.Second)
	got := textContentOf(t, res)

	if !strings.Contains(got, "partial data here") {
		t.Fatalf("partial result must contain the body, got:\n%s", got)
	}
	if !strings.Contains(got, "partial: true") {
		t.Fatalf("partial result must contain 'partial: true' footer, got:\n%s", got)
	}
	if !strings.Contains(got, "LLM analysis skipped") {
		t.Fatalf("partial result must contain what was skipped, got:\n%s", got)
	}
	if !strings.Contains(got, "took_ms=") {
		t.Fatalf("partial result must contain took_ms, got:\n%s", got)
	}
}

// TestBudgetOverride verifies the budget override resolution.
func TestBudgetOverride(t *testing.T) {
	if budgetOverride(0) != 8192 {
		t.Fatal("zero override must yield default")
	}
	if budgetOverride(4096) != 4096 {
		t.Fatal("valid override must be used")
	}
	if budgetOverride(100) < 512 {
		t.Fatal("tiny override must be clamped to MinBudget")
	}
}

// TestShapedPartialResult_LargeBody guards the #572 contract on the large-
// partial path: an over-budget partial body must be SHAPED FIRST so the
// `partial: true` and took_ms footers survive within budget — appending them
// to an un-shaped body would leave them beyond the boundary where the outer
// wrapper's re-shape (or the client's hard cut) silently destroys them.
func TestShapedPartialResult_LargeBody(t *testing.T) {
	body := strings.Repeat("<line>data</line>\n", 800) // ~14KB, over budget
	res := shapedPartialResult(body, mcpmeta.DefaultBudget,
		"narrow with focus=", "LLM stage skipped", 3*time.Second)
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}
	got := tc.Text
	if !strings.Contains(got, "partial: true") {
		t.Fatalf("partial footer must survive shaping, got tail:\n%s", got[len(got)-300:])
	}
	if !strings.Contains(got, "took_ms=") {
		t.Fatal("took_ms footer must survive shaping")
	}
	if !strings.Contains(got, "[truncated:") {
		t.Fatal("over-budget body must carry the truncation footer")
	}
	if len(got) > mcpmeta.DefaultBudget+600 {
		t.Fatalf("shaped partial must stay near budget, got %d bytes", len(got))
	}
	if !mcpmeta.IsShaped(got) {
		t.Fatal("result must be marked shaped so the outer wrapper skips re-shaping")
	}
}

// TestDataflowFetchWindow verifies pagination pages beyond the default fetch
// are actually fetchable (review finding: fixed 50-fetch made offset>50 an
// always-empty page while the count attribute advertised more).
func TestDataflowFetchWindow(t *testing.T) {
	if got := dataflowFetchWindow(0, 0); got != dataflowMaxResults {
		t.Fatalf("default window = %d, want %d", got, dataflowMaxResults)
	}
	if got := dataflowFetchWindow(100, 50); got != 150 {
		t.Fatalf("offset page window = %d, want 150", got)
	}
	if got := dataflowFetchWindow(490, 50); got != dataflowFetchCap {
		t.Fatalf("capped window = %d, want %d", got, dataflowFetchCap)
	}
	if got := dataflowFetchWindow(-5, -5); got != dataflowMaxResults {
		t.Fatalf("negative inputs window = %d, want %d", got, dataflowMaxResults)
	}
}
