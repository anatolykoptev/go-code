package main

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/biomarkers"
	"github.com/anatolykoptev/vaelor/internal/compare"
	"github.com/anatolykoptev/vaelor/internal/mcpmeta"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	// maxHealthDefaultPaths is the maximum number of hotspot paths to score
	// when no explicit paths are provided.
	maxHealthDefaultPaths = 20

	// hotspotHintThreshold is the minimum score for the top file to trigger
	// an advisory hint in the response meta.
	hotspotHintThreshold = 7
)

// healthSourceExts is the allow-list of file extensions treated as
// maintainable source code for health scoring. Covers programming
// languages, schema-as-code (proto/thrift/graphql), infra-as-code
// (terraform), and configuration-as-source-of-truth (yaml/toml).
//
// Excludes documentation (.md, .rst, .adoc), lock files, and binary
// content that churn high without representing defect risk.
//
// Case-sensitive on purpose: matches the lowercase Go convention. A
// repo with PascalCase extensions (rare) won't be scored.
//
// Known limitation: this is hand-maintained, NOT derived from
// internal/parser/handler.Extensions(). A new language added to the
// parser may be silently dropped from health scoring until added here
// as well. Phase 2b follow-up tracks this.
var healthSourceExts = map[string]bool{
	// Programming languages
	".go":     true,
	".rs":     true,
	".ts":     true,
	".tsx":    true,
	".js":     true,
	".jsx":    true,
	".mjs":    true,
	".cjs":    true,
	".svelte": true,
	".astro":  true,
	".py":     true,
	".java":   true,
	".kt":     true,
	".swift":  true,
	".rb":     true,
	".cs":     true,
	".cpp":    true,
	".cc":     true,
	".c":      true,
	".h":      true,
	".hpp":    true,
	".php":    true,
	".sh":     true,
	".sql":    true,
	// Schema-as-code
	".proto":   true,
	".thrift":  true,
	".graphql": true,
	".gql":     true,
	// Infra-as-code
	".tf":     true,
	".tfvars": true,
	// Config-as-source-of-truth (k8s manifests, GitHub Actions, etc.)
	".yml":  true,
	".yaml": true,
	".toml": true,
}

// healthSourceBasenames covers extension-less source files (build scripts,
// dependency declarations). Used as a secondary check by isHealthEligible
// after the extension allow-list returns false.
var healthSourceBasenames = map[string]bool{
	"Dockerfile":      true,
	"Makefile":        true,
	"Rakefile":        true,
	"Gemfile":         true,
	"BUILD":           true,
	"WORKSPACE":       true,
	"BUILD.bazel":     true,
	"WORKSPACE.bazel": true,
}

// healthLockedBasenames is the deny-list of exact filenames that must never
// be scored regardless of extension. Lock files are included here because
// they share extensions with real source (e.g. pnpm-lock.yaml matches .yaml)
// but carry zero defect signal — they churn mechanically on every dep bump.
var healthLockedBasenames = map[string]bool{
	"package-lock.json":  true,
	"yarn.lock":          true,
	"pnpm-lock.yaml":     true,
	"Cargo.lock":         true,
	"Gemfile.lock":       true,
	"poetry.lock":        true,
	"go.sum":             true,
	"composer.lock":      true,
	"mix.lock":           true,
	"pubspec.lock":       true,
	"packages.lock.json": true,
	"Pipfile.lock":       true,
}

// healthExcludedDirSegments lists directory NAMES that exclude any path
// containing that segment. Catches both top-level (`static/foo`) and
// nested (`web/static/foo`, `services/api/static/foo`) — the smoke that
// motivated BUG-FH-1 hit `web/static/audio/c2dec.js`, which a top-level-
// only prefix would miss.
var healthExcludedDirSegments = map[string]bool{
	"vendor":       true,
	"node_modules": true,
	"dist":         true,
	"build":        true,
	"static":       true,
	"docs":         true,
	".claude":      true,
	".cache":       true,
	"target":       true,
	"third_party":  true,
	"generated":    true,
	"gen":          true,
	".git":         true,
}

// isHealthEligible reports whether a repo-relative path should be considered
// for biomarker scoring. Skips paths containing any excluded directory
// segment (catches nested vendored content like web/static/) and paths
// whose extension is not in the source allow-list.
//
// Path traversal is segment-based, not prefix-based: "web/static/foo.js"
// is excluded by the "static" segment match, mirroring how operators
// expect "vendored or generated" to behave irrespective of where in the
// tree it sits.
//
// Check order: lock-file deny-list → segment exclusion → ext allow-list →
// basename allow-list (for extension-less files like Dockerfile).
func isHealthEligible(relPath string) bool {
	base := filepath.Base(relPath)
	// Lock files share extensions with real source (.yaml, .json) but have
	// zero defect signal — they churn mechanically on every dependency bump.
	if healthLockedBasenames[base] {
		return false
	}
	// filepath.ToSlash normalises caller-supplied Windows paths
	// ("web\\static\\foo") so segment match catches them too.
	for _, seg := range strings.Split(filepath.ToSlash(relPath), "/") {
		if healthExcludedDirSegments[seg] {
			return false
		}
	}
	ext := strings.ToLower(filepath.Ext(relPath))
	if healthSourceExts[ext] {
		return true
	}
	// Basename allow-list for ext-less source files (Dockerfile, Makefile, etc.).
	return healthSourceBasenames[base]
}

// defaultHealthWeights are the per-biomarker weights used by defaultHealthRegistry.
// Weights must sum to 1.0 (enforced by NewAggregator).
var defaultHealthWeights = map[string]float64{
	"prior_defect": 0.6,
	"churn_risk":   0.4,
}

// FileHealthArgs is the input schema for the get_file_health tool.
type FileHealthArgs struct {
	Repo  string   `json:"repo"  jsonschema:"Repository: GitHub slug (owner/repo), full GitHub URL, or absolute local host path"`
	Paths []string `json:"paths,omitempty" jsonschema:"File paths (relative to repo root) to score. Defaults to top-20 hotspot files by churn."`
}

// FileHealthResult is the JSON payload returned by the get_file_health tool.
type FileHealthResult struct {
	Files []biomarkers.FileHealth `json:"files"`
	Meta  mcpmeta.Envelope        `json:"_meta"`
}

// defaultHealthRegistry returns a Registry with PriorDefect and ChurnRisk registered
// in the order expected by defaultHealthWeights.
func defaultHealthRegistry() *biomarkers.Registry {
	reg := biomarkers.NewRegistry()
	reg.Register(biomarkers.PriorDefect{})
	reg.Register(biomarkers.ChurnRisk{})
	return reg
}

// topHotspotPaths collects churn data for repo root and returns the top-max
// paths ranked by ChurnScore descending. Returns nil, nil when the repo has
// no git history (CollectChurn returns empty map without error).
func topHotspotPaths(ctx context.Context, repo string, max int) ([]string, error) {
	churn, err := compare.CollectChurn(ctx, repo, 0)
	if err != nil {
		return nil, fmt.Errorf("collect churn: %w", err)
	}
	if len(churn) == 0 {
		return nil, nil
	}

	type entry struct {
		path  string
		score float64
	}
	entries := make([]entry, 0, len(churn))
	for path, stats := range churn {
		if !isHealthEligible(path) {
			continue
		}
		entries = append(entries, entry{path: path, score: stats.ChurnScore()})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].score != entries[j].score {
			return entries[i].score > entries[j].score
		}
		return entries[i].path < entries[j].path
	})

	if max > 0 && len(entries) > max {
		entries = entries[:max]
	}
	paths := make([]string, len(entries))
	for i, e := range entries {
		paths[i] = e.path
	}
	return paths, nil
}

// handleFileHealthCore is the testable core of the get_file_health tool.
func handleFileHealthCore(ctx context.Context, args FileHealthArgs, agg *biomarkers.Aggregator, cfg Config, deps analyze.Deps) (*mcp.CallToolResult, error) {
	if args.Repo == "" {
		return errResult("repo is required"), nil
	}

	root, cleanup, err := resolveRoot(ctx, args.Repo, "", deps)
	if err != nil {
		return errResult(fmt.Sprintf("resolve repo: %s", err)), nil
	}
	defer cleanup()

	t0 := time.Now()

	paths := args.Paths
	if len(paths) == 0 {
		var herr error
		paths, herr = topHotspotPaths(ctx, root, maxHealthDefaultPaths)
		if herr != nil {
			return errResult(fmt.Sprintf("collect churn: %s", herr)), nil
		}
	}

	// Batch-fetch defect counts once; a single git log replaces 20 per-file calls
	// (BUG-FH-2: 11.5s → ≤2s on 20 paths). Falls back to per-file if batch errors.
	defectCounts, batchErr := biomarkers.BatchPriorDefect(ctx, root, paths)
	if batchErr != nil {
		slog.Debug("biomarkers.BatchPriorDefect failed; falling back to per-file",
			"repo_root", root,
			"n_paths", len(paths),
			"err", batchErr,
		)
	} else {
		ctx = biomarkers.WithBatchDefectCache(ctx, defectCounts)
	}

	// Batch-fetch initial-creation line counts once; a single git log replaces
	// N per-file spawns (BUG-FH-2b: 34s → bounded by one git log on 20 paths).
	// Falls back to per-file if batch errors.
	creationCounts, creationErr := biomarkers.BatchInitialCreationLines(ctx, root, paths)
	if creationErr != nil {
		slog.Debug("biomarkers.BatchInitialCreationLines failed; falling back to per-file",
			"repo_root", root,
			"n_paths", len(paths),
			"err", creationErr,
		)
	} else {
		ctx = biomarkers.WithBatchCreationCache(ctx, creationCounts)
	}

	out := FileHealthResult{}
	for _, p := range paths {
		fh, serr := agg.ScoreFile(ctx, root, p)
		if serr != nil {
			fh = biomarkers.FileHealth{
				Path:    p,
				Score:   0,
				Reasons: map[string]string{},
				Raw:     map[string]float64{},
				Error:   serr.Error(),
			}
		}
		out.Files = append(out.Files, fh)
	}

	sort.SliceStable(out.Files, func(i, j int) bool {
		if out.Files[i].Score != out.Files[j].Score {
			return out.Files[i].Score > out.Files[j].Score
		}
		return out.Files[i].Path < out.Files[j].Path
	})

	// Build advisory hint from top file — references only our own response
	// keys (prior_defect, churn_risk), never tool names or arg keys that
	// could be wrong or non-existent.
	var hint string
	if len(out.Files) > 0 && out.Files[0].Score >= hotspotHintThreshold {
		top := out.Files[0]
		hint = fmt.Sprintf(
			"top file %s scored %d/10 — review the reasons map for prior_defect / churn_risk drivers",
			top.Path, top.Score,
		)
	}

	out.Meta = mcpmeta.Wrap(time.Since(t0), hint)

	return jsonMarshalResult(out), nil
}

// registerFileHealth registers the get_file_health tool on the MCP server.
// The aggregator is built once at registration time — NewAggregator panics on
// bad weight config, so panicking here (at server startup) is intentional and
// gives clear diagnostic rather than panicking inside a request handler.
func registerFileHealth(server *mcp.Server, cfg Config, deps analyze.Deps) {
	reg := defaultHealthRegistry()
	agg := biomarkers.NewAggregator(reg, defaultHealthWeights)

	addTool(server, &mcp.Tool{
		Name:        "get_file_health",
		Description: "Report a 1-10 health score per file using prior_defect + churn_risk biomarkers. Optional paths defaults to top-20 hotspots.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args FileHealthArgs) (*mcp.CallToolResult, error) {
		return handleFileHealthCore(ctx, args, agg, cfg, deps)
	})
	_ = cfg // cfg reserved for future use (e.g. WorkspaceDir override)
}
