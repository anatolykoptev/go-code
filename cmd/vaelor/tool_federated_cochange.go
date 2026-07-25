package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/cache"
	"github.com/anatolykoptev/vaelor/internal/coupling"
	"github.com/anatolykoptev/vaelor/internal/federate"
	"github.com/anatolykoptev/vaelor/internal/mcpmeta"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	// federatedCoChangeDefaultWindowHours is the default time window for co-change correlation.
	federatedCoChangeDefaultWindowHours = 24
	// federatedCoChangeDefaultMinPairs is the default minimum co-occurrences to report a pair.
	federatedCoChangeDefaultMinPairs = 2

	// federatedCoChangeInlineBudget is the maximum wall-clock time the handler
	// spends in-request before returning a partial result and continuing in the
	// background.  Set conservatively below a typical MCP client deadline (~30s).
	federatedCoChangeInlineBudget = 20 * time.Second

	// federatedCoChangeRetryAfter is the suggested retry interval returned in a
	// partial/building response.
	federatedCoChangeRetryAfter = 30 // seconds

	// status string constants — hoisted to avoid goconst findings.
	fedStatusReady    = "ready"
	fedStatusPartial  = "partial"
	fedStatusBuilding = "building"
)

// FederatedCoChangeArgs is the input schema for the federated_cochange tool.
type FederatedCoChangeArgs struct {
	Repos       string  `json:"repos"                    jsonschema:"Repo pattern: 'all', a glob like 'acme-*', or a single repo name/absolute path"`
	WindowHours int     `json:"window_hours,omitempty"   jsonschema:"Co-change time window in hours (default 24)"`
	MinPairs    int     `json:"min_pairs,omitempty"      jsonschema:"Minimum co-occurrences to report a pair (default 2)"`
	MinLift     float64 `json:"min_lift,omitempty"       jsonschema:"Optional raw-lift pre-filter floor (default 0 = no filter). Ranking is by Wilson lower bound on directional confidence — not affected by min_lift. Raise min_pairs for higher-confidence pairs."`
}

// FederatedCoChangeResult is the JSON payload returned by the federated_cochange tool.
//
// Back-compat guarantee: Pairs is always a JSON array (never null); Status, PendingRepos,
// Progress, and RetryAfterSeconds are omitted when Status is "ready" (zero-value omitempty).
// Existing consumers that only read "pairs" continue to work unchanged.
type FederatedCoChangeResult struct {
	Pairs             []coupling.VerifiedPair `json:"pairs"`
	Status            string                  `json:"status,omitempty"`              // "ready" | "partial" | "building"
	PendingRepos      []string                `json:"pending_repos,omitempty"`       // repos not yet warm when status != "ready"
	Progress          string                  `json:"progress,omitempty"`            // e.g. "2/4 repos"
	RetryAfterSeconds int                     `json:"retry_after_seconds,omitempty"` // hint: call again after this many seconds
	Meta              mcpmeta.Envelope        `json:"_meta"`
}

// federatedCoChangeCacheEntry holds a completed result or an in-progress marker.
type federatedCoChangeCacheEntry struct {
	result *FederatedCoChangeResult // nil while in flight
	done   bool
	at     time.Time // when the entry was stored; used for TTL expiry
}

const (
	// federatedCoChangeCacheTTL matches internal/federate.touchesCacheTTL so
	// cached results age out together with the underlying touches data they
	// were derived from. Reuses the repo's existing TTL+LRU bounded-cache
	// idiom (same as touchesCache and callgraph.cgCache) — #608.
	federatedCoChangeCacheTTL = 10 * time.Minute
	// federatedCoChangeCacheMaxSize bounds the number of distinct query
	// combinations retained. The cache key includes localDirs + repos pattern
	// + window + minPairs + minLift; without a cap these accumulate
	// indefinitely on a long-running server (#608).
	federatedCoChangeCacheMaxSize = 32
)

// federatedCoChangeCache caches completed federated_cochange results keyed on
// canonical args (repos+window+minPairs+minLift).  Entries live for
// federatedCoChangeCacheTTL so they age out together with the underlying
// touches data, and the cache is bounded by an LRU cap (#608). Reuses the
// repo's existing cache.LRU + TTL-on-get idiom.
var federatedCoChangeCache = &federatedCoChangeCacheType{
	lru: cache.NewLRU[string, *federatedCoChangeCacheEntry](federatedCoChangeCacheMaxSize),
}

// federatedCoChangeCacheType wraps an LRU with a mutex (cache.LRU is not
// concurrent-safe) and a TTL check on load, mirroring touchesCache/cgCache.
type federatedCoChangeCacheType struct {
	mu  sync.Mutex
	lru *cache.LRU[string, *federatedCoChangeCacheEntry]
}

func (c *federatedCoChangeCacheType) load(key string) (*federatedCoChangeCacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.lru.Get(key)
	if !ok {
		return nil, false
	}
	if time.Since(e.at) > federatedCoChangeCacheTTL {
		c.lru.Delete(key)
		return nil, false
	}
	return e, true
}

func (c *federatedCoChangeCacheType) store(key string, entry *federatedCoChangeCacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry.at.IsZero() {
		entry.at = time.Now()
	}
	c.lru.Set(key, entry)
}

func (c *federatedCoChangeCacheType) delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lru.Delete(key)
}

func (c *federatedCoChangeCacheType) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lru.Len()
}

// clear resets the cache to empty; used by tests for isolation.
func (c *federatedCoChangeCacheType) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lru = cache.NewLRU[string, *federatedCoChangeCacheEntry](federatedCoChangeCacheMaxSize)
}

var (
	// fedInFlight deduplicates concurrent background workers for the same key.
	// Mirrors buildingRepos in age_graph_gate.go.
	fedInFlight sync.Map // key string → struct{}

	// fedBgComputeHook is called once per background worker launch (after the
	// LoadOrStore guard succeeds).  Nil in production; set in tests to count
	// actual compute invocations and verify the dedup guard.
	fedBgComputeHook func()
)

// federatedCoChangeCacheKey builds a stable key from the normalized args.
// localDirs is included so tests with different temp dirs don't share cache entries.
func federatedCoChangeCacheKey(repos string, windowHours, minPairs int, minLift float64, localDirs []string) string {
	// Sort-stable join of localDirs for a deterministic key.
	dirs := strings.Join(localDirs, "|")
	return fmt.Sprintf("fedcochange::%s::%s::%d::%d::%.4f", repos, dirs, windowHours, minPairs, minLift)
}

// handleFederatedCoChangeCore is the testable core of the federated_cochange tool.
//
// Resilience pattern (mirrors age_graph_gate.go buildingRepos + semantic_search IndexRepoAsync):
//  1. Deadline-race: compute with a federatedCoChangeInlineBudget child context.
//     Full result within budget → return status:"ready".
//  2. Guaranteed partial: within the budget, collect touches only for repos whose
//     cache is already warm (instant), compute pairs from warm touches, return
//     status:"partial" with the warm pairs (unverified) + pending_repos list.
//  3. Background job + dedup: continue the full computation (cold git-log +
//     VerifyPairs) in a detached goroutine.  fedInFlight guard prevents double-compute.
//  4. Poll returns full: a repeat call with the same args hits federatedCoChangeCache
//     and returns status:"ready" once the background job has written its result.
//
// Stage-2 VerifyPairs decision: partial responses return warm pairs with
// verified:false.  VerifyPairs runs only in the background (or on the inline
// fast-path).  This keeps the partial guarantee unconditional — AST route/symbol
// parsing never blocks the first response.
func handleFederatedCoChangeCore(ctx context.Context, args FederatedCoChangeArgs, deps analyze.Deps) (*mcp.CallToolResult, error) {
	return handleFederatedCoChangeCoreWithBudget(ctx, args, deps, federatedCoChangeInlineBudget)
}

// handleFederatedCoChangeCoreWithBudget is the injectable variant used by tests
// to exercise deadline-hit paths without waiting 20s.
func handleFederatedCoChangeCoreWithBudget(
	ctx context.Context,
	args FederatedCoChangeArgs,
	deps analyze.Deps,
	budget time.Duration,
) (*mcp.CallToolResult, error) {
	if args.Repos == "" {
		return errResult("repos is required (e.g. 'all', 'acme-*', or a repo name)"), nil
	}

	window := args.WindowHours
	if window <= 0 {
		window = federatedCoChangeDefaultWindowHours
	}
	minPairs := args.MinPairs
	if minPairs <= 0 {
		minPairs = federatedCoChangeDefaultMinPairs
	}

	t0 := time.Now()
	asOf := t0
	cacheKey := federatedCoChangeCacheKey(args.Repos, window, minPairs, args.MinLift, deps.LocalRepoDirs)

	// Poll path: check if a previous background job has completed.
	if entry, ok := federatedCoChangeCache.load(cacheKey); ok {
		if entry.done && entry.result != nil {
			return marshalFedResult(entry.result, t0)
		}
	}

	// Step 1: resolve repos (fast — filesystem scan only).
	repos, err := federate.ResolveRepos(ctx, args.Repos, deps.LocalRepoDirs)
	if err != nil {
		return errResult(fmt.Sprintf("resolve repos %q: %v", args.Repos, err)), nil
	}
	if len(repos) < 2 {
		return errResult(fmt.Sprintf("federated co-change needs ≥2 repos, %q resolved to %d", args.Repos, len(repos))), nil
	}

	// Step 2: deadline race — try to complete within the inline budget.
	budgetCtx, budgetCancel := context.WithTimeout(ctx, budget)
	defer budgetCancel()

	resultCh := make(chan *FederatedCoChangeResult, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("federated_cochange inline goroutine panic", "err", r)
				// Send an empty result so the select doesn't block; budget will expire
				// and the partial path will take over.
				resultCh <- &FederatedCoChangeResult{Pairs: []coupling.VerifiedPair{}}
			}
		}()
		rawPairs := federate.CrossRepoCoChange(budgetCtx, repos, asOf, window, minPairs, args.MinLift)
		roots := reposToRootsMap(repos)
		verified := coupling.VerifyPairs(budgetCtx, rawPairs, roots,
			coupling.NewCompositeVerifier(
				coupling.NewRouteVerifier(),
				coupling.NewSymbolVerifier(),
			))
		if verified == nil {
			verified = []coupling.VerifiedPair{}
		}
		resultCh <- &FederatedCoChangeResult{
			Pairs: verified,
		}
	}()

	select {
	case full := <-resultCh:
		// Full result within budget — cache and return.
		full.Meta = mcpmeta.Wrap(time.Since(t0), "")
		federatedCoChangeCache.store(cacheKey, &federatedCoChangeCacheEntry{result: full, done: true})
		return marshalFedResult(full, t0)

	case <-budgetCtx.Done():
		// Budget exceeded — fall through to partial + background path.
	}

	// Step 3: build guaranteed partial from warm-cache repos only.
	partial := buildPartialResult(ctx, repos, args, window, minPairs, t0)

	// Count the short-circuit status response for observability.
	recordToolColdReturn("federated_cochange", partial.Status)

	// Step 4: kick background job (dedup via fedInFlight).
	kickFedBackground(cacheKey, repos, asOf, args, window, minPairs)

	return marshalFedResult(partial, t0)
}

// buildPartialResult assembles the partial response from warm-cache repo touches.
// Returns status "building" when fewer than 2 repos are warm (no cross-repo pairs
// possible yet), "partial" when ≥2 repos are warm and pairs may exist.
// The consumer should keep polling until status is "ready".
func buildPartialResult(
	ctx context.Context,
	repos []federate.RepoRef,
	args FederatedCoChangeArgs,
	window, minPairs int,
	t0 time.Time,
) *FederatedCoChangeResult {
	var warmTouches []federate.RepoTouch
	var pendingRepos []string
	var warmRepoCount int
	for _, r := range repos {
		if wt := federate.WarmTouches(r.Root); wt != nil {
			warmTouches = append(warmTouches, wt...)
			warmRepoCount++
		} else {
			pendingRepos = append(pendingRepos, r.Slug)
		}
	}

	var partialPairs []coupling.VerifiedPair
	if warmRepoCount >= 2 {
		rawWarm := federate.CrossRepoCoChangeFromTouches(ctx, warmTouches, window, minPairs, args.MinLift)
		// Return warm pairs unverified (stage-2 VerifyPairs runs in the background;
		// verified:false is accurate and safe for consumers).
		partialPairs = make([]coupling.VerifiedPair, len(rawWarm))
		for i, p := range rawWarm {
			partialPairs[i] = coupling.VerifiedPair{CrossPair: p}
		}
	}
	if partialPairs == nil {
		partialPairs = []coupling.VerifiedPair{}
	}

	// Need ≥2 warm repos for cross-repo pairs.  A single warm repo can't yield
	// cross-repo pairs, so the consumer must keep polling — return "building".
	status := fedStatusPartial
	if warmRepoCount < 2 {
		status = fedStatusBuilding
	}

	warmCount := len(repos) - len(pendingRepos)
	return &FederatedCoChangeResult{
		Pairs:             partialPairs,
		Status:            status,
		PendingRepos:      pendingRepos,
		Progress:          fmt.Sprintf("%d/%d repos", warmCount, len(repos)),
		RetryAfterSeconds: federatedCoChangeRetryAfter,
		Meta:              mcpmeta.Wrap(time.Since(t0), fmt.Sprintf("retry in %ds for complete result", federatedCoChangeRetryAfter)),
	}
}

// kickFedBackground launches a background worker for the given cacheKey if none
// is already running (dedup via fedInFlight).  The worker caches only successful,
// non-degenerate results; transient failures (timeout, context cancel) are logged
// and do NOT mark the entry done, so the next poll triggers a fresh attempt.
func kickFedBackground(
	cacheKey string,
	repos []federate.RepoRef,
	asOf time.Time,
	args FederatedCoChangeArgs,
	window, minPairs int,
) {
	if _, alreadyRunning := fedInFlight.LoadOrStore(cacheKey, struct{}{}); alreadyRunning {
		return
	}

	bgRepos := repos // capture
	bgArgs := args
	bgWindow := window
	bgMinPairs := minPairs
	go func() { //nolint:gosec // intentional ctx detach — request ctx is cancelled when the partial returns; the background job must outlive it to populate the cache for the next poll
		// fedBgComputeHook is nil in production; called once per launch inside the
		// goroutine (not in the caller) so tests can block here without stalling callers.
		if h := fedBgComputeHook; h != nil {
			h()
		}
		defer fedInFlight.Delete(cacheKey)
		defer func() {
			if r := recover(); r != nil {
				slog.Error("federated_cochange background panic", "err", r)
				// Do NOT mark done — leave the entry absent so the next poll retries.
				fedInFlight.Delete(cacheKey)
			}
		}()

		bgCtx, bgCancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer bgCancel()

		rawPairs := federate.CrossRepoCoChange(bgCtx, bgRepos, asOf, bgWindow, bgMinPairs, bgArgs.MinLift)
		roots := reposToRootsMap(bgRepos)
		verified := coupling.VerifyPairs(bgCtx, rawPairs, roots,
			coupling.NewCompositeVerifier(
				coupling.NewRouteVerifier(),
				coupling.NewSymbolVerifier(),
			))

		// Do NOT cache if the context expired (definite failure — transient).
		// A legitimate empty result (no cross-repo pairs in the window) IS cached:
		// rawPairs==0 with ≥2 repos is indistinguishable from a quiet window and
		// caching it avoids a retry storm.  The bgCtx.Err() guard handles the
		// definite-timeout failure class.
		if bgCtx.Err() != nil {
			slog.Warn("federated_cochange background timed out — not caching",
				slog.String("key", cacheKey), slog.Any("err", bgCtx.Err()))
			return
		}
		if verified == nil {
			verified = []coupling.VerifiedPair{}
		}
		full := &FederatedCoChangeResult{
			Pairs: verified,
			Meta:  mcpmeta.Wrap(0, ""),
		}
		slog.Info("federated_cochange background complete",
			slog.String("key", cacheKey), slog.Int("pairs", len(verified)))
		federatedCoChangeCache.store(cacheKey, &federatedCoChangeCacheEntry{result: full, done: true})
	}()
}

// reposToRootsMap builds a slug→root map for VerifyPairs.
func reposToRootsMap(repos []federate.RepoRef) map[string]string {
	roots := make(map[string]string, len(repos))
	for _, r := range repos {
		roots[r.Slug] = r.Root
	}
	return roots
}

// marshalFedResult serialises a FederatedCoChangeResult.
// status "ready" is the zero value — omitempty omits it for back-compat.
func marshalFedResult(out *FederatedCoChangeResult, t0 time.Time) (*mcp.CallToolResult, error) {
	if out.Meta.DurationMS == 0 {
		out.Meta = mcpmeta.Wrap(time.Since(t0), out.Meta.Hint)
	}
	return jsonMarshalResult(out), nil
}

// registerFederatedCoChange registers the federated_cochange tool on the MCP server.
func registerFederatedCoChange(server *mcp.Server, cfg Config, deps analyze.Deps) {
	addTool(server, &mcp.Tool{
		Name:        "federated_cochange",
		Description: "Find files in DIFFERENT repos that change together (cross-repo co-change) across a workspace. Ranked by Wilson lower bound on directional confidence (support-aware, continuous, never saturates): a thin coincidence (co=2, n=2) ranks well below a well-supported coupling (co=8, n=10) because Wilson penalizes small sample sizes — more evidence always wins. Ubiquitous stop-word files (CHANGELOGs, lockfiles, generated files touched in >85% of windows) are filtered out as noise before scoring. g2/significance are informational (un-capped Dunning log-likelihood); confidence_level derives from the Wilson score. min_lift is an optional raw effect-size pre-filter (not emitted in results). repos='all' | 'acme-*' | a repo name. Surfaces hidden coupling, e.g. a signaling change in one repo that needs a synchronized edit in another. Returns status:'partial' or 'building' with retry_after_seconds when result is not yet ready; re-call with the same args to get the complete 'ready' result.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args FederatedCoChangeArgs) (*mcp.CallToolResult, error) {
		return handleFederatedCoChangeCore(ctx, args, deps)
	})
	_ = cfg // cfg reserved for future use (e.g. WorkspaceDir override)
}
