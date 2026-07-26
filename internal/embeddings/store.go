package embeddings

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/singleflight"

	"github.com/anatolykoptev/go-kit/sparse"
	pgvector "github.com/pgvector/pgvector-go"
)

const (
	defaultTopK    = 20
	maxTopK        = 100
	dimSize        = 768   // code-rank-embed dense embedding dimension (same as jina-code-v2; no schema change)
	sparseDim      = 30522 // splade-v3-distilbert BERT-base WordPiece vocab size
	batchSize      = 50
	fieldsPerDense = 9 // repo_key, file_path, symbol_name, symbol_kind, language, start_line, body_hash, embed_model, embedding

	// sparseBatchSize is the maximum rows per single multi-row sparse UPDATE.
	//
	// The binding constraint is NOT the Postgres 65535-param ceiling (4 params/row
	// allows up to 16383 rows). The real constraint is the sparsevec text literal
	// size: each row carries up to sparseMaxTerms (256) "idx:val" pairs, ~6 bytes
	// each, yielding up to ~1.5 KB per literal. At 500 rows that is ~750 KB of
	// sparsevec text in a single statement — enough parse+write work to exceed the
	// pool's statement_timeout (live evidence: SQLSTATE 57014 on 499-row batches).
	//
	// At 100 rows: ~150 KB/statement, well within statement_timeout on any
	// Postgres statement_timeout ≥ 1 s, with comfortable headroom.
	// Raising beyond 100 requires re-measuring against the actual statement_timeout
	// and indexing load, not just the param ceiling.
	sparseBatchSize    = 100
	sparseParamsPerRow = 4 // repo_key, file_path, symbol_name, vec

	// sparseColRepoKey … sparseColVec are 1-based positional offsets within one
	// VALUES row in the multi-row sparse UPDATE. Named to avoid mnd magic-number
	// violations on the off+N arithmetic below.
	sparseColRepoKey = 1
	sparseColFile    = 2
	sparseColSymbol  = 3
	sparseColVec     = 4
)

// schemaSQL creates the pgvector extension and the two public-schema data tables.
// SR-B: all table references are schema-qualified (public.*) so they resolve
// correctly regardless of the connection's search_path — belt-and-suspenders
// alongside the SR-A pool AfterRelease hook that resets search_path on release.
//
// Symbol identity: (repo_key, file_path, symbol_name) is the canonical 3-part key.
// All three places below must be updated together on any PK migration:
//  1. PRIMARY KEY declaration here
//  2. ON CONFLICT clause in upsertBatch
//  3. WHERE join condition in updateSparseEmbeddingsBatchChunk
const schemaSQL = `CREATE EXTENSION IF NOT EXISTS vector;
CREATE TABLE IF NOT EXISTS public.code_embeddings (
    repo_key TEXT NOT NULL, file_path TEXT NOT NULL, symbol_name TEXT NOT NULL,
    symbol_kind TEXT NOT NULL, language TEXT NOT NULL DEFAULT '',
    start_line INT NOT NULL DEFAULT 0, body_hash BIGINT NOT NULL DEFAULT 0,
    embedding vector(768) NOT NULL, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (repo_key, file_path, symbol_name)); -- identity key [1/3]
CREATE INDEX IF NOT EXISTS idx_code_embeddings_repo ON public.code_embeddings (repo_key);
CREATE INDEX IF NOT EXISTS idx_code_embeddings_hnsw ON public.code_embeddings
    USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 64);
ALTER TABLE public.code_embeddings
    ADD COLUMN IF NOT EXISTS sparse_embedding sparsevec(30522);
CREATE INDEX IF NOT EXISTS code_embeddings_sparse_hnsw ON public.code_embeddings
    USING hnsw (sparse_embedding sparsevec_ip_ops);
CREATE INDEX IF NOT EXISTS idx_code_embeddings_body_hash ON public.code_embeddings
    (repo_key, body_hash) WHERE body_hash <> 0;
CREATE TABLE IF NOT EXISTS public.code_repo_state (
    repo_key TEXT PRIMARY KEY,
    head_sha TEXT NOT NULL,
    indexed_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
ALTER TABLE public.code_repo_state
    ADD COLUMN IF NOT EXISTS embed_model TEXT NOT NULL DEFAULT '';
ALTER TABLE public.code_repo_state
    ADD COLUMN IF NOT EXISTS source_path TEXT NOT NULL DEFAULT '';
ALTER TABLE public.code_embeddings
    ADD COLUMN IF NOT EXISTS embed_model TEXT NOT NULL DEFAULT ''`

// cascadeDeleteFnSQL installs the PL/pgSQL function backing the
// code_repo_state ON DELETE CASCADE trigger (#588). CREATE OR REPLACE makes it
// idempotent — re-running EnsureSchema is a no-op once the function exists.
//
// A FK with ON DELETE CASCADE is NOT used here because the embed-first write
// order (embedChunks commits code_embeddings rows BEFORE writeRepoState commits
// the code_repo_state row) would make a FK reject the first-index INSERT: at the
// moment embedChunks inserts, no parent code_repo_state row exists yet. Even a
// NOT VALID FK enforces NEW inserts immediately, so first indexing would break.
// A DEFERRABLE FK does not help either — embedChunks and writeRepoState run in
// SEPARATE transactions (per-chunk commits), not one deferred tx. The trigger
// gives the cascade guarantee (state-row delete → embeddings delete) WITHOUT
// enforcing INSERT, so the embed-first write order is unaffected. See #588 for
// the full migration-safety analysis.
const cascadeDeleteFnSQL = `CREATE OR REPLACE FUNCTION public.fn_cascade_delete_embeddings()
RETURNS TRIGGER AS $$
BEGIN
    DELETE FROM public.code_embeddings WHERE repo_key = OLD.repo_key;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql`

// cascadeTriggerSQL creates the AFTER DELETE row-level trigger on code_repo_state.
// Postgres has no CREATE TRIGGER IF NOT EXISTS, so ensureCascadeTrigger guards
// creation with a pg_trigger catalog check (and a DROP IF EXISTS belt-and-suspenders
// for a partially-created state from a prior interrupted run).
const cascadeTriggerSQL = `CREATE TRIGGER trg_code_repo_state_cascade
    AFTER DELETE ON public.code_repo_state
    FOR EACH ROW EXECUTE FUNCTION public.fn_cascade_delete_embeddings()`

// EmbeddingRecord holds a single symbol embedding for storage.
type EmbeddingRecord struct {
	RepoKey         string
	FilePath        string
	SymbolName      string
	SymbolKind      string // function, method, class, etc.
	Language        string
	StartLine       int
	BodyHash        uint64              // for change detection
	EmbedModel      string              // embedding model name (e.g. "code-rank-embed"); written to code_embeddings.embed_model
	Embedding       []float32           // dense code-rank-embed vector (768-dim)
	SparseEmbedding sparse.SparseVector // SPLADE sparse vector (30522-dim); zero value → NULL in DB
}

// SearchOpts controls search filtering and result count.
type SearchOpts struct {
	RepoKey     string  // optional filter
	Language    string  // optional filter
	TopK        int     // default 20, max 100
	MaxDistance float32 // 0 = no filter; cosine distance threshold (0.0-1.0)
}

// SearchResult is a single semantic search hit.
type SearchResult struct {
	RepoKey    string
	FilePath   string
	SymbolName string
	SymbolKind string
	Language   string
	StartLine  int
	Distance   float32   // cosine distance (lower = more similar)
	Source     string    // "semantic", "keyword", "hybrid", "graph" — set by caller
	PageRank   float32   // structural importance from graph analysis (0 if not available)
	UpdatedAt  time.Time // last upsert time — used by stale-demote safety-net
}

// Store manages vector embeddings in PostgreSQL with pgvector.
type Store struct {
	pool        *pgxpool.Pool
	schema      schemaQuerier
	schemaGroup singleflight.Group
	schemaDone  atomic.Bool
}

// NewStore creates a Store backed by the given connection pool.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool, schema: pool} }

// EnsureSchema creates the pgvector extension and embeddings table if needed.
// After creating tables it attempts to transfer ownership to CURRENT_USER
// (best-effort: warns instead of failing if the role is not the table owner).
//
// It uses a success-only latch: a transient failure is retried on the next call,
// and concurrent callers are deduplicated by singleflight. On a warm database the
// fast-path catalog checks emit zero CREATE/ALTER DDL.
func (s *Store) EnsureSchema(ctx context.Context) error {
	if s.schemaDone.Load() {
		setSchemaReady(1)
		return nil
	}
	_, err, _ := s.schemaGroup.Do("schema", func() (any, error) {
		if s.schemaDone.Load() {
			return nil, nil
		}
		start := time.Now()
		err := s.runEnsureSchema(ctx)
		if err != nil {
			slog.Error("embeddings: schema init failed", slog.Any("error", err))
			setSchemaReady(0)
			recordSchemaInit(schemaOutcome(err), time.Since(start))
			return nil, err
		}
		s.schemaDone.Store(true)
		setSchemaReady(1)
		recordSchemaInit("ok", time.Since(start))
		return nil, nil
	})
	return err
}

// Upsert stores embeddings for symbols using multi-row INSERT with ON CONFLICT.
func (s *Store) Upsert(ctx context.Context, records []EmbeddingRecord) error {
	if len(records) == 0 {
		return nil
	}
	if err := s.EnsureSchema(ctx); err != nil {
		return err
	}
	for i := 0; i < len(records); i += batchSize {
		if err := s.upsertBatch(ctx, records[i:min(i+batchSize, len(records))]); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) upsertBatch(ctx context.Context, records []EmbeddingRecord) error {
	var b strings.Builder
	// Dense-only INSERT: no sparse_embedding column here. Sparse is written via a
	// separate best-effort batch UPDATE (UpdateSparseEmbeddingsBatch) so a malformed
	// sparsevec literal cannot roll back the dense rows for the whole batch.
	// embed_model is written per-row so the stale-space guard in semantic_search can
	// detect mixed-space rows even when code_repo_state has no entry for that repo_key.
	b.WriteString(`INSERT INTO public.code_embeddings
		(repo_key,file_path,symbol_name,symbol_kind,language,start_line,body_hash,embed_model,embedding,updated_at) VALUES `)
	args := make([]any, 0, len(records)*fieldsPerDense)
	for i, r := range records {
		if i > 0 {
			b.WriteByte(',')
		}
		off := i * fieldsPerDense
		b.WriteByte('(')
		for j := 1; j <= fieldsPerDense; j++ {
			if j > 1 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, "$%d", off+j)
		}
		b.WriteString(",NOW())")
		args = append(args, r.RepoKey, r.FilePath, r.SymbolName, r.SymbolKind,
			r.Language, r.StartLine, int64(r.BodyHash), r.EmbedModel, pgvector.NewVector(r.Embedding))
	}
	b.WriteString(` ON CONFLICT (repo_key, file_path, symbol_name) DO UPDATE SET -- identity key [2/3]
		symbol_kind=EXCLUDED.symbol_kind, language=EXCLUDED.language,
		start_line=EXCLUDED.start_line, body_hash=EXCLUDED.body_hash,
		embed_model=EXCLUDED.embed_model,
		embedding=EXCLUDED.embedding,
		updated_at=NOW()`) // sparse_embedding intentionally excluded: written by UpdateSparseEmbeddingsBatch
	_, err := s.pool.Exec(ctx, b.String(), args...)
	return err
}

// SparseUpdate holds the identity and literal vector for one batch sparse write.
type SparseUpdate struct {
	RepoKey    string
	FilePath   string
	SymbolName string
	// Literal is the pre-formatted sparsevec text literal as returned by
	// SanitizeAndFormatSparseVector. Must be non-empty; callers skip empty ones.
	Literal string
}

// UpdateSparseEmbeddingsBatch writes sparse_embedding for a slice of rows in a
// single multi-row UPDATE statement, reducing round-trips from O(N) to O(1) per
// batch.
//
// The UPDATE shape:
//
//	UPDATE public.code_embeddings AS c
//	   SET sparse_embedding = v.vec::sparsevec, updated_at = NOW()
//	  FROM (VALUES ($1,$2,$3,$4::text), ...) AS v(repo_key,file_path,symbol_name,vec)
//	 WHERE c.repo_key=v.repo_key AND c.file_path=v.file_path AND c.symbol_name=v.symbol_name
//
// Batches are capped at sparseBatchSize (100) rows; callers may pass any number
// and this method splits internally. The cap is data-size-bound (sparsevec literal
// text ~1.5 KB/row × 100 = ~150 KB/statement), not param-count-bound. Non-fatal
// contract: failure leaves those rows NULL; the caller is responsible for
// logging+counting; no row is written on error.
func (s *Store) UpdateSparseEmbeddingsBatch(ctx context.Context, rows []SparseUpdate) error {
	if len(rows) == 0 {
		return nil
	}
	for i := 0; i < len(rows); i += sparseBatchSize {
		j := min(i+sparseBatchSize, len(rows))
		if err := s.updateSparseEmbeddingsBatchChunk(ctx, rows[i:j]); err != nil {
			return err
		}
	}
	return nil
}

// updateSparseEmbeddingsBatchChunk executes one multi-row UPDATE for up to
// sparseBatchSize rows. The VALUES list is built dynamically; each row occupies
// sparseParamsPerRow (4) bind parameters.
func (s *Store) updateSparseEmbeddingsBatchChunk(ctx context.Context, rows []SparseUpdate) error {
	// Build: UPDATE … FROM (VALUES ($1,$2,$3,$4::text), ($5,$6,$7,$8::text), …)
	// We cast the 4th column to ::text explicitly in VALUES so Postgres infers the
	// correct type for the VALUES row; the SET clause then casts it to sparsevec.
	var b strings.Builder
	b.WriteString(`UPDATE public.code_embeddings AS c
	SET sparse_embedding = v.vec::sparsevec, updated_at = NOW()
	FROM (VALUES `)
	args := make([]any, 0, len(rows)*sparseParamsPerRow)
	for i, r := range rows {
		if i > 0 {
			b.WriteByte(',')
		}
		off := i * sparseParamsPerRow
		// ($N,$N+1,$N+2,$N+3::text) — the ::text cast on the vec column avoids
		// "could not determine data type of parameter" on the VALUES subquery.
		fmt.Fprintf(&b, "($%d,$%d,$%d,$%d::text)",
			off+sparseColRepoKey, off+sparseColFile, off+sparseColSymbol, off+sparseColVec)
		args = append(args, r.RepoKey, r.FilePath, r.SymbolName, r.Literal)
	}
	b.WriteString(`) AS v(repo_key,file_path,symbol_name,vec)
	WHERE c.repo_key=v.repo_key AND c.file_path=v.file_path AND c.symbol_name=v.symbol_name`) // identity key [3/3]
	_, err := s.pool.Exec(ctx, b.String(), args...)
	return err
}

// Search finds the top-K most similar symbols to the query embedding using cosine distance.
func (s *Store) Search(ctx context.Context, query []float32, opts SearchOpts) ([]SearchResult, error) {
	if err := s.EnsureSchema(ctx); err != nil {
		return nil, err
	}
	topK := max(min(opts.TopK, maxTopK), 1)
	if opts.TopK <= 0 {
		topK = defaultTopK
	}
	var where []string
	args := []any{pgvector.NewVector(query), topK}
	if opts.RepoKey != "" {
		where = append(where, fmt.Sprintf("repo_key=$%d", len(args)+1))
		args = append(args, opts.RepoKey)
	}
	if opts.Language != "" {
		where = append(where, fmt.Sprintf("language=$%d", len(args)+1))
		args = append(args, opts.Language)
	}
	if opts.MaxDistance > 0 {
		where = append(where, fmt.Sprintf("embedding <=> $1 < $%d", len(args)+1))
		args = append(args, opts.MaxDistance)
	}
	q := `SELECT repo_key,file_path,symbol_name,symbol_kind,language,start_line,
		embedding <=> $1 AS distance,updated_at FROM public.code_embeddings`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY distance LIMIT $2"
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("embeddings search: %w", err)
	}
	defer rows.Close()
	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.RepoKey, &r.FilePath, &r.SymbolName,
			&r.SymbolKind, &r.Language, &r.StartLine, &r.Distance, &r.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan result: %w", err)
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// DeleteRepo removes all embeddings for a given repo_key.
func (s *Store) DeleteRepo(ctx context.Context, repoKey string) error {
	if err := s.EnsureSchema(ctx); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, "DELETE FROM public.code_embeddings WHERE repo_key=$1", repoKey)
	return err
}

// SymbolIdentity holds the identity fields for a single indexed symbol.
// Used by incremental update paths (Pipeline.IndexFile) to compute diffs:
// symbols present in DB but absent from new parse → DELETE;
// symbols whose BodyHash changed → re-embed.
type SymbolIdentity struct {
	SymbolName string
	BodyHash   uint64 // matches EmbeddingRecord.BodyHash (BIGINT in DB)
	StartLine  int
}

// GetSymbolsForFile returns the symbol identity rows currently indexed for a
// specific file in a repo. Used by incremental update paths (Pipeline.IndexFile)
// to compute the diff: symbols present in DB but no longer in the file source
// must be DELETEd; symbols whose BodyHash changed must be re-embedded.
//
// Returns rows sorted by symbol_name for determinism.
func (s *Store) GetSymbolsForFile(ctx context.Context, repoKey, filePath string) ([]SymbolIdentity, error) {
	if err := s.EnsureSchema(ctx); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx,
		`SELECT symbol_name, body_hash, start_line
		 FROM public.code_embeddings
		 WHERE repo_key = $1 AND file_path = $2
		 ORDER BY symbol_name`,
		repoKey, filePath)
	if err != nil {
		return nil, fmt.Errorf("get symbols for file: %w", err)
	}
	defer rows.Close()

	var result []SymbolIdentity
	for rows.Next() {
		var si SymbolIdentity
		var bodyHash int64
		if err := rows.Scan(&si.SymbolName, &bodyHash, &si.StartLine); err != nil {
			return nil, fmt.Errorf("scan symbol identity: %w", err)
		}
		si.BodyHash = uint64(bodyHash)
		result = append(result, si)
	}
	return result, rows.Err()
}

// DeleteSymbolsForFile removes index rows for symbols in (repoKey, filePath)
// EXCEPT those whose names appear in keepSymbolNames. Used to reconcile a file
// after re-parsing: symbols missing from the new parse (deleted / renamed)
// must be evicted. Returns rows-affected count.
//
// If keepSymbolNames is empty, all symbols for the file are deleted (use this
// for IndexFile when the file is removed from the source tree).
func (s *Store) DeleteSymbolsForFile(ctx context.Context, repoKey, filePath string, keepSymbolNames []string) (int64, error) {
	if err := s.EnsureSchema(ctx); err != nil {
		return 0, err
	}

	if len(keepSymbolNames) == 0 {
		ct, err := s.pool.Exec(ctx,
			`DELETE FROM public.code_embeddings WHERE repo_key = $1 AND file_path = $2`,
			repoKey, filePath)
		if err != nil {
			return 0, fmt.Errorf("delete symbols for file: %w", err)
		}
		return ct.RowsAffected(), nil
	}
	ct, err := s.pool.Exec(ctx,
		`DELETE FROM public.code_embeddings
		 WHERE repo_key = $1 AND file_path = $2
		   AND symbol_name != ALL($3::text[])`,
		repoKey, filePath, keepSymbolNames)
	if err != nil {
		return 0, fmt.Errorf("delete symbols for file: %w", err)
	}
	return ct.RowsAffected(), nil
}

// intraKeyOrphanChunkSize is the maximum number of explicit orphan (file_path,
// symbol_name) pairs sent in one positive IN (VALUES ...) DELETE chunk. Each row
// occupies 2 params; Postgres's 65535-param ceiling allows up to 32767 rows per
// statement, but we cap much lower to keep parse+plan work bounded and avoid
// statement_timeout on large repos. The same lesson as sparseBatchSize (#201):
// data-size-bound, not param-bound.
const intraKeyOrphanChunkSize = 500

// DeleteExplicitOrphans removes code_embeddings rows for repoKey whose
// (file_path, symbol_name) appears in the supplied orphanKeys slice.
//
// orphanKeys is the EXPLICIT set of keys to delete -- i.e. (DB keys) minus
// (freshly-parsed keys). The caller is responsible for computing this set.
// Deletion targets only the rows in orphanKeys; no other rows are touched.
//
// When orphanKeys is empty, no rows are deleted (no-op).
//
// Deletion is chunked by intraKeyOrphanChunkSize to bound per-statement size
// and avoid statement_timeout on large repos. Each chunk issues one
// DELETE WHERE (file_path,symbol_name) IN (VALUES ...) -- a positive IN-list.
// Chunking a positive IN is correct: each chunk only targets its own slice of
// orphans, so no valid row is ever collateral.
//
// Returns the total rows deleted across all chunks.
func (s *Store) DeleteExplicitOrphans(ctx context.Context, repoKey string, orphanKeys []string) (int64, error) {
	if err := s.EnsureSchema(ctx); err != nil {
		return 0, err
	}
	if len(orphanKeys) == 0 {
		return 0, nil
	}

	// Flatten orphanKeys into parallel file/sym slices for VALUES binding.
	// Key format: file_path + symKeySep + symbol_name (same as GetHashes / filterSymbols).
	// NUL (\x00) is used as separator so file paths containing colons (legal on Unix)
	// or symbol names containing "::" (C++) are split correctly.
	files := make([]string, 0, len(orphanKeys))
	syms := make([]string, 0, len(orphanKeys))
	for _, key := range orphanKeys {
		file, sym, ok := strings.Cut(key, symKeySep)
		if !ok {
			continue // malformed key -- skip
		}
		files = append(files, file)
		syms = append(syms, sym)
	}

	var totalDeleted int64
	for start := 0; start < len(files); start += intraKeyOrphanChunkSize {
		end := min(start+intraKeyOrphanChunkSize, len(files))
		n, err := s.deleteExplicitOrphanChunk(ctx, repoKey, files[start:end], syms[start:end])
		if err != nil {
			return totalDeleted, err
		}
		totalDeleted += n
	}
	return totalDeleted, nil
}

// deleteExplicitOrphanChunk issues one positive-IN DELETE for a single chunk of
// up to intraKeyOrphanChunkSize explicit orphan (file_path, symbol_name) pairs.
//
// Unlike the old NOT-IN anti-join (which deleted everything NOT in the chunk,
// causing cross-chunk data loss when total rows > intraKeyOrphanChunkSize),
// this DELETE targets ONLY the rows whose keys appear in the supplied slice.
// Chunking is safe: each chunk deletes a disjoint subset of the full orphan set;
// no valid row is ever collateral.
//
// SQL shape:
//
//	DELETE FROM public.code_embeddings
//	WHERE repo_key = $1
//	  AND (file_path, symbol_name) IN (VALUES ($2,$3), ($4,$5), ...)
func (s *Store) deleteExplicitOrphanChunk(ctx context.Context, repoKey string, files, syms []string) (int64, error) {
	n := len(files)
	if n == 0 {
		return 0, nil
	}
	// Build: (file_path, symbol_name) IN (VALUES ($2,$3), ($4,$5), ...)
	// Param $1 = repoKey; pairs start at $2.
	var b strings.Builder
	b.WriteString("DELETE FROM public.code_embeddings WHERE repo_key = $1 AND (file_path, symbol_name) IN (VALUES ")
	args := make([]any, 0, 1+n*2)
	args = append(args, repoKey)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		p1 := 2 + i*2
		p2 := p1 + 1
		fmt.Fprintf(&b, "($%d,$%d)", p1, p2)
		args = append(args, files[i], syms[i])
	}
	b.WriteByte(')')
	ct, err := s.pool.Exec(ctx, b.String(), args...)
	if err != nil {
		return 0, fmt.Errorf("deleteExplicitOrphanChunk: %w", err)
	}
	return ct.RowsAffected(), nil
}

// orphanRepoKeyPredicate is the shared WHERE clause that identifies
// code_embeddings rows whose repo_key has no matching code_repo_state row.
// DeleteOrphanRepoKeys, CountOrphanRepoKeys and PreviewOrphanRepoKeys all
// reference it so the delete set, the gauge count, and the dry-run preview
// can never diverge (a probe once wiped 15076 rows because the preview and
// the delete used different predicates — see orphan_sweep dry-run gate).
const orphanRepoKeyPredicate = "repo_key NOT IN (SELECT repo_key FROM public.code_repo_state)"

// DeleteOrphanRepoKeys removes all code_embeddings rows whose repo_key has no
// corresponding row in code_repo_state. These orphans accumulate when:
//   - a worktree checkout creates a new repo_key (GraphNameFor hashes the root
//     path) but the worktree is later removed without running DeleteRepo;
//   - a repo was bulk-indexed and then deregistered without cleanup.
//
// Safety direction: delete embeddings-keys-not-in-state, NOT the reverse.
// code_repo_state is the canonical set of "repos we actively manage"; deleting
// embeddings for absent-state rows is safe because the next indexRepo run will
// re-populate them if the repo is re-registered. The inverse (deleting state rows
// whose embeddings are absent) would silently wipe valid index-freshness records.
//
// This function does NOT delete code_repo_state rows. The operator controls state
// lifecycle (register/deregister via indexRepo or DeleteRepo).
//
// Returns the number of orphan embedding rows deleted.
func (s *Store) DeleteOrphanRepoKeys(ctx context.Context) (int64, error) {
	if err := s.EnsureSchema(ctx); err != nil {
		return 0, err
	}
	ct, err := s.pool.Exec(ctx, "DELETE FROM public.code_embeddings WHERE "+orphanRepoKeyPredicate)
	if err != nil {
		return 0, fmt.Errorf("DeleteOrphanRepoKeys: %w", err)
	}
	return ct.RowsAffected(), nil
}

// PreviewOrphanRepoKeys returns the orphan repo_keys and the total number of
// code_embeddings rows that DeleteOrphanRepoKeys would remove, WITHOUT
// deleting anything. Used by the orphan_sweep dry-run path so an operator can
// confirm the blast radius before committing to a bulk DELETE.
//
// The orphan predicate is shared with DeleteOrphanRepoKeys via
// orphanRepoKeyPredicate so preview and delete can never diverge.
func (s *Store) PreviewOrphanRepoKeys(ctx context.Context) (repoKeys []string, rowCount int64, err error) {
	if err := s.EnsureSchema(ctx); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, "SELECT DISTINCT repo_key FROM public.code_embeddings WHERE "+orphanRepoKeyPredicate)
	if err != nil {
		return nil, 0, fmt.Errorf("PreviewOrphanRepoKeys: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, 0, fmt.Errorf("PreviewOrphanRepoKeys: scan: %w", err)
		}
		repoKeys = append(repoKeys, k)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("PreviewOrphanRepoKeys: %w", err)
	}
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM public.code_embeddings WHERE "+orphanRepoKeyPredicate).Scan(&rowCount); err != nil {
		return nil, 0, fmt.Errorf("PreviewOrphanRepoKeys: count: %w", err)
	}
	return repoKeys, rowCount, nil
}

// CountOrphanRepoKeys returns the number of distinct repo_keys present in
// code_embeddings but absent from code_repo_state. Used for the
// gocode_orphan_repo_keys gauge.
func (s *Store) CountOrphanRepoKeys(ctx context.Context) (int64, error) {
	if err := s.EnsureSchema(ctx); err != nil {
		return 0, err
	}
	var n int64
	err := s.pool.QueryRow(ctx,
		"SELECT COUNT(DISTINCT repo_key) FROM public.code_embeddings WHERE "+orphanRepoKeyPredicate).Scan(&n)
	return n, err
}

// orphanRepoKeyForRepoPredicate is the per-repoKey form of
// orphanRepoKeyPredicate: the same set-difference AND a repo_key equality
// filter. CountOrphanRepoKeysForRepo and DeleteOrphanRepoKeysForRepo both
// reference it so the per-key count and the per-key delete can never diverge
// — the same invariant orphanRepoKeyPredicate enforces for the bulk paths
// (see the 15076-row incident documented at store.go:549-554). The sweep's
// in-flight guard is applied per-key by the handler; these methods are the
// per-key SQL surface the guard drives, and they share the orphan predicate
// so a future change to one cannot silently disagree with the other.
const orphanRepoKeyForRepoPredicate = orphanRepoKeyPredicate + " AND repo_key = $1"

// CountOrphanRepoKeysForRepo returns the number of code_embeddings rows for
// repoKey that have no matching code_repo_state row. Used by the orphan_sweep
// handler so the per-key preview (dry-run "rows that would be deleted for
// this key") and the per-key delete report the same number — the guard for
// requirement 1 of #741 applied to the per-key path.
func (s *Store) CountOrphanRepoKeysForRepo(ctx context.Context, repoKey string) (int64, error) {
	if err := s.EnsureSchema(ctx); err != nil {
		return 0, err
	}
	var n int64
	err := s.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM public.code_embeddings WHERE "+orphanRepoKeyForRepoPredicate, repoKey).Scan(&n)
	return n, err
}

// DeleteOrphanRepoKeysForRepo removes the code_embeddings rows for a single
// repoKey that have no matching code_repo_state row. It is the per-key
// counterpart of DeleteOrphanRepoKeys, used by the orphan_sweep handler once
// it has claimed the index slot for repoKey (so an in-flight first index
// cannot have its not-yet-state-row-committed chunks deleted underneath it).
// Shares orphanRepoKeyForRepoPredicate with CountOrphanRepoKeysForRepo so the
// per-key delete set and the per-key count can never diverge.
//
// Returns the number of orphan embedding rows deleted for this repoKey.
func (s *Store) DeleteOrphanRepoKeysForRepo(ctx context.Context, repoKey string) (int64, error) {
	if err := s.EnsureSchema(ctx); err != nil {
		return 0, err
	}
	ct, err := s.pool.Exec(ctx,
		"DELETE FROM public.code_embeddings WHERE "+orphanRepoKeyForRepoPredicate, repoKey)
	if err != nil {
		return 0, fmt.Errorf("DeleteOrphanRepoKeysForRepo: %w", err)
	}
	return ct.RowsAffected(), nil
}

// GetEmbedModelForRepo returns the embed_model stored in code_embeddings for
// repoKey (reading from any row for that repo_key), or "" when no rows exist
// or on error. Used as a fallback by the semantic_search stale-space guard
// when code_repo_state has no entry for the repo (e.g. orphan vectors left
// behind by a removed checkout). The per-row embed_model added in 2026-06-13
// closes the blind spot: even without a state row the guard can detect that
// the stored vectors are in the wrong embedding space.
func (s *Store) GetEmbedModelForRepo(ctx context.Context, repoKey string) string {
	if err := s.EnsureSchema(ctx); err != nil {
		return ""
	}
	var model string
	err := s.pool.QueryRow(ctx,
		`SELECT embed_model FROM public.code_embeddings WHERE repo_key = $1 LIMIT 1`, repoKey).
		Scan(&model)
	if err != nil {
		return ""
	}
	return model
}

// CountEmbeddings returns the number of code_embeddings rows for a given
// repoKey. Used by the same-SHA index gate to detect the frozen-empty state:
// when indexed_sha == HEAD but COUNT == 0 the repo needs recovery re-indexing.
//
// The query uses the (repo_key) index so it is cheap (index scan + aggregate).
// It runs only on the same-SHA branch so it never adds latency to populated repos.
//
// On error: returns (0, err). All three callers treat a non-nil error as
// fail-open — they fall through to re-index rather than skipping. This is the
// correct behaviour: a transient COUNT failure should not freeze a repo forever
// (fail-open: a frozen repo recovers even if COUNT transiently fails).
func (s *Store) CountEmbeddings(ctx context.Context, repoKey string) (int, error) {
	if err := s.EnsureSchema(ctx); err != nil {
		return 0, err
	}
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM public.code_embeddings WHERE repo_key = $1`, repoKey).
		Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count embeddings: %w", err)
	}
	return n, nil
}

// Stats returns embedding counts per repo.
func (s *Store) Stats(ctx context.Context) (map[string]int, error) {
	if err := s.EnsureSchema(ctx); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, "SELECT repo_key,COUNT(*) FROM public.code_embeddings GROUP BY repo_key")
	if err != nil {
		return nil, fmt.Errorf("embeddings stats: %w", err)
	}
	defer rows.Close()
	result := make(map[string]int)
	for rows.Next() {
		var key string
		var count int
		if err := rows.Scan(&key, &count); err != nil {
			return nil, fmt.Errorf("scan stats: %w", err)
		}
		result[key] = count
	}
	return result, rows.Err()
}
