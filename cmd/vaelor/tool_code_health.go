package main

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/codegraph"
	"github.com/anatolykoptev/vaelor/internal/compare"
	"github.com/anatolykoptev/vaelor/internal/explore"
	"github.com/anatolykoptev/vaelor/internal/freshness"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// buildingHealth prevents concurrent code_health builds for the same repo.
var buildingHealth sync.Map

// healthBuildTimeout bounds a single background code_health computation. Large
// repos take 1-2 minutes; 5 minutes leaves headroom without leaking goroutines.
const healthBuildTimeout = 5 * time.Minute

// spawnHealthBuild runs compute on a background goroutine that is the SOLE owner
// of the resolved clone for the lifetime of the build. cleanup (which deletes a
// temporary clone) runs via defer INSIDE the goroutine, so it fires on every
// exit path — success, compute error, or ctx-cancel — but only AFTER compute
// has finished reading the tree. This is the invariant that fixes the
// use-after-delete race: the handler must transfer clone ownership here instead
// of deleting the clone when it returns its synchronous "computing" response.
//
// repoKey is cleared from buildingHealth on exit so a later retry can rebuild.
// Callers must NOT also run cleanup for this code path.
func spawnHealthBuild(repoKey string, cleanup func(), compute func(ctx context.Context) error) {
	go func() {
		// Deferred LIFO: healthBuildDone fires LAST (after cleanup), so a test
		// observing it is guaranteed the clone cleanup has already run.
		defer healthBuildDone(repoKey) // test hook; no-op in production.
		defer cleanup()                // delete the clone only after compute is done reading it.
		defer buildingHealth.Delete(repoKey)
		ctx, cancel := context.WithTimeout(context.Background(), healthBuildTimeout)
		defer cancel()
		if err := compute(ctx); err != nil {
			recordHealthBuildFailure(err)
			slog.Warn("code_health: background computation failed",
				slog.String("repo", repoKey), slog.Any("error", err))
		} else {
			slog.Info("code_health: background computation complete", slog.String("repo", repoKey))
		}
	}()
}

// healthBuildDone is a test seam fired at the very end of a background build,
// after the deferred cleanup has already run (LIFO ordering above). It defaults
// to a no-op in production; tests swap it to observe completion deterministically
// without sleeping.
var healthBuildDone = func(string) {}

// CodeHealthInput is the input schema for the code_health tool.
type CodeHealthInput struct {
	Repo     string `json:"repo" jsonschema:"Repository: GitHub slug (owner/repo), full GitHub URL, or absolute local host path (e.g. /home/user/src/project)"`
	Language string `json:"language,omitempty" jsonschema:"Limit analysis to files of this language (e.g. go, python, rust)"`
	Focus    string `json:"focus,omitempty" jsonschema:"Subdirectory path to limit scope (e.g. internal/auth, pkg/api), space-separated keywords (e.g. 'auth handler'), or 'magic_numbers' for detailed magic number report"`
	Format   string `json:"format,omitempty" jsonschema:"Output format: 'xml' (default) or 'sarif' (SARIF v2.1.0 JSON for GitHub Code Scanning)"`
}

// registerCodeHealth registers the code_health MCP tool.
func registerCodeHealth(server *mcp.Server, cfg Config, deps analyze.Deps, semDeps *SemanticDeps, graphStore *codegraph.Store) {
	outputDir := cfg.OutputDir

	addTool(server, &mcp.Tool{
		Name: "code_health",
		Description: "Assess code quality of a single repository. " +
			"Returns grade (A-F), numeric score (0-100), metrics " +
			"(complexity, test coverage, docs, error handling, " +
			"dependency freshness, vulnerability security via OSV), " +
			"maintenance hotspots, type relationships, and prioritized " +
			"recommendations with estimated score impact. No LLM — " +
			"fast, static analysis with registry version and CVE checks.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CodeHealthInput) (*mcp.CallToolResult, error) {
		if input.Repo == "" {
			return errResult("repo is required"), nil
		}

		root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps)
		if err != nil {
			return errResult(fmt.Sprintf("resolve repo: %s", err)), nil
		}
		// cleanupTransferred guards against the use-after-delete race: when the
		// background path takes ownership of the clone, the goroutine runs
		// cleanup AFTER it finishes reading. Every other path (cache-hit,
		// inline, error) runs cleanup synchronously here. The deferred call is a
		// no-op once ownership has transferred — the flag is read on the
		// handler goroutine after the spawn, so no atomic is needed.
		cleanupTransferred := false
		defer func() {
			if !cleanupTransferred {
				cleanup()
			}
		}()

		// Cache-first: large repos take 30-90s and exceed the 60s MCP timeout.
		// Skip cache for sarif format (different output structure) and special focus modes.
		if graphStore != nil && input.Format != "sarif" && input.Focus == "" {
			repoKey := codegraph.GraphNameFor(root)
			_ = graphStore.EnsureHealthCacheTable(ctx)
			if cached := graphStore.LoadHealthCache(ctx, repoKey); cached != nil {
				return largeTextResult(cached.ResultXML, "code_health", outputDir), nil
			}
			// Not cached: start background computation. The goroutine becomes the
			// SOLE owner of the clone for the lifetime of the build — it runs
			// cleanup on every exit (success, error, or ctx-cancel) only after
			// computeCodeHealth has finished reading the tree. The handler must
			// NOT delete the clone here: returning the synchronous "computing"
			// response would otherwise fire the deferred cleanup while the
			// goroutine is still walking the files (the original race).
			if _, alreadyBuilding := buildingHealth.LoadOrStore(repoKey, true); !alreadyBuilding {
				spawnHealthBuild(repoKey, cleanup, func(bgCtx context.Context) error {
					_, computeErr := computeCodeHealth(bgCtx, input, root, deps, semDeps, graphStore, outputDir)
					return computeErr
				})
				cleanupTransferred = true // ownership moved to the spawned goroutine.
			}
			return textResult(fmt.Sprintf(
				`<response tool="code_health"><status>computing</status>`+
					`<message>Health analysis started. Retry in 60 seconds — large repos take 1-2 minutes.</message>`+
					`<repo>%s</repo></response>`, input.Repo)), nil
		}

		// No graphStore, sarif format, or focus mode: run inline. cleanup fires
		// via the deferred guard above after the synchronous read completes.
		return computeCodeHealth(ctx, input, root, deps, semDeps, graphStore, outputDir)
	})
}

// computeCodeHealth runs the full code health analysis and returns the result.
// When graphStore is non-nil and format is xml with no focus, the result is
// persisted to code_health_cache for fast subsequent lookups.
func computeCodeHealth(
	ctx context.Context,
	input CodeHealthInput,
	root string,
	deps analyze.Deps,
	semDeps *SemanticDeps,
	graphStore *codegraph.Store,
	outputDir string,
) (*mcp.CallToolResult, error) {
	// Stage 1: snapshot + special focus modes.
	sr, err := buildHealthSnapshot(ctx, root, input.Language, input.Focus)
	if err != nil {
		return errResult(fmt.Sprintf("snapshot: %s", err)), nil
	}

	// Surface any silent truncation (drop counters + warning) before deriving
	// metrics. The Partial flag rides through to the XML below.
	reportSnapshotPartial(root, sr.snap)

	if sr.isMagicMode {
		entries := compare.CollectMagicNumbers(sr.snap)
		resp := buildMagicNumbersXML(sr.snap.Name, sr.snap.Language, entries)
		return xmlMarshalResult(resp, "code_health", outputDir), nil
	}

	if sr.isSemanticDup {
		if semDeps == nil || semDeps.Store == nil {
			return errResult("semantic search not configured: set EMBED_URL and DATABASE_URL"), nil
		}
		groups := collectSemanticDupGroups(ctx, semDeps, root, sr.snap)
		resp := buildSemanticDupXML(sr.snap.Name, sr.snap.Language, groups)
		return xmlMarshalResult(resp, "code_health", outputDir), nil
	}

	// Stage 2: core metrics + semantic dup enrichment.
	metrics := compare.ComputeMetrics(sr.snap)

	// Stage 3-6: Run independent analyses in parallel.
	var wg sync.WaitGroup
	var fr healthFreshnessResult
	var hotspots []compare.HotspotFile
	var archMetrics *compare.ArchMetrics

	// Semantic duplication (optional, may be fast if no store).
	wg.Add(1)
	go func() {
		defer wg.Done()
		gatherHealthSemanticDup(ctx, semDeps, root, sr.snap, &metrics)
	}()

	// Freshness + vulnerability (HTTP calls).
	wg.Add(1)
	go func() {
		defer wg.Done()
		fr = gatherHealthFreshness(ctx, root, &metrics)
	}()

	// Hotspots (git churn).
	wg.Add(1)
	go func() {
		defer wg.Done()
		hotspots = gatherHealthHotspots(ctx, root, input.Repo, sr.snap)
	}()

	// Architecture metrics (graph DB query).
	wg.Add(1)
	go func() {
		defer wg.Done()
		archMetrics = gatherHealthArchMetrics(ctx, graphStore, root)
	}()

	// Dead code metrics (from CE scores in code_dead_code_scores).
	var deadCodeCandidates int
	var deadCodeTopNames []string
	wg.Add(1)
	go func() {
		defer wg.Done()
		gatherHealthDeadCode(ctx, graphStore, root, &deadCodeCandidates, &deadCodeTopNames)
	}()

	// Relationship stats and outliers (CPU-bound, parallel with above).
	relStats := compare.ComputeRelStats(sr.snap.Rels)
	outliers := compare.CollectOutliers(sr.snap)

	// Wait for all parallel stages.
	wg.Wait()

	// Compute the final score/grade once, after all stages have written their
	// ratio fields. Stages must NOT self-assign Score/Grade to avoid a data race
	// (two goroutines writing to the same metrics field simultaneously). This is
	// the single authoritative scoring call.
	metrics.Score = compare.GradeScore(metrics)
	metrics.Grade = compare.ComputeGrade(metrics)

	if deadCodeCandidates > 0 {
		metrics.DeadCodeCandidates = deadCodeCandidates
		metrics.DeadCodeTopNames = deadCodeTopNames
		// Recompute score/grade with dead code penalty applied on top.
		metrics.Score = compare.GradeScore(metrics)
		metrics.Grade = compare.ComputeGrade(metrics)
	}

	if input.Format == "sarif" {
		sarifReport := compare.BuildSARIF(sr.snap.Name, metrics, nil, nil, hotspots, outliers)
		data, err := json.Marshal(sarifReport)
		if err != nil {
			return errResult(fmt.Sprintf("sarif marshal: %s", err)), nil
		}
		return largeTextResult(string(data), "code_health", outputDir), nil
	}

	recs := compare.ComputeRecommendations(metrics, outliers, 5)
	oxChecks := explore.RunOxCodesHealthChecks(ctx, deps.OxCodes, root, input.Language)

	resp := buildHealthXML(sr.snap.Name, sr.snap.Language, metrics, metrics.Score, hotspots, relStats, recs, fr.fr, fr.vr, oxChecks, archMetrics, sr.snap.Partial)

	// Marshal to raw XML string for caching (before largeTextResult file fallback).
	data, marshalErr := xml.Marshal(resp)
	if marshalErr != nil {
		return errResult(fmt.Sprintf("marshal: %s", marshalErr)), nil
	}
	rawXML := xml.Header + string(data)

	// Persist to cache for fast subsequent lookups (1h TTL).
	if graphStore != nil && input.Focus == "" {
		repoKey := codegraph.GraphNameFor(root)
		score, grade := extractScoreGrade(rawXML)
		if cacheErr := graphStore.UpsertHealthCache(ctx, repoKey, root, grade, rawXML, score, 3600); cacheErr != nil {
			slog.Warn("code_health: cache upsert failed", slog.String("repo", root), slog.Any("error", cacheErr))
		}
	}

	return largeTextResult(rawXML, "code_health", outputDir), nil
}

// reportSnapshotPartial emits the drop counters for snap and logs a warning when
// the snapshot is partial (some enumerated files were not folded in). Splitting
// this out of computeCodeHealth keeps that function within the statement budget
// and isolates the observability concern.
func reportSnapshotPartial(root string, snap *compare.RepoSnapshot) {
	if snap == nil {
		return
	}
	recordSnapshotDrops(snap.DroppedReadError, snap.DroppedCtxCancel, snap.FileCount, len(snap.Files))
	if !snap.Partial {
		return
	}
	slog.Warn("code_health: snapshot is partial — metrics under-count the repo",
		slog.String("repo", root),
		slog.Int("enumerated", snap.FileCount),
		slog.Int("kept", len(snap.Files)),
		slog.Int("dropped_read_error", snap.DroppedReadError),
		slog.Int("dropped_ctx_cancel", snap.DroppedCtxCancel))
}

// extractScoreGrade parses score and grade from a code_health XML response string.
func extractScoreGrade(xmlStr string) (int, string) {
	score := 0
	grade := "?"
	if m := regexp.MustCompile(`score="(\d+(?:\.\d+)?)"`).FindStringSubmatch(xmlStr); len(m) > 1 {
		if f, err := strconv.ParseFloat(m[1], 64); err == nil {
			score = int(f)
		}
	}
	if m := regexp.MustCompile(`grade="([A-F][+]?)"`).FindStringSubmatch(xmlStr); len(m) > 1 {
		grade = m[1]
	}
	return score, grade
}

func buildHealthXML(
	name, language string,
	metrics compare.RepoMetrics,
	score float64,
	hotspots []compare.HotspotFile,
	relStats *compare.RelStats,
	recs []compare.Recommendation,
	fr *freshness.FreshnessResult,
	vr *freshness.VulnResult,
	oxChecks *explore.OxCodesHealthChecks,
	archMetrics *compare.ArchMetrics,
	partial bool,
) xmlHealthResponse {
	resp := xmlHealthResponse{
		Health: xmlHealth{
			Repo:     name,
			Partial:  partial,
			Language: language,
			Metrics:  convertMetrics(metrics),
			Score:    score,
		},
	}

	if len(hotspots) > 0 {
		resp.Health.Hotspots = convertHotspots(hotspots)
	}
	if relStats != nil {
		resp.Health.RelStats = &xmlRelStats{
			Total: relStats.Total, Extends: relStats.Extends,
			Implements: relStats.Implements, Embeds: relStats.Embeds,
			UniqueSubjects: relStats.UniqueSubjects,
		}
	}
	if len(recs) > 0 {
		resp.Health.Recommendations = convertRecommendations(recs)
	}
	if fr != nil && fr.Total > 0 {
		resp.Health.DepFreshness = convertDepFreshness(fr)
	}
	if vr != nil && vr.Total > 0 {
		resp.Health.Vulnerabilities = convertVulnerabilities(vr)
	}
	if oxChecks != nil {
		resp.Health.OxChecks = convertOxCodesChecks(oxChecks)
	}
	if archMetrics != nil {
		resp.Health.ArchMetrics = convertArchMetrics(archMetrics)
	}

	return resp
}
