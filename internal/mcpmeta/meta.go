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
// Every optional field is omitted from the JSON payload via its `omitempty`
// tag, so the caller sees only signal. DurationMS is always populated on the Go
// struct (clamped to >= 1 by Wrap to enforce the "always populated" contract) —
// but that is a struct-level guarantee, not a wire one: the response-footer
// renderers omit the `<!-- meta: ... -->` footer entirely unless HasSignal
// reports something actionable, so a bare duration_ms never reaches the
// consumer — duration-only telemetry has zero analytic value to an agent that
// can't act on it.
//
// Convention — every signal field stays EMPTY unless it has something to say,
// because a field that speaks on every call trains the calling agent to ignore
// it. HasSignal is the single list of what counts; add a field there or it is
// written and never rendered.
//   - DurationMS is always populated on the struct, but reaches the consumer
//     only alongside some other signal.
//   - Hint: only when a clear next-call is cheap and obvious.
//   - StaleWarning (+IndexedSHA/LiveSHA): only when the indexed commit no
//     longer matches the checkout's main-branch tip.
//   - CheckoutLag (+CheckoutSHA/OriginSHA): only when the checkout's main
//     branch differs from the same branch on origin.
//   - SourcePath: only when the caller named the repo by a path that is not
//     where it lives.
//   - GraphStaleAgeS: only when the retrieval path fused a stale graph.
type Envelope struct {
	DurationMS   int64  `json:"duration_ms"`
	Hint         string `json:"hint,omitempty"`
	StaleWarning string `json:"stale_warning,omitempty"`
	IndexedSHA   string `json:"indexed_sha,omitempty"`
	LiveSHA      string `json:"live_sha,omitempty"`
	// SourcePath is the server-side repository root the answer was computed
	// from. Populated only when the caller named the repo by a different path
	// (a PATH_MAPPINGS alias), because that is the only case where a reader
	// could mistake the answer for one about their own working tree.
	// See WithSourcePath.
	SourcePath string `json:"source_path,omitempty"`
	// CheckoutLag reports that the server-side checkout's main branch differs
	// from the SAME branch on origin — the indexed tree is not the forge's
	// current tip. Empty when they match. This is a different axis from
	// StaleWarning, which compares the INDEX against the CHECKOUT.
	// See WithCheckoutLag.
	CheckoutLag string `json:"checkout_lag,omitempty"`
	// CheckoutSHA and OriginSHA carry the two tips behind CheckoutLag so a
	// consumer can compare them without parsing them back out of the prose,
	// mirroring IndexedSHA/LiveSHA on the freshness axis. Set together with
	// CheckoutLag and empty otherwise.
	CheckoutSHA string `json:"checkout_sha,omitempty"`
	OriginSHA   string `json:"origin_sha,omitempty"`
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

// HasSignal reports whether the envelope carries anything the caller can act
// on. The response-footer renderers use this as their single gate so they
// cannot drift apart: before it existed appendMetaFooter counted
// GraphStaleAgeS as signal and metaLargeTextResult did not, so a
// graph-staleness-only envelope rendered a footer on a small response and
// silently lost it on a large one.
//
// DurationMS is deliberately excluded — a bare duration is telemetry the
// calling agent cannot act on, and counting it would put a footer on every
// single response, which is the noise the silence contract exists to prevent.
func (e Envelope) HasSignal() bool {
	return e.Hint != "" ||
		e.StaleWarning != "" ||
		e.SourcePath != "" ||
		e.CheckoutLag != "" ||
		e.GraphStaleAgeS != 0
}
