package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// capabilityGauge is the single, unified capability-status surface. Every
// feature that can be silently disabled by missing/malformed config publishes
// its enabled state here as a 0/1 gauge labelled by capability name, so the
// full set of dead retrieval arms / features is scrapeable from /metrics in
// one place instead of being scattered across boot logs and per-feature
// metrics.
//
// This complements (does not replace) the per-feature observability metrics
// added in #634 (gocode_sparse_embedder_active, gocode_learnings_db_fallback,
// gocode_github_auth_mode) and the startup WARNs from #630: those remain as
// legacy surfaces; gocode_capability_enabled is the canonical one-row-per-
// capability view an operator alerts on.
//
// Pre-touched at init() so every known series exists from a cold start
// (mirrors gocode_graph_refresh_total in graph_refresher.go) — a Prometheus
// increase/rate over a series that materialises only on first Set has nothing
// to subtract from, so a capability disabled for the whole process lifetime
// must still export a 0 sample.
var capabilityGauge = promauto.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "gocode_capability_enabled",
		Help: "1 when a capability is enabled, 0 when disabled by missing or malformed config (env var / URL / DSN). Labelled by capability name. Pre-touched at init so every series exists from a cold start.",
	},
	[]string{"capability"},
)

// knownCapabilities is the canonical list of capabilities whose enabled state
// is published on gocode_capability_enabled. Adding a capability here makes its
// series exist from boot; the corresponding setCapability call at the wiring
// site flips it to 1 when the feature is actually on.
//
// Keep this list in sync with the setCapability call sites — a capability
// listed here but never set to 1 is always 0 (correctly "disabled"); a
// capability set to 1 but not listed here still works but its series
// materialises only on first enable (defeating the cold-start pre-touch).
var knownCapabilities = []string{
	"semantic_search",  // EMBED_URL + DATABASE_URL (#600)
	"sparse_retrieval", // SPARSE_EMBED_URL (#602)
	"learnings_store",  // LEARNINGS_DATABASE_URL / DATABASE_URL (#601)
	"github_app_auth",  // GITHUB_APP_ID + INSTALLATION_ID + KEY_PATH (#603)
	"code_graph",       // DATABASE_URL + Apache AGE
	"find_duplicates",  // DATABASE_URL + EMBED_URL
	"sparse_backfill",  // SPARSE_EMBED_URL + DATABASE_URL
	"design_search",    // DESIGN_EMBED_URL + DATABASE_URL
	"resolve_frame",    // SOURCEMAP_ALLOWED_HOSTS
	"dataflow_analyze", // OX_CODES_URL
	"web_search",       // GO_SEARCH_URL
	"llm",              // LLM_API_KEY
	"graph_signals",    // DATABASE_URL (PageRank/community/cross-refs enrichment)
}

func init() {
	// Pre-touch every series so /metrics exports them from a cold start.
	for _, c := range knownCapabilities {
		capabilityGauge.WithLabelValues(c).Set(0)
	}
}

// setCapability publishes the enabled state of a capability on the unified
// gauge. Called from each subsystem's wiring site (newSemanticDeps,
// wireSparse, buildLearningsStore, loadGithubAppConfig, registerTools) once
// the enable/disable decision is resolved.
func setCapability(name string, enabled bool) {
	if enabled {
		capabilityGauge.WithLabelValues(name).Set(1)
	} else {
		capabilityGauge.WithLabelValues(name).Set(0)
	}
}
