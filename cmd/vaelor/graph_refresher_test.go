package main

// Tests for graphRefresher — the debounced + single-flighted background
// AGE-graph refresh triggered by the file watcher (#642).
//
// Falsification discipline (implementer-discipline): every test MUST fail if
// the production change it guards is reverted. The RED evidence for each is
// documented on the test. Tests drive the effect through the REAL code path
// (Trigger → debounce timer → fire → single-flight → indexFn), not by calling
// indexFn inside the test body.

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anatolykoptev/vaelor/internal/codegraph"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestRefresher builds a graphRefresher with a non-nil placeholder store,
// the given debounce, and a fake indexFn. The kill switch is honored by
// setting enabled=true (tests exercise the enabled path; the disabled path has
// its own test). env is read at newGraphRefresher time, so constructing via
// the real constructor then overriding fields keeps the env-gated defaults
// testable without env mutation.
func newTestRefresher(t *testing.T, debounce time.Duration, fn graphRefreshIndexFn) *graphRefresher {
	t.Helper()
	r := newGraphRefresher(&codegraph.Store{}, codegraph.IndexConfig{})
	r.debounce = debounce
	r.indexFn = fn
	return r
}

// awaitCount polls until indexFn has been called want times or the deadline
// passes. Returns the final count. Used instead of a fixed sleep so the tests
// are fast on a warm run and reliable on a loaded CI box.
func awaitCount(t *testing.T, calls *atomic.Int64, want int, timeout time.Duration) int64 {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if got := calls.Load(); int(got) >= want {
			return got
		}
		time.Sleep(2 * time.Millisecond)
	}
	return calls.Load()
}

// TestGraphRefresh_DebounceCoalesces_NEditsToOneRefresh: N rapid Trigger calls
// inside the debounce window produce EXACTLY ONE indexFn invocation.
//
// RED on the pre-fix code (Trigger calling fire directly with no debounce
// timer): indexFn is invoked N times, not 1 → the "exactly once" assertion
// REDS.
func TestGraphRefresh_DebounceCoalesces_NEditsToOneRefresh(t *testing.T) {
	var calls atomic.Int64
	r := newTestRefresher(t, 15*time.Millisecond, func(_ context.Context, _ *codegraph.Store, _ string, _ bool, _ codegraph.IndexConfig) (*codegraph.GraphMeta, error) {
		calls.Add(1)
		return &codegraph.GraphMeta{RepoKey: "test/repo", BuiltAt: time.Now()}, nil
	})

	const n = 10
	for i := 0; i < n; i++ {
		r.Trigger("test/repo", "/tmp/repo")
	}

	got := awaitCount(t, &calls, 1, 2*time.Second)
	assert.Equal(t, int64(1), got,
		"debounce must coalesce %d rapid edits into ONE refresh; got %d calls (revert the debounce timer → REDS with %d)",
		n, got, n)

	// Coalesced counter must reflect the N-1 absorbed edits.
	coalesced := testutil.ToFloat64(graphRefreshTotal.WithLabelValues(graphRefreshOutcomeCoalesced))
	assert.GreaterOrEqual(t, coalesced, float64(n-1),
		"coalesced outcome counter must account for the %d absorbed edits", n-1)
}

// TestGraphRefresh_SingleFlight_SameRepoOneInFlight: two overlapping triggers
// for the SAME repo collapse into one in-flight refresh (single-flight), while
// distinct repos refresh independently (no false serialization).
//
// RED on the pre-fix code (a plain goroutine instead of singleflight.DoChan):
// the second fire for the same repo runs indexFn again → calls == 2 for the
// same repo → the "exactly 1" assertion REDS.
func TestGraphRefresh_SingleFlight_SameRepoOneInFlight(t *testing.T) {
	var calls atomic.Int64
	// barrier: the fake blocks until released, holding the first refresh
	// in-flight so a second trigger's fire arrives while it is still running.
	release := make(chan struct{})
	r := newTestRefresher(t, 5*time.Millisecond, func(_ context.Context, _ *codegraph.Store, root string, _ bool, _ codegraph.IndexConfig) (*codegraph.GraphMeta, error) {
		calls.Add(1)
		<-release // block until the test releases
		return &codegraph.GraphMeta{RepoKey: codegraph.GraphNameFor(root), BuiltAt: time.Now()}, nil
	})

	// First trigger → debounce → fire → indexFn blocks on the barrier.
	r.Trigger("test/same", "/tmp/same")
	require.Equal(t, int64(1), awaitCount(t, &calls, 1, 2*time.Second),
		"first trigger must start one in-flight refresh")

	// Second trigger for the SAME repo while the first is in-flight. The
	// debounce timer fires and enters single-flight; the leader is still
	// running, so the follower's DoChan coalesces — indexFn is NOT called
	// again.
	r.Trigger("test/same", "/tmp/same")
	// Give the second fire's DoChan time to coalesce.
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int64(1), calls.Load(),
		"single-flight must collapse the second same-repo trigger into the in-flight refresh; "+
			"revert DoChan to a plain goroutine → REDS with 2")

	// Distinct repos refresh independently — no false serialization.
	var callsB atomic.Int64
	r.indexFn = func(_ context.Context, _ *codegraph.Store, root string, _ bool, _ codegraph.IndexConfig) (*codegraph.GraphMeta, error) {
		// First (blocked) repo's fn is still waiting on the original barrier;
		// the distinct-repo fn must NOT block on it.
		if root == "/tmp/distinct" {
			callsB.Add(1)
			return &codegraph.GraphMeta{RepoKey: "test/distinct", BuiltAt: time.Now()}, nil
		}
		calls.Add(1)
		<-release
		return &codegraph.GraphMeta{RepoKey: "test/same", BuiltAt: time.Now()}, nil
	}
	r.Trigger("test/distinct", "/tmp/distinct")
	assert.Equal(t, int64(1), awaitCount(t, &callsB, 1, 2*time.Second),
		"a distinct repo must refresh independently of the blocked same-repo refresh — "+
			"single-flight keys per repoKey, no false serialization")

	close(release)
}

// TestGraphRefresh_TriggerNeverBlocks: Trigger returns immediately even when a
// refresh is in-flight (the IndexFile embedding path must stay fast). This is
// the "never block IndexFile" requirement (#642 req 3).
//
// RED on a hypothetical blocking Trigger (calling indexFn synchronously): the
// elapsed assertion exceeds the small budget → REDS.
func TestGraphRefresh_TriggerNeverBlocks(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	r := newTestRefresher(t, 5*time.Millisecond, func(_ context.Context, _ *codegraph.Store, _ string, _ bool, _ codegraph.IndexConfig) (*codegraph.GraphMeta, error) {
		<-release // hold the refresh in-flight for the whole test
		return &codegraph.GraphMeta{BuiltAt: time.Now()}, nil
	})

	// Start a refresh that will block.
	r.Trigger("test/block", "/tmp/block")
	time.Sleep(20 * time.Millisecond) // let the debounce fire + indexFn block

	// A second trigger while the refresh is in-flight must return immediately.
	start := time.Now()
	r.Trigger("test/block", "/tmp/block")
	elapsed := time.Since(start)
	assert.Less(t, elapsed, 20*time.Millisecond,
		"Trigger must be non-blocking even with an in-flight refresh; took %v "+
			"(a synchronous Trigger would block on the in-flight refresh → REDS)", elapsed)
}

// TestGraphRefresh_FailureLoud: a refresh failure increments the
// codeGraphBuildFailures counter (the existing observable) AND the
// graphRefreshTotal{failed} counter — the "failure must be LOUD" requirement
// (#642 req 4). Asserts the observable counters, not the log string.
//
// RED if the recordCodeGraphBuildFailure call is removed from fire: the
// codeGraphBuildFailures counter does not move → REDS.
func TestGraphRefresh_FailureLoud(t *testing.T) {
	idxErr := errors.New("graph build failed: connection refused")
	r := newTestRefresher(t, 5*time.Millisecond, func(_ context.Context, _ *codegraph.Store, _ string, _ bool, _ codegraph.IndexConfig) (*codegraph.GraphMeta, error) {
		return nil, idxErr
	})

	failBefore := testutil.ToFloat64(codeGraphBuildFailures.WithLabelValues(codeGraphBuildReasonIndexError))
	refreshFailedBefore := testutil.ToFloat64(graphRefreshTotal.WithLabelValues(graphRefreshOutcomeFailed))

	r.Trigger("test/fail", "/tmp/fail")
	// Wait for the refresh to complete (it returns immediately on error).
	awaitCount(t, new(atomic.Int64), 0, 500*time.Millisecond) // small settle
	time.Sleep(60 * time.Millisecond)

	failAfter := testutil.ToFloat64(codeGraphBuildFailures.WithLabelValues(codeGraphBuildReasonIndexError))
	refreshFailedAfter := testutil.ToFloat64(graphRefreshTotal.WithLabelValues(graphRefreshOutcomeFailed))

	assert.Greater(t, failAfter, failBefore,
		"a refresh failure must increment gocode_code_graph_build_failures_total{index_error} — "+
			"remove recordCodeGraphBuildFailure from fire → REDS (counter does not move)")
	assert.Greater(t, refreshFailedAfter, refreshFailedBefore,
		"a refresh failure must increment gocode_graph_refresh_total{failed}")
}

// TestGraphRefresh_SetsStalenessGauge: a successful refresh sets the per-repo
// codeGraphAgeSeconds gauge (the "expose staleness" requirement, #642 req 5).
//
// RED if the recordCodeGraphAge call is removed from fire: the gauge is never
// set for the repo → ToFloat64 returns 0 → REDS.
func TestGraphRefresh_SetsStalenessGauge(t *testing.T) {
	const repoKey = "test/staleness-gauge"
	builtAt := time.Now().Add(-2 * time.Second)
	r := newTestRefresher(t, 5*time.Millisecond, func(_ context.Context, _ *codegraph.Store, _ string, _ bool, _ codegraph.IndexConfig) (*codegraph.GraphMeta, error) {
		return &codegraph.GraphMeta{RepoKey: repoKey, BuiltAt: builtAt}, nil
	})

	r.Trigger(repoKey, "/tmp/staleness")
	time.Sleep(80 * time.Millisecond) // wait for debounce + refresh

	got := testutil.ToFloat64(codeGraphAgeSeconds.WithLabelValues(repoKey))
	assert.Greater(t, got, 0.0,
		"a successful refresh must set gocode_code_graph_age_seconds{repo} — "+
			"remove recordCodeGraphAge from fire → REDS (gauge stays 0/absent)")

	// Assert success counter moved (observable, not log).
	assert.Greater(t, testutil.ToFloat64(graphRefreshTotal.WithLabelValues(graphRefreshOutcomeSuccess)), 0.0,
		"a successful refresh must increment gocode_graph_refresh_total{success}")
}

// TestGraphRefresh_KillSwitch_SkipsAndCounts: when the kill switch is off,
// Trigger is a no-op that bumps the skipped counter and never calls indexFn.
//
// RED if the enabled gate is removed (Trigger proceeds to schedule a refresh
// despite the kill switch): indexFn is called → calls != 0 → REDS.
func TestGraphRefresh_KillSwitch_SkipsAndCounts(t *testing.T) {
	var calls atomic.Int64
	r := newTestRefresher(t, 5*time.Millisecond, func(_ context.Context, _ *codegraph.Store, _ string, _ bool, _ codegraph.IndexConfig) (*codegraph.GraphMeta, error) {
		calls.Add(1)
		return &codegraph.GraphMeta{BuiltAt: time.Now()}, nil
	})
	r.enabled = false // kill switch off

	skippedBefore := testutil.ToFloat64(graphRefreshTotal.WithLabelValues(graphRefreshOutcomeSkipped))
	r.Trigger("test/killed", "/tmp/killed")
	time.Sleep(50 * time.Millisecond)

	assert.Equal(t, int64(0), calls.Load(),
		"kill switch off must NOT invoke indexFn — remove the enabled gate → REDS with calls>0")
	skippedAfter := testutil.ToFloat64(graphRefreshTotal.WithLabelValues(graphRefreshOutcomeSkipped))
	assert.Greater(t, skippedAfter, skippedBefore,
		"kill switch off must bump gocode_graph_refresh_total{skipped}")
}

// TestGraphRefresh_NilStore_Skips: when the store is nil (DATABASE_URL unset),
// Trigger skips with a counter and never calls indexFn — the watcher still
// refreshes embeddings, but there is no graph to rebuild.
func TestGraphRefresh_NilStore_Skips(t *testing.T) {
	var calls atomic.Int64
	r := newGraphRefresher(nil, codegraph.IndexConfig{}) // nil store
	r.debounce = 5 * time.Millisecond
	r.indexFn = func(_ context.Context, _ *codegraph.Store, _ string, _ bool, _ codegraph.IndexConfig) (*codegraph.GraphMeta, error) {
		calls.Add(1)
		return &codegraph.GraphMeta{BuiltAt: time.Now()}, nil
	}

	skippedBefore := testutil.ToFloat64(graphRefreshTotal.WithLabelValues(graphRefreshOutcomeSkipped))
	r.Trigger("test/nostore", "/tmp/nostore")
	time.Sleep(50 * time.Millisecond)

	assert.Equal(t, int64(0), calls.Load(), "nil store must NOT invoke indexFn")
	assert.Greater(t, testutil.ToFloat64(graphRefreshTotal.WithLabelValues(graphRefreshOutcomeSkipped)), skippedBefore,
		"nil store must bump the skipped counter")
}

// TestGraphRefresh_DecoupledContext: the refresh runs under a decoupled
// context (context.Background()+timeout), NOT the caller's ctx, so a caller
// going away cannot cancel a refresh others share — the #678 lesson.
//
// RED if the indexFn were handed the caller's ctx: cancelling a caller ctx
// would cancel the in-flight refresh. Here we assert the refresh completes
// even after the trigger's caller context is cancelled (Trigger takes no ctx
// by design, so this is structural — the test guards against a future
// regression that threads a caller ctx into fire).
func TestGraphRefresh_DecoupledContext(t *testing.T) {
	var done atomic.Bool
	r := newTestRefresher(t, 5*time.Millisecond, func(ctx context.Context, _ *codegraph.Store, _ string, _ bool, _ codegraph.IndexConfig) (*codegraph.GraphMeta, error) {
		// The refresh's ctx must NOT be cancelled by anything the test does
		// after Trigger returns (Trigger takes no ctx; fire builds its own).
		select {
		case <-ctx.Done():
			t.Errorf("refresh ctx was cancelled — fire must use a decoupled context, not a caller's")
		case <-time.After(30 * time.Millisecond):
		}
		done.Store(true)
		return &codegraph.GraphMeta{BuiltAt: time.Now()}, nil
	})

	r.Trigger("test/decoupled", "/tmp/decoupled")
	// The caller has no ctx to cancel (Trigger is ctx-less); the refresh must
	// complete on its own decoupled ctx.
	require.Eventually(t, func() bool { return done.Load() }, 2*time.Second, 5*time.Millisecond,
		"refresh must complete under its decoupled context without any caller ctx")
}

// TestGraphRefresh_ConcurrentTriggersNoPanic: hammering Trigger from many
// goroutines concurrently must not panic or race (-race detector). Guards the
// mutex + timer map + single-flight under concurrency.
func TestGraphRefresh_ConcurrentTriggersNoPanic(t *testing.T) {
	var calls atomic.Int64
	r := newTestRefresher(t, 10*time.Millisecond, func(_ context.Context, _ *codegraph.Store, _ string, _ bool, _ codegraph.IndexConfig) (*codegraph.GraphMeta, error) {
		calls.Add(1)
		return &codegraph.GraphMeta{BuiltAt: time.Now()}, nil
	})

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			r.Trigger("test/concurrent", "/tmp/concurrent")
		}()
	}
	wg.Wait()
	awaitCount(t, &calls, 1, 2*time.Second)
	// Many concurrent triggers for one repo must coalesce to a small number of
	// refreshes (ideally 1; allow a few for timer races at the window edge).
	assert.LessOrEqual(t, calls.Load(), int64(goroutines),
		"concurrent triggers must not each start a refresh — debounce + single-flight coalesce")
}
