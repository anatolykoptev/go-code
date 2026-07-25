package main

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-kit/embed"
	"github.com/anatolykoptev/vaelor/internal/embeddings"
)

// --- #709: zero-results misclassification test doubles ---

// indexedStateSpy implements indexedStateReader with canned values, so the
// no-results branch can be driven end-to-end without a live Postgres pool.
// The *Err fields inject read failures so the cold-path collapse arms of
// repoIsIndexed (GetRepoState error, CountEmbeddings error) can be exercised;
// nil preserves the canned-value behaviour the sibling tests rely on.
type indexedStateSpy struct {
	storedSHA   string
	storedModel string
	embCount    int
	// Optional injected errors. nil ⇒ return the canned value above.
	getRepoStateErr    error
	countEmbeddingsErr error
	// call counters help assert whether the indexed-state check ran.
	getRepoStateCalls    int
	countEmbeddingsCalls int
}

func (s *indexedStateSpy) GetRepoState(_ context.Context, _ string) (string, error) {
	s.getRepoStateCalls++
	if s.getRepoStateErr != nil {
		// Return the canned SHA alongside the error so the cold-path
		// collapse is the ONLY thing guarding a false no_match: dropping
		// the `err != nil` check must let a non-empty SHA through and RED
		// the test (anti-vacuity).
		return s.storedSHA, s.getRepoStateErr
	}
	return s.storedSHA, nil
}

func (s *indexedStateSpy) GetStoredModel(_ context.Context, _ string) string {
	return s.storedModel
}

func (s *indexedStateSpy) CountEmbeddings(_ context.Context, _ string) (int, error) {
	s.countEmbeddingsCalls++
	if s.countEmbeddingsErr != nil {
		// Return the canned count alongside the error — same anti-vacuity
		// rationale as GetRepoState: dropping `err != nil` must let a >0
		// count through and RED the test.
		return s.embCount, s.countEmbeddingsErr
	}
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

// --- #709 cold-path collapse arms ---
//
// repoIsIndexed documents a cold-path guarantee: any read failure (no row, no
// git repo, transient DB error) collapses to false so the caller falls through
// to the indexing path rather than claiming a truthful no_match. The four
// tests above cover the happy/indexed arms; the three below cover each error
// collapse arm. An unreadable state must NEVER produce a no_match response —
// that would be the exact false-promise #709 was filed against.
//
// Anti-tautology (red-on-revert contract) — verified in the anti-vacuity run:
//   - Arm 1: drop the `err != nil` check on GetRepoState (return true on err)
//     ⇒ status becomes "no_match" AND indexAsyncCalled=false ⇒ BOTH assertions
//     FAIL.
//   - Arm 2: drop the `err != nil` check on MainBranchHeadSHA (return true on
//     err) ⇒ same.
//   - Arm 3: drop the `err != nil` check on CountEmbeddings (return true on
//     err) ⇒ same.

// TestHandleSemanticSearch_NoResults_GetRepoStateError_SchedulesIndex covers
// arm 1 of repoIsIndexed's cold-path collapse: GetRepoState returns an error
// (e.g. transient DB failure, connection reset). The repo must be treated as
// not-indexed — index scheduled, "indexing" status — never a no_match.
func TestHandleSemanticSearch_NoResults_GetRepoStateError_SchedulesIndex(t *testing.T) {
	const activeModel = "code-rank-embed"

	dir, sha := noResultGitRepo(t)

	state := &indexedStateSpy{
		storedSHA:       sha, // would match — but the read fails first
		storedModel:     activeModel,
		embCount:        12007,
		getRepoStateErr: errors.New("simulated connection reset: SQLSTATE 08006"),
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
	if strings.Contains(text, "<status>no_match</status>") {
		t.Error("GetRepoState error collapsed to no_match: " +
			"an unreadable state must route to indexing, not claim a truthful empty result")
	}
	if !strings.Contains(text, "<status>indexing</status>") {
		t.Errorf("GetRepoState error: expected 'indexing' status, got: %s", text)
	}
	if !invalidator.indexAsyncCalled {
		t.Error("GetRepoState error: IndexRepoAsyncWithTool was NOT called — " +
			"unreadable state left the repo un-indexed")
	}
}

// TestHandleSemanticSearch_NoResults_MainBranchHeadSHAError_SchedulesIndex
// covers arm 2 of repoIsIndexed's cold-path collapse: MainBranchHeadSHA fails
// because the root is not a git repo (no .git, no ref resolves). Uses the REAL
// mcpmeta.MainBranchHeadSHA against a bare TempDir — no stub — so the collapse
// is exercised on the same live-SHA reader WithFreshness uses.
func TestHandleSemanticSearch_NoResults_MainBranchHeadSHAError_SchedulesIndex(t *testing.T) {
	const activeModel = "code-rank-embed"

	// Bare TempDir: no .git, no refs ⇒ MainBranchHeadSHA returns an error.
	nonGitDir := t.TempDir()

	state := &indexedStateSpy{
		storedSHA:   "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", // present, but unreadable checkout
		storedModel: activeModel,
		embCount:    12007,
	}
	invalidator := &pipelineInvalidatorSpy{activeModel: activeModel}
	deps := noResultTestDeps(state, invalidator)

	res, err := handleSemanticSearch(context.Background(), SemanticSearchInput{
		Repo:  nonGitDir,
		Query: "something that matches nothing",
	}, deps, "")
	if err != nil {
		t.Fatalf("handleSemanticSearch returned error: %v", err)
	}
	if res == nil {
		t.Fatal("handleSemanticSearch returned nil result")
	}

	text := resultText(res)
	if strings.Contains(text, "<status>no_match</status>") {
		t.Error("MainBranchHeadSHA error collapsed to no_match: " +
			"an unreadable checkout must route to indexing, not claim a truthful empty result")
	}
	if !strings.Contains(text, "<status>indexing</status>") {
		t.Errorf("MainBranchHeadSHA error: expected 'indexing' status, got: %s", text)
	}
	if !invalidator.indexAsyncCalled {
		t.Error("MainBranchHeadSHA error: IndexRepoAsyncWithTool was NOT called — " +
			"unreadable checkout left the repo un-indexed")
	}
}

// TestHandleSemanticSearch_NoResults_CountEmbeddingsError_SchedulesIndex
// covers arm 3 of repoIsIndexed's cold-path collapse: CountEmbeddings returns
// an error (e.g. SQLSTATE 42P01 — code_embeddings table missing mid-migration,
// or a transient pool error). SHA matches, model matches, but the count read
// failed — the repo must be treated as not-indexed, never a no_match.
func TestHandleSemanticSearch_NoResults_CountEmbeddingsError_SchedulesIndex(t *testing.T) {
	const activeModel = "code-rank-embed"

	dir, sha := noResultGitRepo(t)

	state := &indexedStateSpy{
		storedSHA:          sha, // SHA matches
		storedModel:        activeModel,
		embCount:           12007, // would be >0 — but the read fails
		countEmbeddingsErr: errors.New("simulated SQLSTATE 42P01: relation code_embeddings does not exist"),
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
	if strings.Contains(text, "<status>no_match</status>") {
		t.Error("CountEmbeddings error collapsed to no_match: " +
			"an unreadable count must route to indexing, not claim a truthful empty result")
	}
	if !strings.Contains(text, "<status>indexing</status>") {
		t.Errorf("CountEmbeddings error: expected 'indexing' status, got: %s", text)
	}
	if !invalidator.indexAsyncCalled {
		t.Error("CountEmbeddings error: IndexRepoAsyncWithTool was NOT called — " +
			"unreadable count left the repo un-indexed")
	}
}
