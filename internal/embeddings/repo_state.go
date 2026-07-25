package embeddings

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// repoMainBranchSHA returns the sha of the repo's main branch (main → master →
// HEAD fallback). Used as a per-repo fingerprint to skip indexing when the
// branch hasn't moved since the last successful run.
//
// Why main-branch and not HEAD: developers on feature branches should not
// trigger reindexing of every repo on every container restart; only commits
// merged to main legitimately invalidate the cached embedding set.
//
// Returns ("", nil) when the path is not a git repo (e.g. tarball checkout) —
// callers should treat this as "no fingerprint, fall through to full index".
func repoMainBranchSHA(root string) (string, error) {
	for _, ref := range []string{"main", "master", "HEAD"} {
		out, err := exec.Command("git", "-C", root, "rev-parse", ref).Output()
		if err == nil {
			sha := strings.TrimSpace(string(out))
			if sha != "" {
				return sha, nil
			}
		}
	}
	// Path is probably not a git repo — distinguish from real errors.
	if _, err := exec.Command("git", "-C", root, "rev-parse", "--git-dir").Output(); err != nil {
		return "", nil
	}
	return "", errors.New("repo: no main/master/HEAD ref found")
}

// GetRepoState returns the last-indexed sha for a repo, or "" when no row exists.
func (s *Store) GetRepoState(ctx context.Context, repoKey string) (string, error) {
	if err := s.EnsureSchema(ctx); err != nil {
		return "", err
	}
	var sha string
	err := s.pool.QueryRow(ctx,
		`SELECT head_sha FROM public.code_repo_state WHERE repo_key = $1`, repoKey).
		Scan(&sha)
	if err != nil {
		// pgx returns ErrNoRows on empty — caller treats as "first index".
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return sha, nil
}

// GetRepoSourcePath returns the source_path recorded in code_repo_state for
// repoKey, or "" when no row exists or on any error. Used by the index-pass
// reconciliation hook to resolve the root from STATE rather than the caller's
// root argument — so a pathless key (empty source_path) takes the
// source_path_empty skip branch instead of being reconciled against an
// unrecorded root (#714).
//
// Mirrors GetRepoState's error-collapsing convention: a missing row returns
// ("", nil) so the caller treats it as "pathless / first index" and lets
// ReconcileRepoPaths take the skip branch. A real lookup error returns ("",
// err) so the caller can log and skip.
func (s *Store) GetRepoSourcePath(ctx context.Context, repoKey string) (string, error) {
	if err := s.EnsureSchema(ctx); err != nil {
		return "", err
	}
	var sourcePath string
	err := s.pool.QueryRow(ctx,
		`SELECT source_path FROM public.code_repo_state WHERE repo_key = $1`, repoKey).
		Scan(&sourcePath)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return sourcePath, nil
}

// RepoStateExists reports whether a code_repo_state row exists for repoKey.
// GetRepoState collapses "no row", "row with empty head_sha" (non-git repo)
// and "lookup error" all to "" — none distinguishable by the caller. The
// compensating-delete path MUST distinguish a genuine first index (no row —
// rollback is safe) from a re-index or an uncertain lookup (deleting a live
// repo's embeddings would be data loss), so it needs this explicit probe.
func (s *Store) RepoStateExists(ctx context.Context, repoKey string) (bool, error) {
	if err := s.EnsureSchema(ctx); err != nil {
		return false, err
	}
	var exists bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM public.code_repo_state WHERE repo_key = $1)`, repoKey).
		Scan(&exists)
	return exists, err
}

// GetIndexedAt returns the indexed_at timestamp for a repo, or zero time when no
// row exists (repo never indexed). Used by stale-demote to determine the
// current index generation: any embedding row with updated_at < indexed_at
// was not re-touched this generation and is a candidate orphan.
//
// Returns zero time on any error (schema init failure, no rows) so callers can
// treat zero as "generation unknown → skip demote" safely.
func (s *Store) GetIndexedAt(ctx context.Context, repoKey string) time.Time {
	if err := s.EnsureSchema(ctx); err != nil {
		return time.Time{}
	}
	var ts time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT indexed_at FROM public.code_repo_state WHERE repo_key = $1`, repoKey).
		Scan(&ts)
	if err != nil {
		return time.Time{}
	}
	return ts
}

// SetRepoState records the sha and the active embedding model that was successfully indexed.
// model is stored alongside head_sha so that a model switch triggers a full reindex
// on next startup even when the repo's git SHA has not changed.
func (s *Store) SetRepoState(ctx context.Context, repoKey, sha, model string) error {
	return s.SetRepoStateWithPath(ctx, repoKey, sha, model, "")
}

// SetRepoStateWithPath is the source-path-aware variant of SetRepoState.
// sourcePath is the absolute filesystem path of the repo root, stored so
// RecentRepoKeys can return human-readable display names instead of opaque
// hashed repo_keys (#582). Empty sourcePath preserves backward compatibility
// (the column defaults to ”).
func (s *Store) SetRepoStateWithPath(ctx context.Context, repoKey, sha, model, sourcePath string) error {
	if err := s.EnsureSchema(ctx); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO public.code_repo_state (repo_key, head_sha, indexed_at, embed_model, source_path)
		 VALUES ($1, $2, NOW(), $3, $4)
		 ON CONFLICT (repo_key) DO UPDATE
		     SET head_sha = EXCLUDED.head_sha,
		         indexed_at = NOW(),
		         embed_model = EXCLUDED.embed_model,
		         source_path = EXCLUDED.source_path`,
		repoKey, sha, model, sourcePath)
	return err
}

// ListRepoKeys returns every repo_key present in code_repo_state — the set of
// repos go-code has indexed at least once. Used at boot to pre-touch
// gocode_repo_state_advanced_with_zero_embeddings_total{repo} for known repos
// (see WarmRepoStateAdvancedZeroEmbeddings) so the counter series exists
// before any new bad event, not just after it.
func (s *Store) ListRepoKeys(ctx context.Context) ([]string, error) {
	if err := s.EnsureSchema(ctx); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT repo_key FROM public.code_repo_state`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var key string
		if scanErr := rows.Scan(&key); scanErr != nil {
			return nil, scanErr
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

// RecentRepoKeys returns up to limit repo display names ordered by most
// recently indexed (indexed_at DESC). Used by the short missing-repo error
// (issue #569) to name a few actionable candidate repos instead of dumping the
// whole catalog. Returns the source_path basename when available (human-
// readable, e.g. "vaelor" instead of "code_a3f2b1c0"), falling back to the
// hashed repo_key when source_path is empty (pre-#582 rows). Errors collapse
// to an empty slice so the caller can fall back to LocalRepoDirs.
func (s *Store) RecentRepoKeys(ctx context.Context, limit int) []string {
	if s == nil || limit <= 0 {
		return nil
	}
	if err := s.EnsureSchema(ctx); err != nil {
		return nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT source_path, repo_key FROM public.code_repo_state
		 ORDER BY indexed_at DESC
		 LIMIT $1`, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var sourcePath, repoKey string
		if scanErr := rows.Scan(&sourcePath, &repoKey); scanErr != nil {
			return nil
		}
		if display := repoDisplayName(sourcePath); display != "" {
			keys = append(keys, display)
		} else {
			keys = append(keys, repoKey)
		}
	}
	return keys
}

// repoDisplayName returns the basename of sourcePath, or "" when sourcePath is
// empty. Used by RecentRepoKeys to surface human-readable repo names.
func repoDisplayName(sourcePath string) string {
	if sourcePath == "" {
		return ""
	}
	// Use the basename — the full path is host-specific and not actionable
	// for the agent, but the basename (e.g. "vaelor", "go-mcpserver") is.
	sourcePath = strings.TrimRight(sourcePath, "/")
	if i := strings.LastIndex(sourcePath, "/"); i >= 0 {
		return sourcePath[i+1:]
	}
	return sourcePath
}

// GetStoredModel returns the embed_model stored in code_repo_state for repoKey,
// or "" when no row exists or on any error. Used by semantic_search to detect
// stale-space hits at query time: results returned from a repo whose stored
// model differs from the active model are in the wrong embedding space and must
// be discarded, triggering a full reindex.
//
// This is a cheap single-row SELECT; the common case (model matches) adds one
// round-trip to the search path, which is negligible next to the vector scan.
func (s *Store) GetStoredModel(ctx context.Context, repoKey string) string {
	if err := s.EnsureSchema(ctx); err != nil {
		return ""
	}
	var model string
	err := s.pool.QueryRow(ctx,
		`SELECT embed_model FROM public.code_repo_state WHERE repo_key = $1`, repoKey).
		Scan(&model)
	if err != nil {
		return ""
	}
	return model
}

// InvalidateRepoIfModelChanged purges code_embeddings for repoKey and resets
// its head_sha to "" when the stored embed_model differs from activeModel.
// This forces a full reindex on next query, producing vectors in the new
// embedding space. No-ops when the stored model matches.
//
// The purge is atomic per repo: DELETE code_embeddings + DELETE code_repo_state
// row both run inside a single transaction so semantic_search never sees a
// half-purged state (either all old vectors remain or none do).
//
// Returns (true, nil) when a purge was performed, (false, nil) when not needed.
func (s *Store) InvalidateRepoIfModelChanged(ctx context.Context, repoKey, activeModel string) (bool, error) {
	if err := s.EnsureSchema(ctx); err != nil {
		return false, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var storedModel string
	err = tx.QueryRow(ctx,
		`SELECT embed_model FROM public.code_repo_state WHERE repo_key = $1`,
		repoKey).Scan(&storedModel)
	if err != nil {
		// No row → no embeddings to purge; first index will write the correct model.
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	if storedModel == activeModel {
		_ = tx.Rollback(ctx)
		return false, nil
	}
	// Model mismatch → purge all embeddings for this repo and reset state.
	// Roll back the read-only probe transaction before delegating to WipeRepo,
	// which opens its own write transaction (nested transactions are not used
	// here to keep the dual-table DELETE seam single-owner).
	_ = tx.Rollback(ctx)
	if err := s.WipeRepo(ctx, repoKey); err != nil {
		return false, err
	}
	return true, nil
}
