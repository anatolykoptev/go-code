package mcpmeta

import (
	"fmt"
	"strings"
	"time"
)

// DefaultBudget is the default per-tool response budget in bytes (~8 KB).
// MCP clients (Devin CLI) truncate tool results at ~10149 chars; staying
// below this ceiling ensures the tail (summaries, verdicts, continuation
// handles) is never silently lost. Tools that return ranked lists should
// truncate at this budget and emit a continuation footer via Shape.
const DefaultBudget = 8192

// MaxBudget is the ceiling for a per-call budget override. Clients hard-cut
// tool results at ~10149 bytes (observed: Devin CLI); an uncapped override at
// or above that ceiling would make Shape a no-op and reintroduce silent
// client-side truncation — tail AND continuation footer lost. 9000 leaves
// headroom for the took_ms footer and the argnorm note under the ceiling.
const MaxBudget = 9000

// MinBudget is the floor for a per-call budget override — anything smaller
// would leave no room for even a single result item plus the footer.
const MinBudget = 512

// truncationFooterPrefix is the sentinel prefix of the continuation footer
// emitted by Shape. Tools can check for it to detect already-shaped output.
const truncationFooterPrefix = "\n[truncated:"

// budgetAppliedMarker is a sentinel appended by ShapeWithHint when the text
// fits within the budget — it signals to the addTool wrapper that a per-call
// budget was already applied and the wrapper must NOT re-shape with the
// default budget (#582). Without this, a tool that passes max_bytes > default
// would have its text re-shaped by the wrapper with the smaller default budget,
// losing the tool-specific pagination hint.
const budgetAppliedMarker = "\n[budget-applied]"

// tookFooterPrefix is the sentinel prefix of the took_ms footer.
const tookFooterPrefix = "\ntook_ms="

// metaFooterPrefix is the sentinel prefix of the provenance envelope footer
// (<!-- meta: {...} -->). Used by HasMetaFooter to detect an already-attached
// footer so the central attachment in applyBudgetAndTook is idempotent — a
// tool that already folded the footer into its body (and the footer survived
// shaping) must not get a second one.
const metaFooterPrefix = "<!-- meta:"

// HasMetaFooter reports whether text ENDS with a provenance envelope footer
// (<!-- meta: {...} -->). Used by the central attachment in applyBudgetAndTook
// to skip double-appending when a tool already attached one that survived
// budget shaping.
//
// Anchored to the last line rather than a substring search, unlike the sibling
// HasTookFooter: a response BODY can legitimately contain the sentinel — a
// code search over vaelor's own source returns the very line that builds this
// footer — and a Contains check reads that as "already attached", silently
// dropping the real footer from exactly the query most likely to be run
// against this repo.
//
// The last line is a sound anchor because the footer is the final thing
// appended before took_ms, and truncation only ever cuts before the end:
// Shape leaves the footer whole or removes it outright, never a fragment at
// the tail. A text with no newline is treated as a single line.
func HasMetaFooter(text string) bool {
	last := text[strings.LastIndex(text, "\n")+1:]
	return strings.HasPrefix(last, metaFooterPrefix) && strings.HasSuffix(last, "-->")
}

// Shape applies a response budget to text. When the text fits within budget
// (or budget <= 0), it is returned unchanged. When it exceeds the budget,
// Shape truncates at the last newline that fits within the budget and
// appends a continuation footer:
//
//	[truncated: N more chars — <hint>]
//
// continuationHint is the tool-specific guidance shown to the agent (e.g.
// "narrow with tier=exact or pass offset=20"). When empty, a generic hint
// is used.
//
// If the text has no newline within the budget, it is hard-truncated at the
// budget boundary. The footer itself is NOT counted against the budget so
// the agent always sees the continuation handle.
func Shape(text string, budget int, continuationHint string) string {
	if budget <= 0 || len(text) <= budget {
		return text
	}
	if budget < MinBudget {
		budget = MinBudget
	}

	// Try to truncate at the last newline within the budget.
	cut := budget
	lastNL := strings.LastIndex(text[:budget], "\n")
	if lastNL > budget/2 { //nolint:mnd // only back up if we keep at least half
		cut = lastNL
	}

	head := text[:cut]
	remaining := len(text) - cut

	hint := continuationHint
	if hint == "" {
		hint = "narrow your query or pass a tighter limit"
	}
	footer := fmt.Sprintf("%s %d more chars — %s]", truncationFooterPrefix, remaining, hint)
	return head + footer
}

// ShapeWithHint is like Shape but guarantees the tool-specific continuationHint
// is preserved even when the text fits within the budget. When the text fits,
// ShapeWithHint appends a budget-applied marker so the addTool wrapper detects
// already-shaped output (IsShaped=true) and skips re-shaping with the default
// budget — which would truncate the text and replace the tool-specific hint
// with a generic one (#582).
//
// Use ShapeWithHint (instead of Shape) when the tool has a per-call max_bytes
// override that may be larger than the default budget. When the text fits
// within the override, only the budget-applied marker is appended; the wrapper
// strips it (StripBudgetMarker) before the agent sees the output, so it's
// invisible. When the text exceeds the override, the hint is appended as a
// truncation footer (same as Shape).
func ShapeWithHint(text string, budget int, continuationHint string) string {
	if budget <= 0 || len(text) <= budget {
		// Text fits — mark as budget-applied so the wrapper doesn't re-shape
		// with the default budget (which would lose the tool-specific hint).
		if budget > 0 {
			return text + budgetAppliedMarker
		}
		return text
	}
	// Text exceeds budget — truncate with the tool-specific hint (same as Shape).
	return Shape(text, budget, continuationHint)
}

// IsShaped reports whether text already carries a truncation footer emitted
// by Shape, or a budget-applied marker emitted by ShapeWithHint. Used by the
// addTool wrapper to skip double-shaping when a tool handler already applied
// a custom budget (#582).
func IsShaped(text string) bool {
	return strings.Contains(text, truncationFooterPrefix) ||
		strings.Contains(text, budgetAppliedMarker)
}

// HasTookFooter reports whether text already carries a took_ms footer.
func HasTookFooter(text string) bool {
	return strings.Contains(text, tookFooterPrefix)
}

// StripBudgetMarker removes the budget-applied sentinel from text if present.
// Called by the addTool wrapper after IsShaped check so the marker is not
// visible in the final agent-facing output (#582).
func StripBudgetMarker(text string) string {
	return strings.ReplaceAll(text, budgetAppliedMarker, "")
}

// MarkBudgetApplied appends the budget-applied sentinel so the addTool wrapper
// detects already-shaped output (IsShaped=true) and skips re-shaping with the
// default budget. Use after a ladder (or other custom budget mechanism) has
// fit text to a per-call budget that may exceed DefaultBudget — without this,
// the wrapper would re-shape at DefaultBudget and truncate the tail (#582).
// The wrapper strips the marker (StripBudgetMarker) before the agent sees it.
func MarkBudgetApplied(text string) string {
	return text + budgetAppliedMarker
}

// TookFooter returns a compact one-line observability footer:
//
//	took_ms=N
//
// elapsed is clamped to >= 1 ms. The footer is newline-prefixed so it can
// be appended to any response body (text, XML, JSON) without merging into
// the last line.
func TookFooter(elapsed time.Duration) string {
	ms := elapsed.Milliseconds()
	if ms < 1 {
		ms = 1
	}
	return fmt.Sprintf("%s%d", tookFooterPrefix, ms)
}

// AppendTook returns text with the took_ms footer appended, unless the text
// already carries one (idempotent).
func AppendTook(text string, elapsed time.Duration) string {
	if HasTookFooter(text) {
		return text
	}
	return text + TookFooter(elapsed)
}

// ResolveBudget picks the effective budget from a per-call override and the
// default. A non-positive override yields the default; an override below
// MinBudget is clamped to MinBudget; an override above MaxBudget is clamped
// to MaxBudget (see MaxBudget for why the ceiling exists).
func ResolveBudget(override, defaultBudget int) int {
	if override <= 0 {
		return defaultBudget
	}
	if override < MinBudget {
		return MinBudget
	}
	if override > MaxBudget {
		return MaxBudget
	}
	return override
}

// condensationNotePrefix is the sentinel prefix of the marker appended to
// every rung below the first (full) one returned by PickFitting. It tells
// the agent the result was condensed and by which rung, so it can narrow its
// query instead of assuming it saw everything. A silently-shortened result
// is the same failure class as the hard truncation this ladder replaces.
//
// The marker is an XML comment (<!-- ... -->) so it survives a strict XML
// parse — trailing character data after the root element is rejected by
// strict parsers even though Go's encoding/xml tolerates it. This mirrors
// the appendMetaFooter precedent (<!-- meta: ... -->).
const condensationNotePrefix = "<!-- condensed:"

// cutMarkerPrefix is the sentinel prefix of the last-resort marker emitted
// by PickFitting when even the cheapest rung does not fit the budget. The
// cheapest rendering is then truncated and this marker is appended so the
// consumer knows the result was cut — this is the honest fallback, not the
// normal path. XML comment shape for strict-parse survival (see
// condensationNotePrefix). The cut path truncates mid-document by design,
// so well-formedness is impossible there — only the marker's shape is kept
// consistent.
const cutMarkerPrefix = "<!-- cut:"

// Rung is one progressively cheaper rendering in a Ladder. Render is a
// closure that produces the full rendered string for this rung; it is only
// invoked when PickFitting reaches this rung (laziness is load-bearing — an
// expensive rendering is never computed unless it is actually reached).
type Rung struct {
	Name   string
	Render func() string
}

// Ladder is an ordered list of rungs, most expensive (fullest) first. The
// cheapest (last) rung is the last-resort fallback when no rung fits. Each
// rung's Render closure MUST produce a self-consistent, parseable document
// on its own — PickFitting never returns a partial document on the normal
// path (the only exception is the last-resort truncation of the cheapest
// rung, which is explicitly marked with cutMarkerPrefix).
type Ladder []Rung

// PickFitting walks the ladder in order and returns the FIRST rendering that
// fits within budget (bytes) as `body`, plus the rung-1 (fullest) rendering
// as `full`, plus `rung` — the 1-based index of the chosen rung. Rungs after
// the first carry a condensation note naming the chosen rung, so the agent
// knows the result was condensed. Laziness: a rung's Render closure is only
// invoked when that rung is reached — rungs past the chosen one are never
// rendered. Rung 1 is always rendered (it is the first iteration) and is
// returned as `full` so the caller can persist it to a file without
// re-rendering — the expensive full rendering is produced AT MOST ONCE per
// call.
//
// `rung` lets the caller detect condensation: rung == 1 means the full
// rendering fit inline (body == full, nothing was condensed); rung > 1 means
// a cheaper rung was chosen (the full rendering is available as `full` for
// the caller to persist to a file). On the cut path (no rung fit), rung is
// the last (cheapest) rung's index — still > 1 for any multi-rung ladder, so
// the condensation signal is correct.
//
// If NO rung fits (even the cheapest), the cheapest rendering is truncated
// to fit and an explicit cut marker is appended. This is the last-resort
// branch, not the normal one; the marker is honest about the cut.
//
// An empty ladder or budget <= 0 yields ("", "", 0).
func PickFitting(l Ladder, budget int) (body, full string, rung int) {
	if len(l) == 0 || budget <= 0 {
		return "", "", 0
	}
	n := len(l)
	// Rung 1 is always rendered first — capture it as `full` for the caller
	// so the file-save path never re-renders it (at-most-once guarantee).
	full = l[0].Render()
	var lastRaw string
	for i, rung := range l {
		var raw string
		if i == 0 {
			raw = full // reuse the already-rendered rung 1
		} else {
			raw = rung.Render()
		}
		if i == n-1 {
			lastRaw = raw
		}
		text := raw
		if i > 0 {
			text += condensationNote(i+1, n, rung.Name)
		}
		if len(text) <= budget {
			return text, full, i + 1
		}
	}
	// No rung fits — truncate the cheapest (last) raw rendering and emit an
	// explicit cut marker so the consumer knows the result was cut.
	name := l[n-1].Name
	marker := cutMarker(budget, n, name)
	// If the full marker itself exceeds the budget (absurdly small budget),
	// fall back to the minimal honest signal so the result still fits.
	if len(marker) >= budget {
		marker = cutMarkerPrefix + " -->"
	}
	cut := budget - len(marker)
	if cut < 0 {
		cut = 0
	}
	if cut > len(lastRaw) {
		cut = len(lastRaw)
	}
	return lastRaw[:cut] + marker, full, n
}

// condensationNote builds the marker appended to rungs below the first.
// Format: <!-- condensed: rung <i>/<n> — <name> -->
func condensationNote(i, n int, name string) string {
	return fmt.Sprintf("%s rung %d/%d — %s -->", condensationNotePrefix, i, n, name)
}

// cutMarker builds the last-resort marker. Format:
// <!-- cut: result truncated at <budget> bytes — even rung <n>/<n> (<name>) did not fit -->
func cutMarker(budget, n int, name string) string {
	return fmt.Sprintf("%s result truncated at %d bytes — even rung %d/%d (%s) did not fit -->",
		cutMarkerPrefix, budget, n, n, name)
}
