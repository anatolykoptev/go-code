package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/anatolykoptev/vaelor/internal/codegraph"
	"github.com/anatolykoptev/vaelor/internal/embeddings"
	"github.com/anatolykoptev/vaelor/internal/ingest"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Issue #691: the RRF retrieval path fuses graph (0.25) + hotspot (0.15) — 40%
// of fused weight — from the AGE graph with NO freshness check, silently
// blending an arbitrarily stale graph into every search result. This file
// implements three sub-changes, split by risk:
//
//   A. Self-heal (default ON): when the retrieval path observes a stale graph,
//      trigger the SAME deduplicated background rebuild the tool gate uses
//      (triggerBackgroundGraphBuild). Fire-and-forget — search never blocks.
//   B. Observability (default ON): a counter for stale-graph retrievals + a
//      gauge for the age actually used, plus a GraphStaleAgeS marker on the
//      response envelope so a caller can tell it received degraded ranking.
//   C. Drop-and-renormalise (default OFF — dark): when the graph is stale
//      beyond the threshold, drop the graph + hotspot arms and renormalise
//      the remaining RRF weights. Behind RRF_DROP_STALE_GRAPH_ARMS (default
//      false), mirroring the existing dark-launch convention (rrf_weight_sparse
//      = 0.0 until A/B).

// defaultGraphStalenessThresholdS is the default staleness threshold for the
// retrieval path. A graph older than this is considered stale and triggers
// self-heal + the degradation marker.
//
// 30 minutes (1800s) is chosen because:
//   - It is well below the measured prod staleness (18.3h / 65813s) — the bug
//     is caught instantly.
//   - It is below GRAPH_TTL_LOCAL (3600s) and far below GRAPH_TTL_REMOTE
//     (86400s), so the retrieval path is stricter than the tool gate — the
//     intended behaviour per #691 ("staleness then self-corrects under search
//     traffic").
//   - The self-heal is deduplicated (buildingRepos sync.Map), so a 30-min
//     threshold does not stampede the shared 4-core box — at most one rebuild
//     per repo per 30 min.
//   - 30 min is short enough that a graph going stale during a coding session
//     self-corrects within the same session, not the next day.
//
// Override via RRF_GRAPH_STALENESS_THRESHOLD_S env.
const defaultGraphStalenessThresholdS = 1800

// Stale-graph retrieval outcome labels for staleGraphRetrievals.
const (
	staleGraphOutcomeFused   = "fused"   // stale graph fused into results (flag OFF)
	staleGraphOutcomeDropped = "dropped" // graph+hotspot arms dropped (flag ON)
)

// staleGraphRetrievals counts searches that observed a stale AGE graph,
// labelled by outcome (fused = stale hits still blended, dropped = arms
// zeroed + renormalised). Pre-touched at 0 so /metrics exports both series
// from a cold start.
var staleGraphRetrievals = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gocode_stale_graph_retrievals_total",
		Help: "semantic_search queries that observed a stale AGE graph (age > threshold), by outcome (fused, dropped).",
	},
	[]string{"outcome"},
)

// staleGraphRetrievalAgeSeconds records the AGE graph age (seconds) at the
// time of a stale-graph retrieval. Set alongside staleGraphRetrievals so an
// operator can correlate how stale the graph was with each degraded search.
var staleGraphRetrievalAgeSeconds = promauto.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "gocode_stale_graph_retrieval_age_seconds",
		Help: "Age of the AGE graph (seconds) at the time of a stale-graph retrieval. Set alongside gocode_stale_graph_retrievals_total.",
	},
	[]string{"repo"},
)

func init() {
	staleGraphRetrievals.WithLabelValues(staleGraphOutcomeFused).Add(0)
	staleGraphRetrievals.WithLabelValues(staleGraphOutcomeDropped).Add(0)
}

// graphAgeFn is the test seam for codegraph.GraphAge. Production uses the real
// function; tests swap this to inject a fake age without a live DB connection.
// Mirrors the ageGraphCacheStatus / ageGraphIndexRepo seam pattern in
// age_graph_gate.go.
var graphAgeFn = codegraph.GraphAge

// checkRetrievalGraphFreshness checks whether the AGE graph for root is fresh
// enough for the retrieval path. Returns (stale=false, 0) when the graph is
// fresh or when no freshness check is possible (no store, no graph arms
// active). Returns (stale=true, age) when the graph age exceeds the staleness
// threshold.
//
// The age check uses codegraph.GraphAge which is cached for graphAgeCacheTTL
// (5s) — a burst of semantic_search queries within the TTL doesn't cause a
// DB round-trip per query. The added hot-path cost on a cache hit is one map
// lookup + mutex acquire (sub-microsecond).
func checkRetrievalGraphFreshness(
	ctx context.Context,
	store *codegraph.Store,
	root string,
	threshold time.Duration,
) (stale bool, age time.Duration) {
	if store == nil || threshold <= 0 {
		return false, 0
	}
	graphAge, fresh, err := graphAgeFn(ctx, store, root)
	if err != nil {
		// On error, treat as not stale — search must NEVER fail because of the
		// freshness check. The error is logged but not surfaced to the caller.
		slog.Debug("retrieval freshness: GraphAge check failed, treating as fresh",
			slog.String("repo", root), slog.Any("error", err))
		return false, 0
	}
	if graphAge == 0 {
		// No graph exists yet — not stale (nothing to fuse), not fresh.
		return false, 0
	}
	if fresh {
		return false, graphAge
	}
	// Graph is temporally stale per its TTL. Check against the retrieval
	// threshold — the threshold may be stricter than the TTL.
	if graphAge > threshold {
		return true, graphAge
	}
	return false, graphAge
}

// dropStaleGraphArms zeroes the graph and hotspot weights and renormalises
// the remaining weights so their sum is preserved. This changes ranking only
// when RRF_DROP_STALE_GRAPH_ARMS=true (dark, default off).
//
// Renormalisation: the remaining weights (Semantic, Keyword, Sparse, Recency)
// are scaled by originalSum / remainingSum so the total fused weight is
// unchanged. This is a ranking no-op (WeightedRRF preserves ordering under
// uniform scaling) but keeps the weight sum observable for A/B comparison.
func dropStaleGraphArms(w embeddings.RRFWeights) embeddings.RRFWeights {
	originalSum := w.Semantic + w.Keyword + w.Sparse + w.Graph + w.Hotspot + w.Recency
	remainingSum := w.Semantic + w.Keyword + w.Sparse + w.Recency
	w.Graph = 0
	w.Hotspot = 0
	if remainingSum <= 0 || originalSum <= 0 {
		return w
	}
	scale := originalSum / remainingSum
	w.Semantic *= scale
	w.Keyword *= scale
	w.Sparse *= scale
	w.Recency *= scale
	return w
}

// gateRetrievalGraphFreshness is the integration point called by
// handleSemanticHits. It checks freshness, and when stale:
//   - bumps the stale-graph-retrievals counter (by outcome)
//   - sets the age gauge
//   - triggers a fire-and-forget dedup background rebuild (self-heal)
//   - when dropStaleArms is true, zeroes graph+hotspot weights in-place
//
// Returns the graph age in seconds (0 when fresh) for the response envelope
// marker. Modifies *weights in-place when dropStaleArms is true.
//
// Search never blocks and never fails because of this gate — the self-heal is
// fire-and-forget, and the freshness check error is swallowed (treated as
// fresh).
func gateRetrievalGraphFreshness(
	ctx context.Context,
	store *codegraph.Store,
	root, repoKey string,
	isRemote bool,
	indexCfg codegraph.IndexConfig,
	threshold time.Duration,
	dropStaleArms bool,
	weights *embeddings.RRFWeights,
) float64 {
	stale, age := checkRetrievalGraphFreshness(ctx, store, root, threshold)
	if !stale {
		return 0
	}
	ageS := age.Seconds()

	// B. Observability: count + age gauge.
	staleGraphRetrievalAgeSeconds.WithLabelValues(repoKey).Set(ageS)

	// A. Self-heal: fire-and-forget dedup background rebuild.
	triggerBackgroundGraphBuild("semantic_search", store, root, repoKey, isRemote, indexCfg)

	// C. Drop-and-renormalise (dark, default off).
	if dropStaleArms {
		*weights = dropStaleGraphArms(*weights)
		staleGraphRetrievals.WithLabelValues(staleGraphOutcomeDropped).Inc()
	} else {
		staleGraphRetrievals.WithLabelValues(staleGraphOutcomeFused).Inc()
	}

	return ageS
}

// retrievalIsRemote determines isRemote for the retrieval self-heal. Mirrors
// the tool gate's use of ingest.IsRemote(input.Repo) — the retrieval path
// needs the same TTL selection (remote repos get a longer TTL).
func retrievalIsRemote(repoInput string) bool {
	return ingest.IsRemote(repoInput)
}
