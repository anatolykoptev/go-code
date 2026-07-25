package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/anatolykoptev/vaelor/internal/codegraph"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// buildingRepos tracks repos currently being indexed to prevent concurrent AGE
// graph builds. AGE is not concurrency-safe for writes to the same graph, so
// only one IndexRepo per repoKey may run at a time across all tools.
var buildingRepos sync.Map

const (
	// ageGraphStatusBuilding is the status value returned when a fresh AGE graph
	// is not yet available and a background build has been started.
	ageGraphStatusBuilding = "building"
	// ageGraphRetryMessage is the human-readable retry hint included in every
	// building short-circuit response.
	ageGraphRetryMessage = "code graph is being built — retry in 2-3 minutes"
)

// ageGraphStatusBuilder produces a tool-appropriate status result for the
// "graph is building" short-circuit response.
type ageGraphStatusBuilder func(status, message string) *mcp.CallToolResult

var (
	// ageGraphCacheStatus is the test seam for codegraph.CacheStatus.
	ageGraphCacheStatus = codegraph.CacheStatus
	// ageGraphIndexRepo is the test seam for codegraph.IndexRepo.
	ageGraphIndexRepo = codegraph.IndexRepo
	// ageGraphMemGuardWatchdog is the test seam for codegraph.MemGuardWatchdog.
	ageGraphMemGuardWatchdog = codegraph.MemGuardWatchdog
)

// triggerBackgroundGraphBuild starts a deduplicated background IndexRepo for
// root/repoKey if one is not already in flight. Fire-and-forget: returns
// immediately, never blocks the caller. Reuses buildingRepos (sync.Map) for
// dedup — the same seam as ensureAgeGraphOrStatus — so the tool gate and the
// retrieval self-heal path share a single dedup registry (no second parallel
// staleness mechanism). Returns true if a new build was started, false if one
// was already in flight.
//
// Extracted from ensureAgeGraphOrStatus so the retrieval path (#691) can
// trigger the same dedup background rebuild without duplicating the goroutine
// spawn, panic recovery, mem-guard, and age-gauge recording logic.
func triggerBackgroundGraphBuild(
	tool string,
	store *codegraph.Store,
	root, repoKey string,
	isRemote bool,
	cfg codegraph.IndexConfig,
) bool {
	if _, alreadyBuilding := buildingRepos.LoadOrStore(repoKey, true); alreadyBuilding {
		return false
	}
	bgRoot := root
	// Capture the test/production seams here so the background goroutine uses
	// the same function values even if a test restores the package vars
	// immediately after the caller returns.
	indexRepo := ageGraphIndexRepo
	memGuard := ageGraphMemGuardWatchdog
	go func() {
		// Recover panics from the background AGE build so a single repo does not
		// crash the entire MCP process. The failure is recorded as a build error
		// and logged for observability.
		defer func() {
			if r := recover(); r != nil {
				recordCodeGraphBuildFailure(fmt.Errorf("panic in background AGE index: %v", r))
				slog.Error("age graph: background index panic",
					slog.String("tool", tool), slog.String("repo", bgRoot), slog.Any("panic", r))
			}
		}()
		defer buildingRepos.Delete(repoKey)
		bgCtx, bgCancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer bgCancel()
		// Memory watchdog: polls /proc/pressure/memory every 10s and cancels
		// the build context if the host enters memory pressure during the
		// build (second line of defense after the pre-build gate in IndexRepo).
		go memGuard(bgCtx, bgCancel)
		if bgMeta, err := indexRepo(bgCtx, store, bgRoot, isRemote, cfg); err != nil {
			recordCodeGraphBuildFailure(err)
			slog.Warn("age graph: background index failed",
				slog.String("tool", tool), slog.String("repo", bgRoot), slog.Any("error", err))
		} else if bgMeta != nil {
			recordCodeGraphAge(repoKey, bgMeta.BuiltAt)
			slog.Info("age graph: background index complete",
				slog.String("tool", tool), slog.String("repo", bgRoot))
		}
	}()
	return true
}

// ensureAgeGraphOrStatus checks whether a fresh AGE graph exists for root.
// If fresh, it returns (true, nil) so the caller can continue synchronously.
// If not fresh, it ensures a background IndexRepo is running (deduplicated by
// repoKey) and returns a tool-appropriate "building" status result.
func ensureAgeGraphOrStatus(
	ctx context.Context,
	tool string,
	store *codegraph.Store,
	root, repoKey string,
	isRemote bool,
	cfg codegraph.IndexConfig,
	buildStatus ageGraphStatusBuilder,
) (bool, *mcp.CallToolResult) {
	fresh, cacheErr := ageGraphCacheStatus(ctx, store, root)
	if cacheErr != nil {
		slog.Warn("age graph: cache status check failed, treating as not fresh",
			slog.String("tool", tool), slog.String("repo", root), slog.Any("error", cacheErr))
	}
	if !fresh {
		// Not cached: build in the background (deduplicated by repoKey via
		// buildingRepos) and tell the caller to retry. Whether or not a build
		// was already in flight, the caller gets the same building status.
		triggerBackgroundGraphBuild(tool, store, root, repoKey, isRemote, cfg)
		recordToolColdReturn(tool, ageGraphStatusBuilding)
		return false, buildStatus(ageGraphStatusBuilding, ageGraphRetryMessage)
	}
	return true, nil
}
