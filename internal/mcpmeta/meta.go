// Package mcpmeta provides a small response-envelope helper used by
// MCP tools to carry timing, optional next-call hints, and optional
// staleness warnings without forcing every tool to repeat boilerplate.
//
// The envelope is intentionally minimal: a tool may emit zero or more
// fields. Empty fields are omitted from the JSON payload so the
// caller sees only what is signal.
package mcpmeta

import (
	"time"
)

// Envelope is the meta block attached to a tool response.
//
// Empty optional fields are omitted from the JSON payload via the
// `omitempty` tags on Hint / StaleWarning / IndexedSHA / LiveSHA, so
// the caller sees only signal. DurationMS is always populated on the Go
// struct (clamped to >= 1 by Wrap to enforce the "always populated"
// contract) — but that is a struct-level guarantee, not a wire one:
// cmd/go-code's response-footer renderer (appendMetaFooter) omits the
// `<!-- meta: ... -->` footer entirely when Hint and StaleWarning are both
// empty, so a bare duration_ms with no hint and no staleness never actually
// reaches the consumer — duration-only telemetry has zero analytic value to
// an agent that can't act on it.
//
// Convention:
//   - DurationMS is always populated on the struct, but is rendered to the
//     consumer only when paired with a Hint or StaleWarning — a bare
//     duration is suppressed at the response-footer layer (cmd/go-code).
//   - Hint is populated only when a clear next-call is cheap and obvious.
//     A noisy hint trains the calling agent to ignore the field.
//   - StaleWarning is populated only when the indexed commit no longer
//     matches the on-disk HEAD. Silence is the calibrated signal.
type Envelope struct {
	DurationMS   int64  `json:"duration_ms"`
	Hint         string `json:"hint,omitempty"`
	StaleWarning string `json:"stale_warning,omitempty"`
	IndexedSHA   string `json:"indexed_sha,omitempty"`
	LiveSHA      string `json:"live_sha,omitempty"`
	// GraphStaleAgeS is the age (seconds) of the AGE graph when the retrieval
	// path fused a stale graph into the search results. Zero (omitted) when
	// the graph is fresh — the fresh path is byte-identical to pre-#691
	// behavior. A non-zero value is the degradation marker: the caller
	// received ranking that blended an outdated graph (#691).
	GraphStaleAgeS float64 `json:"graph_stale_age_s,omitempty"`
}

// Wrap builds an Envelope from a measured tool duration and an optional hint.
// Pass hint == "" when no next-call is obvious. Sub-millisecond durations
// are clamped to 1 so the envelope's "always populated" contract holds.
func Wrap(elapsed time.Duration, hint string) Envelope {
	ms := elapsed.Milliseconds()
	if ms < 1 {
		ms = 1
	}
	return Envelope{
		DurationMS: ms,
		Hint:       hint,
	}
}
