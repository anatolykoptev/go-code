package main

import (
	"context"
	"log/slog"
	"reflect"
	"time"

	"github.com/anatolykoptev/vaelor/internal/argnorm"
	"github.com/anatolykoptev/vaelor/internal/mcpmeta"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registeredToolInput records one tool's name and the reflect.Type of its MCP
// input struct. It is populated as a side effect of addTool — the single
// registration seam every tool MUST go through (guarded by
// TestNoDirectMCPServerAddTool). TestAllRegisteredToolsHaveParamDescriptions
// iterates this slice instead of a hardcoded tool list, so the description
// guard cannot rot the moment someone adds a tool: a new registerXxx → addTool
// call appends here automatically.
var registeredToolInputs []registeredToolInput

type registeredToolInput struct {
	Name string
	In   reflect.Type
}

// addTool is the budget-aware wrapper around argnorm.AddTool (which itself
// registers through mcpserver.AddTool and records the tool's accepted
// property set in the argnorm registry — see internal/argnorm/registry.go).
// Every tool registration in this package MUST go through addTool (guarded
// by TestNoDirectMCPServerAddTool in argnorm_registration_test.go): calling
// mcpserver.AddTool directly would bypass the argnorm registry, and the
// normalization middleware fail-closes on registry membership — the tool
// would be silently uncallable ("unknown tool"). addTool wraps the handler
// so every response also gets:
//
//  1. Response budget shaping (default 8 KB) — when the response text
//     exceeds the budget, the RANKED HEAD is kept and a continuation footer
//     is appended so the agent knows the tail was truncated and how to
//     narrow/paginate.
//  2. A compact took_ms footer — one-line observability on every response.
//
// Tools that accept a max_bytes / max_tokens override should call
// mcpmeta.Shape on their output text themselves before returning; the
// wrapper detects already-shaped output (mcpmeta.IsShaped) and skips
// double-shaping. The took_ms footer is always appended (idempotent —
// tools that already emitted one are not double-tagged).
//
// Error results (IsError=true) are returned unchanged — they are already
// short and budget-shaping an error message would bury the diagnostic.
func addTool[In any](
	s *mcp.Server,
	t *mcp.Tool,
	h func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, error),
) {
	registeredToolInputs = append(registeredToolInputs, registeredToolInput{Name: t.Name, In: reflect.TypeFor[In]()})
	argnorm.AddTool(s, t, func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, error) {
		// Seed the provenance slot so resolveRoot can record the (requested,
		// resolved) root pair. After the handler returns, the wrapper reads
		// the snapshot and attaches the provenance footer centrally — after
		// shaping, so it survives budget truncation.
		ctx = seedProvenanceSlot(ctx)
		t0 := time.Now()
		res, err := h(ctx, req, in)
		if err != nil {
			// mcpserver.AddTool (via argnorm.AddTool) converts errors to
			// toolError results; we let that happen by returning the error
			// as-is. No footer on errors.
			return res, err
		}
		if res == nil {
			return res, nil
		}
		// Skip shaping/footer for error results — they are short by construction.
		if res.IsError {
			return res, nil
		}
		elapsed := time.Since(t0)
		requested, resolved, _ := provenanceSnapshot(ctx)
		applyBudgetAndTook(res, elapsed, requested, resolved)
		return res, nil
	})
}

// applyBudgetAndTook mutates res in place: applies the default response
// budget shaping to the first text content block, appends the provenance
// footer, then appends the took_ms footer. Already-shaped output (from a tool
// that applied a custom budget) is not re-shaped; already-took-tagged output
// is not double-tagged; the provenance footer is idempotent — if the body
// already carries one that survived shaping, it is not re-attached.
//
// The provenance footer is attached AFTER shaping (not before) so it sits
// outside the truncated tail — the per-tool annotateEnv path folds the footer
// into the body before the wrapper runs, where Shape cuts it. The central
// attachment here restores it. requested and resolved are the (requested,
// resolved) repo root pair recorded by resolveRoot; empty when no repo was
// resolved (non-repo tools, or the handler did not call resolveRoot).
func applyBudgetAndTook(res *mcp.CallToolResult, elapsed time.Duration, requested, resolved string) {
	if res == nil || res.IsError {
		return
	}
	if len(res.Content) == 0 {
		return
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		return
	}
	text := tc.Text
	// Budget shaping — skip if the tool already shaped its output.
	if !mcpmeta.IsShaped(text) {
		text = mcpmeta.Shape(text, mcpmeta.DefaultBudget, "")
	}
	// Strip the budget-applied marker (if any) so it's not visible to the
	// agent — it was only there to prevent re-shaping (#582).
	text = mcpmeta.StripBudgetMarker(text)
	// Provenance footer — attached centrally, AFTER shaping, so it survives
	// budget truncation. Idempotent: if the tool already attached a meta
	// footer (via annotateEnv) and it survived shaping, skip. On a large
	// response the per-tool footer is truncated away by Shape, so this step
	// sees none and restores it — which is the fix.
	//
	// resolved == "" means no repo was resolved (a non-repo tool, or a handler
	// that never reached resolveRoot) and MUST short-circuit: gitDir joins its
	// argument with ".git", so WithCheckoutLag("") stats ".git" relative to the
	// PROCESS working directory. A binary started from inside a checkout would
	// then report that checkout's lag on responses that touched no repo at all.
	// The suite cannot see this by accident — `go test ./cmd/vaelor/` runs with
	// CWD at cmd/vaelor/, where no .git exists — so it is pinned explicitly by
	// TestApplyBudgetAndTook_NoRepoResolved_DoesNotReportProcessCWD.
	if resolved != "" && !mcpmeta.HasMetaFooter(text) {
		// Wrap, not a bare Envelope: DurationMS has no omitempty (it is
		// deliberately always serialized), so a bare envelope publishes
		// "duration_ms":0 on every centrally-attached footer — a measured-
		// looking zero in a record read for provenance. The wrapper already
		// timed the handler; use that number.
		env := mcpmeta.Wrap(elapsed, "")
		env = mcpmeta.WithSourcePath(env, requested, resolved)
		env = mcpmeta.WithCheckoutLag(env, resolved)
		text = appendMetaFooter(text, env)
	}
	// took_ms footer — idempotent.
	text = mcpmeta.AppendTook(text, elapsed)
	tc.Text = text
	res.Content[0] = tc
}

// softDeadlineResult wraps a partial result text with the partial footer
// and the took_ms tag. Used by tools that hit the soft deadline and need
// to return what they have so far.
func softDeadlineResult(text string, skipped string, elapsed time.Duration) *mcp.CallToolResult {
	return shapedPartialResult(text, mcpmeta.DefaultBudget, "", skipped, elapsed)
}

// shapedPartialResult shapes a partial result BODY first, then appends the
// partial and took_ms footers. Shaping must precede the footers: appending
// them to an un-shaped over-budget body would leave them beyond the budget
// boundary, where the outer wrapper's re-shape (or the client's hard cut)
// silently destroys the `partial: true` signal — exactly the failure #572
// exists to prevent.
func shapedPartialResult(text string, budget int, hint, skipped string, elapsed time.Duration) *mcp.CallToolResult {
	out := text
	if !mcpmeta.IsShaped(out) {
		out = mcpmeta.Shape(out, budget, hint)
	}
	out += mcpmeta.PartialFooter(skipped)
	out = mcpmeta.AppendTook(out, elapsed)
	return textResult(out)
}

// budgetOverride resolves a per-call max_bytes override against the default
// budget. Returns the effective budget in bytes. override <= 0 → default.
func budgetOverride(override int) int {
	return mcpmeta.ResolveBudget(override, mcpmeta.DefaultBudget)
}

// logSoftDeadlineHit records that a tool hit its soft deadline, for ops
// visibility. Non-fatal — just a structured log line.
func logSoftDeadlineHit(tool string, elapsed time.Duration) {
	slog.Warn("soft deadline hit — returning partial result",
		slog.String("tool", tool),
		slog.Duration("elapsed", elapsed),
	)
}
