package main

import (
	"log/slog"

	"github.com/anatolykoptev/go-kit/sparse"
	"github.com/anatolykoptev/vaelor/internal/embeddings"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// --- Learnings DB fallback observability (#594, #610 gap 3) -----------------
//
// LEARNINGS_DATABASE_URL silently falls back to DATABASE_URL when unset
// (config.go loadConfig), co-locating review_learnings with the main DB. The
// operator cannot tell whether learnings are on a dedicated store, falling back
// to the main DB, or disabled entirely. This gauge makes the state visible.

// gocode_learnings_db_fallback:
//
//	0 = dedicated (LEARNINGS_DATABASE_URL explicitly set)
//	1 = fallback  (LEARNINGS_DATABASE_URL unset, falls back to DATABASE_URL)
//	2 = disabled  (neither set → learnings store nil)
var learningsDBFallbackGauge = promauto.NewGauge(
	prometheus.GaugeOpts{
		Name: "gocode_learnings_db_fallback",
		Help: "Learnings DB state: 0=dedicated (LEARNINGS_DATABASE_URL set), 1=fallback to DATABASE_URL, 2=disabled (#594).",
	},
)

// learningsFallbackState classifies the learnings DB wiring from the raw env
// values into the gauge value (0/1/2). Pure so it is unit-testable in isolation.
func learningsFallbackState(learningsURL, databaseURL string) float64 {
	switch {
	case learningsURL != "":
		return 0 // dedicated
	case databaseURL != "":
		return 1 // fallback
	default:
		return 2 // disabled
	}
}

// publishLearningsDBFallback sets the gauge from the raw env values. Called from
// loadConfig after LearningsDSN is resolved.
func publishLearningsDBFallback(learningsURL, databaseURL string) {
	learningsDBFallbackGauge.Set(learningsFallbackState(learningsURL, databaseURL))
}

// --- Sparse embedder active observability (#602, #610 gap 6) ----------------
//
// SPARSE_EMBED_URL misconfiguration (empty, unreachable, or vocab-size mismatch
// in embeddings.newSparseEmbedder) silently disables the sparse retrieval arm —
// the pipeline stays dense-only with no operator signal. This gauge publishes 1
// when a sparse embedder is wired into the pipeline, 0 when nil.
var sparseEmbedderActiveGauge = promauto.NewGauge(
	prometheus.GaugeOpts{
		Name: "gocode_sparse_embedder_active",
		Help: "1 when the SPLADE sparse embedder is wired into the indexing pipeline, 0 when disabled (SPARSE_EMBED_URL empty or misconfigured, #602).",
	},
)

// wireSparse constructs the sparse embedder + pipeline opts from cfg, mirroring
// the inline wiring that lived in newSemanticDeps. Extracted to a single site so
// the active-state gauge is published from the real production path. Returns
// (nil, nil-opts) when SPARSE_EMBED_URL is empty — byte-identical to the prior
// inline behaviour (dense-only cold-path).
func wireSparse(cfg Config, rrfWeights embeddings.RRFWeights) (sparse.SparseEmbedder, []embeddings.PipelineOpt) {
	var sparseClient sparse.SparseEmbedder
	var pipelineOpts []embeddings.PipelineOpt
	if sc := newSparseEmbedder(cfg); sc != nil {
		sparseClient = sc
		pipelineOpts = append(pipelineOpts, embeddings.WithSparseEmbedder(sc))
		pipelineOpts = append(pipelineOpts, embeddings.WithSparseMaxBatch(cfg.SparseEmbedMaxArray))
		slog.Info("sparse embed: enabled (P4 dark-launch: rrf_weight_sparse=0.0 until A/B)",
			slog.String("url", cfg.SparseEmbedURL),
			slog.String("model", cfg.SparseEmbedModel),
			slog.Int("max_array", cfg.SparseEmbedMaxArray),
			slog.Float64("rrf_weight_sparse", rrfWeights.Sparse))
	}
	if sparseClient != nil {
		sparseEmbedderActiveGauge.Set(1)
		setCapability("sparse_retrieval", true)
	} else {
		sparseEmbedderActiveGauge.Set(0)
		setCapability("sparse_retrieval", false)
		// Preserve the #602 warning (from the config-warnings work): if the
		// sparse arm is off but a positive RRF sparse weight is configured,
		// the operator has a silently-inert knob — say so.
		warnSparseDisabled(cfg, rrfWeights)
	}
	return sparseClient, pipelineOpts
}
