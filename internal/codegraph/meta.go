package codegraph

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/anatolykoptev/vaelor/internal/ingest"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// getMeta retrieves the stored GraphMeta for repoKey, or returns nil if none exists.
func getMeta(ctx context.Context, store *Store, repoKey string) (*GraphMeta, error) {
	conn, err := store.acquireAGE(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	var m GraphMeta
	err = conn.QueryRow(ctx, `
		SELECT repo_key, repo_path, graph_name,
		       file_count, symbol_count, edge_count,
		       built_at, ttl_seconds, content_hash
		FROM code_graph_meta
		WHERE repo_key = $1`, repoKey,
	).Scan(
		&m.RepoKey, &m.RepoPath, &m.GraphName,
		&m.FileCount, &m.SymbolCount, &m.EdgeCount,
		&m.BuiltAt, &m.TTLSeconds, &m.ContentHash,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		// Tolerate schema-not-yet-migrated errors as cache misses so IndexRepo
		// falls through to EnsureGraph (which runs the ALTER) and self-heals:
		//   42P01 = undefined_table: code_graph_meta does not exist yet.
		//   42703 = undefined_column: content_hash missing on a table created
		//           by the OLD schema (#592 review BLOCKER). The SELECT now
		//           references content_hash, so a pre-migration prod table
		//           fails at PLAN time with 42703 BEFORE EnsureGraph runs the
		//           ALTER — without this tolerance the read errors, IndexRepo
		//           returns before the migration, and every AGE-graph tool is
		//           permanently broken post-deploy (CI can't catch it: a fresh
		//           DB already has the column).
		if isSchemaNotReady(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("query meta: %w", err)
	}
	return &m, nil
}

// ListMeta returns every stored GraphMeta row — one per repo that has ever
// completed a successful build. Used at boot to seed
// gocode_code_graph_age_seconds with the REAL age of each repo's last build
// (see cmd/go-code's publishCodeGraphAgeGauge) instead of leaving the series
// absent — and therefore invisible to GocodeCodeGraphStale — until the next
// build cycle completes.
//
// Returns (nil, nil) when code_graph_meta does not exist yet, matching
// getMeta's cold-path treatment: an uninitialised schema is "no repos known
// yet", not an error.
func ListMeta(ctx context.Context, store *Store) ([]GraphMeta, error) {
	conn, err := store.acquireAGE(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	rows, err := conn.Query(ctx, `
		SELECT repo_key, repo_path, graph_name,
		       file_count, symbol_count, edge_count,
		       built_at, ttl_seconds, content_hash
		FROM code_graph_meta`)
	if err != nil {
		// Tolerate schema-not-yet-migrated errors as "no known repos",
		// matching getMeta's cold-path handling (see getMeta for the full
		// 42703 rationale — the same pre-migration BLOCKER applies here).
		if isSchemaNotReady(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("query meta: %w", err)
	}
	defer rows.Close()

	var metas []GraphMeta
	for rows.Next() {
		var m GraphMeta
		if scanErr := rows.Scan(
			&m.RepoKey, &m.RepoPath, &m.GraphName,
			&m.FileCount, &m.SymbolCount, &m.EdgeCount,
			&m.BuiltAt, &m.TTLSeconds, &m.ContentHash,
		); scanErr != nil {
			return nil, fmt.Errorf("scan meta row: %w", scanErr)
		}
		metas = append(metas, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate meta rows: %w", err)
	}
	return metas, nil
}

// upsertMeta inserts or updates the GraphMeta row.
func upsertMeta(ctx context.Context, store *Store, meta *GraphMeta) error {
	conn, err := store.acquireAGE(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	_, err = conn.Exec(ctx, `
		INSERT INTO code_graph_meta
		    (repo_key, repo_path, graph_name, file_count, symbol_count, edge_count, built_at, ttl_seconds, content_hash)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (repo_key) DO UPDATE SET
		    repo_path     = EXCLUDED.repo_path,
		    graph_name    = EXCLUDED.graph_name,
		    file_count    = EXCLUDED.file_count,
		    symbol_count  = EXCLUDED.symbol_count,
		    edge_count    = EXCLUDED.edge_count,
		    built_at      = EXCLUDED.built_at,
		    ttl_seconds   = EXCLUDED.ttl_seconds,
		    content_hash  = EXCLUDED.content_hash`,
		meta.RepoKey, meta.RepoPath, meta.GraphName,
		meta.FileCount, meta.SymbolCount, meta.EdgeCount,
		meta.BuiltAt, meta.TTLSeconds, meta.ContentHash,
	)
	if err != nil {
		return fmt.Errorf("upsert meta: %w", err)
	}
	return nil
}

// isFresh reports whether builtAt is within ttlSeconds of the current time.
func isFresh(builtAt time.Time, ttlSeconds int) bool {
	if ttlSeconds <= 0 {
		return false
	}
	return time.Since(builtAt) < time.Duration(ttlSeconds)*time.Second
}

// isSchemaNotReady reports whether err is a PostgreSQL "schema not yet
// initialised/migrated" error that the meta accessors should treat as a
// cache miss rather than a hard failure:
//   - 42P01 undefined_table: code_graph_meta does not exist yet.
//   - 42703 undefined_column: a column the SELECT references (content_hash)
//     is missing on a table created by an older schema version.
//
// Detection uses pgconn.PgError (the typed pgx v5 error) via errors.As, which
// unwraps pgx's layered errors correctly; a string-match fallback is kept as
// defence-in-depth for any wrapper that loses the typed error.
func isSchemaNotReady(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "42P01", "42703":
			return true
		}
	}
	// Fallback: some wrappers (e.g. logged/re-wrapped errors) may not unwrap
	// to *pgconn.PgError. Keep the original string-match for 42P01 so we
	// don't regress the pre-existing cold-path behaviour.
	return strings.Contains(err.Error(), "42P01")
}

// contentHashCacheTTL bounds how long a freshness hash is reused on the hot
// AGE-gate path (CacheStatus → RepoContentHash). A burst of tool calls within
// the TTL doesn't re-walk the tree. Kept small (5s) so a git checkout or file
// edit is detected within one tool-call burst, not minutes (#592 review MAJOR
// perf fix). This memoization is gate-path-only — ingest/callgraph caches call
// RepoContentHash directly and need a fresh hash for their own correctness.
const contentHashCacheTTL = 5 * time.Second

// contentHashMemo is the short-TTL memo for RepoContentHash on the gate path.
var contentHashMemo = &contentHashCache{}

type contentHashCache struct {
	mu      sync.Mutex
	entries map[string]contentHashMemoEntry
}

type contentHashMemoEntry struct {
	hash string
	at   time.Time
}

// get returns the memoized hash for root if still within the TTL, else ("" , false).
func (c *contentHashCache) get(root string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[root]
	if !ok || time.Since(e.at) > contentHashCacheTTL {
		return "", false
	}
	return e.hash, true
}

func (c *contentHashCache) set(root, hash string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]contentHashMemoEntry)
	}
	c.entries[root] = contentHashMemoEntry{hash: hash, at: time.Now()}
}

// resetContentHashMemo is exposed for tests that need to bypass the TTL.
func resetContentHashMemo() {
	contentHashMemo.mu.Lock()
	defer contentHashMemo.mu.Unlock()
	contentHashMemo.entries = nil
}

// graphAgeCacheTTL bounds how long a GraphAge result is reused on the
// retrieval hot path. A burst of semantic_search queries within the TTL
// doesn't re-read code_graph_meta. Kept at 5s to match contentHashCacheTTL
// — a git checkout or edit is detected within one query burst, not minutes.
const graphAgeCacheTTL = 5 * time.Second

// graphAgeMemo is the short-TTL memo for getMeta on the retrieval path.
var graphAgeMemo = &graphAgeCache{}

type graphAgeCache struct {
	mu      sync.Mutex
	entries map[string]graphAgeEntry
}

type graphAgeEntry struct {
	meta *GraphMeta
	at   time.Time
}

func (c *graphAgeCache) get(root string) (*GraphMeta, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[root]
	if !ok || time.Since(e.at) > graphAgeCacheTTL {
		return nil, false
	}
	return e.meta, true
}

func (c *graphAgeCache) set(root string, meta *GraphMeta) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]graphAgeEntry)
	}
	c.entries[root] = graphAgeEntry{meta: meta, at: time.Now()}
}

// ResetGraphAgeMemo is exposed for tests that need to bypass the TTL.
func ResetGraphAgeMemo() {
	graphAgeMemo.mu.Lock()
	defer graphAgeMemo.mu.Unlock()
	graphAgeMemo.entries = nil
}

// GraphAge returns the age of the cached graph for root and whether it is
// temporally fresh (BuiltAt within TTLSeconds). Uses a short-TTL in-memory
// cache (graphAgeCacheTTL = 5s) so a burst of retrieval-path queries doesn't
// cause a DB round-trip per query. Returns (0, false, nil) when no graph
// exists yet — the caller treats this as "no graph" (not stale, not fresh).
//
// Unlike CacheStatus, this does NOT do the content-hash tree walk — the
// retrieval path only needs temporal staleness (an 18h-old graph is stale
// regardless of content hash). The content-hash check is the tool gate's job.
func GraphAge(ctx context.Context, store *Store, root string) (age time.Duration, fresh bool, err error) {
	key := GraphNameFor(root)
	if meta, ok := graphAgeMemo.get(root); ok {
		if meta == nil {
			return 0, false, nil
		}
		return time.Since(meta.BuiltAt), isFresh(meta.BuiltAt, meta.TTLSeconds), nil
	}
	meta, err := getMeta(ctx, store, key)
	if err != nil {
		return 0, false, err
	}
	graphAgeMemo.set(root, meta)
	if meta == nil {
		return 0, false, nil
	}
	return time.Since(meta.BuiltAt), isFresh(meta.BuiltAt, meta.TTLSeconds), nil
}

// CacheStatus checks if a valid cached graph exists for root.
// Returns (true, nil) if cached and fresh, (false, nil) if not present or stale.
// Freshness requires BOTH temporal TTL and content-hash match (#592).
//
// The content-hash re-walk is memoized for contentHashCacheTTL (5s) keyed on
// root so a burst of AGE-backed tool calls (code_graph, understand,
// prepare_change, …) within the TTL doesn't re-walk the tree on every call —
// the gate path was O(1) timestamp compare before #592 and is now O(walk)
// without this memo (#592 review MAJOR perf fix).
func CacheStatus(ctx context.Context, store *Store, root string) (bool, error) {
	key := GraphNameFor(root)
	meta, err := getMeta(ctx, store, key)
	if err != nil {
		return false, err
	}
	if meta == nil || !isFresh(meta.BuiltAt, meta.TTLSeconds) {
		return false, nil
	}
	// Temporal TTL hit — validate content hash.
	if meta.ContentHash != "" {
		currentHash, ok := contentHashMemo.get(root)
		if !ok {
			currentHash = ingest.RepoContentHash(root)
			contentHashMemo.set(root, currentHash)
		}
		return currentHash == meta.ContentHash, nil
	}
	return true, nil
}
