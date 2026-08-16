package main

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anatolykoptev/vaelor/internal/codegraph"
	"github.com/anatolykoptev/vaelor/internal/embeddings"
	"github.com/anatolykoptev/vaelor/internal/mcpmeta"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// --- test doubles ---

// graphAgeStub is a configurable fake for the graphAgeFn seam. It returns a
// fixed (age, fresh, err) tuple and counts how many times it was called —
// the call count lets tests assert the short-TTL cache is working (one DB
// round-trip per 5s burst, not per query).
type graphAgeStub struct {
	age   time.Duration
	fresh bool
	err   error
	calls atomic.Int64
}

func (s *graphAgeStub) GraphAge(_ context.Context, _ *codegraph.Store, _ string) (time.Duration, bool, error) {
	s.calls.Add(1)
	return s.age, s.fresh, s.err
}

// --- helpers ---

// withGraphAgeSeam swaps the graphAgeFn package var and restores it on test
// cleanup. Every test that exercises the freshness gate MUST use this —
// calling the real codegraph.GraphAge would hit a live DB.
func withGraphAgeSeam(t *testing.T, stub *graphAgeStub) {
	t.Helper()
	orig := graphAgeFn
	graphAgeFn = stub.GraphAge
	t.Cleanup(func() { graphAgeFn = orig })
}

// withBuildSeams swaps the ageGraphIndexRepo + ageGraphMemGuardWatchdog seams
// so the self-heal background build is instrumented without a live AGE
// connection. Mirrors the pattern in age_graph_gate_test.go. Returns an
// atomic.Bool that is set true when the fake IndexRepo is entered.
func withBuildSeams(t *testing.T, indexFn func(context.Context, *codegraph.Store, string, bool, codegraph.IndexConfig) (*codegraph.GraphMeta, error)) *atomic.Bool {
	t.Helper()
	origIndex := ageGraphIndexRepo
	origMemGuard := ageGraphMemGuardWatchdog
	called := &atomic.Bool{}
	ageGraphIndexRepo = func(ctx context.Context, store *codegraph.Store, root string, isRemote bool, cfg codegraph.IndexConfig) (*codegraph.GraphMeta, error) {
		called.Store(true)
		return indexFn(ctx, store, root, isRemote, cfg)
	}
	ageGraphMemGuardWatchdog = func(context.Context, context.CancelFunc) {}
	t.Cleanup(func() {
		ageGraphIndexRepo = origIndex
		ageGraphMemGuardWatchdog = origMemGuard
	})
	return called
}

// --- tests ---

// TestGraphFreshness_Fresh_NoMarkerNoMetricNoRebuild is the byte-identical
// back-compat guard. When the graph is fresh, the retrieval gate must NOT:
//   - bump the stale-graph-retrievals counter,
//   - trigger a background rebuild.
//
// And gateRetrievalGraphFreshness must return 0 (no marker).
//
// Anti-tautology: revert the `if !stale { return 0 }` early return in
// gateRetrievalGraphFreshness → stale becomes true on every call → the counter
// bumps and the return is non-zero → this test fails on all three assertions.
func TestGraphFreshness_Fresh_NoMarkerNoMetricNoRebuild(t *testing.T) {
	stub := &graphAgeStub{age: 5 * time.Minute, fresh: true}
	withGraphAgeSeam(t, stub)

	buildCalled := withBuildSeams(t, func(_ context.Context, _ *codegraph.Store, _ string, _ bool, _ codegraph.IndexConfig) (*codegraph.GraphMeta, error) {
		t.Error("background rebuild triggered on fresh graph")
		return nil, nil
	})

	fusedBefore := testutil.ToFloat64(staleGraphRetrievals.WithLabelValues(staleGraphOutcomeFused))
	droppedBefore := testutil.ToFloat64(staleGraphRetrievals.WithLabelValues(staleGraphOutcomeDropped))

	store := &codegraph.Store{}
	repoDir := t.TempDir()
	repoKey := codegraph.GraphNameFor(repoDir)
	defer buildingRepos.Delete(repoKey)

	weights := embeddings.RRFWeights{Semantic: 1.0, Keyword: 0.5, Graph: 0.25, Hotspot: 0.15, Recency: 0.1}
	ageS := gateRetrievalGraphFreshness(
		context.Background(), store, repoDir, repoKey, false,
		codegraph.IndexConfig{}, 30*time.Minute, false, &weights,
	)

	// Return is 0 — no marker.
	if ageS != 0 {
		t.Errorf("fresh graph: ageS = %v, want 0 (no marker)", ageS)
	}

	// Weights unchanged — fresh path is byte-identical.
	if weights.Graph != 0.25 || weights.Hotspot != 0.15 {
		t.Errorf("fresh graph: weights modified, Graph=%v Hotspot=%v (want unchanged)", weights.Graph, weights.Hotspot)
	}

	// No stale-graph-retrievals counter bump.
	fusedAfter := testutil.ToFloat64(staleGraphRetrievals.WithLabelValues(staleGraphOutcomeFused))
	droppedAfter := testutil.ToFloat64(staleGraphRetrievals.WithLabelValues(staleGraphOutcomeDropped))
	if fusedAfter != fusedBefore {
		t.Errorf("fused counter bumped on fresh graph: before=%v after=%v", fusedBefore, fusedAfter)
	}
	if droppedAfter != droppedBefore {
		t.Errorf("dropped counter bumped on fresh graph: before=%v after=%v", droppedBefore, droppedAfter)
	}

	// No background rebuild triggered.
	if buildCalled.Load() {
		t.Errorf("background rebuild triggered on fresh graph, want none")
	}
}

// TestGraphFreshness_Stale_FlagOff_FusedMarkerMetricRebuild verifies that when
// the graph is stale and the drop flag is OFF:
//   - the stale-graph-retrievals{outcome="fused"} counter is bumped,
//   - gateRetrievalGraphFreshness returns a non-zero age (marker),
//   - a background rebuild is triggered,
//   - weights are NOT modified (flag off → stale hits still fused).
//
// Anti-tautology: revert the triggerBackgroundGraphBuild call in
// gateRetrievalGraphFreshness → buildCalled stays false → the rebuild
// assertion fails. Revert the `return ageS` to `return 0` → the marker
// assertion fails.
func TestGraphFreshness_Stale_FlagOff_FusedMarkerMetricRebuild(t *testing.T) {
	stub := &graphAgeStub{age: 2 * time.Hour, fresh: false}
	withGraphAgeSeam(t, stub)

	buildDone := &atomic.Bool{}
	withBuildSeams(t, func(_ context.Context, _ *codegraph.Store, _ string, _ bool, _ codegraph.IndexConfig) (*codegraph.GraphMeta, error) {
		buildDone.Store(true)
		return &codegraph.GraphMeta{BuiltAt: time.Now()}, nil
	})

	fusedBefore := testutil.ToFloat64(staleGraphRetrievals.WithLabelValues(staleGraphOutcomeFused))

	store := &codegraph.Store{}
	repoDir := t.TempDir()
	repoKey := codegraph.GraphNameFor(repoDir)
	defer buildingRepos.Delete(repoKey)

	weights := embeddings.RRFWeights{Semantic: 1.0, Keyword: 0.5, Graph: 0.25, Hotspot: 0.15, Recency: 0.1}
	ageS := gateRetrievalGraphFreshness(
		context.Background(), store, repoDir, repoKey, false,
		codegraph.IndexConfig{}, 30*time.Minute, false, &weights,
	)

	// Non-zero age returned (marker).
	if ageS <= 0 {
		t.Errorf("stale graph (flag off): ageS = %v, want > 0 (marker)", ageS)
	}
	// Age is approximately 2h in seconds.
	wantAge := (2 * time.Hour).Seconds()
	if ageS < wantAge*0.9 || ageS > wantAge*1.1 {
		t.Errorf("stale graph: ageS = %v, want ~%v", ageS, wantAge)
	}

	// Weights NOT modified (flag off).
	if weights.Graph != 0.25 || weights.Hotspot != 0.15 {
		t.Errorf("stale graph (flag off): weights modified, Graph=%v Hotspot=%v (want unchanged)", weights.Graph, weights.Hotspot)
	}

	// Fused counter bumped exactly once.
	fusedAfter := testutil.ToFloat64(staleGraphRetrievals.WithLabelValues(staleGraphOutcomeFused))
	if fusedAfter != fusedBefore+1 {
		t.Errorf("fused counter: before=%v after=%v, want before+1", fusedBefore, fusedAfter)
	}

	// Background rebuild triggered.
	if !waitForFlag(buildDone, 2*time.Second) {
		t.Fatal("background rebuild was not triggered on stale graph")
	}
}

// TestGraphFreshness_Stale_Dedup_ConcurrentSearches verifies that N concurrent
// calls against the same stale repo trigger the background rebuild exactly
// ONCE — the dedup via buildingRepos (sync.Map) is the single-flight seam
// shared with the tool gate. A second parallel staleness mechanism is a
// finding against the implementer; this test guards against that.
//
// Anti-tautology: if triggerBackgroundGraphBuild is replaced with a naive
// `go indexRepo(...)` without the buildingRepos.LoadOrStore dedup, N concurrent
// calls spawn N goroutines → buildCalls == N → this test fails.
func TestGraphFreshness_Stale_Dedup_ConcurrentSearches(t *testing.T) {
	stub := &graphAgeStub{age: 2 * time.Hour, fresh: false}
	withGraphAgeSeam(t, stub)

	// Barrier: the fake IndexRepo blocks until we release it, so all N
	// concurrent calls arrive while the first build is still in flight.
	buildStarted := make(chan struct{}, 1)
	releaseBuild := make(chan struct{})
	var buildCalls atomic.Int64
	withBuildSeams(t, func(_ context.Context, _ *codegraph.Store, _ string, _ bool, _ codegraph.IndexConfig) (*codegraph.GraphMeta, error) {
		buildCalls.Add(1)
		select {
		case buildStarted <- struct{}{}:
		default:
		}
		<-releaseBuild
		return &codegraph.GraphMeta{BuiltAt: time.Now()}, nil
	})

	store := &codegraph.Store{}
	repoDir := t.TempDir()
	repoKey := codegraph.GraphNameFor(repoDir)
	defer buildingRepos.Delete(repoKey)

	const n = 5
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			weights := embeddings.RRFWeights{Semantic: 1.0, Graph: 0.25, Hotspot: 0.15}
			gateRetrievalGraphFreshness(
				context.Background(), store, repoDir, repoKey, false,
				codegraph.IndexConfig{}, 30*time.Minute, false, &weights,
			)
		}()
	}

	// Wait for the first build to start, then give the other calls time
	// to arrive at the dedup gate.
	select {
	case <-buildStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("background rebuild never started")
	}
	time.Sleep(200 * time.Millisecond) // let the other N-1 calls hit the gate

	// Release the build so all goroutines can complete.
	close(releaseBuild)
	wg.Wait()

	if buildCalls.Load() != 1 {
		t.Errorf("background rebuild called %d times for %d concurrent calls, want 1 (dedup via buildingRepos)",
			buildCalls.Load(), n)
	}
}

// TestGraphFreshness_Stale_FlagOn_DropsArmsRenormalised verifies that when the
// graph is stale and the drop flag is ON:
//   - the graph+hotspot weights are zeroed,
//   - the remaining weights are renormalised (sum preserved),
//   - the dropped counter (not fused) is bumped,
//   - a background rebuild is still triggered (self-heal runs regardless).
//
// Anti-tautology: revert the `*weights = dropStaleGraphArms(*weights)` call
// in gateRetrievalGraphFreshness → weights stay unchanged → the Graph==0,
// Hotspot==0 assertions fail.
func TestGraphFreshness_Stale_FlagOn_DropsArmsRenormalised(t *testing.T) {
	stub := &graphAgeStub{age: 2 * time.Hour, fresh: false}
	withGraphAgeSeam(t, stub)

	buildDone := &atomic.Bool{}
	withBuildSeams(t, func(_ context.Context, _ *codegraph.Store, _ string, _ bool, _ codegraph.IndexConfig) (*codegraph.GraphMeta, error) {
		buildDone.Store(true)
		return &codegraph.GraphMeta{BuiltAt: time.Now()}, nil
	})

	droppedBefore := testutil.ToFloat64(staleGraphRetrievals.WithLabelValues(staleGraphOutcomeDropped))

	store := &codegraph.Store{}
	repoDir := t.TempDir()
	repoKey := codegraph.GraphNameFor(repoDir)
	defer buildingRepos.Delete(repoKey)

	weights := embeddings.RRFWeights{Semantic: 1.0, Keyword: 0.5, Graph: 0.25, Hotspot: 0.15, Recency: 0.1}
	origSum := weights.Semantic + weights.Keyword + weights.Sparse + weights.Graph + weights.Hotspot + weights.Recency

	ageS := gateRetrievalGraphFreshness(
		context.Background(), store, repoDir, repoKey, false,
		codegraph.IndexConfig{}, 30*time.Minute, true, &weights,
	)

	// Non-zero age returned (marker).
	if ageS <= 0 {
		t.Errorf("stale graph (flag on): ageS = %v, want > 0 (marker)", ageS)
	}

	// Graph + Hotspot zeroed.
	if weights.Graph != 0 {
		t.Errorf("stale graph (flag on): Graph = %v, want 0 (dropped)", weights.Graph)
	}
	if weights.Hotspot != 0 {
		t.Errorf("stale graph (flag on): Hotspot = %v, want 0 (dropped)", weights.Hotspot)
	}

	// Remaining weights renormalised — sum preserved.
	gotSum := weights.Semantic + weights.Keyword + weights.Sparse + weights.Recency
	const eps = 1e-9
	if d := gotSum - origSum; d > eps || d < -eps {
		t.Errorf("renormalised sum = %v, want %v (original sum preserved)", gotSum, origSum)
	}

	// Dropped counter bumped (not fused).
	droppedAfter := testutil.ToFloat64(staleGraphRetrievals.WithLabelValues(staleGraphOutcomeDropped))
	if droppedAfter != droppedBefore+1 {
		t.Errorf("dropped counter: before=%v after=%v, want before+1", droppedBefore, droppedAfter)
	}

	// Self-heal still runs regardless of the drop flag.
	if !waitForFlag(buildDone, 2*time.Second) {
		t.Fatal("self-heal rebuild not triggered on stale graph (flag on)")
	}
}

// TestDropStaleGraphArms_Renormalises verifies the weight math directly:
// graph+hotspot are zeroed, the remaining weights are scaled so the sum is
// preserved.
//
// Anti-tautology: remove the scale computation → remaining weights stay at
// their original values → the sum assertion fails (sum < originalSum because
// graph+hotspot were zeroed but the rest weren't scaled up).
func TestDropStaleGraphArms_Renormalises(t *testing.T) {
	orig := embeddings.RRFWeights{
		Semantic: 1.0, Keyword: 0.5, Sparse: 0.0, Graph: 0.25, Hotspot: 0.15, Recency: 0.1,
	}
	origSum := orig.Semantic + orig.Keyword + orig.Sparse + orig.Graph + orig.Hotspot + orig.Recency

	got := dropStaleGraphArms(orig)

	if got.Graph != 0 {
		t.Errorf("Graph weight = %v, want 0 (dropped)", got.Graph)
	}
	if got.Hotspot != 0 {
		t.Errorf("Hotspot weight = %v, want 0 (dropped)", got.Hotspot)
	}

	gotSum := got.Semantic + got.Keyword + got.Sparse + got.Recency
	if gotSum <= 0 {
		t.Fatalf("renormalised sum = %v, want > 0", gotSum)
	}
	// The sum of the remaining weights after renormalisation must equal the
	// original total sum (graph+hotspot weight is redistributed proportionally).
	const eps = 1e-9
	if abs := gotSum - origSum; abs > eps || abs < -eps {
		t.Errorf("renormalised sum = %v, want %v (original sum preserved)", gotSum, origSum)
	}

	// Relative proportions preserved: Semantic/Keyword ratio unchanged.
	if orig.Semantic > 0 && orig.Keyword > 0 {
		origRatio := orig.Semantic / orig.Keyword
		gotRatio := got.Semantic / got.Keyword
		if r := gotRatio - origRatio; r > eps || r < -eps {
			t.Errorf("Semantic/Keyword ratio changed: orig=%v, got=%v (renormalisation must preserve proportions)",
				origRatio, gotRatio)
		}
	}
}

// TestGraphFreshness_RebuildFailure_SearchStillAnswers verifies that when the
// self-heal background build fails, the gate still returns the age marker and
// the failure is counted (not swallowed silently). The search path itself
// never sees the failure — gateRetrievalGraphFreshness returns normally.
//
// Anti-tautology: if the build-failure path in triggerBackgroundGraphBuild
// panicked without recovery, the background goroutine would crash the test
// process. The recordCodeGraphBuildFailure call is verified via the
// gocode_code_graph_build_failures_total counter.
func TestGraphFreshness_RebuildFailure_SearchStillAnswers(t *testing.T) {
	stub := &graphAgeStub{age: 2 * time.Hour, fresh: false}
	withGraphAgeSeam(t, stub)

	buildDone := &atomic.Bool{}
	withBuildSeams(t, func(_ context.Context, _ *codegraph.Store, _ string, _ bool, _ codegraph.IndexConfig) (*codegraph.GraphMeta, error) {
		buildDone.Store(true)
		return nil, context.DeadlineExceeded
	})

	failuresBefore := testutil.ToFloat64(codeGraphBuildFailures.WithLabelValues(codeGraphBuildReasonCtxTimeout))

	store := &codegraph.Store{}
	repoDir := t.TempDir()
	repoKey := codegraph.GraphNameFor(repoDir)
	defer buildingRepos.Delete(repoKey)

	weights := embeddings.RRFWeights{Semantic: 1.0, Graph: 0.25, Hotspot: 0.15}
	ageS := gateRetrievalGraphFreshness(
		context.Background(), store, repoDir, repoKey, false,
		codegraph.IndexConfig{}, 30*time.Minute, false, &weights,
	)

	// The gate still returns the marker even when the build fails.
	if ageS <= 0 {
		t.Errorf("stale graph (build failure): ageS = %v, want > 0 (marker must be set regardless)", ageS)
	}

	// The build failure must be counted, not swallowed.
	if !waitForFlag(buildDone, 2*time.Second) {
		t.Fatal("self-heal build was not triggered")
	}
	// Give the background goroutine time to call recordCodeGraphBuildFailure
	// after the IndexRepo returns the error.
	time.Sleep(100 * time.Millisecond)
	failuresAfter := testutil.ToFloat64(codeGraphBuildFailures.WithLabelValues(codeGraphBuildReasonCtxTimeout))
	if failuresAfter <= failuresBefore {
		t.Errorf("build failure not counted: before=%v after=%v (failure swallowed silently)",
			failuresBefore, failuresAfter)
	}
}

// TestCheckRetrievalGraphFreshness_NoStore_NoCheck verifies the nil-store guard:
// when no GraphStore is configured, the freshness check is a no-op (not stale).
//
// Anti-tautology: remove the `if store == nil` guard → GraphAge is called with
// a nil store → it may panic or error → the test fails.
func TestCheckRetrievalGraphFreshness_NoStore_NoCheck(t *testing.T) {
	stub := &graphAgeStub{age: 100 * time.Hour, fresh: false}
	withGraphAgeSeam(t, stub)

	stale, age := checkRetrievalGraphFreshness(context.Background(), nil, "/test", 30*time.Minute)
	if stale {
		t.Errorf("nil store must return stale=false, got true")
	}
	if age != 0 {
		t.Errorf("nil store must return age=0, got %v", age)
	}
	if stub.calls.Load() != 0 {
		t.Errorf("nil store must not call GraphAge, got %d calls", stub.calls.Load())
	}
}

// TestFinalResult_GraphStaleAgeS_MarkerInResponse verifies that a non-zero
// graphStaleAgeS threads through finalResult into the response envelope as
// the graph_stale_age_s JSON field — the degradation marker a caller uses to
// tell it received ranking that blended an outdated graph.
//
// This tests the marker threading (handleSemanticHits → hybridResult/
// semanticOnlyResult → finalResult) at the finalResult level, avoiding the
// buildSignalHits DB dependency by calling finalResult directly with nil
// GraphStore deps.
//
// Anti-tautology: revert the `env.GraphStaleAgeS = graphStaleAgeS` assignment
// in finalResult → the marker disappears from the response → this test fails.
func TestFinalResult_GraphStaleAgeS_MarkerInResponse(t *testing.T) {
	deps := SemanticDeps{
		RRFWeights: embeddings.DefaultRRFWeights(),
	}
	input := SemanticSearchInput{
		Repo:  "/tmp/stale-marker-test",
		Query: "validate token",
	}
	results := []embeddings.SearchResult{
		{FilePath: "pkg/foo.go", SymbolName: "Foo", Distance: 0.3, Source: "semantic"},
	}

	// finalResult records the envelope into the slot; the wrapper renders it.
	// Seed the slot so recordEnvelope has somewhere to write, then drive the
	// real wrapper render (applyBudgetAndTook) to produce the footer.
	ctx := seedProvenanceSlot(context.Background())
	res, err := finalResult(ctx, input, deps, "stale-marker-test", "/tmp/stale-marker-test",
		results, nil, 10, 65813.0, "", time.Now())
	if err != nil {
		t.Fatalf("finalResult error: %v", err)
	}
	env := envelopeSnapshot(ctx)
	applyBudgetAndTook(res, 5*time.Millisecond, env)

	text := textContentOf(t, res)
	if !strings.Contains(text, "graph_stale_age_s") {
		t.Errorf("response must contain graph_stale_age_s marker when graphStaleAgeS > 0, got:\n%s", text)
	}
	if !strings.Contains(text, "65813") {
		t.Errorf("response must contain the age value 65813, got:\n%s", text)
	}
}

// TestFinalResult_ZeroGraphStaleAgeS_NoMarker verifies that a zero
// graphStaleAgeS does NOT emit the marker — the fresh path is byte-identical
// to pre-#691 behavior.
//
// Anti-tautology: remove the `if graphStaleAgeS > 0` guard in finalResult →
// env.GraphStaleAgeS is always set (to 0) → the omitempty hides it but
// appendMetaFooter's `env.GraphStaleAgeS == 0` check would still suppress the
// footer when there's no hint/warning. However, if the guard is removed AND
// the appendMetaFooter check is also removed, the footer appears with a 0
// value → this test catches that.
func TestFinalResult_ZeroGraphStaleAgeS_NoMarker(t *testing.T) {
	deps := SemanticDeps{
		RRFWeights: embeddings.DefaultRRFWeights(),
	}
	input := SemanticSearchInput{
		Repo:  "/tmp/fresh-marker-test",
		Query: "validate token",
	}
	results := []embeddings.SearchResult{
		{FilePath: "pkg/foo.go", SymbolName: "Foo", Distance: 0.3, Source: "semantic"},
	}

	ctx := seedProvenanceSlot(context.Background())
	res, err := finalResult(ctx, input, deps, "fresh-marker-test", "/tmp/fresh-marker-test",
		results, nil, 10, 0, "", time.Now())
	if err != nil {
		t.Fatalf("finalResult error: %v", err)
	}
	env := envelopeSnapshot(ctx)
	applyBudgetAndTook(res, 5*time.Millisecond, env)

	text := textContentOf(t, res)
	if strings.Contains(text, "graph_stale_age_s") {
		t.Errorf("fresh graph (ageS=0) must NOT emit graph_stale_age_s marker, got:\n%s", text)
	}
}

// TestAppendMetaFooter_GraphStaleAgeS_EmitsFooter verifies that
// appendMetaFooter emits the meta footer when GraphStaleAgeS is the only
// non-zero field (no hint, no stale warning). This is the #691 marker-only
// case — the footer must appear so the caller can see the degradation.
//
// Anti-tautology: revert the `env.GraphStaleAgeS == 0` addition to
// appendMetaFooter's guard → a GraphStaleAgeS-only envelope is treated as
// empty → no footer → the marker is lost → this test fails.
func TestAppendMetaFooter_GraphStaleAgeS_EmitsFooter(t *testing.T) {
	env := mcpmeta.Envelope{GraphStaleAgeS: 65813.0}
	got := appendMetaFooter("body", env)
	if !strings.Contains(got, "graph_stale_age_s") {
		t.Errorf("appendMetaFooter must emit footer for GraphStaleAgeS-only envelope, got:\n%s", got)
	}
	if !strings.Contains(got, "65813") {
		t.Errorf("appendMetaFooter must contain the age value, got:\n%s", got)
	}
}

// TestAppendMetaFooter_ZeroGraphStaleAgeS_NoFooter verifies that a zero
// GraphStaleAgeS with no hint and no stale warning produces NO footer —
// byte-identical to pre-#691.
func TestAppendMetaFooter_ZeroGraphStaleAgeS_NoFooter(t *testing.T) {
	env := mcpmeta.Envelope{}
	got := appendMetaFooter("body", env)
	if strings.Contains(got, "<!-- meta:") {
		t.Errorf("zero envelope must not emit footer, got:\n%s", got)
	}
}
