package main

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-kit/embed"
	"github.com/anatolykoptev/vaelor/internal/embeddings"
)

// --- #709: zero-results misclassification test doubles ---

// indexedStateSpy implements indexedStateReader with canned values, so the
// no-results branch can be driven end-to-end without a live Postgres pool.
type indexedStateSpy struct {
	storedSHA   string
	storedModel string
	embCount    int
	// call counters help assert whether the indexed-state check ran.
	getRepoStateCalls    int
	countEmbeddingsCalls int
}

func (s *indexedStateSpy) GetRepoState(_ context.Context, _ string) (string, error) {
	s.getRepoStateCalls++
	return s.storedSHA, nil
}

func (s *indexedStateSpy) GetStoredModel(_ context.Context, _ string) string {
	return s.storedModel
}

func (s *indexedStateSpy) CountEmbeddings(_ context.Context, _ string) (int, error) {
	s.countEmbeddingsCalls++
	return s.embCount, nil
}

// noResultGitRepo creates a throwaway git repo with one commit on main and
// returns its root + the main-branch tip SHA. Used so mcpmeta.MainBranchHeadSHA
// reads a real loose ref that can be matched against the spy's storedSHA.
func noResultGitRepo(t *testing.T) (dir, headSHA string) {
	t.Helper()
	dir = t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "t@t.t"},
		{"config", "user.name", "t"},
		{"commit", "--allow-empty", "-m", "init"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	out, err := exec.Command("git", "-C", dir, "rev-parse", "main").Output()
	if err != nil {
		t.Fatalf("rev-parse main: %v", err)
	}
	return dir, strings.TrimSpace(string(out))
}

// noResultTestDeps builds SemanticDeps wired with test doubles for the
// no-results branch: an empty storeSearcherSeam (zero vector hits), an
// indexedStateSeam spy, and a pipelineInvalidatorSpy to observe whether an
// index was scheduled. deps.Store is left nil so handleSemanticHits paths
// (not exercised here — zero results) skip Store-dependent calls cleanly.
func noResultTestDeps(state *indexedStateSpy, invalidator *pipelineInvalidatorSpy) SemanticDeps {
	return SemanticDeps{
		QueryClient:             queryEmbedderStub{},
		Client:                  &embed.Client{},
		storeSearcherSeam:       &storeStub{searchResults: nil}, // zero vector hits
		indexedStateSeam:        state,
		pipelineInvalidatorSeam: invalidator,
		RRFWeights:              embeddings.DefaultRRFWeights(),
	}
}

// --- tests ---

// TestHandleSemanticSearch_NoResults_Indexed_ReturnsNoMatch verifies the #709
// fix: when the vector search returns zero hits BUT the repo is genuinely
// indexed (code_repo_state row exists, head_sha matches the checkout's main
// tip, embed_model matches the active model, embeddings rows > 0), the handler
// must return an explicit no-match response and MUST NOT schedule a background
// index. A negative result is a legitimate answer, not "not ready yet".
//
// Drives the REAL production path: handleSemanticSearch with test seams
// (storeSearcherSeam=nil hits, indexedStateSeam, pipelineInvalidatorSeam).
//
// Anti-tautology (red-on-revert contract):
//   - Revert the indexed-state check (zero results ⇒ always schedule) →
//     status becomes "indexing" (not "no_match") AND indexAsyncCalled=true →
//     BOTH assertions FAIL. Verified in the anti-vacuity run.
func TestHandleSemanticSearch_NoResults_Indexed_ReturnsNoMatch(t *testing.T) {
	const activeModel = "code-rank-embed"

	dir, sha := noResultGitRepo(t)

	state := &indexedStateSpy{
		storedSHA:   sha,         // matches checkout main tip
		storedModel: activeModel, // matches active model
		embCount:    12007,       // populated index (issue's live measurement)
	}
	invalidator := &pipelineInvalidatorSpy{activeModel: activeModel}
	deps := noResultTestDeps(state, invalidator)

	res, err := handleSemanticSearch(context.Background(), SemanticSearchInput{
		Repo:  dir,
		Query: "LLM retry loop budget accounting for an agent conversation turn",
	}, deps, "")
	if err != nil {
		t.Fatalf("handleSemanticSearch returned error: %v", err)
	}
	if res == nil {
		t.Fatal("handleSemanticSearch returned nil result")
	}

	text := resultText(res)

	// (a) The response must be an explicit no-match, NOT an "indexing" promise.
	if !strings.Contains(text, "<status>no_match</status>") {
		t.Errorf("expected status 'no_match' for indexed repo with zero hits, got: %s", text)
	}
	if strings.Contains(text, "<status>indexing</status>") {
		t.Errorf("zero hits on an indexed repo returned 'indexing' (false promise — nothing to wait for): %s", text)
	}

	// (b) NO background index may be scheduled — the repo is already indexed.
	if invalidator.indexAsyncCalled {
		t.Error("IndexRepoAsyncWithTool was called on an indexed repo with zero hits: " +
			"wasted scheduling on a no-op pass (competes with the PSI memory-pressure guard)")
	}
}

// TestHandleSemanticSearch_NoResults_EmptyIndex_SchedulesIndex verifies the
// existing correct path is preserved: zero hits AND no embeddings rows ⇒ the
// repo is not indexed, so the "indexing started, retry" response is right and
// an index IS scheduled.
func TestHandleSemanticSearch_NoResults_EmptyIndex_SchedulesIndex(t *testing.T) {
	const activeModel = "code-rank-embed"

	dir, sha := noResultGitRepo(t)

	state := &indexedStateSpy{
		storedSHA:   sha, // SHA present but...
		storedModel: activeModel,
		embCount:    0, // ...no embeddings rows → not actually indexed
	}
	invalidator := &pipelineInvalidatorSpy{activeModel: activeModel}
	deps := noResultTestDeps(state, invalidator)

	res, err := handleSemanticSearch(context.Background(), SemanticSearchInput{
		Repo:  dir,
		Query: "something that matches nothing",
	}, deps, "")
	if err != nil {
		t.Fatalf("handleSemanticSearch returned error: %v", err)
	}
	if res == nil {
		t.Fatal("handleSemanticSearch returned nil result")
	}

	text := resultText(res)
	if !strings.Contains(text, "<status>indexing</status>") {
		t.Errorf("empty index: expected 'indexing' status, got: %s", text)
	}
	if !invalidator.indexAsyncCalled {
		t.Error("empty index: IndexRepoAsyncWithTool was NOT called — first index never triggered")
	}
}

// TestHandleSemanticSearch_NoResults_StaleSHA_SchedulesIndex verifies the
// existing correct path is preserved: zero hits with rows present BUT a stale
// head_sha (checkout moved past the index) ⇒ the repo needs re-indexing, so
// the "indexing started, retry" response is right and an index IS scheduled.
func TestHandleSemanticSearch_NoResults_StaleSHA_SchedulesIndex(t *testing.T) {
	const activeModel = "code-rank-embed"

	dir, _ := noResultGitRepo(t)

	state := &indexedStateSpy{
		storedSHA:   "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", // does NOT match checkout
		storedModel: activeModel,
		embCount:    12007, // rows present but stale
	}
	invalidator := &pipelineInvalidatorSpy{activeModel: activeModel}
	deps := noResultTestDeps(state, invalidator)

	res, err := handleSemanticSearch(context.Background(), SemanticSearchInput{
		Repo:  dir,
		Query: "something that matches nothing",
	}, deps, "")
	if err != nil {
		t.Fatalf("handleSemanticSearch returned error: %v", err)
	}
	if res == nil {
		t.Fatal("handleSemanticSearch returned nil result")
	}

	text := resultText(res)
	if !strings.Contains(text, "<status>indexing</status>") {
		t.Errorf("stale SHA: expected 'indexing' status, got: %s", text)
	}
	if !invalidator.indexAsyncCalled {
		t.Error("stale SHA: IndexRepoAsyncWithTool was NOT called — stale index never refreshed")
	}
}

// TestHandleSemanticSearch_NoResults_ModelChanged_SchedulesIndex verifies the
// model-mismatch arm of the indexed-state check: rows present, SHA matches, but
// the stored embed_model differs from the active model ⇒ vectors are in the
// wrong embedding space, so the repo is NOT considered indexed and an index IS
// scheduled (mirrors the stale-hit guard on the populated path).
func TestHandleSemanticSearch_NoResults_ModelChanged_SchedulesIndex(t *testing.T) {
	const (
		activeModel = "code-rank-embed"
		oldModel    = "jina-code-v2"
	)

	dir, sha := noResultGitRepo(t)

	state := &indexedStateSpy{
		storedSHA:   sha,      // SHA matches
		storedModel: oldModel, // model does NOT match
		embCount:    12007,
	}
	invalidator := &pipelineInvalidatorSpy{activeModel: activeModel}
	deps := noResultTestDeps(state, invalidator)

	res, err := handleSemanticSearch(context.Background(), SemanticSearchInput{
		Repo:  dir,
		Query: "something that matches nothing",
	}, deps, "")
	if err != nil {
		t.Fatalf("handleSemanticSearch returned error: %v", err)
	}
	if res == nil {
		t.Fatal("handleSemanticSearch returned nil result")
	}

	text := resultText(res)
	if !strings.Contains(text, "<status>indexing</status>") {
		t.Errorf("model changed: expected 'indexing' status, got: %s", text)
	}
	if !invalidator.indexAsyncCalled {
		t.Error("model changed: IndexRepoAsyncWithTool was NOT called — stale-space index never refreshed")
	}
}
