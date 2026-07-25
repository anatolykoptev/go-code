package main

import (
	"context"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-kit/embed"
	"github.com/anatolykoptev/vaelor/internal/embeddings"
)

// --- test doubles ---

// modelCheckerSpy implements modelChecker with an injectable stored model.
type modelCheckerSpy struct {
	storedModel string
}

func (s *modelCheckerSpy) GetStoredModel(_ context.Context, _ string) string {
	return s.storedModel
}

// pipelineInvalidatorSpy implements pipelineInvalidator and records all calls.
// invalidateCalled tracks whether InvalidateIfModelChanged was called;
// indexAsyncCalled tracks whether IndexRepoAsyncWithTool was called.
// activeModel controls what EmbedModel() returns.
type pipelineInvalidatorSpy struct {
	activeModel       string
	invalidateCalled  bool
	indexAsyncCalled  bool
	indexAsyncTool    string
	isIndexingRunning bool
	progressDone      int
	progressTotal     int
}

func (s *pipelineInvalidatorSpy) EmbedModel() string { return s.activeModel }

func (s *pipelineInvalidatorSpy) InvalidateIfModelChanged(_ context.Context, _ string) bool {
	s.invalidateCalled = true
	return true
}

func (s *pipelineInvalidatorSpy) IsIndexing(_ string) bool { return s.isIndexingRunning }

func (s *pipelineInvalidatorSpy) IndexRepoAsyncWithTool(tool, _, _ string) bool {
	s.indexAsyncCalled = true
	s.indexAsyncTool = tool
	return true
}

func (s *pipelineInvalidatorSpy) IndexProgress(_ string) (done, total int, running bool) {
	return s.progressDone, s.progressTotal, s.isIndexingRunning
}

// queryEmbedderStub returns a fixed zero vector for any query.
// Satisfies embeddings.QueryEmbedder without a live embed-server.
type queryEmbedderStub struct{}

func (queryEmbedderStub) EmbedQuery(_ context.Context, _ string) ([]float32, error) {
	return make([]float32, 768), nil
}

// storeStub implements vectorSearcher with a fixed result slice (simulating
// stale-space hits from the old model). Used as the storeSearcherSeam so
// handleSemanticSearch can run end-to-end without a live Postgres pool.
type storeStub struct {
	searchResults []embeddings.SearchResult
}

func (s *storeStub) Search(_ context.Context, _ []float32, _ embeddings.SearchOpts) ([]embeddings.SearchResult, error) {
	return s.searchResults, nil
}

// --- tests ---

// staleTestDeps builds a SemanticDeps wired with test doubles sufficient to drive
// handleSemanticSearch end-to-end through the stale-hit guard, without a live
// embed-server or Postgres pool. The storeSearcherSeam intercepts Store.Search,
// staleModelChecker intercepts GetStoredModel, and pipelineInvalidatorSeam
// intercepts the pipeline operations. deps.Store is left nil so handleSemanticHits
// (the post-guard path) skips all Store-dependent calls cleanly.
func staleTestDeps(checker modelChecker, invalidator *pipelineInvalidatorSpy, hits []embeddings.SearchResult) SemanticDeps {
	return SemanticDeps{
		QueryClient:             queryEmbedderStub{},
		Client:                  &embed.Client{},
		storeSearcherSeam:       &storeStub{searchResults: hits},
		staleModelChecker:       checker,
		pipelineInvalidatorSeam: invalidator,
		RRFWeights:              embeddings.DefaultRRFWeights(),
	}
}

// TestHandleSemanticSearch_StaleSpaceHit_TriggersReindex verifies the MAJOR bug fix:
// when semantic_search returns non-empty results from a repo whose stored embed_model
// differs from the active model (stale-space hit), the handler must:
//
//	(a) NOT return the mixed-space rows to the caller,
//	(b) call InvalidateIfModelChanged to atomically purge stale vectors, and
//	(c) call IndexRepoAsyncWithTool to start a fresh reindex.
//
// This test drives the REAL production path: handleSemanticSearch is called directly
// with test seams (storeSearcherSeam, staleModelChecker, pipelineInvalidatorSeam).
// The assertion is anchored on the real CallToolResult output (status="indexing",
// message contains "model changed") and on the pipelineInvalidatorSpy flags.
//
// Anti-tautology (red-on-revert contract):
//   - Remove the stale-hit guard entirely → invalidateCalled stays false → FAIL.
//   - Return stale hits instead of discarding → result status != "indexing" → FAIL.
//   - Call guard but skip InvalidateIfModelChanged → invalidateCalled false → FAIL.
//   - Call guard but skip IndexRepoAsyncWithTool → indexAsyncCalled false → FAIL.
func TestHandleSemanticSearch_StaleSpaceHit_TriggersReindex(t *testing.T) {
	const (
		oldModel    = "jina-code-v2"
		activeModel = "code-rank-embed"
	)

	repoDir := t.TempDir()

	staleHits := []embeddings.SearchResult{
		{RepoKey: "testrepo/stale", FilePath: "pkg/foo.go", SymbolName: "Foo", Distance: 0.1},
		{RepoKey: "testrepo/stale", FilePath: "pkg/bar.go", SymbolName: "Bar", Distance: 0.2},
	}

	checker := &modelCheckerSpy{storedModel: oldModel}
	invalidator := &pipelineInvalidatorSpy{
		activeModel:       activeModel,
		isIndexingRunning: false, // reindex not yet started
	}
	deps := staleTestDeps(checker, invalidator, staleHits)

	res, err := handleSemanticSearch(context.Background(), SemanticSearchInput{
		Repo:  repoDir,
		Query: "function that validates JWT tokens",
	}, deps, "")
	if err != nil {
		t.Fatalf("handleSemanticSearch returned error: %v", err)
	}
	if res == nil {
		t.Fatal("handleSemanticSearch returned nil result")
	}

	// (a) The stale hits must NOT be returned to the caller — the guard discards
	// them and returns an "indexing" status response instead.
	text := resultText(res)
	if !strings.Contains(text, "<status>indexing</status>") {
		t.Errorf("expected status 'indexing' in response (stale hits discarded), got: %s", text)
	}
	if strings.Contains(text, "Foo") || strings.Contains(text, "Bar") {
		t.Errorf("stale-space hits leaked to caller (should have been discarded): %s", text)
	}

	// (b) InvalidateIfModelChanged must have been called to purge stale vectors.
	if !invalidator.invalidateCalled {
		t.Error("InvalidateIfModelChanged was NOT called on stale-hit: stale vectors NOT purged — mixed-space results would be returned to caller")
	}

	// (c) IndexRepoAsyncWithTool must have been called to start a fresh reindex.
	if !invalidator.indexAsyncCalled {
		t.Error("IndexRepoAsyncWithTool was NOT called on stale-hit: fresh reindex not triggered — index stays permanently stale (lazy-only-forever bug)")
	}
	if invalidator.indexAsyncTool != "semantic_search" {
		t.Errorf("IndexRepoAsyncWithTool tool attribution = %q, want %q", invalidator.indexAsyncTool, "semantic_search")
	}
}

// TestHandleSemanticSearch_StaleSpaceHit_AlreadyIndexing verifies that when a
// reindex is already in progress (IsIndexing=true), the guard reports progress
// and does NOT call IndexRepoAsyncWithTool a second time.
//
// Anti-tautology: if the IsIndexing check is removed, indexAsyncCalled becomes
// true even while indexing is running → duplicate indexing goroutine attempt.
func TestHandleSemanticSearch_StaleSpaceHit_AlreadyIndexing(t *testing.T) {
	const (
		oldModel    = "jina-code-v2"
		activeModel = "code-rank-embed"
	)

	repoDir := t.TempDir()

	staleHits := []embeddings.SearchResult{
		{RepoKey: "testrepo/stale", FilePath: "pkg/foo.go", SymbolName: "Foo", Distance: 0.1},
	}

	checker := &modelCheckerSpy{storedModel: oldModel}
	invalidator := &pipelineInvalidatorSpy{
		activeModel:       activeModel,
		isIndexingRunning: true, // reindex already in progress
		progressDone:      42,
		progressTotal:     200,
	}
	deps := staleTestDeps(checker, invalidator, staleHits)

	res, err := handleSemanticSearch(context.Background(), SemanticSearchInput{
		Repo:  repoDir,
		Query: "function that validates JWT tokens",
	}, deps, "")
	if err != nil {
		t.Fatalf("handleSemanticSearch returned error: %v", err)
	}
	if res == nil {
		t.Fatal("handleSemanticSearch returned nil result")
	}

	text := resultText(res)
	if !strings.Contains(text, "<status>indexing</status>") {
		t.Errorf("expected status 'indexing' in response, got: %s", text)
	}
	if !strings.Contains(text, "42/200") {
		t.Errorf("expected progress '42/200' in indexing message, got: %s", text)
	}

	// InvalidateIfModelChanged is called (purge stale vectors) even while indexing.
	if !invalidator.invalidateCalled {
		t.Error("InvalidateIfModelChanged was NOT called on stale-hit even while indexing: stale vectors not purged")
	}
	// IndexRepoAsyncWithTool must NOT be called — reindex already in progress.
	if invalidator.indexAsyncCalled {
		t.Error("IndexRepoAsyncWithTool called while indexing already in progress: duplicate goroutine spawn")
	}
}

// TestHandleSemanticSearch_ModelMatch_DoesNotDiscard verifies the steady-state
// correctness: when stored model == active model, the guard is a no-op and valid
// search results are NOT discarded — they flow through to the caller.
//
// Anti-tautology: if the guard fires unconditionally (ignoring model equality),
// invalidateCalled becomes true and the response status becomes "indexing"
// instead of returning the actual results → this test fails on both assertions.
func TestHandleSemanticSearch_ModelMatch_DoesNotDiscard(t *testing.T) {
	const activeModel = "code-rank-embed"

	repoDir := t.TempDir()

	validHits := []embeddings.SearchResult{
		{RepoKey: "testrepo/fresh", FilePath: "pkg/foo.go", SymbolName: "Foo", Distance: 0.1},
	}

	checker := &modelCheckerSpy{storedModel: activeModel} // same as active
	invalidator := &pipelineInvalidatorSpy{activeModel: activeModel}
	deps := staleTestDeps(checker, invalidator, validHits)

	res, err := handleSemanticSearch(context.Background(), SemanticSearchInput{
		Repo:  repoDir,
		Query: "function that validates JWT tokens",
	}, deps, "")
	if err != nil {
		t.Fatalf("handleSemanticSearch returned error: %v", err)
	}
	if res == nil {
		t.Fatal("handleSemanticSearch returned nil result")
	}

	// The guard must NOT have fired — results flow through to the caller.
	if invalidator.invalidateCalled {
		t.Error("InvalidateIfModelChanged called when models match: guard has a false positive — valid hits are being discarded")
	}
	if invalidator.indexAsyncCalled {
		t.Error("IndexRepoAsyncWithTool called when models match: spurious reindex on every query")
	}

	// The response should contain the actual search result, not an "indexing" status.
	text := resultText(res)
	if strings.Contains(text, "<status>indexing</status>") {
		t.Errorf("guard fired on model-match: response is 'indexing' instead of returning valid results: %s", text)
	}
	if !strings.Contains(text, "Foo") {
		t.Errorf("valid search result 'Foo' not in response (should have been returned, not discarded): %s", text)
	}
}

// TestHandleSemanticSearch_NoStoredModel_PassesThrough verifies that when the
// repo has no code_repo_state row yet (first index, GetStoredModel returns ""),
// the guard is a no-op. A brand-new repo with no prior index MUST return its
// first results once indexed, not be immediately purged.
//
// Anti-tautology: if the "storedModel != "" guard is removed, "" != activeModel
// triggers an invalid purge for a freshly-indexed repo → invalidateCalled
// becomes true and the response becomes "indexing" → this test fails.
func TestHandleSemanticSearch_NoStoredModel_PassesThrough(t *testing.T) {
	const activeModel = "code-rank-embed"

	repoDir := t.TempDir()

	validHits := []embeddings.SearchResult{
		{RepoKey: "testrepo/new", FilePath: "pkg/baz.go", SymbolName: "Baz", Distance: 0.15},
	}

	checker := &modelCheckerSpy{storedModel: ""} // no prior index row
	invalidator := &pipelineInvalidatorSpy{activeModel: activeModel}
	deps := staleTestDeps(checker, invalidator, validHits)

	res, err := handleSemanticSearch(context.Background(), SemanticSearchInput{
		Repo:  repoDir,
		Query: "function that validates JWT tokens",
	}, deps, "")
	if err != nil {
		t.Fatalf("handleSemanticSearch returned error: %v", err)
	}
	if res == nil {
		t.Fatal("handleSemanticSearch returned nil result")
	}

	if invalidator.invalidateCalled {
		t.Error("InvalidateIfModelChanged called on empty stored model: new repos are being spuriously purged")
	}
	if invalidator.indexAsyncCalled {
		t.Error("IndexRepoAsyncWithTool called on empty stored model: spurious reindex for freshly-indexed repo")
	}

	text := resultText(res)
	if strings.Contains(text, "<status>indexing</status>") {
		t.Errorf("guard fired on empty stored model: response is 'indexing' instead of returning valid results: %s", text)
	}
	if !strings.Contains(text, "Baz") {
		t.Errorf("valid search result 'Baz' not in response (should have been returned, not discarded): %s", text)
	}
}

// perRowModelCheckerSpy satisfies both modelChecker and perRowModelChecker.
// It simulates an orphan repo: code_repo_state has no row (GetStoredModel → "")
// but code_embeddings still has rows from an old model (GetEmbedModelForRepo → old).
type perRowModelCheckerSpy struct {
	storedModel string // from code_repo_state (typically "" for orphans)
	perRowModel string // from code_embeddings rows
}

func (s *perRowModelCheckerSpy) GetStoredModel(_ context.Context, _ string) string {
	return s.storedModel
}

func (s *perRowModelCheckerSpy) GetEmbedModelForRepo(_ context.Context, _ string) string {
	return s.perRowModel
}

// TestHandleSemanticSearch_OrphanPerRowFallback verifies that when GetStoredModel
// returns "" (no code_repo_state row — orphan vectors from a removed checkout),
// the guard falls back to GetEmbedModelForRepo on the code_embeddings table.
// If that per-row model is stale (old model != active), the guard MUST fire,
// discarding the results and triggering reindex.
//
// This closes the blind spot identified in the 2026-06-13 incident: an orphan repo
// with no state row previously bypassed the stale-space guard entirely, silently
// returning jina-space rows against a code-rank query.
//
// This test drives the REAL production path: handleSemanticSearch is called directly
// with a perRowModelCheckerSpy wired as the staleModelChecker seam.
//
// Anti-tautology (red-on-revert contract):
//   - Remove the perRowModelChecker type assertion → storedModel stays "" → guard
//     not triggered → invalidateCalled stays false → FAIL.
//   - Remove the per-row fallback but keep the type assertion → same result → FAIL.
func TestHandleSemanticSearch_OrphanPerRowFallback_TriggersReindex(t *testing.T) {
	const (
		oldModel    = "jina-code-v2"
		activeModel = "code-rank-embed"
	)

	repoDir := t.TempDir()

	staleHits := []embeddings.SearchResult{
		{RepoKey: "testrepo/orphan", FilePath: "pkg/qux.go", SymbolName: "Qux", Distance: 0.1},
	}

	// Orphan checker: no state row, but old model visible in code_embeddings rows.
	checker := &perRowModelCheckerSpy{
		storedModel: "",       // no code_repo_state row (orphan)
		perRowModel: oldModel, // old vectors still in code_embeddings
	}
	invalidator := &pipelineInvalidatorSpy{
		activeModel:       activeModel,
		isIndexingRunning: false,
	}
	deps := staleTestDeps(checker, invalidator, staleHits)

	res, err := handleSemanticSearch(context.Background(), SemanticSearchInput{
		Repo:  repoDir,
		Query: "function that validates JWT tokens",
	}, deps, "")
	if err != nil {
		t.Fatalf("handleSemanticSearch returned error: %v", err)
	}
	if res == nil {
		t.Fatal("handleSemanticSearch returned nil result")
	}

	text := resultText(res)
	if !strings.Contains(text, "<status>indexing</status>") {
		t.Errorf("expected status 'indexing' in response (orphan stale hits discarded via per-row fallback), got: %s", text)
	}
	if strings.Contains(text, "Qux") {
		t.Errorf("stale orphan hits leaked to caller (should have been discarded): %s", text)
	}

	if !invalidator.invalidateCalled {
		t.Error("InvalidateIfModelChanged not called for orphan repo with stale per-row model: guard did not fire via per-row fallback")
	}
	if !invalidator.indexAsyncCalled {
		t.Error("IndexRepoAsyncWithTool not called for orphan repo with stale per-row model: reindex not triggered")
	}
	if invalidator.indexAsyncTool != "semantic_search" {
		t.Errorf("IndexRepoAsyncWithTool tool attribution = %q, want %q", invalidator.indexAsyncTool, "semantic_search")
	}
}
