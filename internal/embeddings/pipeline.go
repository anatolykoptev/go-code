package embeddings

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	kitcache "github.com/anatolykoptev/go-kit/cache"
	"github.com/anatolykoptev/go-kit/embed"
	"github.com/anatolykoptev/go-kit/sparse"

	"github.com/anatolykoptev/vaelor/internal/ingest"
	"github.com/anatolykoptev/vaelor/internal/parser"
	"github.com/anatolykoptev/vaelor/internal/strutil"
)

const (
	// maxEmbedText: cap at 1500 chars (~450 tokens) so the full pre-truncation
	// text reaches the model and the line-boundary cut in buildEmbedText is
	// the truncation policy instead of a hidden token-cap chop.
	// CodeRankEmbed (code-rank-embed) supports up to 8192 tokens so 450 tokens
	// is well within the model's context window. Verified 2026-04-29:
	// embed_batch_tokens p99 stuck at 256 (= cap) under prior 256 setting,
	// indicating saturation; 1500 chars maps to ~450 tokens for typical code.
	maxEmbedText      = 1500
	maxIndexFileBytes = 512 * 1024
	indexChunkSize    = 100

	// loiBodyLines is the number of body lines included in the expanded embed
	// text (Aider LOI — Lines of Interest — approach). When EXPAND_SYMBOL_KINDS
	// is ON, buildEmbedTextExpanded limits the body to the first loiBodyLines
	// lines instead of the full body, producing a more focused embedding that
	// emphasizes the signature + @doc docstring + opening implementation lines.
	// The controller measures recall lift vs index-size cost before enabling.
	loiBodyLines = 8
)

// indexProgress tracks the progress of a background indexing run.
// All fields are accessed from concurrent goroutines (the spawned indexer
// goroutine writes; callers of IsIndexing/IndexProgress read), so they use
// atomic operations: running via atomic.Bool, done/total via atomic int64.
type indexProgress struct {
	total   int64
	done    int64
	running atomic.Bool
}

// defaultIndexBudget is the fallback timeout for the background index goroutine
// when no WithIndexBudget option is provided. 30 minutes gives ample headroom for
// the largest repos (go-code itself is ~5k symbols × multiple chunks) while still
// bounding a goroutine that is stuck waiting on a permanently-unreachable embed server.
const defaultIndexBudget = 30 * time.Minute

// repoStateRetryBackoff is the brief wait before the single retry of a failed
// writeRepoState. Covers transient Postgres deadlock/statement_timeout without
// adding meaningful latency to the happy path (retry only fires on failure).
// Kept short so the first-index compensate path (which runs after the retry is
// exhausted) is not delayed materially on a genuinely-broken DB.
const repoStateRetryBackoff = 150 * time.Millisecond

// Pipeline orchestrates embedding indexing for repository symbols.
type Pipeline struct {
	client            *embed.Client
	store             *Store
	embedModel        string                                                                                       // active embedding model name; stored alongside head_sha for cross-model reindex detection
	writeRepoState    func(ctx context.Context, repoKey, sha, sourcePath string) error                             // defaults to a closure over store.SetRepoStateWithPath + embedModel; injectable for testing
	writeSparsesBatch func(ctx context.Context, rows []SparseUpdate) error                                         // defaults to store.UpdateSparseEmbeddingsBatch; injectable for testing
	deleteRepo        func(ctx context.Context, repoKey string) error                                              // defaults to store.DeleteRepo; injectable for testing (compensating rollback)
	reconcilePaths    func(ctx context.Context, repoKey, sourcePath string, dryRun bool) (*ReconcileResult, error) // defaults to store.ReconcileRepoPaths; injectable for testing (#708 path-existence reconciliation hook)
	progress          sync.Map                                                                                     // repoKey -> *indexProgress
	fileCache         *kitcache.Cache                                                                              // optional per-file symbol-entry cache; nil disables.
	sparseClient      sparse.SparseEmbedder                                                                        // optional SPLADE embedder; nil disables sparse indexing (cold-path: byte-identical to dense-only)
	sparseMaxBatch    int                                                                                          // per-request cap for sparse server (EMBED_MAX_INPUT_ARRAY); defaults to sparseServerMaxDocs
	indexBudget       time.Duration                                                                                // per-goroutine timeout for IndexRepoAsyncWithTool; 0 uses defaultIndexBudget
	expandSymbolKinds bool                                                                                         // #664: gate macro/module/type-alias extraction + LOI embed text (EXPAND_SYMBOL_KINDS, default false)
}

// NewPipeline creates a Pipeline backed by the given client and store.
//
// Pass a non-nil fileCache via WithFileCache to enable per-file symbol-entry
// caching keyed on (repoKey, file.RelPath) and validated by file modTime+size.
// When fileCache is nil, behavior is byte-identical to the v0.32.0 baseline.
//
// model is the active embedding model name (e.g. "code-rank-embed"). It is
// stored alongside head_sha so that a model switch on next startup triggers a
// full reindex. Pass "" to retain legacy behaviour (no model tracking).
func NewPipeline(client *embed.Client, store *Store, model string, opts ...PipelineOpt) *Pipeline {
	p := &Pipeline{
		client:            client,
		store:             store,
		embedModel:        model,
		writeSparsesBatch: store.UpdateSparseEmbeddingsBatch,
		deleteRepo:        store.DeleteRepo,
		reconcilePaths:    store.ReconcileRepoPaths,
	}
	// writeRepoState closes over model so the injectable fn keeps a (ctx, repoKey, sha)
	// signature (no model param) — avoids a breaking change in test injectors.
	m := model
	p.writeRepoState = func(ctx context.Context, repoKey, sha, sourcePath string) error {
		return store.SetRepoStateWithPath(ctx, repoKey, sha, m, sourcePath)
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// PipelineOpt configures a Pipeline at construction time.
type PipelineOpt func(*Pipeline)

// WithFileCache wires a *kitcache.Cache to memoize per-file symbol entries.
// Validator (modTime+size) ensures stale entries are evicted on the next
// indexRepo pass after a file is touched. Pass nil to disable explicitly.
func WithFileCache(c *kitcache.Cache) PipelineOpt {
	return func(p *Pipeline) { p.fileCache = c }
}

// WithSparseEmbedder wires a SparseEmbedder into the indexing pipeline. When
// set, each batch of symbols also gets a SPLADE sparse vector, written to the
// sparse_embedding column (Phase P2). When nil (default), the pipeline is
// byte-identical to the dense-only path — no sparse calls, no sparse writes.
//
// Sparse embedding is strictly additive: the dense vector is always persisted
// regardless of sparse outcome. Failures in the sparse leg (embed call or DB
// write) log at WARN, bump gocode_sparse_embed_failures_total, and leave that
// row's sparse_embedding NULL — never blocking dense persistence or SHA advance.
//
// VocabSize guard: if e.VocabSize() != 30522 (the sparsevec column dimension),
// sparse indexing is refused (logs WARN, pipeline stays dense-only). This
// prevents out-of-range index corruption when a non-standard SPLADE head is wired.
func WithSparseEmbedder(e sparse.SparseEmbedder) PipelineOpt {
	return func(p *Pipeline) { p.sparseClient = newSparseEmbedder(e) }
}

// WithSparseMaxBatch overrides the per-request input cap for the sparse server
// (EMBED_MAX_INPUT_ARRAY on the embed-server side). The default is
// sparseServerMaxDocs (32). Override when the server cap changes without a
// go-code redeploy (via SPARSE_EMBED_MAX_ARRAY → Config.SparseEmbedMaxArray).
func WithSparseMaxBatch(n int) PipelineOpt {
	return func(p *Pipeline) {
		if n > 0 {
			p.sparseMaxBatch = n
		}
	}
}

// WithIndexBudget sets the per-goroutine deadline for IndexRepoAsyncWithTool.
// When the budget expires the background goroutine's context is cancelled,
// which propagates to embedAndUpsert and terminates the stuck goroutine.
// d=0 uses the defaultIndexBudget (30m). Set via INDEX_BUDGET env var in the
// cmd layer (e.g. "INDEX_BUDGET=45m"); test code passes short durations directly.
func WithIndexBudget(d time.Duration) PipelineOpt {
	return func(p *Pipeline) {
		if d > 0 {
			p.indexBudget = d
		}
	}
}

// WithExpandSymbolKinds enables the #664 low-volume symbol-kind expansion
// (macro, module, type-alias) and the Aider LOI embed-text format. When false
// (the default), the pipeline is byte-identical to the pre-#664 behavior — no
// new kinds enter the embed set, and buildEmbedText uses the full-body format.
// Wired from Config.ExpandSymbolKinds (env EXPAND_SYMBOL_KINDS, default false).
func WithExpandSymbolKinds(v bool) PipelineOpt {
	return func(p *Pipeline) { p.expandSymbolKinds = v }
}

// withWriteRepoStateFn overrides the SetRepoState implementation used by all
// pipeline code paths. For testing only — allows injection of a failing writer
// without touching the real Postgres store.
func withWriteRepoStateFn(fn func(ctx context.Context, repoKey, sha, sourcePath string) error) PipelineOpt {
	return func(p *Pipeline) { p.writeRepoState = fn }
}

// withWriteSparsesBatchFn overrides the UpdateSparseEmbeddingsBatch implementation.
// For testing only — allows a spy that records rows, injects errors, or verifies
// the batch shape without a real Postgres store.
func withWriteSparsesBatchFn(fn func(ctx context.Context, rows []SparseUpdate) error) PipelineOpt {
	return func(p *Pipeline) { p.writeSparsesBatch = fn }
}

// withDeleteRepoFn overrides the DeleteRepo implementation used by the
// compensating-rollback paths. For testing only — allows a spy that counts
// calls and verifies the compensate fired (or did not) without relying on
// log-string matching.
func withDeleteRepoFn(fn func(ctx context.Context, repoKey string) error) PipelineOpt {
	return func(p *Pipeline) { p.deleteRepo = fn }
}

// InvalidateIfModelChanged purges code_embeddings for repoKey when the stored
// embed_model differs from the active model (p.embedModel). This ensures stale
// vectors from a previous model are not mixed with new query vectors after a
// model upgrade (they live in different embedding spaces even when dimension
// is equal). When a purge occurs, the next IncrementalSync call treats the
// repo as unindexed and runs a full re-embed.
//
// Called by autoindex.go per-repo before triggering IncrementalSync. No-op
// when embedModel is "" (legacy / test pipelines without model tracking).
func (p *Pipeline) InvalidateIfModelChanged(ctx context.Context, repoKey string) bool {
	if p.embedModel == "" {
		return false
	}
	purged, err := p.store.InvalidateRepoIfModelChanged(ctx, repoKey, p.embedModel)
	if err != nil {
		slog.Warn("embeddings: model-mismatch invalidate failed",
			slog.String("repo_key", repoKey),
			slog.String("active_model", p.embedModel),
			slog.Any("error", err))
		return false
	}
	if purged {
		slog.Info("embeddings: model changed — purged stale vectors; repo will re-embed on next IncrementalSync",
			slog.String("repo_key", repoKey),
			slog.String("active_model", p.embedModel))
	}
	return purged
}

// EmbedModel returns the active embedding model name configured for this pipeline.
// Returns "" for legacy pipelines created without model tracking.
// Used by semantic_search to detect stale-space hits: a stored model that does
// not match EmbedModel() means the indexed vectors are in the wrong space.
func (p *Pipeline) EmbedModel() string { return p.embedModel }

// IsIndexing returns true if background indexing is running for the given repo.
func (p *Pipeline) IsIndexing(repoKey string) bool {
	v, ok := p.progress.Load(repoKey)
	if !ok {
		return false
	}
	return v.(*indexProgress).running.Load()
}

// IndexProgress returns (done, total, running) for the given repo.
func (p *Pipeline) IndexProgress(repoKey string) (done, total int, running bool) {
	v, ok := p.progress.Load(repoKey)
	if !ok {
		return 0, 0, false
	}
	prog := v.(*indexProgress)
	return int(atomic.LoadInt64(&prog.done)), int(atomic.LoadInt64(&prog.total)), prog.running.Load()
}

// IndexRepoAsync starts background indexing if not already running.
// Returns true if indexing was started, false if already in progress.
// The background goroutine uses context.Background() (not the caller's context)
// so that client disconnects do not abort the indexing — Bug #2 fix.
//
// This is a backward-compatible wrapper around IndexRepoAsyncWithTool that passes
// tool="autoindex". Callers that know their triggering MCP tool should use
// IndexRepoAsyncWithTool directly so the cancel counter is properly attributed.
func (p *Pipeline) IndexRepoAsync(repoKey, root string) bool {
	return p.IndexRepoAsyncWithTool("autoindex", repoKey, root)
}

// claimIndexSlot is the shared per-repoKey single-flight claim used by BOTH the
// async (IndexRepoAsyncWithTool) and sync (IndexRepo) index entrypoints. It
// atomically reserves the progress slot via LoadOrStore so two indexers for one
// repoKey never run concurrently — the residual TOCTOU from #589 where a sync
// IndexRepo bypassed this gate and raced an async indexer for the same repoKey
// (the loser's compensating DeleteRepo could wipe the winner's just-committed
// embeddings in the gap between the winner's embed-commit and its state-write).
//
// running is set BEFORE LoadOrStore so the winner's slot is always observed as
// running=true by IsIndexing. On success the caller MUST call the returned
// release when its index completes (sets running=false and deletes the slot).
// On loss (won=false) the caller must NOT run indexRepoWithTool — a concurrent
// indexer owns the repoKey.
func (p *Pipeline) claimIndexSlot(repoKey string) (prog *indexProgress, release func(), won bool) {
	prog = &indexProgress{}
	prog.running.Store(true)
	if _, loaded := p.progress.LoadOrStore(repoKey, prog); loaded {
		// Slot already taken by a concurrent or still-running indexer.
		// The loser discards prog; release is nil so there is nothing to undo.
		return nil, nil, false
	}
	release = func() {
		prog.running.Store(false)
		p.progress.Delete(repoKey)
	}
	return prog, release, true
}

// IndexRepoAsyncWithTool starts background indexing if not already running, with
// tool attribution for observability. Returns true if indexing was started, false
// if already in progress.
//
// The background goroutine runs under a bounded context (p.indexBudget, default 30m)
// derived from context.Background() — decoupled from any request context (Bug #2 fix)
// but bounded so a goroutine waiting on a permanently-unreachable embed server does
// not leak indefinitely.
//
// tool is the MCP tool name that triggered this index (e.g. "semantic_search",
// "code_research"). It labels the gocode_index_cancelled_total counter so
// cancellations are attributable. Callers that do not know the tool should pass
// "autoindex".
//
// Concurrency: the claim is atomic (claimIndexSlot → LoadOrStore); only one
// goroutine per repoKey runs at a time. The sync IndexRepo path shares the same
// slot, so a sync and an async indexer for one repoKey never overlap (#589).
func (p *Pipeline) IndexRepoAsyncWithTool(tool, repoKey, root string) bool {
	// Model-fingerprint guard: purge stale vectors before indexing if the active
	// model changed since the last index. This covers the lazy per-query path
	// (semantic_search triggers indexing on first query). The AutoIndex path has
	// its own pre-loop invalidation in autoindex.go.
	p.InvalidateIfModelChanged(context.Background(), repoKey)

	prog, release, won := p.claimIndexSlot(repoKey)
	if !won {
		// Slot already taken by a concurrent or still-running indexer (sync or
		// async). No goroutine is spawned.
		return false
	}
	budget := p.indexBudget
	if budget <= 0 {
		budget = defaultIndexBudget
	}
	// We won the slot. The stored prog already has running=true.
	go func() {
		defer release()
		ctx, cancel := context.WithTimeout(context.Background(), budget)
		defer cancel()
		result, err := p.indexRepoWithTool(ctx, tool, repoKey, root, prog)
		if err != nil {
			slog.Error("background index failed",
				slog.String("repo", repoKey),
				slog.String("tool", tool),
				slog.Any("error", err))
			return
		}
		slog.Info("background index complete",
			slog.String("repo", repoKey),
			slog.String("tool", tool),
			slog.Int("indexed", result.Indexed),
			slog.Int("skipped", result.Skipped),
			slog.Int("total", result.Total))
	}()
	return true
}

// IndexResult summarizes the outcome of an indexing run.
type IndexResult struct {
	Indexed int
	Skipped int
	Total   int
}

// IndexRepo indexes all functions and methods in a repository for semantic search.
//
// Concurrency (#589): the sync path claims the SAME per-repoKey single-flight slot
// the async path (IndexRepoAsyncWithTool) uses, so a sync and an async indexer for
// one repoKey never run concurrently. Previously the sync path bypassed the slot and
// could race a concurrent async indexer: the loser's compensating DeleteRepo wiped
// the winner's just-committed embeddings in the gap between the winner's embed-commit
// and its state-write (inverted orphan — state row, zero embeddings). When the slot
// is already held by a concurrent indexer, this call returns an empty IndexResult
// without running — the winner performs the index, so the loser doing nothing is the
// safe outcome (no concurrent DeleteRepo to invert). The non-racing case is
// byte-identical to the prior behavior (claim → indexRepoWithTool → release).
func (p *Pipeline) IndexRepo(ctx context.Context, repoKey, root string) (*IndexResult, error) {
	// Register the (repo key → path) mapping so dashboards can resolve the
	// opaque hash. IncrementalSync also calls this; the double-set is harmless.
	SetRepoInfoGauge(repoKey, root)
	_, release, won := p.claimIndexSlot(repoKey)
	if !won {
		// A concurrent indexer (sync or async) already holds the per-repoKey slot.
		// Serialize: do NOT run indexRepoWithTool concurrently — the winner is
		// performing the index. Returning an empty result avoids the TOCTOU where
		// the loser's compensating DeleteRepo could wipe the winner's embeddings.
		// If the winner subsequently fails-and-compensates, this repo is left
		// unindexed for this cycle — strictly safer than the data-loss it replaces,
		// and it converges via the next autoindex cycle / lazy semantic_search.
		slog.Info("indexRepo: concurrent index in progress for repoKey; skipping (single-flight)",
			slog.String("repo", repoKey))
		return &IndexResult{}, nil
	}
	defer release()
	return p.indexRepoWithTool(ctx, "unknown", repoKey, root, nil)
}

// shortSHA returns the first shortSHALen chars of a SHA for log messages.
const shortSHALen = 8

func shortSHA(sha string) string { return sha[:min(shortSHALen, len(sha))] }

// checkSameSHAFastPath returns (result, true) when it is safe to skip indexing
// because the repo's main branch is unchanged AND the store has ≥1 embedding.
// Returns (nil, false) when the caller must proceed to full re-index (empty store
// or DB error — the Bug #1 frozen-empty recovery path).
//
// prevSHA/prevErr are the result of the GetRepoState lookup the caller already
// performed (for first-index detection) — reused here to avoid a redundant
// query. prevSHA=="" with prevErr==nil means no state row (first index);
// prevErr!=nil means the lookup failed and the caller must fall through.
//
// root is the absolute filesystem path to the repo; it is used to distinguish a
// real desync (code repo with 0 stored rows) from a docs-only repo that
// legitimately has 0 embeddable symbols — the counter must only fire for the
// former.
//
// Side-effects on skip: bumps indexed_at via writeRepoState (liveness), sets
// gocode_repo_embeddings_present gauge.
// Side-effects on desync (0 rows, code repo): bumps gocode_repo_state_advanced_with_zero_embeddings_total.
func (p *Pipeline) checkSameSHAFastPath(ctx context.Context, repoKey, root, currentSHA, prevSHA string, prevErr error) (*IndexResult, bool) {
	if prevErr != nil || prevSHA != currentSHA {
		return nil, false // not same-SHA — fall through
	}
	embCount, countErr := p.store.CountEmbeddings(ctx, repoKey)
	switch {
	case countErr != nil:
		slog.Warn("indexRepo: CountEmbeddings failed; falling through to re-index",
			slog.String("repo", repoKey), slog.Any("error", countErr))
		return nil, false
	case embCount > 0:
		slog.Debug("indexRepo: skip — main branch unchanged",
			slog.String("repo", repoKey), slog.String("sha", shortSHA(currentSHA)))
		if setErr := p.writeRepoState(ctx, repoKey, currentSHA, root); setErr != nil {
			recordRepoStateWriteFailure(repoKey, "indexRepo:same-sha", setErr)
		}
		SetEmbeddingsPresentGauge(repoKey, embCount)
		return &IndexResult{}, true
	default:
		// 0 rows despite same SHA — frozen-empty desync (Bug #1).
		// Only bump the "operator-investigation-required" counter when the repo root
		// has embeddable source files. Docs-only repos (e.g. /host/src/wiki with .md only)
		// legitimately produce 0 embeddings — bumping the counter for them is a false
		// positive that fires on every boot and can cause spurious alerts.
		// The real desync class (code repo with source files but 0 stored rows) is still
		// caught — rootHasEmbeddableFiles walks the directory cheaply (early-exit on first
		// match). See also advanceStateNoEmbed which gates the counter on result.Total > 0.
		if rootHasEmbeddableFiles(root) {
			repoStateAdvancedWithZeroEmbeddingsTotal.WithLabelValues(repoKey).Inc()
		}
		slog.Warn("indexRepo: same SHA but 0 embeddings — recovery re-index",
			slog.String("repo", repoKey), slog.String("sha", shortSHA(currentSHA)))
		return nil, false
	}
}

// advanceStateNoEmbed handles the len(toEmbed)==0 path: all parsed symbols
// matched existing hashes so nothing new needs embedding. It advances the
// repo fingerprint (SHA + indexed_at) and sets the embeddings-present gauge.
//
// Defense-in-depth: if CountEmbeddings returns 0 despite parsed symbols > 0
// (data inconsistency), the SHA is NOT advanced — the caller will retry next
// boot. This guards against advancing the SHA when the store is unexpectedly
// empty (would freeze the repo forever — same root cause as Bug #1).
func (p *Pipeline) advanceStateNoEmbed(ctx context.Context, repoKey, root, currentSHA string, result *IndexResult) (*IndexResult, error) {
	existingCount, countErr := p.store.CountEmbeddings(ctx, repoKey)
	if countErr != nil {
		slog.Warn("indexRepo: CountEmbeddings failed on no-embed path",
			slog.String("repo", repoKey), slog.Any("error", countErr))
	}
	if countErr == nil && existingCount == 0 && result.Total > 0 {
		// Parsed symbols exist but store is empty — data inconsistency.
		// Do not advance SHA; next boot will retry.
		repoStateAdvancedWithZeroEmbeddingsTotal.WithLabelValues(repoKey).Inc()
		slog.Warn("indexRepo: no-embed path with 0 store rows despite parsed symbols; skipping SHA advance",
			slog.String("repo", repoKey), slog.Int("parsed", result.Total))
		return result, nil
	}
	if currentSHA != "" {
		if err := p.writeRepoState(ctx, repoKey, currentSHA, root); err != nil {
			recordRepoStateWriteFailure(repoKey, "indexRepo:no-embed", err)
		}
	}
	SetEmbeddingsPresentGauge(repoKey, existingCount)
	return result, nil
}

// firstIndexVerdict decides whether a compensating rollback of just-written
// embeddings is safe. It fails CLOSED: only a confirmed-absent state row
// (stateExists=false with no error on EITHER the GetRepoState or the
// RepoStateExists probe) is a first index. A lookup error on either probe, or
// a present row, is treated as NOT first index — compensating-deleting on
// uncertainty would erase a live repo's embeddings. Pure function so the
// dangerous error branches are unit-testable without a DB.
func firstIndexVerdict(prevErr error, stateExists bool, existErr error) bool {
	return prevErr == nil && existErr == nil && !stateExists
}

// compensateFirstIndexOrphan rolls back the just-written embeddings for a first
// index whose state-row write (or embedChunks) failed — UNLESS a concurrent
// indexer for the same repoKey has meanwhile committed the state row. A
// bypassing sync IndexRepo can race the async single-flight; without this
// re-check, the loser's compensating delete would erase the winner's committed
// embeddings while the winner's state row survives (an inverted orphan). The
// re-check narrows that window: if a state row now exists, our embeddings are
// no longer orphaned, so we do NOT delete. orphanPreventedTotal counts only a
// successful rollback (a failed delete leaves the orphan intact — not prevented).
func (p *Pipeline) compensateFirstIndexOrphan(ctx context.Context, repoKey, stage string) {
	if exists, err := p.store.RepoStateExists(ctx, repoKey); err == nil && exists {
		return // a concurrent indexer wrote the state row — our rows are backed, not orphans
	}
	if delErr := p.deleteRepo(ctx, repoKey); delErr != nil {
		slog.Warn("indexRepo: compensating DeleteRepo failed",
			slog.String("repo", repoKey), slog.String("stage", stage), slog.Any("error", delErr))
		return
	}
	orphanPreventedTotal.Inc()
}

// indexRepoWithTool is the internal implementation that optionally reports progress.
// tool is passed down to embedChunks for cancel-counter attribution.
func (p *Pipeline) indexRepoWithTool(
	ctx context.Context, tool, repoKey, root string, prog *indexProgress,
) (*IndexResult, error) {
	// Fast path: skip the entire parse + embed cycle when the repo's main
	// branch has not moved since the last successful index. Cuts boot-time
	// embed-server load from "48 repos × N symbols" to zero for unchanged
	// repos. A repo with no main/master/HEAD ref (non-git path) returns
	// sha="" and falls through to the full path.
	currentSHA, _ := repoMainBranchSHA(root)

	// First-index detection decides whether a post-embed/embedChunks failure
	// COMPENSATES (first index, no state row: roll back embeddings → no orphan)
	// or preserves the swallowed-failure behavior (re-index: a stale state row
	// is acceptable, not an orphan). This gate guards a DESTRUCTIVE delete of
	// the whole repoKey's embeddings, so it MUST fail CLOSED: a wrong "first
	// index" verdict on a live repo wipes its embeddings.
	//
	// GetRepoState collapses three cases to "": no row (true first index),
	// an empty-head_sha row (non-git re-index), and a lookup error (DB
	// instability — the very window this fix targets). Only the first is
	// compensate-safe. So when the SHA is empty we resolve existence
	// explicitly via RepoStateExists and treat ANY error as NOT first index.
	prevSHA, prevErr := p.store.GetRepoState(ctx, repoKey)
	stateExists := true // present unless proven absent
	var existErr error
	if prevErr == nil && prevSHA == "" {
		stateExists, existErr = p.store.RepoStateExists(ctx, repoKey)
	}
	firstIndex := firstIndexVerdict(prevErr, stateExists, existErr)

	if currentSHA != "" {
		if result, skip := p.checkSameSHAFastPath(ctx, repoKey, root, currentSHA, prevSHA, prevErr); skip {
			return result, nil
		}
	}

	symbols, files, err := p.collectSymbolsCached(ctx, repoKey, root)
	if err != nil {
		return nil, err
	}

	result := &IndexResult{Total: len(symbols)}

	existing, err := p.store.GetHashes(ctx, repoKey)
	if err != nil {
		return nil, fmt.Errorf("get existing hashes: %w", err)
	}

	toEmbed, seen := filterSymbols(symbols, files, existing, result, p.expandSymbolKinds)

	// Path-existence reconciliation (#708): delete rows under this repo_key
	// whose file_path does not resolve under the repo root. Runs AFTER the
	// parse so it is gated by the same shrink-guard as the orphan delete
	// (shrinkGuardFires) — a partial parse must not trigger a stale-path
	// mass-delete any more than an orphan-key mass-delete. The intra-key
	// orphan reconciliation (deleteIntraKeyOrphans) only covers per-symbol
	// cleanup; it cannot catch a file_path that belongs to a different
	// project sharing the same repo_key (the FNV(path) rename collision at
	// the heart of #708).
	//
	// Non-fatal: a reconciliation error is logged and swallowed so it never
	// blocks indexing. The root-missing guard inside ReconcileRepoPaths
	// ensures a mount blip deletes nothing.
	//
	// Does NOT run on the same-SHA fast path (above): a fast-path skip means
	// the repo hasn't changed since the last successful index, so no stale
	// paths can have appeared. A full-walk index (SHA changed or non-git) is
	// where renames surface.
	if p.reconcilePaths != nil && root != "" && !shrinkGuardFires(seen, existing) {
		if _, rErr := p.reconcilePaths(ctx, repoKey, root, false); rErr != nil {
			slog.Warn("indexRepo: path reconciliation failed (non-fatal)",
				slog.String("repo", repoKey), slog.Any("error", rErr))
		}
	}

	// Compute the explicit orphan set: DB keys present for this repo_key that
	// are NOT in the freshly-parsed symbol set. `existing` is the full DB-hash
	// map read above; `seen` is the complete parsed key set from filterSymbols.
	// We pass an explicit slice so DeleteExplicitOrphans uses a positive IN-list
	// (not a per-chunk NOT-IN anti-join) -- fixing the >500-key data-loss bug
	// introduced in PR #209 (caa34d5, 2026-06-02).
	var orphanKeys []string
	for key := range existing {
		if !seen[key] {
			orphanKeys = append(orphanKeys, key)
		}
	}

	if prog != nil {
		atomic.StoreInt64(&prog.total, int64(len(toEmbed)))
	}

	if len(toEmbed) == 0 {
		// Still reconcile orphans on the no-embed path: all parsed symbols hash-
		// matched existing rows (nothing new to embed), but symbols deleted from
		// source since the last index are still in the DB and must be cleaned up.
		deleteIntraKeyOrphans(ctx, p.store, repoKey, seen, existing, orphanKeys)
		noEmbedResult, noEmbedErr := p.advanceStateNoEmbed(ctx, repoKey, root, currentSHA, result)
		if noEmbedErr == nil {
			// Refresh the coverage gauge after reconciliation settles.
			if coverageCount, countErr := p.store.CountEmbeddings(ctx, repoKey); countErr == nil {
				SetEmbeddingsCoverageRows(repoKey, coverageCount)
			}
		}
		return noEmbedResult, noEmbedErr
	}

	if err := p.embedChunks(ctx, repoKey, tool, toEmbed, result, prog); err != nil {
		// First-index orphan guard: on a first index (no state row), a partial
		// embedChunks failure leaves committed chunk rows with no state row →
		// orphan. Roll them back so the next index re-embeds cleanly, and
		// return the error so the caller retries. On re-index the prior state
		// row means partial rows are not an orphan — leave as-is.
		if firstIndex {
			p.compensateFirstIndexOrphan(ctx, repoKey, "embedChunks")
			return nil, fmt.Errorf("first-index embedChunks failed; partial embeddings rolled back: %w", err)
		}
		return nil, err
	}

	// Intra-key orphan reconciliation: delete rows no longer in the parsed set.
	// Runs AFTER embedChunks succeeds so that symbols counted as Skipped (hash-
	// matched-existing) are never deleted before their re-insert path completes.
	// On embedChunks failure we skip deletion to avoid corrupting a partial index.
	deleteIntraKeyOrphans(ctx, p.store, repoKey, seen, existing, orphanKeys)

	// Set the coverage gauge to the post-reconciliation row count.
	// result.Indexed (newly embedded) + result.Skipped (hash-matched existing) =
	// all symbols now present in the DB for this repo after orphan deletion.
	SetEmbeddingsCoverageRows(repoKey, result.Indexed+result.Skipped)

	if currentSHA != "" {
		if err := p.writeRepoStateWithRetry(ctx, repoKey, currentSHA, root); err != nil {
			recordRepoStateWriteFailure(repoKey, "indexRepo:post-embed", err)
			if firstIndex {
				// First-index orphan guard (the dominant orphan source): no
				// state row exists, so persisting embeddings without a state
				// row would create an orphan. COMPENSATE — roll back the
				// just-written embeddings so the next index re-embeds cleanly
				// (hash-skip makes the retry cheap) — and RETURN the error so
				// the caller (AutoIndex/IndexRepoAsync) sees the failure and
				// can retry. Net: no orphan — either state is written, or the
				// embeddings are rolled back.
				p.compensateFirstIndexOrphan(ctx, repoKey, "writeRepoState")
				return nil, fmt.Errorf("first-index repo_state write failed; embeddings rolled back: %w", err)
			}
			// Re-index: a state row already exists, so a swallowed failure
			// leaves a stale row — NOT an orphan. Preserve current behavior.
		}
	}
	SetEmbeddingsPresentGauge(repoKey, result.Indexed)

	return result, nil
}

// writeRepoStateWithRetry calls writeRepoState once, and on failure waits a
// brief backoff (respecting ctx cancellation) and retries exactly once. Returns
// the first error if both attempts fail, nil if either succeeds. The single
// retry covers transient Postgres deadlock/statement_timeout — the dominant
// orphan source was a swallowed writeRepoState failure on first index, and the
// retry eliminates the large majority of those before the compensate path runs.
func (p *Pipeline) writeRepoStateWithRetry(ctx context.Context, repoKey, sha, sourcePath string) error {
	err := p.writeRepoState(ctx, repoKey, sha, sourcePath)
	if err == nil {
		return nil
	}
	// Brief backoff then one retry. Respect ctx so a cancelled index does not
	// burn the full backoff waiting to retry an op that will be discarded.
	select {
	case <-time.After(repoStateRetryBackoff):
	case <-ctx.Done():
		return err
	}
	return p.writeRepoState(ctx, repoKey, sha, sourcePath)
}

// filterSymbols deduplicates symbols by (file_path+symKeySep+symbol_name) key
// and returns the subset that differ from the existing hash map (toEmbed) along
// with the complete parsed key set (seen). Matching symbols increment result.Skipped.
//
// seen must be the COMPLETE (file_path+symKeySep+symbol_name) set; callers use it
// for orphan detection (deleteIntraKeyOrphans). Do not call on a partial parse.
//
// Note on dedup lossiness: filterSymbols collapses by (file_path, symbol_name)
// ignoring kind/signature. Two symbols with the same file+name but different
// build-tags or C++ overloads collapse to one entry; only the first is embedded,
// making overloads and build-tag variants independently un-searchable.
// Root cause: the storage PK is (repo_key, file_path, symbol_name); a future
// PK migration adding kind/signature would be required to fix this.
// In practice, same-name funcs under different build-tags count as one seen key
// (e.g. 854 raw symbols -> 794 unique). This is NOT data-loss for general search:
// all build-tag variants remain in DB until orphan-delete runs; the dedup only
// affects toEmbed sizing and result.Skipped counts.
func filterSymbols(
	symbols []*parser.Symbol, files []*ingest.File,
	existing map[string]uint64, result *IndexResult,
	expanded bool,
) (toEmbed []symbolEntry, seen map[string]bool) {
	seen = make(map[string]bool, len(symbols))
	for i, sym := range symbols {
		key := files[i].RelPath + symKeySep + sym.Name
		if seen[key] {
			continue
		}
		seen[key] = true
		embedText := buildEmbedTextExpanded(sym, files[i].RelPath, expanded)
		h := strutil.TextHash(embedText)
		if prev, ok := existing[key]; ok && prev == h {
			result.Skipped++
			continue
		}
		toEmbed = append(toEmbed, symbolEntry{sym: sym, file: files[i], hash: h, embedText: embedText})
	}
	return toEmbed, seen
}

// shrinkGuardFires returns true when the parsed symbol-key set is suspiciously
// small relative to the existing DB-key set — a partial-parse signal that means
// NO deletes (orphan symbol keys OR stale file_paths) should proceed. The 0.7
// threshold catches accidental partial-parse mass-delete without blocking
// legitimate large deletions. Shared by deleteIntraKeyOrphans and the
// path-existence reconciliation so both delete paths are gated by ONE mechanism
// and ONE metric (#708 round 2 finding 0).
func shrinkGuardFires(seen map[string]bool, existing map[string]uint64) bool {
	return len(existing) > 0 && float64(len(seen)) < 0.7*float64(len(existing))
}

// deleteIntraKeyOrphans reconciles the DB for repoKey against the freshly-parsed
// symbol set. Non-fatal: logs a WARN on failure; increments counters on success.
// Separated from indexRepoWithTool to reduce cognitive complexity.
//
// Parameters:
//   - seen: complete (file_path:symbol_name) set from the current parse.
//   - existing: the DB-hash map read before filterSymbols (keyed file_path:symbol_name).
//   - orphanKeys: pre-computed explicit orphan slice = keys in existing NOT in seen.
//     Passed in so the caller owns the computation and the store method is pure.
//
// Shrink-guard: if shrinkGuardFires(seen, existing), the delete is SKIPPED and
// a WARN is logged. This prevents a partial parse (e.g. parser error on half
// the files) from mass-deleting valid rows. filterSymbols doc-comment: 'Do not
// call on a partial parse.'
func deleteIntraKeyOrphans(
	ctx context.Context, store *Store, repoKey string,
	seen map[string]bool, existing map[string]uint64, orphanKeys []string,
) {
	if shrinkGuardFires(seen, existing) {
		slog.Warn("indexRepo: orphan-delete skipped (shrink guard)",
			slog.String("repo", repoKey),
			slog.Int("seen", len(seen)),
			slog.Int("existing", len(existing)))
		orphanDeleteSkippedTotal.WithLabelValues("shrink_guard").Inc()
		return
	}

	orphansDeleted, orphanErr := store.DeleteExplicitOrphans(ctx, repoKey, orphanKeys)
	if orphanErr != nil {
		slog.Warn("indexRepo: intra-key orphan delete failed",
			slog.String("repo", repoKey),
			slog.Any("error", orphanErr))
		orphanDeleteSkippedTotal.WithLabelValues("error").Inc()
		return
	}
	if orphansDeleted > 0 {
		indexOrphansDeletedTotal.Add(float64(orphansDeleted))
		slog.Info("indexRepo: intra-key orphans deleted",
			slog.String("repo", repoKey),
			slog.Int64("deleted", orphansDeleted))
	}
}

// embedChunks processes toEmbed in indexChunkSize batches, writing dense (and
// sparse when configured) vectors to the store. Updates result.Indexed and prog
// as each chunk completes. Returns the first error encountered.
//
// Observability: each successfully committed chunk logs at Info so mid-run stalls
// are visible in the log stream ("chunk 1 committed, chunk 2 never logged"). On any
// error, if ≥1 chunk was already committed (rowsWritten > 0), RecordIndexPartialAbort
// is bumped — this is the "0→100→0→100" churn signature: rows survive but SHA is
// frozen because indexRepo only advances SHA on full success.
//
// tool is threaded through for attributable cancel accounting; pass "unknown" when
// the triggering tool is not available (legacy callers and non-async paths).
func (p *Pipeline) embedChunks(ctx context.Context, repoKey, tool string, toEmbed []symbolEntry, result *IndexResult, prog *indexProgress) error {
	chunkIdx := 0
	for start := 0; start < len(toEmbed); start += indexChunkSize {
		if ctx.Err() != nil {
			RecordIndexCancelled(tool, "chunk_loop")
			if result.Indexed > 0 {
				RecordIndexPartialAbort(repoKey)
			}
			return ctx.Err()
		}
		end := min(start+indexChunkSize, len(toEmbed))
		chunkStart := time.Now()
		indexed, err := p.embedAndUpsert(ctx, repoKey, toEmbed[start:end])
		if err != nil {
			if isContextErr(err) {
				RecordIndexCancelled(tool, "chunk_loop")
			}
			if result.Indexed > 0 {
				RecordIndexPartialAbort(repoKey)
			}
			return err
		}
		result.Indexed += indexed
		slog.Info("index chunk committed",
			slog.String("repo", repoKey),
			slog.Int("chunk_idx", chunkIdx),
			slog.Int("rows", indexed),
			slog.Duration("elapsed", time.Since(chunkStart)),
		)
		chunkIdx++
		if prog != nil {
			atomic.StoreInt64(&prog.done, int64(end))
		}
	}
	return nil
}

// embedAndUpsert embeds a chunk of symbols and upserts them into the store.
//
// Dense path: always calls p.client.Embed and writes the dense vector. Failure is fatal.
// Sparse path: when p.sparseClient != nil, also calls embedSparseBatched then writes
// each row's sparse_embedding via a separate best-effort UPDATE — completely decoupled
// from the dense INSERT. A sparse UPDATE failure:
//   - does NOT roll back the dense row (the INSERT committed independently)
//   - logs at WARN
//   - bumps gocode_sparse_embed_failures_total{stage="write"}
//   - leaves that row's sparse_embedding NULL (Phase-5 IS NULL cursor retries later)
//   - NEVER propagates fatal to embedAndUpsert or IncrementalSync
//
// The dense vector is always persisted regardless of sparse outcome.
func (p *Pipeline) embedAndUpsert(
	ctx context.Context, repoKey string, chunk []symbolEntry,
) (int, error) {
	texts := make([]string, len(chunk))
	for i, e := range chunk {
		texts[i] = e.embedText
	}

	// 1. Dense embed (load-bearing; failure aborts the chunk).
	vectors, err := p.client.Embed(ctx, texts)
	if err != nil {
		return 0, fmt.Errorf("embed symbols: %w", err)
	}

	// 2. Sparse embed (additive; failure degrades ranking only, never fatal).
	//    sparseVecs[i] is zero-valued (→ NULL) when the sparse leg is disabled
	//    or fails, preserving byte-identical behaviour on the dense path.
	sparseVecs := make([]sparse.SparseVector, len(chunk)) // zero-valued = empty = NULL
	if p.sparseClient != nil {
		svecs, serr := embedSparseBatched(ctx, p.sparseClient, texts, p.sparseMaxBatch)
		if serr != nil {
			// Non-fatal: log once per chunk failure; counter already bumped inside
			// embedSparseBatched. Rows upserted with NULL sparse_embedding — the
			// Phase-5 backfill will fill them on the next resumable pass.
			slog.Warn("sparse embed failed; writing NULL sparse_embedding for chunk",
				slog.String("repo", repoKey),
				slog.Int("chunk_size", len(chunk)),
				slog.Any("error", serr))
		} else {
			sparseVecs = svecs
		}
	}

	records := make([]EmbeddingRecord, len(chunk))
	for i, e := range chunk {
		records[i] = EmbeddingRecord{
			RepoKey:         repoKey,
			FilePath:        e.file.RelPath,
			SymbolName:      e.sym.Name,
			SymbolKind:      string(e.sym.Kind),
			Language:        e.sym.Language,
			StartLine:       int(e.sym.StartLine),
			BodyHash:        e.hash,
			EmbedModel:      p.embedModel,
			Embedding:       vectors[i],
			SparseEmbedding: sparseVecs[i],
		}
	}

	// 3. Dense upsert — always the source of truth; sparse decoupled below.
	if err := p.store.Upsert(ctx, records); err != nil {
		return 0, fmt.Errorf("upsert embeddings: %w", err)
	}

	// 4. Sparse write — best-effort UPDATE per row, completely independent of step 3.
	//    A failure here never aborts the batch or blocks SHA advance.
	if p.sparseClient != nil {
		p.runSparseWrites(ctx, repoKey, records, sparseVecs)
	}

	return len(chunk), nil
}

// runSparseWrites accumulates all sanitized sparse vectors for a chunk into a
// single batch UPDATE (one round-trip per chunk instead of one per row).
// Failures bump the write counter by the batch's row count and log at WARN,
// but are never propagated — leaving rows with NULL sparse_embedding is correct
// (the Phase-5 IS NULL backfill cursor will retry them later).
//
// Dense-independence invariant: this function is called AFTER p.store.Upsert
// (step 3 in embedAndUpsert) has already committed the dense rows. A failure
// here leaves sparse_embedding NULL for those rows but never rolls back or
// blocks the dense INSERT.
//
// writeSparsesBatch is used instead of p.store.UpdateSparseEmbeddingsBatch
// directly so that tests can inject a spy without a real Postgres store.
func (p *Pipeline) runSparseWrites(ctx context.Context, repoKey string, records []EmbeddingRecord, sparseVecs []sparse.SparseVector) {
	var batch []SparseUpdate
	for i, r := range records {
		sv := sparseVecs[i]
		if sv.IsEmpty() {
			continue // NULL already in DB from INSERT default; skip UPDATE
		}
		lit := SanitizeAndFormatSparseVector(sv, sparseDim)
		if lit == "" {
			// Sanitized to nothing (all-zero / all-OOB); leave NULL.
			continue
		}
		batch = append(batch, SparseUpdate{
			RepoKey:    r.RepoKey,
			FilePath:   r.FilePath,
			SymbolName: r.SymbolName,
			Literal:    lit,
		})
	}
	if len(batch) == 0 {
		return
	}
	if werr := p.writeSparsesBatch(ctx, batch); werr != nil {
		sparseEmbedFailTotal.WithLabelValues("write").Add(float64(len(batch)))
		slog.Warn("sparse batch write failed; rows stay NULL",
			slog.String("repo", repoKey),
			slog.Int("rows", len(batch)),
			slog.Any("error", werr))
	}
}

// symbolEntry pairs a symbol with its source file, precomputed hash, and embed text.
type symbolEntry struct {
	sym       *parser.Symbol
	file      *ingest.File
	hash      uint64
	embedText string
}

// isContextErr reports whether err wraps context.Canceled or context.DeadlineExceeded.
// Used by embedChunks to detect when the embed client propagated a context error
// through multiple wrapping layers (e.g. "embed symbols: ... context canceled").
func isContextErr(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
