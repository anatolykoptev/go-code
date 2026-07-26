package cache

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// cacheValidatorEvictTotal counts L1 entries evicted because the caller's
// Validator hook (passed to GetIfValid) reported them stale.
//
// reason is a free-form, low-cardinality label: the package emits "stale" by
// default. Future per-caller reasons (e.g. "modtime_changed", "etag_drift")
// can be added by exposing a recordValidatorEvict variant if a real need
// surfaces — keep cardinality bounded by validating reason against a known set.
var cacheValidatorEvictTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "cache_validator_evict_total",
		Help: "Total L1 entries evicted because GetIfValid's Validator returned false.",
	},
	[]string{"reason"},
)

// recordValidatorEvict increments the validator-evict counter for the given
// reason. Reason should be a stable, low-cardinality label.
func recordValidatorEvict(reason string) {
	cacheValidatorEvictTotal.WithLabelValues(reason).Inc()
}

// MetricsConfig configures opt-in Prometheus metric publication for a Cache
// instance. Construct via WithMetrics. A nil *MetricsConfig (or unset
// Config.Metrics) disables emission entirely — no goroutine, no allocation,
// no Prometheus registration cost.
type MetricsConfig struct {
	reg  prometheus.Registerer
	name string
}

// WithMetrics returns a *MetricsConfig that wires the Cache to publish its
// Stats() snapshot as Prometheus metrics on the given Registerer. The name
// is required and labels every metric (cache="<name>"); two Cache instances
// sharing the same Registerer must use distinct names — otherwise the second
// New() call panics on duplicate registration. Pass nil reg or empty name to
// disable: the returned *MetricsConfig is nil-equivalent, no registration.
//
// The published shape (CounterFunc-based, evaluated lazily on each scrape):
//
//	gokit_cache_hits_total{cache="<name>",tier="L1"}
//	gokit_cache_hits_total{cache="<name>",tier="L2"}
//	gokit_cache_misses_total{cache="<name>",tier="L1"}
//	gokit_cache_misses_total{cache="<name>",tier="L2"}
//	gokit_cache_evictions_total{cache="<name>"}
//	gokit_cache_size{cache="<name>"} (gauge — current L1 entry count)
//
// Counters are monotonic since cache construction.
func WithMetrics(reg prometheus.Registerer, name string) *MetricsConfig {
	if reg == nil || name == "" {
		return nil
	}
	return &MetricsConfig{reg: reg, name: name}
}

// registerCacheMetrics wires the CounterFunc/GaugeFunc instances for c on
// mc.reg. Called once from New when cfg.Metrics is non-nil. Reads atomic
// counters directly on each Prometheus scrape — no goroutine, no polling.
//
// Panics on duplicate registration (Prometheus contract); callers MUST give
// each Cache a unique name when sharing a Registerer.
func registerCacheMetrics(c *Cache, mc *MetricsConfig) {
	labels := prometheus.Labels{"cache": mc.name}

	hitsL1 := prometheus.NewCounterFunc(prometheus.CounterOpts{
		Name:        "gokit_cache_hits_total",
		Help:        "Total cache hits per tier.",
		ConstLabels: mergeLabels(labels, prometheus.Labels{"tier": "L1"}),
	}, func() float64 { return float64(c.hits.Load()) })

	hitsL2 := prometheus.NewCounterFunc(prometheus.CounterOpts{
		Name:        "gokit_cache_hits_total",
		Help:        "Total cache hits per tier.",
		ConstLabels: mergeLabels(labels, prometheus.Labels{"tier": "L2"}),
	}, func() float64 { return float64(c.l2hits.Load()) })

	missesL1 := prometheus.NewCounterFunc(prometheus.CounterOpts{
		Name:        "gokit_cache_misses_total",
		Help:        "Total cache misses per tier.",
		ConstLabels: mergeLabels(labels, prometheus.Labels{"tier": "L1"}),
	}, func() float64 { return float64(c.misses.Load()) })

	missesL2 := prometheus.NewCounterFunc(prometheus.CounterOpts{
		Name:        "gokit_cache_misses_total",
		Help:        "Total cache misses per tier.",
		ConstLabels: mergeLabels(labels, prometheus.Labels{"tier": "L2"}),
	}, func() float64 { return float64(c.l2misses.Load()) })

	evictions := prometheus.NewCounterFunc(prometheus.CounterOpts{
		Name:        "gokit_cache_evictions_total",
		Help:        "Total L1 entries evicted (capacity, expiry, weight, idle, explicit).",
		ConstLabels: labels,
	}, func() float64 { return float64(c.evictions.Load()) })

	l2Available := prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name:        "gokit_cache_l2_available",
		Help:        "1 if the L2 (Redis) tier is configured and active, 0 if L1-only (incl. silent Redis-connect downgrade).",
		ConstLabels: labels,
	}, func() float64 {
		if c.L2Available() {
			return 1
		}
		return 0
	})

	size := prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name:        "gokit_cache_size",
		Help:        "Current number of entries in L1.",
		ConstLabels: labels,
	}, func() float64 {
		c.mu.Lock()
		n := len(c.items)
		c.mu.Unlock()
		return float64(n)
	})

	mc.reg.MustRegister(hitsL1, hitsL2, missesL1, missesL2, evictions, l2Available, size)
}

// mergeLabels returns a new Labels with the union of a and b. b wins on conflict.
func mergeLabels(a, b prometheus.Labels) prometheus.Labels {
	out := make(prometheus.Labels, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
