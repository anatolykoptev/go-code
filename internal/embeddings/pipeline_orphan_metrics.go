package embeddings

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// gocode_index_orphans_deleted_total counts intra-key orphan embedding rows
// deleted during a full indexRepo walk. An orphan is a (file_path, symbol_name)
// row in code_embeddings for a repo_key that is no longer present in the freshly-
// parsed symbol set — i.e. a symbol was deleted, renamed, or moved between files.
//
// This counter increments only on the full-walk path (not on the same-SHA fast-
// path or incremental git-diff path, where the complete parsed set is unavailable
// and deletion would be unsafe). A non-zero rate indicates symbols are being
// cleaned up as expected on each full re-index.
//
// Cardinality: 1 series (unlabelled). repo_key cardinality is acceptable at ~100
// repos, but the repo label is omitted to keep alert rules simple; the delete
// count and the gocode_orphan_repo_keys gauge together provide enough signal.
var indexOrphansDeletedTotal = promauto.NewCounter(
	prometheus.CounterOpts{
		Name: "gocode_index_orphans_deleted_total",
		Help: "Intra-key orphan embedding rows deleted during full indexRepo walks (deleted/renamed symbols).",
	},
)

// gocode_orphan_repo_keys is a gauge recording the number of distinct repo_keys
// present in code_embeddings but absent from code_repo_state. Non-zero indicates
// stale worktree snapshots or deregistered repos whose embedding rows were not
// cleaned up. The orphan_sweep MCP tool resets this to zero.
//
// The gauge is set by the operator-initiated orphan_sweep tool, not on every
// indexRepo call (which would require an extra COUNT query per boot).
var orphanRepoKeysGauge = promauto.NewGauge(
	prometheus.GaugeOpts{
		Name: "gocode_orphan_repo_keys",
		Help: "Distinct repo_keys in code_embeddings with no matching code_repo_state row; 0 = clean.",
	},
)

// SetOrphanRepoKeysGauge sets the gocode_orphan_repo_keys gauge. Called by the
// orphan_sweep MCP tool after a sweep to report the post-sweep count.
func SetOrphanRepoKeysGauge(n float64) { orphanRepoKeysGauge.Set(n) }

// gocode_index_orphan_delete_skipped_total counts times the intra-key orphan
// delete was skipped by the shrink-guard (seen < 70% of existing rows). A non-zero
// rate indicates a partial parse was detected and mass-deletion was avoided.
// reason="shrink_guard" fires on a partial-parse skip; reason="error" fires on
// a delete error.
var orphanDeleteSkippedTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gocode_index_orphan_delete_skipped_total",
		Help: "Times the intra-key orphan delete was skipped (e.g. shrink_guard: seen < 70%% of existing).",
	},
	[]string{"reason"},
)

// gocode_index_shrink_guard_consecutive_skips is a per-repo gauge recording
// how many CONSECUTIVE times the shrink-guard has fired for a repo's
// deleteIntraKeyOrphans call. It increments on each skip and resets to 0 on
// any non-skip outcome (successful delete, delete error, or no orphans).
//
// A perpetually-rising gauge means a stuck index — the ratchet state where
// orphans exceed ~30% of the table, seen < 0.7*existing is permanently true,
// and the guard fires every run without the index ever self-healing. The
// companion orphanDeleteSkippedTotal counter records total skips but cannot
// distinguish "skipped 3 times in a row" from "skipped 3 times over 100 runs"
// — this gauge closes that gap.
//
// This mirrors the consecFails pattern from go-kit's CircuitBreaker
// (consecutive-failure counter that resets on success) but as a simple per-repo
// gauge — the existing idiom in this package (stalePathRatioGauge,
// embeddingsCoverageRows).
//
// Cardinality: 1 label (repo) — bounded by indexed repo count (~100).
var shrinkGuardConsecutiveSkips = promauto.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "gocode_index_shrink_guard_consecutive_skips",
		Help: "Consecutive shrink-guard skips per repo (deleteIntraKeyOrphans). A rising gauge = stuck index; 0 = healthy or just-recovered.",
	},
	[]string{"repo"},
)

// gocode_index_embeddings_coverage_rows is a gauge set after each full indexRepo
// walk to the current embedding row count for that repo_key. A sudden drop signals
// a half-empty index (e.g. from a previous bug or unexpected delete).
//
// Cardinality: 1 label (repo) — bounded by indexed repo count (~100).
var embeddingsCoverageRows = promauto.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "gocode_index_embeddings_coverage_rows",
		Help: "Current embedding row count for a repo_key after a full indexRepo walk.",
	},
	[]string{"repo"},
)

// SetEmbeddingsCoverageRows sets the gocode_index_embeddings_coverage_rows gauge.
// Called after a successful full-walk orphan reconciliation.
func SetEmbeddingsCoverageRows(repoKey string, n int) {
	embeddingsCoverageRows.WithLabelValues(repoKey).Set(float64(n))
}

// gocode_orphan_sweep_category_keys is a per-category gauge recording the
// number of distinct repo_keys the orphan_sweep tool identified in each
// category during its last run. Categories:
//   - embeddings_not_in_state: code_embeddings rows whose repo_key has no
//     code_repo_state row (the original sweep; also reported by
//     gocode_orphan_repo_keys for backward compatibility).
//   - path_missing: code_repo_state rows whose source_path is non-empty but
//     the directory is gone from disk — the repo was deleted via
//     ReleaseCloneRef/CleanupCloneDir without a state-row cleanup.
//   - pathless: code_repo_state rows with an empty source_path —
//     unidentifiable tombstones whose original root is gone.
//
// Pre-touched to 0 for all three categories at boot so a zero reads as
// "measured zero" and not "never wired" — a gauge that only appears when
// non-zero cannot be alerted on with Prometheus absent().
//
// Cardinality: 1 label (category) — bounded to 3 values.
var orphanSweepCategoryKeys = promauto.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "gocode_orphan_sweep_category_keys",
		Help: "Distinct repo_keys per orphan_sweep category (embeddings_not_in_state, path_missing, pathless); 0 = clean or not run.",
	},
	[]string{"category"},
)

// SetOrphanSweepCategoryKeys sets the gocode_orphan_sweep_category_keys gauge
// for one category. Called by the orphan_sweep handler after each category's
// preview or delete pass.
func SetOrphanSweepCategoryKeys(category string, n float64) {
	orphanSweepCategoryKeys.WithLabelValues(category).Set(n)
}

// WarmOrphanSweepCategoryKeys pre-touches the gauge to 0 for all three
// categories at boot so the series exists before any sweep — a gauge that
// only appears when non-zero cannot be alerted on with absent().
func WarmOrphanSweepCategoryKeys() {
	for _, c := range []string{"embeddings_not_in_state", "path_missing", "pathless"} {
		orphanSweepCategoryKeys.WithLabelValues(c).Set(0)
	}
}

// gocode_orphan_prevented_total counts first-index embedding rows rolled back
// via a compensating DeleteRepo after a repo_state write failure (or embedChunks
// partial failure). Each increment is one orphan averted: without the compensate,
// the just-written embeddings would persist with no code_repo_state row — the
// dominant orphan source (15076 swept historically).
//
// A non-zero rate means the retry+compensate fix is actively preventing orphans;
// pair with embed_repo_state_write_failures_total to see how often the retry
// alone was insufficient.
//
// Cardinality: 1 series (unlabelled).
var orphanPreventedTotal = promauto.NewCounter(
	prometheus.CounterOpts{
		Name: "gocode_orphan_prevented_total",
		Help: "First-index embedding rows rolled back via compensating DeleteRepo after a repo_state write or embedChunks failure (orphan prevented).",
	},
)
