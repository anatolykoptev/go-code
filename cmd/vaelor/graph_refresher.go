package main

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/anatolykoptev/go-kit/env"
	"github.com/anatolykoptev/vaelor/internal/codegraph"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"golang.org/x/sync/singleflight"
)

// graphRefreshIndexFn is the signature of codegraph.IndexRepo, captured as a
// type so white-box tests can inject a counting/barrier-controlled fake — the
// same seam shape as internal/ingest/clone.go's runCloneFn and
// internal/goanalysis/cached_loader.go's loadPackagesFn.
type graphRefreshIndexFn func(ctx context.Context, store *codegraph.Store, root string, isRemote bool, cfg codegraph.IndexConfig) (*codegraph.GraphMeta, error)

// graphRefreshIndexRepo is the production implementation of graphRefreshIndexFn.
// Swapping this package var (or the struct field set by newGraphRefresher) is
// the test seam; production code must never swap it.
var graphRefreshIndexRepo graphRefreshIndexFn = codegraph.IndexRepo

// defaultGraphRefreshDebounce is the coalescing window for background graph
// refreshes triggered by the file watcher. N source-file edits to the same
// repo within this window produce ONE codegraph.IndexRepo call, not N —
// protecting the shared 4-core box from a refresh stampede on a burst save
// (git checkout / pull / formatter run). Override via GRAPH_REFRESH_DEBOUNCE_MS.
//
// 45s is a balance between freshness (the graph lags <1min behind an edit) and
// cost (a full graph rebuild is tens of seconds for a large repo; coalescing a
// 45s burst into one rebuild is the right tradeoff for a background,
// non-blocking refresh that does not gate any tool response).
const defaultGraphRefreshDebounce = 45 * time.Second

// graphRefreshTimeout bounds a single background graph rebuild. Applied to a
// DECOUPLED context (context.Background(), not any caller's ctx) so a caller
// going away cannot cancel a refresh other waiters share — the leader-ctx-
// poisoning failure internal/goanalysis PR #294 fixed and internal/ingest
// clone.go's #678 single-flight reused. Matches age_graph_gate.go's 30m
// background build budget.
const graphRefreshTimeout = 30 * time.Minute

// graphRefreshOutcome* are the label values for gocode_graph_refresh_total.
const (
	graphRefreshOutcomeStarted   = "started"   // a refresh began executing (single-flight leader)
	graphRefreshOutcomeCoalesced = "coalesced" // a trigger absorbed by the debounce window
	graphRefreshOutcomeSuccess   = "success"   // refresh completed without error
	graphRefreshOutcomeFailed    = "failed"    // refresh returned an error
	graphRefreshOutcomeSkipped   = "skipped"   // kill switch off or no store
)

// graphRefreshTotal counts background graph-refresh triggers by outcome. The
// debounce + single-flight coalescing means `started` ≪ `coalesced` under
// burst load — a rising coalesced/started ratio is the signal that the
// coalescing is doing its job (many edits, few rebuilds).
var graphRefreshTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gocode_graph_refresh_total",
		Help: "Background AGE-graph refreshes triggered by the file watcher, by outcome (started, coalesced, success, failed, skipped).",
	},
	[]string{"outcome"},
)

func init() {
	// Pre-touch every series so /metrics exports them from a cold start,
	// mirroring codeGraphBuildFailures's init().
	for _, o := range []string{
		graphRefreshOutcomeStarted, graphRefreshOutcomeCoalesced,
		graphRefreshOutcomeSuccess, graphRefreshOutcomeFailed,
		graphRefreshOutcomeSkipped,
	} {
		graphRefreshTotal.WithLabelValues(o).Add(0)
	}
}

// graphRefresher debounces + single-flights background codegraph.IndexRepo
// calls triggered by the file watcher. It closes issue #642: IndexFile
// refreshed only code_embeddings, leaving the AGE graph (call edges, PageRank,
// communities) stale after a watched edit — silently degrading half the
// on-by-default RRF weight (graph 0.25 + hotspot 0.15 + recency 0.1) plus
// understand / call_trace / impact_analysis.
//
// Properties (see the issue requirements):
//  1. Debounced + coalesced: N edits within the debounce window → ONE refresh.
//  2. Single-flight per repoKey: concurrent triggers for the same repo collapse
//     into one in-flight refresh (golang.org/x/sync/singleflight.Group.DoChan),
//     reusing the #678 cold-clone pattern. Distinct repoKeys refresh
//     independently — no false serialization.
//  3. Never blocks the caller: Trigger returns immediately; the refresh runs in
//     the time.AfterFunc goroutine.
//  4. Failure is loud: recordCodeGraphBuildFailure + WARN on error; a skipped
//     refresh (disabled / no store) bumps the skipped counter.
//  5. Staleness is observable: recordCodeGraphAge sets the per-repo age gauge
//     after each successful refresh.
//  6. Kill switch: GRAPH_REFRESH_ON_WATCH (default ENABLED) disables the
//     background refresh; the debounce + single-flight keep an enabled
//     refresher from stampeding the shared 4-core box.
//
// Stale-drop: IndexRepo's own checkCache (internal/codegraph/index_helpers.go)
// detects the content-hash mismatch a watched edit produces and drops+rebuilds
// the graph, so fire does NOT manually DropGraph — reusing the existing cache
// invalidation rather than duplicating it.
type graphRefresher struct {
	store    *codegraph.Store
	indexCfg codegraph.IndexConfig
	enabled  bool
	debounce time.Duration
	indexFn  graphRefreshIndexFn // test seam; defaults to codegraph.IndexRepo

	mu     sync.Mutex
	timers map[string]*time.Timer // repoKey → pending debounce timer

	refreshGroup singleflight.Group
}

// newGraphRefresher constructs a graphRefresher from the AGE store + index
// config. store may be nil (DATABASE_URL unset): Trigger then skips with a
// counter (the watcher still refreshes embeddings). The kill switch
// GRAPH_REFRESH_ON_WATCH defaults to ENABLED; setting it false disables the
// background refresh and logs a WARN so the operator knows the graph will go
// stale on watched edits.
func newGraphRefresher(store *codegraph.Store, cfg codegraph.IndexConfig) *graphRefresher {
	enabled := env.Bool("GRAPH_REFRESH_ON_WATCH", true)
	debounce := time.Duration(env.Int("GRAPH_REFRESH_DEBOUNCE_MS",
		int(defaultGraphRefreshDebounce/time.Millisecond))) * time.Millisecond
	if debounce <= 0 {
		debounce = defaultGraphRefreshDebounce
	}
	r := &graphRefresher{
		store:    store,
		indexCfg: cfg,
		enabled:  enabled,
		debounce: debounce,
		indexFn:  graphRefreshIndexRepo,
		timers:   make(map[string]*time.Timer),
	}
	switch {
	case !enabled:
		slog.Warn("graph refresh: disabled by kill switch (GRAPH_REFRESH_ON_WATCH=false); "+
			"watched edits will refresh embeddings but NOT the AGE graph — "+
			"graph/hotspot/recency RRF arms go stale until a manual code_graph refresh",
			slog.Duration("debounce", debounce))
	case store == nil:
		slog.Warn("graph refresh: enabled but DATABASE_URL is unset (no codegraph.Store); "+
			"watched edits refresh embeddings only — AGE graph refreshes are skipped",
			slog.Duration("debounce", debounce))
	default:
		slog.Info("graph refresh: enabled",
			slog.Duration("debounce", debounce))
	}
	return r
}

// Trigger schedules a debounced background graph refresh for repoKey/root. It
// returns immediately and never blocks the caller (the IndexFile embedding
// path stays as fast as today). N calls within the debounce window coalesce
// into one refresh; a call arriving while a debounce timer is already pending
// for repoKey is absorbed (coalesced counter bumped) and resets the timer so
// the refresh fires only after the burst settles.
func (r *graphRefresher) Trigger(repoKey, root string) {
	if !r.enabled || r.store == nil {
		graphRefreshTotal.WithLabelValues(graphRefreshOutcomeSkipped).Inc()
		return
	}

	r.mu.Lock()
	if t, ok := r.timers[repoKey]; ok {
		// A refresh is already pending for this repo — absorb this edit into
		// it and reset the timer so the fire happens after the burst settles.
		t.Stop()
		graphRefreshTotal.WithLabelValues(graphRefreshOutcomeCoalesced).Inc()
	}
	t := time.AfterFunc(r.debounce, func() {
		r.fire(repoKey, root)
	})
	r.timers[repoKey] = t
	r.mu.Unlock()
}

// fire runs after the debounce timer expires (in time.AfterFunc's own
// goroutine). It enters the per-repoKey single-flight so a fire whose refresh
// would overlap a still-running one collapses into the leader (the fn runs
// exactly once). The refresh runs under a DECOUPLED context so a caller going
// away cannot cancel it for other waiters. Blocking here is safe — this is a
// background goroutine, not the IndexFile/watcher path.
func (r *graphRefresher) fire(repoKey, root string) {
	// The timer has fired; remove the pending entry so subsequent Triggers
	// start a fresh debounce cycle.
	r.mu.Lock()
	delete(r.timers, repoKey)
	r.mu.Unlock()

	ch := r.refreshGroup.DoChan(repoKey, func() (any, error) {
		graphRefreshTotal.WithLabelValues(graphRefreshOutcomeStarted).Inc()
		ctx, cancel := context.WithTimeout(context.Background(), graphRefreshTimeout)
		defer cancel()
		// isRemote=false: the file watcher only watches local AUTO_INDEX_DIRS
		// repos (repofind.Discover on local paths), never remote clones.
		meta, err := r.indexFn(ctx, r.store, root, false, r.indexCfg)
		if err != nil {
			recordCodeGraphBuildFailure(err)
			graphRefreshTotal.WithLabelValues(graphRefreshOutcomeFailed).Inc()
			slog.Warn("graph refresh: IndexRepo failed",
				slog.String("repo", repoKey), slog.Any("error", err))
			return nil, err
		}
		if meta != nil {
			recordCodeGraphAge(repoKey, meta.BuiltAt)
		}
		graphRefreshTotal.WithLabelValues(graphRefreshOutcomeSuccess).Inc()
		slog.Info("graph refresh: IndexRepo complete", slog.String("repo", repoKey))
		return meta, nil
	})
	// Block until the refresh completes. For a follower (a fire whose DoChan
	// coalesced into a running leader) this shares the leader's result; the
	// leader's closure already recorded the outcome counters, so there is
	// nothing to do here for followers.
	<-ch
}
