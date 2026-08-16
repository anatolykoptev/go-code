package main

import (
	"context"
	"sync"

	"github.com/anatolykoptev/vaelor/internal/mcpmeta"
)

// provenanceSlot carries two independent pieces of request-scoped provenance:
//
//   - the (requested, resolved) repo root pair, write-once (first writer wins);
//   - an accumulating mcpmeta.Envelope, merged per-field (first writer wins
//     per field, NOT per envelope).
//
// The root pair is write-once because first-writer-wins is deterministic for
// it: handlers spawn goroutines that outlive the response
// (tool_federated_cochange.go calls kickFedBackground and returns a partial
// result on the next line; the internal/compare/enrich.go fan-out derives
// from the request context). A last-writer-wins slot would be a data race and
// would make the reported root depend on goroutine scheduling. Write-once
// also gives a deterministic answer for a tool that resolves several roots
// (code_compare): the first one.
//
// The envelope is NOT write-once. It accumulates: resolveRoot contributes the
// path signals (SourcePath, CheckoutLag), the tool contributes freshness
// (StaleWarning) and its Hint. Write-once would silently drop whichever wrote
// second and render a footer missing half its content — the same silent class
// in a new place. Merge precedence:
//
//   - per FIELD, first writer wins — a non-empty field is never overwritten by
//     a later non-empty value; an empty field is filled by whoever supplies it.
//   - DurationMS is owned by the wrapper alone, from its own measurement. A
//     recorded envelope's DurationMS is never merged.
//   - A late write (a background goroutine outliving the response) must not be
//     able to change what was already rendered — the wrapper takes a snapshot
//     under the mutex before rendering, so writes after the snapshot are inert.
type provenanceSlot struct {
	mu        sync.Mutex
	requested string
	resolved  string
	set       bool
	env       mcpmeta.Envelope
}

type provenanceCtxKey struct{}

// seedProvenanceSlot returns ctx with an empty provenance slot stored. The
// addTool wrapper calls this before dispatching the handler so resolveRoot
// (and any other path that resolves a repo root) can record into it.
func seedProvenanceSlot(ctx context.Context) context.Context {
	return context.WithValue(ctx, provenanceCtxKey{}, &provenanceSlot{})
}

// recordProvenance records (requested, resolved) into the context's slot.
// First writer wins; later calls are dropped. No-op when the context carries
// no slot (e.g. resolveRoot called outside the addTool wrapper, as in tests).
func recordProvenance(ctx context.Context, requested, resolved string) {
	slot, ok := ctx.Value(provenanceCtxKey{}).(*provenanceSlot)
	if !ok || slot == nil {
		return
	}
	slot.mu.Lock()
	defer slot.mu.Unlock()
	if slot.set {
		return
	}
	slot.requested = requested
	slot.resolved = resolved
	slot.set = true
}

// recordEnvelope merges env into the context's provenance slot, per-field
// first-writer-wins. DurationMS is never merged — it is owned by the wrapper
// alone. No-op when the context carries no slot (tests calling tool handlers
// directly without the addTool wrapper).
func recordEnvelope(ctx context.Context, env mcpmeta.Envelope) {
	slot, ok := ctx.Value(provenanceCtxKey{}).(*provenanceSlot)
	if !ok || slot == nil {
		return
	}
	slot.mu.Lock()
	defer slot.mu.Unlock()
	mergeEnvelope(&slot.env, env)
}

// mergeEnvelope merges src into dst per-field: a non-empty field in dst is
// never overwritten by a non-empty field in src; an empty field in dst is
// filled by src. DurationMS is never merged — the wrapper owns it.
func mergeEnvelope(dst *mcpmeta.Envelope, src mcpmeta.Envelope) {
	if dst.Hint == "" && src.Hint != "" {
		dst.Hint = src.Hint
	}
	if dst.StaleWarning == "" && src.StaleWarning != "" {
		dst.StaleWarning = src.StaleWarning
	}
	if dst.IndexedSHA == "" && src.IndexedSHA != "" {
		dst.IndexedSHA = src.IndexedSHA
	}
	if dst.LiveSHA == "" && src.LiveSHA != "" {
		dst.LiveSHA = src.LiveSHA
	}
	if dst.SourcePath == "" && src.SourcePath != "" {
		dst.SourcePath = src.SourcePath
	}
	if dst.CheckoutLag == "" && src.CheckoutLag != "" {
		dst.CheckoutLag = src.CheckoutLag
	}
	if dst.CheckoutSHA == "" && src.CheckoutSHA != "" {
		dst.CheckoutSHA = src.CheckoutSHA
	}
	if dst.OriginSHA == "" && src.OriginSHA != "" {
		dst.OriginSHA = src.OriginSHA
	}
	if dst.GraphStaleAgeS == 0 && src.GraphStaleAgeS != 0 {
		dst.GraphStaleAgeS = src.GraphStaleAgeS
	}
}

// provenanceSnapshot returns the (requested, resolved) pair recorded in the
// slot. Returns ok=false when the context carries no slot or nothing was
// recorded.
func provenanceSnapshot(ctx context.Context) (requested, resolved string, ok bool) {
	slot, ok := ctx.Value(provenanceCtxKey{}).(*provenanceSlot)
	if !ok || slot == nil {
		return "", "", false
	}
	slot.mu.Lock()
	defer slot.mu.Unlock()
	if !slot.set {
		return "", "", false
	}
	return slot.requested, slot.resolved, true
}

// envelopeSnapshot returns a copy of the merged envelope recorded in the
// slot. Returns a zero Envelope when the context carries no slot. The wrapper
// reads this after the handler returns, sets DurationMS from its own
// measurement, and renders the footer once — so a late write by a background
// goroutine after this snapshot cannot change what was rendered.
func envelopeSnapshot(ctx context.Context) mcpmeta.Envelope {
	slot, ok := ctx.Value(provenanceCtxKey{}).(*provenanceSlot)
	if !ok || slot == nil {
		return mcpmeta.Envelope{}
	}
	slot.mu.Lock()
	defer slot.mu.Unlock()
	return slot.env
}
