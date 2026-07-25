package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"sync/atomic"
	"time"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/compare"
	"github.com/anatolykoptev/vaelor/internal/graphx"
	"github.com/anatolykoptev/vaelor/internal/impact"
	"github.com/anatolykoptev/vaelor/internal/mcpmeta"
	"github.com/anatolykoptev/vaelor/internal/prompts"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ImpactInput is the input schema for the impact_analysis tool.
type ImpactInput struct {
	Repo     string `json:"repo" jsonschema:"Repository: GitHub slug (owner/repo), full GitHub URL, or absolute local host path"`
	Symbol   string `json:"symbol" jsonschema:"Function or method name to analyze impact for"`
	Depth    int    `json:"depth,omitempty" jsonschema:"Max traversal depth for transitive callers (default 5, max 10)"`
	Focus    string `json:"focus,omitempty" jsonschema:"Subdirectory path to limit scope (e.g. internal/auth), or space-separated keywords (e.g. 'auth handler')"`
	Language string `json:"language,omitempty" jsonschema:"Limit to files of this language (e.g. go, python)"`
	MaxBytes int    `json:"max_bytes,omitempty" jsonschema:"Response budget in bytes (default 8192). When the response exceeds this, a progressively condensed rendering is returned with a note saying how it was shortened."`
}

const (
	defaultImpactDepth = 5
	maxImpactDepth     = 10
	// maxHotspotFiles caps how many top churn-weighted files are treated as
	// hotspots when reordering impact callers. Ten is the rule-of-thumb top-N.
	maxHotspotFiles = 10
)

// impactBuildFromRepo is the production seam for callgraph.BuildFromRepo;
// handler-level tests can override it to avoid heavy parsing.
var impactBuildFromRepo = callgraph.BuildFromRepo

func registerImpact(server *mcp.Server, cfg Config, deps analyze.Deps, sem *SemanticDeps) {
	outputDir := cfg.OutputDir
	addTool(server, &mcp.Tool{
		Name: "impact_analysis",
		Description: "Analyze the blast radius of changing a function or method. " +
			"Shows direct callers, transitive callers, affected packages, " +
			"and risk classification (low/medium/high). " +
			"Useful before refactoring to understand what might break. " +
			"Suggests semantically similar symbols when the target is not found.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ImpactInput) (*mcp.CallToolResult, error) {
		return handleImpact(ctx, input, deps, sem, outputDir)
	})
}

func handleImpact(ctx context.Context, input ImpactInput, deps analyze.Deps, sem *SemanticDeps, outputDir string) (*mcp.CallToolResult, error) {
	if input.Repo == "" {
		return errResult("repo is required"), nil
	}
	if input.Symbol == "" {
		return errResult("symbol is required"), nil
	}

	root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps)
	if err != nil {
		return errResult(fmt.Sprintf("resolve repo: %s", err)), nil
	}
	defer cleanup()

	depth := input.Depth
	if depth <= 0 {
		depth = defaultImpactDepth
	}
	if depth > maxImpactDepth {
		depth = maxImpactDepth
	}

	cg, err := impactBuildFromRepo(ctx, callgraph.TraceRepoInput{
		Root:     root,
		Focus:    input.Focus,
		Language: input.Language,
	})
	if err != nil {
		return errResult(fmt.Sprintf("build call graph: %s", err)), nil
	}

	result := impact.Analyze(ctx, cg, input.Symbol, impact.Options{
		MaxDepth: depth,
		OxCodes:  deps.OxCodes,
		Root:     root,
		Language: input.Language,
		Refs:     deps.Refs,
		Repo:     input.Repo,
	})

	if !result.Found {
		msg := fmt.Sprintf("symbol %q not found in repository", input.Symbol)
		if suggestions := semanticSuggest(ctx, sem, root, input.Symbol, input.Language); suggestions != "" {
			return textResult(formatToolErrorWithSuggestions("impact_analysis", msg, suggestions)), nil
		}
		return errResult(msg), nil
	}

	// Compute git-churn hotspots and annotate callers whose files are among the
	// top-10 hotspots. Non-fatal: if churn data or snapshot is unavailable we
	// skip annotation entirely.
	var hotspotSet map[string]bool
	hctx, hcancel := context.WithTimeout(ctx, 15*time.Second)
	defer hcancel()
	churn, _ := compare.CollectChurn(hctx, root, 0)
	if len(churn) > 0 {
		snap, snapErr := compare.BuildSnapshot(hctx, root, compare.SnapshotOpts{Language: input.Language})
		var fc map[string]float64
		if snapErr == nil && snap != nil {
			fc = compare.FileComplexityFromSnapshot(snap)
		}
		hotspots := compare.ComputeHotspots(churn, fc)
		hotspotSet = topHotspotSet(hotspots, maxHotspotFiles)
	}

	// Reorder direct and transitive callers so hotspot-file callers come first,
	// preserving relative order within each group (stable partition).
	if hotspotSet != nil {
		result.DirectCallers = partitionByHotspot(result.DirectCallers, root, hotspotSet)
		result.TransitiveCallers = partitionByHotspot(result.TransitiveCallers, root, hotspotSet)
	}

	// Cap direct callers passed to expensive post-processing.
	// Symbols like "new" can have 289+ callers; sort+churn analysis on all would be slow.
	const maxDirectCallersForProcessing = 100
	var directCallersTruncNote string
	if len(result.DirectCallers) > maxDirectCallersForProcessing {
		totalDirect := len(result.DirectCallers)
		result.DirectCallers = result.DirectCallers[:maxDirectCallersForProcessing]
		directCallersTruncNote = fmt.Sprintf("showing top %d of %d direct callers (too many to process all)", maxDirectCallersForProcessing, totalDirect)
	}

	// Sort callers within each tier by PageRank (most architecturally important first).
	// Applied after hotspot partition so hotspot/non-hotspot tiers are preserved.
	repoKey := root // pass root path — graph.Symbol() calls GraphNameFor internally
	result.DirectCallers = sortCallersByPageRank(ctx, result.DirectCallers, deps.Graph, repoKey)
	if len(result.TransitiveCallers) > 0 {
		result.TransitiveCallers = sortCallersByPageRank(ctx, result.TransitiveCallers, deps.Graph, repoKey)
	}

	// Collect deduplicated hotspot caller names (Name field) in reordered order.
	var hotspotCallers []string
	if hotspotSet != nil {
		seen := make(map[string]bool)
		for _, caller := range append(result.DirectCallers, result.TransitiveCallers...) {
			rel := gitRelPath(root, caller.File)
			if hotspotSet[rel] && !seen[caller.Name] {
				seen[caller.Name] = true
				hotspotCallers = append(hotspotCallers, caller.Name)
			}
		}
	}

	// Build output with optional narrative.
	var notes []string
	if directCallersTruncNote != "" {
		notes = append(notes, directCallersTruncNote)
	}
	output := impactOutput{Result: result, Tier: cg.Tier, HotspotCallers: hotspotCallers, Notes: notes}

	if result.TotalAffected > 0 {
		prefix := fmt.Sprintf("Changed symbol: %s\n\nImpact analysis:\n", input.Symbol)
		output.Narrative = generateNarrative(ctx, deps.LLM, prompts.SystemPromptImpact, result, prefix)
	}

	// Progressive result-shortening ladder (#685 part 3): full →
	// no-narrative (drop the LLM prose — the "surrounding context" around
	// the structured caller data) → counts (drop per-caller lists + hotspot
	// callers, keep counts + blast_radius + risk_score + affected_packages).
	// renderLadder owns the five invariants so this tool cannot forget one.
	//
	// Rung choice reasoning: impact's primary value is the caller lists
	// (the actionable blast-radius data). The narrative is LLM prose
	// "surrounding context" — dropped first. The per-caller lists are the
	// core payload — dropped last, leaving counts so the agent still knows
	// "changing X affects N callers across M packages, blast_radius=high".
	//
	// Laziness: each rung's rendering work (json.Marshal + appendMetaFooter)
	// is INSIDE the closure, so an unreached rung is never rendered. The
	// common case (full result fits rung 1) does exactly one render, not
	// three. The narrative generation happens before the ladder (data
	// assembly, not rendering) — matching call_trace's precedent.
	//
	// Budget ownership: the LADDER owns the budget. When max_bytes > 0,
	// MarkBudgetApplied appends the sentinel so the wrapper skips
	// re-shaping at DefaultBudget (matches semantic_search's behaviour).
	ladder := mcpmeta.Ladder{
		{Name: "full", Render: func() string { return formatImpactFull(output) }},
		{Name: "no-narrative", Render: func() string { return formatImpactNoNarrative(output) }},
		{Name: "counts", Render: func() string { return formatImpactCounts(output) }},
	}
	budget := mcpmeta.ResolveBudget(input.MaxBytes, mcpmeta.DefaultBudget)
	body := renderLadder(ladder, "impact_analysis", outputDir, budget)
	if input.MaxBytes > 0 {
		body = mcpmeta.MarkBudgetApplied(body)
	}
	return textResult(body), nil
}

// topHotspotSet returns a set of the top-N hotspot file paths.
func topHotspotSet(hotspots []compare.HotspotFile, n int) map[string]bool {
	if len(hotspots) == 0 {
		return nil
	}
	if n > len(hotspots) {
		n = len(hotspots)
	}
	set := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		set[hotspots[i].File] = true
	}
	return set
}

// gitRelPath normalises an absolute file path to a git-relative path.
// If filepath.Rel fails or the path is already relative, the original is returned.
func gitRelPath(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil {
		return rel
	}
	return path
}

// partitionByHotspot performs a stable partition of callers, placing those
// whose file is in hotspotSet first while preserving relative order within
// each group.
func partitionByHotspot(callers []impact.AffectedSymbol, root string, hotspotSet map[string]bool) []impact.AffectedSymbol {
	if len(callers) == 0 {
		return callers
	}
	hot := callers[:0:0]
	cold := callers[:0:0]
	for _, c := range callers {
		rel := gitRelPath(root, c.File)
		if hotspotSet[rel] {
			hot = append(hot, c)
		} else {
			cold = append(cold, c)
		}
	}
	return append(hot, cold...)
}

// sortCallersByPageRank performs a stable sort of callers by their symbol PageRank
// descending. Uses graph.Symbol() for per-symbol lookup.
// Non-fatal: callers with lookup errors keep their original position (rank 0).
// sortCallersByPageRank sorts callers by structural importance (PageRank).
//
// Uses ONE batch TopPageRank query instead of N individual Symbol() lookups.
// This is critical for common symbols (e.g. "new") that can have 289+ callers —
// N individual queries × 5ms = 1.45s+ vs 1 batch query = ~30ms.
//
// Only the top-200 repo-wide symbols by PageRank are fetched; callers outside
// that set keep their relative position (their PageRank is architecturally
// negligible anyway).
func sortCallersByPageRank(ctx context.Context, callers []impact.AffectedSymbol, graph graphx.Analytics, repoKey string) []impact.AffectedSymbol {
	if graph == nil || len(callers) <= 1 {
		return callers
	}

	// Single batch query: fetch top-200 symbols by PageRank across the whole repo.
	const batchSize = 200
	signals, err := graph.TopPageRank(ctx, repoKey, batchSize)
	if err != nil || len(signals) == 0 {
		return callers // graph cold or unavailable — keep original order
	}

	// Build a local map: "file:name" → PageRank. O(batchSize).
	prMap := make(map[string]float64, len(signals))
	for _, sig := range signals {
		key := sig.Symbol.File + ":" + sig.Symbol.Name
		prMap[key] = sig.PageRank
	}

	// Look up each caller's PageRank from the map (O(N), no DB round-trips).
	ranks := make([]float64, len(callers))
	for i, c := range callers {
		ranks[i] = prMap[c.File+":"+c.Name]
	}

	// Stable sort descending by PageRank.
	idx := make([]int, len(callers))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		return ranks[idx[a]] > ranks[idx[b]]
	})

	sorted := make([]impact.AffectedSymbol, len(callers))
	for i, orig := range idx {
		sorted[i] = callers[orig]
	}
	return sorted
}

// impactOutput is the JSON response struct for impact_analysis. It embeds
// impact.Result and adds the tier, LLM narrative, hotspot caller names, and
// informational notes. It is package-level so the ladder rendering functions
// can accept it.
type impactOutput struct {
	*impact.Result
	Tier           string   `json:"tier,omitempty"`
	Narrative      string   `json:"narrative,omitempty"`
	HotspotCallers []string `json:"hotspot_callers,omitempty"` // caller symbol names whose file is a top hotspot
	Notes          []string `json:"notes,omitempty"`           // informational messages about truncation etc.
}

// impactFormatCount is a test-only seam for the render-count laziness
// assertion. Nil in production (zero overhead); tests set it to an int64
// counter that the three formatImpact* functions increment via
// atomic.AddInt64. The test then asserts EXACTLY ONE increment when rung 1
// fits — proving the unreached rungs were never rendered. The spy is on the
// RENDERING FUNCTION, not on the closure: PickFitting invokes only the
// closure it reaches, so a closure-level spy cannot distinguish lazy from
// eager and will pass either way.
var impactFormatCount *int64

// formatImpactFull is ladder rung 1: the complete impactOutput JSON. This
// is the fullest rendering — every caller, the LLM narrative, hotspot
// callers, notes, etc.
func formatImpactFull(output impactOutput) string {
	if impactFormatCount != nil {
		atomic.AddInt64(impactFormatCount, 1)
	}
	data, err := json.Marshal(&output)
	if err != nil {
		return fmt.Sprintf(`{"error":"marshal: %s"}`, err.Error())
	}
	return string(data)
}

// formatImpactNoNarrative is ladder rung 2: the impactOutput JSON with the
// LLM narrative dropped — the "surrounding context" around the structured
// caller data. The caller lists (the primary payload) are preserved.
func formatImpactNoNarrative(output impactOutput) string {
	if impactFormatCount != nil {
		atomic.AddInt64(impactFormatCount, 1)
	}
	condensed := output
	condensed.Narrative = ""
	data, err := json.Marshal(&condensed)
	if err != nil {
		return fmt.Sprintf(`{"error":"marshal: %s"}`, err.Error())
	}
	return string(data)
}

// impactCountsOutput is ladder rung 3 for impact_analysis: the blast-radius
// verdict + per-tier counts with the per-caller lists and narrative dropped.
// The cheapest rendering — the agent gets "changing X affects N callers
// across M packages, blast_radius=high" instead of a hard-truncated JSON
// fragment when even the no-narrative rendering overflows the budget.
type impactCountsOutput struct {
	Symbol                 string   `json:"symbol"`
	Found                  bool     `json:"found"`
	TotalAffected          int      `json:"total_affected"`
	DirectCallersCount     int      `json:"direct_callers_count"`
	TransitiveCallersCount int      `json:"transitive_callers_count"`
	HiddenCallersCount     int      `json:"hidden_callers_count,omitempty"`
	AffectedPackages       []string `json:"affected_packages"`
	CommunitiesCrossed     int      `json:"communities_crossed"`
	BlastRadius            string   `json:"blast_radius"`
	RiskScore              float64  `json:"risk_score"`
	Tier                   string   `json:"tier,omitempty"`
	HotspotCallersCount    int      `json:"hotspot_callers_count,omitempty"`
	TestsCoveringCount     int      `json:"tests_covering_count,omitempty"`
	Notes                  []string `json:"notes,omitempty"`
}

// formatImpactCounts is ladder rung 3: per-tier counts with the per-caller
// lists and narrative dropped.
func formatImpactCounts(output impactOutput) string {
	if impactFormatCount != nil {
		atomic.AddInt64(impactFormatCount, 1)
	}
	counts := impactCountsOutput{
		Symbol:                 output.Symbol,
		Found:                  output.Found,
		TotalAffected:          output.TotalAffected,
		DirectCallersCount:     len(output.DirectCallers),
		TransitiveCallersCount: len(output.TransitiveCallers),
		HiddenCallersCount:     len(output.HiddenCallers),
		AffectedPackages:       output.AffectedPackages,
		CommunitiesCrossed:     output.CommunitiesCrossed,
		BlastRadius:            output.BlastRadius,
		RiskScore:              output.RiskScore,
		Tier:                   output.Tier,
		HotspotCallersCount:    len(output.HotspotCallers),
		TestsCoveringCount:     len(output.TestsCovering),
		Notes:                  output.Notes,
	}
	data, err := json.Marshal(&counts)
	if err != nil {
		return fmt.Sprintf(`{"error":"marshal: %s"}`, err.Error())
	}
	return string(data)
}
