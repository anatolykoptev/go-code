package cache

import (
	"sync"
	"time"
)

// TTLLRU is a generic, concurrency-safe LRU cache with a per-entry TTL: Set
// stamps time.Now() at insertion under the cache's default TTL, SetWithTTL
// overrides that TTL for one entry, and a read past its TTL is lazily
// evicted rather than returned.
//
// Extracted from the TTL+LRU skeleton that had accreted independently across
// internal/callgraph/repo_cache.go, internal/compare/churn_cache.go,
// internal/compare/coupling_cache.go, and internal/federate/touches_cache.go
// (mu.Lock → lru.Get → lazy-expiry-delete → return / mu.Lock → lru.Set with
// a time.Now() stamp, repeated four times) — new TTL+LRU call sites should
// use this instead of hand-rolling a fifth copy. The four pre-existing sites
// are left as-is here (separate migration, tracked as debt). (A fifth call
// site, goanalysis.CachedLoadPackages, was removed in issue #747 — it pinned
// the go/types arena for the cache TTL and OOM-killed the indexer; the load
// is now passed through the request seam instead of cached globally.)
//
// Unlike the plain LRU (which pushes its mutex to the caller), TTLLRU holds
// its own — every method here is safe for concurrent use.
type TTLLRU[K comparable, V any] struct {
	mu  sync.Mutex
	lru *LRU[K, ttlEntry[V]]
	ttl time.Duration
}

type ttlEntry[V any] struct {
	value V
	at    time.Time
	ttl   time.Duration
}

// NewTTLLRU creates a TTL+LRU cache with the given max capacity and default
// per-entry TTL. The default applies to every Set call; SetWithTTL overrides
// it for a single entry (e.g. a shorter TTL for a cached failure than for a
// cached success).
func NewTTLLRU[K comparable, V any](maxSize int, ttl time.Duration) *TTLLRU[K, V] {
	return &TTLLRU[K, V]{
		lru: NewLRU[K, ttlEntry[V]](maxSize),
		ttl: ttl,
	}
}

// Get returns the value for key and true, or the zero value and false if
// absent or past its TTL — an expired entry is lazily deleted on read.
func (c *TTLLRU[K, V]) Get(key K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.lru.Get(key)
	if !ok {
		var zero V
		return zero, false
	}
	if time.Since(e.at) > e.ttl {
		c.lru.Delete(key)
		var zero V
		return zero, false
	}
	return e.value, true
}

// Set inserts or updates key with value under the cache's default TTL.
func (c *TTLLRU[K, V]) Set(key K, value V) {
	c.SetWithTTL(key, value, c.ttl)
}

// SetWithTTL inserts or updates key with value under an entry-specific TTL,
// overriding the cache's default for this call only.
//
// A ttl <= 0 is a deliberate no-op: the entry is never stored, so every
// subsequent Get misses and every caller re-attempts from scratch. This is
// how a caller-budget-specific outcome (e.g. one caller's context deadline
// expiring) can be excluded from caching entirely, rather than cached under
// a token TTL that would still let it poison a different caller with a
// longer remaining budget.
func (c *TTLLRU[K, V]) SetWithTTL(key K, value V, ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lru.Set(key, ttlEntry[V]{value: value, at: time.Now(), ttl: ttl})
}

// Delete removes key from the cache. No-op if absent.
func (c *TTLLRU[K, V]) Delete(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lru.Delete(key)
}

// Len returns the number of entries currently in the cache, including any
// not-yet-lazily-evicted expired entries.
func (c *TTLLRU[K, V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lru.Len()
}
