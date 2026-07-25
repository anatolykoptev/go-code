package embeddings

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// IncrementalSyncMode is the typed label for which code path IncrementalSync took.
// Values are stable wire strings used in Prometheus labels — do not rename.
type IncrementalSyncMode string

const (
	// IncrementalSyncIncremental is the normal git-diff path.
	IncrementalSyncIncremental IncrementalSyncMode = "incremental"
	// IncrementalSyncSkipSHAMatch is the fast-path when SHA is unchanged.
	IncrementalSyncSkipSHAMatch IncrementalSyncMode = "skip-sha-match"
	// IncrementalSyncFullFallbackBootstrap is triggered when no prior SHA exists in DB.
	IncrementalSyncFullFallbackBootstrap IncrementalSyncMode = "full-fallback-bootstrap"
	// IncrementalSyncFullFallbackNoGit is triggered when the path is not a git repo.
	IncrementalSyncFullFallbackNoGit IncrementalSyncMode = "full-fallback-no-git"
	// IncrementalSyncFullFallbackDiffError is triggered when git diff exec fails.
	IncrementalSyncFullFallbackDiffError IncrementalSyncMode = "full-fallback-diff-error"
)

// IncrementalSyncResult is the return value of Pipeline.IncrementalSync.
type IncrementalSyncResult struct {
	// Mode describes which code path was taken.
	Mode IncrementalSyncMode

	PrevSHA    string
	CurrentSHA string

	// FilesChanged is the count of files returned by git diff (0 on full-fallback paths).
	FilesChanged int
	// FilesEmbedded is the sum of FileIndexResult.Embedded across processed files
	// (or IndexResult.Indexed on full-fallback paths).
	FilesEmbedded int
	// FilesSkipped is the sum of FileIndexResult.Skipped (hash-matched symbols).
	FilesSkipped int
	// FilesDeleted is the sum of FileIndexResult.Deleted (tombstoned symbols).
	FilesDeleted int64

	// Errors collects per-file errors. When non-empty, SHA is NOT advanced so the
	// next boot retries the affected files (hash-skip from IndexFile makes retries cheap).
	Errors []error
}

// IncrementalSync reconciles a repo's embedding index via git-diff against the
// last-indexed SHA, calling Pipeline.IndexFile per changed file. Bumps
// SetRepoState only on full success (Errors empty).
//
// Falls back to full IndexRepo when:
//   - currentSHA == ""         (non-git path)
//   - prevSHA == ""            (never indexed — bootstrap)
//   - git diff exec failure    (corrupted state — safer to full re-scan)
//
// Same-SHA path (prevSHA == currentSHA) bumps indexed_at timestamp via
// SetRepoState but does no file work. Returns mode="skip-sha-match".
//
// Returns non-nil result describing the path taken. Returns error only on
// catastrophic failures (DB connection lost). Per-file errors are collected
// in Result.Errors, not returned — caller decides retry policy.
func (p *Pipeline) IncrementalSync(ctx context.Context, repoKey, root string) (*IncrementalSyncResult, error) {
	// Register the (repo key → path) mapping for metric resolution.
	// Called here so the info gauge is populated before any other gocode_*
	// metric for this repo is emitted. Idempotent on repeated calls.
	SetRepoInfoGauge(repoKey, root)

	// Step 1: resolve current main-branch SHA.
	// Treat both a real error (e.g. git repo with no main/master/HEAD ref) and an
	// empty return (non-git path) identically: we have no usable fingerprint, so
	// bulk fallback is the safe path. IndexRepo handles non-git via its own walk.
	// This ensures outcome="full-fallback-no-git" is always recorded by
	// fallbackToFull → recordIncrementalSync; the error branch previously bypassed
	// recordIncrementalSync entirely (code-quality MAJOR finding).
	currentSHA, _ := repoMainBranchSHA(root)
	if currentSHA == "" {
		// No fingerprint available — fall through to full index.
		res, fullErr := p.fallbackToFull(ctx, repoKey, root, IncrementalSyncFullFallbackNoGit)
		recordIncrementalSync(res, fullErr)
		// No git repo → commits-behind is not meaningful; skip gauge.
		return res, fullErr
	}

	// Step 2: fetch previous SHA. Swallow the error (treat "" on failure).
	prevSHA, _ := p.store.GetRepoState(ctx, repoKey)
	if prevSHA == "" {
		// Never indexed — bootstrap.
		res, fullErr := p.fallbackToFull(ctx, repoKey, root, IncrementalSyncFullFallbackBootstrap)
		recordIncrementalSync(res, fullErr)
		recordCommitsBehind(ctx, repoKey, root, p.store, currentSHA)
		return res, fullErr
	}

	// Step 3: same-SHA fast path — bump timestamp only, but only when the store
	// has ≥1 embedding row. A same-SHA with 0 rows is the Bug #1 frozen-empty
	// desync state: fall through to full re-index instead of skipping.
	if prevSHA == currentSHA {
		embCount, countErr := p.store.CountEmbeddings(ctx, repoKey)
		if countErr != nil {
			slog.Warn("incrementalSync: CountEmbeddings failed; falling through to full re-index",
				slog.String("repo", repoKey), slog.Any("error", countErr))
			// Fall through to full re-index (safe).
		} else if embCount > 0 {
			// Healthy: SHA unchanged AND rows present → safe to skip.
			// #720: run the dry-run path reconciliation through the SAME shared
			// seam as checkSameSHAFastPath (IndexRepo). The boot autoindex calls
			// IncrementalSync, not IndexRepo, so without this call 72/77 repos
			// skip without ever reconciling or backfilling a pathless source_path
			// (the v1.59.12 regression). The seam (runFastPathDryRunReconcile)
			// owns the reconcile + backfill calls so they cannot drift between
			// the two same-SHA paths. Dry-run deletes nothing.
			p.runFastPathDryRunReconcile(ctx, repoKey, root)
			if err := p.writeRepoState(ctx, repoKey, currentSHA, root); err != nil {
				recordRepoStateWriteFailure(repoKey, "incrementalSync:same-sha", err)
			}
			SetEmbeddingsPresentGauge(repoKey, embCount)
			res := &IncrementalSyncResult{
				Mode:       IncrementalSyncSkipSHAMatch,
				PrevSHA:    prevSHA,
				CurrentSHA: currentSHA,
			}
			recordIncrementalSync(res, nil)
			recordCommitsBehind(ctx, repoKey, root, p.store, currentSHA)
			return res, nil
		}
		// 0 embeddings despite same SHA — frozen-empty (Bug #1 recovery).
		// The incremental diff path (git diff prevSHA..currentSHA) would return
		// no changed files (same SHA → empty diff) and advance the SHA again with
		// 0 rows. Route to fallbackToFull so IndexRepo detects the desync and
		// re-embeds all symbols.
		//
		// Gate the "operator-investigation-required" counter on the repo actually having
		// embeddable source files: a docs-only repo (e.g. /host/src/wiki) legitimately
		// has 0 embeddings and must not trip this alarm on every boot.
		if rootHasEmbeddableFiles(root) {
			repoStateAdvancedWithZeroEmbeddingsTotal.WithLabelValues(repoKey).Inc()
		}
		slog.Warn("incrementalSync: same SHA but 0 embeddings — routing to full re-index for recovery",
			slog.String("repo", repoKey), slog.String("sha", shortSHA(currentSHA)))
		res, fullErr := p.fallbackToFull(ctx, repoKey, root, IncrementalSyncFullFallbackBootstrap)
		recordIncrementalSync(res, fullErr)
		recordCommitsBehind(ctx, repoKey, root, p.store, currentSHA)
		return res, fullErr
	}

	// Step 4: compute diff.
	changedFiles, diffErr := gitDiffNames(ctx, root, prevSHA, currentSHA)
	if diffErr != nil {
		slog.Debug("incrementalSync: git diff failed, falling back to full",
			slog.String("repo", repoKey),
			slog.String("prev", prevSHA),
			slog.String("current", currentSHA),
			slog.Any("error", diffErr))
		res, fullErr := p.fallbackToFull(ctx, repoKey, root, IncrementalSyncFullFallbackDiffError)
		recordIncrementalSync(res, fullErr)
		recordCommitsBehind(ctx, repoKey, root, p.store, currentSHA)
		return res, fullErr
	}

	// Step 5-7: index each changed file, collect errors.
	result := &IncrementalSyncResult{
		Mode:       IncrementalSyncIncremental,
		PrevSHA:    prevSHA,
		CurrentSHA: currentSHA,
	}

	for _, relPath := range changedFiles {
		result.FilesChanged++
		fileStart := time.Now()
		fr, fileErr := p.IndexFile(ctx, repoKey, root, relPath)
		elapsed := time.Since(fileStart)
		if fileErr != nil {
			indexFileDuration.WithLabelValues("error").Observe(elapsed.Seconds())
			result.Errors = append(result.Errors, fmt.Errorf("%s: %w", relPath, fileErr))
			// Continue — don't abort the batch. SHA will not advance if errors remain.
			continue
		}
		indexFileDuration.WithLabelValues("success").Observe(elapsed.Seconds())
		result.FilesEmbedded += fr.Embedded
		result.FilesSkipped += fr.Skipped
		result.FilesDeleted += fr.Deleted
	}

	// Record per-file kind counters.
	if result.FilesEmbedded > 0 {
		incrementalFilesTotal.WithLabelValues("embedded").Add(float64(result.FilesEmbedded))
	}
	if result.FilesSkipped > 0 {
		incrementalFilesTotal.WithLabelValues("skipped").Add(float64(result.FilesSkipped))
	}
	if result.FilesDeleted > 0 {
		incrementalFilesTotal.WithLabelValues("deleted").Add(float64(result.FilesDeleted))
	}

	// Step 7: advance SHA only on full success.
	if len(result.Errors) == 0 {
		if err := p.writeRepoState(ctx, repoKey, currentSHA, root); err != nil {
			recordRepoStateWriteFailure(repoKey, "incrementalSync:sha-advance", err)
		}
	}

	recordIncrementalSync(result, nil)
	recordCommitsBehind(ctx, repoKey, root, p.store, currentSHA)
	return result, nil
}

// fallbackToFull runs IndexRepo and maps its result to an IncrementalSyncResult.
// mode is the typed label recorded in result.Mode.
func (p *Pipeline) fallbackToFull(ctx context.Context, repoKey, root string, mode IncrementalSyncMode) (*IncrementalSyncResult, error) {
	full, err := p.IndexRepo(ctx, repoKey, root)
	if err != nil {
		return &IncrementalSyncResult{Mode: mode}, err
	}
	return &IncrementalSyncResult{
		Mode:          mode,
		FilesEmbedded: full.Indexed,
		FilesSkipped:  full.Skipped,
	}, nil
}

// gitDiffNames returns the list of files changed between prevSHA and currentSHA
// in the git repo at root. Returns an error if the git command fails.
// Stderr from git is captured and included in the error so operators can debug
// "bad object" / "ambiguous argument" / "unknown revision" failures.
func gitDiffNames(ctx context.Context, root, prevSHA, currentSHA string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "diff", "--name-only", prevSHA, currentSHA)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff %s..%s: %w (stderr: %s)",
			shortSHA(prevSHA), shortSHA(currentSHA),
			err, strings.TrimSpace(stderr.String()))
	}

	var files []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}
