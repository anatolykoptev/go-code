package embeddings

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// gocode_index_stale_path_ratio is a per-repo gauge recording the share of
// indexed embedding rows whose file_path does not resolve under the repo's
// root. Updated at index time by ReconcileRepoPaths. Pre-touched to 0.0 at
// boot for every known repo (WarmStalePathRatioGauge) so "no data" and "zero
// divergence" are distinguishable — a gauge that only appears when non-zero
// cannot be alerted on with Prometheus absent().
//
// The 14% divergence from #708 went unnoticed for weeks because no metric
// exposed it; this gauge makes it visible so an alert can catch the next
// rename-induced contamination before a human stumbles on it.
//
// Cardinality: 1 label (repo) — bounded by indexed repo count (~100).
//
// IMPORTANT: this gauge is ONLY set on a MEASURED pass (root exists, source_path
// non-empty). Skip branches (root_missing, source_path_empty) do NOT touch it —
// a false 0.0 would hide the problem. The companion gocode_index_stale_path_unmeasured
// gauge distinguishes "could not measure" from "measured, clean" (#708 round 2
// finding 1).
var stalePathRatioGauge = promauto.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "gocode_index_stale_path_ratio",
		Help: "Share of a repo's indexed embedding rows whose file_path does not resolve under the repo root (0.0 = clean). Updated at index time ONLY when the ratio was actually measured; see gocode_index_stale_path_unmeasured for skip branches.",
	},
	[]string{"repo"},
)

// SetStalePathRatioGauge sets the gocode_index_stale_path_ratio gauge for one
// repo. Called by ReconcileRepoPaths after each MEASURED reconciliation pass.
// Skip branches must NOT call this — they set stalePathUnmeasuredGauge instead.
func SetStalePathRatioGauge(repoKey string, ratio float64) {
	stalePathRatioGauge.WithLabelValues(repoKey).Set(ratio)
}

// WarmStalePathRatioGauge pre-touches the gauge to 0.0 for every known repo
// at boot so the series exists before any divergence — a gauge that only
// appears when non-zero cannot be alerted on with absent().
func WarmStalePathRatioGauge(repoKeys []string) {
	for _, repo := range repoKeys {
		stalePathRatioGauge.WithLabelValues(repo).Set(0)
	}
}

// gocode_index_stale_path_unmeasured is a companion to
// gocode_index_stale_path_ratio. It is 1 when the stale-path ratio could NOT
// be measured (root_missing or source_path_empty skip branch) and 0 when it
// was measured. The reason label carries the skip reason ("root_missing",
// "source_path_empty", or "none" for a measured pass).
//
// Without this gauge, "could not measure" is indistinguishable from "measured,
// clean" — the ratio gauge reads 0.0 in both cases (warmed to 0 at boot, never
// overwritten on a skip). 54 of 131 live keys (16.2% of rows) report a false
// 0.0; one is KNOWN contaminated (#708 round 2 finding 1).
//
// Cardinality: 2 labels (repo, reason) — reason is bounded to 3 values, repo
// bounded by indexed repo count (~100).
var stalePathUnmeasuredGauge = promauto.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "gocode_index_stale_path_unmeasured",
		Help: "1 when the stale-path ratio could not be measured (root missing or empty source_path); 0 when measured. The reason label carries the skip reason.",
	},
	[]string{"repo", "reason"},
)

// SetStalePathUnmeasuredGauge sets the gocode_index_stale_path_unmeasured
// gauge for one repo. reason is "root_missing", "source_path_empty", or
// "none" (measured pass). value is 1 on a skip, 0 on a measured pass.
func SetStalePathUnmeasuredGauge(repoKey, reason string, value float64) {
	stalePathUnmeasuredGauge.WithLabelValues(repoKey, reason).Set(value)
}

// WarmStalePathUnmeasuredGauge pre-touches the gauge to 0 for every known repo
// at boot (reason="none") so the series exists and is alertable — a gauge that
// only appears when non-zero cannot be alerted on with absent().
func WarmStalePathUnmeasuredGauge(repoKeys []string) {
	for _, repo := range repoKeys {
		stalePathUnmeasuredGauge.WithLabelValues(repo, "none").Set(0)
	}
}

// stalePathWarnThreshold is the miss-rate fraction above which the
// reconciliation logs a WARN with the numbers. 0.10 = 10%; the 14%
// divergence from #708 would have triggered it. The delete still proceeds —
// the root-missing guard is the only hard stop. A WARN is the signal that a
// human should look, not a gate that blocks cleanup.
const stalePathWarnThreshold = 0.10

// PathCount is one distinct file_path under a repo_key with its row count.
type PathCount struct {
	Path  string
	Count int64
}

// RepoKeySourcePath pairs a repo_key with its source_path from code_repo_state.
type RepoKeySourcePath struct {
	RepoKey    string
	SourcePath string
}

// ReconcileResult is the outcome of one repo's path-existence reconciliation.
type ReconcileResult struct {
	RepoKey    string
	SourcePath string
	TotalRows  int64  // sum of all row counts for this repo_key
	StalePaths int64  // distinct file_paths that do not resolve under root
	StaleRows  int64  // rows under stale paths
	Deleted    int64  // rows actually deleted (0 in dry-run)
	Skipped    bool   // true when reconciliation was aborted (root missing or source_path empty)
	SkipReason string // "root_missing" | "source_path_empty" | ""
	DryRun     bool
}

// ListFilePathCounts returns each distinct file_path for repoKey with its
// row count. Used by ReconcileRepoPaths to enumerate the on-disk existence
// check set. A single GROUP BY query on the (repo_key) index.
func (s *Store) ListFilePathCounts(ctx context.Context, repoKey string) ([]PathCount, error) {
	if err := s.EnsureSchema(ctx); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx,
		`SELECT file_path, COUNT(*) FROM public.code_embeddings
		 WHERE repo_key = $1 GROUP BY file_path`, repoKey)
	if err != nil {
		return nil, fmt.Errorf("ListFilePathCounts: %w", err)
	}
	defer rows.Close()

	var result []PathCount
	for rows.Next() {
		var pc PathCount
		if err := rows.Scan(&pc.Path, &pc.Count); err != nil {
			return nil, fmt.Errorf("ListFilePathCounts: scan: %w", err)
		}
		result = append(result, pc)
	}
	return result, rows.Err()
}

// DeleteRowsByFilePaths deletes all code_embeddings rows for repoKey whose
// file_path is in paths. Returns rows-affected. Uses file_path = ANY($2) so
// one statement handles the full set (the file_path set per repo is bounded
// by the repo's file count, well within the 65535-param ceiling).
func (s *Store) DeleteRowsByFilePaths(ctx context.Context, repoKey string, paths []string) (int64, error) {
	if len(paths) == 0 {
		return 0, nil
	}
	if err := s.EnsureSchema(ctx); err != nil {
		return 0, err
	}
	ct, err := s.pool.Exec(ctx,
		`DELETE FROM public.code_embeddings WHERE repo_key = $1 AND file_path = ANY($2::text[])`,
		repoKey, paths)
	if err != nil {
		return 0, fmt.Errorf("DeleteRowsByFilePaths: %w", err)
	}
	return ct.RowsAffected(), nil
}

// ListRepoKeysWithSourcePath returns every (repo_key, source_path) from
// code_repo_state where source_path is non-empty. Used by the reconcile_paths
// MCP tool to iterate all reconcilable repos. Keys with an empty source_path
// (pathless tombstones — 13.1% of the corpus per #708) are excluded: they
// are unreachable by root-based reconciliation and are a separate dispatch.
func (s *Store) ListRepoKeysWithSourcePath(ctx context.Context) ([]RepoKeySourcePath, error) {
	if err := s.EnsureSchema(ctx); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx,
		`SELECT repo_key, source_path FROM public.code_repo_state WHERE source_path <> ''`)
	if err != nil {
		return nil, fmt.Errorf("ListRepoKeysWithSourcePath: %w", err)
	}
	defer rows.Close()

	var result []RepoKeySourcePath
	for rows.Next() {
		var r RepoKeySourcePath
		if err := rows.Scan(&r.RepoKey, &r.SourcePath); err != nil {
			return nil, fmt.Errorf("ListRepoKeysWithSourcePath: scan: %w", err)
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// CheckStalePaths returns the file_paths in counts that do NOT resolve under
// root, plus the total row count under those paths. If root itself does not
// exist (os.Stat fails), returns ok=false — the caller MUST delete nothing.
// This is the data-loss guard: a mount blip or transient NFS hang must not
// wipe a good index.
//
// Pure function (no DB) so the guard is unit-testable without a pool.
// Exported so the reconcile_paths MCP tool handler (cmd/vaelor) can share the
// exact same guard logic as the index-pass reconciliation without duplicating
// it.
func CheckStalePaths(root string, counts []PathCount) (stale []PathCount, totalRows int64, ok bool) {
	// ROOT-MISSING GUARD — the most important line in this file.
	// If the root itself does not exist OR is not a directory, we cannot trust
	// any per-file stat (every file would appear "stale" under a missing or
	// file-replaced root). Delete nothing. Requiring IsDir catches the case
	// where a root path is replaced by a regular file (e.g. a checkout path
	// reused as a file) — os.Stat succeeds on a file, but every file_path
	// joined under it would be "stale", causing a whole-key wipe (#708 round 2
	// finding 4).
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil, 0, false
	}
	for _, pc := range counts {
		full := filepath.Join(root, pc.Path)
		if _, err := os.Stat(full); err != nil {
			stale = append(stale, pc)
			totalRows += pc.Count
		}
	}
	return stale, totalRows, true
}

// ReconcileRepoPaths deletes (or previews) code_embeddings rows for repoKey
// whose file_path does not resolve under sourcePath.
//
// SAFETY GUARD — root existence: if sourcePath itself does not exist on disk
// (os.Stat fails), the function deletes NOTHING and returns Skipped=true with
// SkipReason="root_missing". A mount blip or transient NFS hang must not wipe
// a good index — this is the data-loss guard.
//
// Empty sourcePath: skips with SkipReason="source_path_empty" — pathless keys
// (13.1% of the corpus per #708) are unreachable by root-based reconciliation
// and are a separate dispatch.
//
// dryRun=true: counts and reports, deletes nothing.
// dryRun=false: deletes stale-path rows, then reports.
//
// The gauge gocode_index_stale_path_ratio{repo} is set to staleRows/totalRows
// (0.0 when totalRows==0). When the miss rate exceeds stalePathWarnThreshold,
// a WARN is logged with the numbers — the 14% divergence from #708 would have
// triggered it.
func (s *Store) ReconcileRepoPaths(ctx context.Context, repoKey, sourcePath string, dryRun bool) (*ReconcileResult, error) {
	res := &ReconcileResult{
		RepoKey:    repoKey,
		SourcePath: sourcePath,
		DryRun:     dryRun,
	}

	// Empty source_path — pathless tombstone, skip (separate dispatch per #708).
	if sourcePath == "" {
		res.Skipped = true
		res.SkipReason = "source_path_empty"
		slog.Info("reconcile_paths: skipped (empty source_path)",
			slog.String("repo", repoKey))
		// Do NOT set the ratio gauge — a false 0.0 would hide the problem.
		// Mark unmeasured so the companion gauge distinguishes this from clean.
		SetStalePathUnmeasuredGauge(repoKey, "source_path_empty", 1)
		return res, nil
	}

	counts, err := s.ListFilePathCounts(ctx, repoKey)
	if err != nil {
		return nil, fmt.Errorf("reconcile_paths: list file paths: %w", err)
	}

	for _, pc := range counts {
		res.TotalRows += pc.Count
	}

	stale, staleRows, ok := CheckStalePaths(sourcePath, counts)
	if !ok {
		// ROOT-MISSING GUARD: delete nothing, report the skip.
		res.Skipped = true
		res.SkipReason = "root_missing"
		slog.Warn("reconcile_paths: root missing — deleting nothing (data-loss guard)",
			slog.String("repo", repoKey),
			slog.String("source_path", sourcePath),
			slog.Int64("total_rows", res.TotalRows))
		// Do NOT set the ratio gauge — a false 0.0 would hide the problem.
		// Mark unmeasured so the companion gauge distinguishes this from clean.
		SetStalePathUnmeasuredGauge(repoKey, "root_missing", 1)
		return res, nil
	}

	res.StalePaths = int64(len(stale))
	res.StaleRows = staleRows

	// Set the gauge: staleRows / totalRows (0.0 when totalRows == 0).
	ratio := 0.0
	if res.TotalRows > 0 {
		ratio = float64(res.StaleRows) / float64(res.TotalRows)
	}
	SetStalePathRatioGauge(repoKey, ratio)
	// Measured pass — clear the unmeasured flag.
	SetStalePathUnmeasuredGauge(repoKey, "none", 0)

	// Loud log when the miss rate exceeds the threshold — a 14% divergence
	// went unnoticed for weeks (#708); this WARN makes it visible.
	if ratio > stalePathWarnThreshold {
		slog.Warn("reconcile_paths: high stale-path ratio",
			slog.String("repo", repoKey),
			slog.String("source_path", sourcePath),
			slog.Int64("total_rows", res.TotalRows),
			slog.Int64("stale_paths", res.StalePaths),
			slog.Int64("stale_rows", res.StaleRows),
			slog.Float64("ratio", ratio))
	}

	if dryRun {
		slog.Info("reconcile_paths: dry-run preview",
			slog.String("repo", repoKey),
			slog.Int64("stale_paths", res.StalePaths),
			slog.Int64("stale_rows", res.StaleRows))
		return res, nil
	}

	// Real delete.
	var pathsToDelete []string
	for _, pc := range stale {
		pathsToDelete = append(pathsToDelete, pc.Path)
	}
	deleted, err := s.DeleteRowsByFilePaths(ctx, repoKey, pathsToDelete)
	if err != nil {
		return nil, fmt.Errorf("reconcile_paths: delete: %w", err)
	}
	res.Deleted = deleted

	slog.Info("reconcile_paths: complete",
		slog.String("repo", repoKey),
		slog.Int64("stale_paths", res.StalePaths),
		slog.Int64("rows_deleted", deleted))
	return res, nil
}
