package main

import (
	"context"
	"sync"
)

// provenanceSlot is a write-once container for the (requested, resolved) repo
// root pair, stored on the request context. resolveRoot writes to it; the
// addTool wrapper reads a snapshot after the handler returns to attach the
// provenance footer centrally (after budget shaping, so it survives
// truncation).
//
// Write-once (first writer wins) is required, not defensive: handlers spawn
// goroutines that outlive the response (tool_federated_cochange.go calls
// kickFedBackground and returns a partial result on the next line; the
// internal/compare/enrich.go fan-out derives from the request context). A
// last-writer-wins slot would be a data race and would make the reported root
// depend on goroutine scheduling. Write-once also gives a deterministic answer
// for a tool that resolves several roots (code_compare): the first one.
type provenanceSlot struct {
	mu        sync.Mutex
	requested string
	resolved  string
	set       bool
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
