// Package cache provides a tiered L1 (memory) + optional L2 (Redis) cache.
// L1 uses S3-FIFO eviction with 3 queues (small, main, ghost) for high hit rates.
// If RedisURL is empty, operates as L1-only (no external dependencies needed at runtime).
package cache

import (
	"container/list"
	"math/rand/v2"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"weak"
)

// maxFreq is the S3-FIFO frequency counter ceiling (0-3).
const maxFreq = 3

// entry is an item stored in the S3-FIFO cache.
type entry struct {
	key        string
	data       []byte
	expiresAt  time.Time
	freq       uint8         // 0-3, S3-FIFO frequency counter
	elem       *list.Element // back-ref in small or main list
	inMain     bool          // false=small, true=main
	tags       []string      // tag-based invalidation groups
	weight     int64         // byte weight; 0 when Weigher is nil
	lastAccess time.Time     // last Get hit time; zero when IdleTTL == 0
}

// Cache is a tiered L1 (memory) + optional L2 (Redis) cache.
// L1 uses the S3-FIFO eviction algorithm with three queues.
type Cache struct {
	cfg Config

	mu       sync.Mutex
	items    map[string]*entry              // all active entries
	small    *list.List                     // probation queue (10% capacity)
	main     *list.List                     // main queue (90% capacity)
	ghost    *list.List                     // ghost queue (evicted keys, no values)
	ghostMap map[string]*list.Element       // ghost key lookups
	tagIndex map[string]map[string]struct{} // tag → set of keys

	smallCap int // 10% of L1MaxItems
	mainCap  int // 90% of L1MaxItems
	ghostCap int // = mainCap

	totalWeight int64 // protected by mu; 0 when Weigher is nil

	hits      atomic.Int64
	misses    atomic.Int64
	evictions atomic.Int64
	l2hits    atomic.Int64
	l2misses  atomic.Int64
	l2errors  atomic.Int64

	flight group
	l2     L2 // optional L2 store; nil = L1-only
	done   chan struct{}
}

// New creates a new Cache. If cfg.RedisURL is empty, L2 is disabled.
// Call Close() when done to stop the background cleanup goroutine.
func New(cfg Config) *Cache {
	cfg.applyDefaults()

	smallCap := cfg.L1MaxItems / 10
	if smallCap < 1 {
		smallCap = 1
	}
	mainCap := cfg.L1MaxItems - smallCap

	c := &Cache{
		cfg:      cfg,
		items:    make(map[string]*entry),
		small:    list.New(),
		main:     list.New(),
		ghost:    list.New(),
		ghostMap: make(map[string]*list.Element),
		tagIndex: make(map[string]map[string]struct{}),
		smallCap: smallCap,
		mainCap:  mainCap,
		ghostCap: mainCap,
		done:     make(chan struct{}),
	}

	// Use explicitly provided L2, else try Redis, else nil.
	if cfg.L2 != nil {
		c.l2 = cfg.L2
	} else if cfg.RedisURL != "" {
		// Guard: NewRedisL2 returns nil on failure — must NOT assign nil
		// concrete pointer to interface (Go typed-nil trap causes SIGSEGV).
		if l2 := NewRedisL2(cfg.RedisURL, cfg.RedisDB, cfg.Prefix); l2 != nil {
			c.l2 = l2
		}
	}

	// Background cleanup every 1/10 of TTL, minimum 10s.
	interval := cfg.L1TTL / 10
	if interval < 10*time.Second {
		interval = 10 * time.Second
	}
	// Opt-in Prometheus metrics — registered lazily via CounterFunc, so no
	// background goroutine and no per-Get/Set overhead. Skipped entirely when
	// cfg.Metrics is nil (default).
	//
	// Registered BEFORE the cleanup goroutine + finalizer are started so that
	// a panic during registration (e.g. duplicate metric name on a shared
	// Registerer) cannot leak the cleanup goroutine: registerCacheMetrics
	// builds CounterFunc/GaugeFunc closures that capture c, which keeps c
	// reachable via the Registerer and defeats the runtime finalizer — so a
	// goroutine started before a registration panic would never be reclaimed
	// (neither Close nor the finalizer can run). Starting the goroutine only
	// after registration succeeds makes construction panic-safe.
	if cfg.Metrics != nil {
		registerCacheMetrics(c, cfg.Metrics)
	}

	// Launch the background cleanup goroutine. The goroutine holds only
	// the done channel (a separate heap object) and a weak pointer to the
	// Cache, so it does NOT prevent the Cache from being garbage-collected.
	// This allows the finalizer below to fire even if Close() is never
	// called, breaking the reference cycle that would otherwise keep the
	// Cache (and its goroutine) alive forever.
	done := c.done
	wp := weak.Make(c)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if cc := wp.Value(); cc != nil {
					cc.cleanupTick()
				} else {
					// Cache was garbage-collected; exit to avoid a leak.
					return
				}
			}
		}
	}()

	// Register a finalizer so that if the caller forgets Close(), the
	// background cleanup goroutine is still stopped when the Cache is
	// garbage-collected. This prevents goroutine leaks in long-running
	// services that create transient caches (per-request / per-tenant).
	// Close() clears the finalizer to avoid a double-close.
	runtime.SetFinalizer(c, func(c *Cache) { c.Close() })

	return c
}

func (c *Cache) jitteredTTL(base time.Duration) time.Duration {
	if c.cfg.JitterPercent <= 0 {
		return base
	}
	jitter := int64(float64(base) * c.cfg.JitterPercent)
	if jitter <= 0 {
		return base
	}
	return base + time.Duration(rand.Int64N(2*jitter+1)-jitter)
}

// Clear removes all entries from L1 and returns the number cleared.
// L2 is not affected. OnEvict callbacks are NOT fired (bulk operation).
func (c *Cache) Clear() int {
	c.mu.Lock()
	n := len(c.items)
	c.items = make(map[string]*entry)
	c.small.Init()
	c.main.Init()
	c.ghost.Init()
	c.ghostMap = make(map[string]*list.Element)
	c.tagIndex = make(map[string]map[string]struct{})
	c.mu.Unlock()
	return n
}

// Close stops the background cleanup goroutine and closes L2 if set.
// Callers MUST call Close when finished with the Cache to stop the
// background cleanup goroutine; otherwise it leaks for the Cache's
// lifetime. As a safety net, New registers a runtime finalizer that
// calls Close when the Cache is garbage-collected, but explicit Close
// is strongly preferred — finalizers are not guaranteed to run promptly.
//
//go:noinline
func (c *Cache) Close() {
	// Clear the finalizer first so a GC-triggered Close cannot double-close.
	runtime.SetFinalizer(c, nil)

	select {
	case <-c.done:
	default:
		close(c.done)
	}
	if c.l2 != nil {
		c.l2.Close()
	}
}

// L2Available reports whether a second-tier (L2) store is configured and
// active on this Cache. It returns true when an L2 was successfully wired
// (either via Config.L2 or a reachable Config.RedisURL) and false when the
// Cache is operating in L1-only mode — including the silent-downgrade case
// where RedisURL was requested but Redis was unreachable at construction
// time (NewRedisL2 returns nil on connection failure and logs a warning).
//
// This gives callers a programmatic way to detect the silent-downgrade that
// previously surfaced only as a slog.Warn log line, enabling metrics/alerts
// on cache tier availability. Non-breaking: it is a new read-only method.
func (c *Cache) L2Available() bool {
	return c.l2 != nil
}
