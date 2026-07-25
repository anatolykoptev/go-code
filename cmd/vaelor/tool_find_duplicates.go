package main

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anatolykoptev/vaelor/internal/codegraph"
	"github.com/anatolykoptev/vaelor/internal/mcpmeta"
	"github.com/anatolykoptev/vaelor/internal/semhealth"
)

// Default and validation constants for find_duplicates.
const (
	// defaultDupLimit is the maximum number of duplicate groups returned when
	// the caller does not specify a limit. Tightened from 50 to 20 so the
	// DEFAULT call fits the ~8 KB response budget (#571) — 50 groups at
	// ~200 chars each exceeded the 10149-char client truncation ceiling.
	defaultDupLimit = 20

	// validTiers are the accepted tier filter values. Kept as constants so callers
	// cannot pass free-form strings into the formatted output.
	dupTierExact     = "exact"
	dupTierVeryClose = "very-close"
	dupTierRelated   = "related"
)

// tierOrder is the display sequence for tier section headers. exact first,
// very-close second, related last — matches AnalyzeTriage's merge order.
var tierOrder = []string{dupTierExact, dupTierVeryClose, dupTierRelated}

// FindDuplicatesInput is the input schema for the find_duplicates tool.
type FindDuplicatesInput struct {
	Repo            string  `json:"repo" jsonschema:"Repository path or identifier to scan for intra-repo semantic duplicates (GitHub slug, full URL, or absolute local path)"`
	IncludeSameFile bool    `json:"include_same_file,omitempty" jsonschema:"Include same-file near-duplicates (default false — overloads and helpers in the same file are excluded)"`
	Tier            string  `json:"tier,omitempty" jsonschema:"Filter output to a single tier: exact | very-close | related (default: all tiers)"`
	Limit           int     `json:"limit,omitempty" jsonschema:"Maximum number of duplicate groups to report (default 20)"`
	Offset          int     `json:"offset,omitempty" jsonschema:"Skip the first N groups (for pagination). Use with limit to page through large result sets."`
	Language        string  `json:"language,omitempty" jsonschema:"Restrict to symbols in files of this language (e.g. go, python, typescript). Inferred from file extension."`
	MinLines        int     `json:"min_lines,omitempty" jsonschema:"Drop groups where any symbol's body is shorter than this many lines (best-effort brace scan from the symbol's start line)."`
	Threshold       float64 `json:"threshold,omitempty" jsonschema:"Minimum average similarity (0.0-1.0) to report a group. Exact-tier groups (sim=1.0) always pass."`
	Path            string  `json:"path,omitempty" jsonschema:"Restrict to symbols whose file path is under this directory (relative to repo root, e.g. \"internal/query\")."`
	MaxBytes        int     `json:"max_bytes,omitempty" jsonschema:"Response budget in bytes (default 8192). When the response exceeds this, the ranked head is returned with a continuation footer."`
}

// registerFindDuplicates registers the find_duplicates MCP tool.
//
// The tool surfaces intra-repo semantic duplicate candidates grouped in three tiers
// (exact body-hash clones, very-close, related) after the Phase-2 filter chain has
// removed false-positive classes:
//   - interface-implementation siblings (multiple types implementing the same interface)
//   - caller/callee CALLS pairs (look similar due to shared vocabulary, not duplication)
//   - test-mirror functions (test code mirrors production code by design)
//   - same-file overloads (within-file helper variants, unless include_same_file=true)
//   - low-signal symbol kinds (const, var, field, import)
//
// The tool is operator-invoked — not run automatically — because it queries pgvector
// with a self-join (O(n²) up to semhealthMaxFuncs guard). Output lists candidate pairs
// for human or agent review; the tool does not auto-flag or auto-fix.
//
// Disabled (no-op) when DATABASE_URL is not configured or the embedding store is nil.
func registerFindDuplicates(server *mcp.Server, deps SemanticDeps) {
	if deps.Store == nil {
		slog.Info("find_duplicates: DATABASE_URL not set or EMBED_URL not configured — tool disabled")
		return
	}

	addTool(server, &mcp.Tool{
		Name: "find_duplicates",
		Description: "Operator-invoked: find pairs of symbols in ONE repo that are semantically near-identical. " +
			"Targets the 'agent re-implemented ProcessX as HandleX' class of drift, which is invisible to " +
			"byte-level diff but shows high cosine similarity in the embedding space. " +
			"Three tiers: 'exact' (body-hash clones — textually identical), 'very-close' (similarity ≥ 0.88, " +
			"strong refactor candidate), 'related' (similarity ≥ 0.80, worth human review). " +
			"False-positive filters run before output: interface-implementation siblings, caller/callee CALLS " +
			"pairs, test mirrors, same-file overloads, and low-signal kinds (const/var/field/import). " +
			"The filter-drop breakdown in the output header shows which filter is doing the most work — " +
			"use it to assess precision. " +
			"Output is a triage list for human or agent review, not an auto-flag list. " +
			"Requires EMBED_URL + DATABASE_URL. Skips repos with > 5000 indexed symbols (O(n²) self-join guard).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in FindDuplicatesInput) (*mcp.CallToolResult, error) {
		return handleFindDuplicates(ctx, deps, in)
	})
}

// handleFindDuplicates is the extracted handler, callable from tests.
func handleFindDuplicates(ctx context.Context, deps SemanticDeps, in FindDuplicatesInput) (*mcp.CallToolResult, error) {
	if in.Repo == "" {
		return errResult("find_duplicates: repo is required"), nil
	}

	// Validate the tier filter early so we fail fast before any DB work.
	if in.Tier != "" && in.Tier != dupTierExact && in.Tier != dupTierVeryClose && in.Tier != dupTierRelated {
		return errResult(fmt.Sprintf(
			"find_duplicates: unknown tier %q — valid values: exact, very-close, related",
			in.Tier,
		)), nil
	}

	limit := in.Limit
	if limit <= 0 {
		limit = defaultDupLimit
	}

	root, cleanup, err := resolveRoot(ctx, in.Repo, "", deps.AnalyzeDeps)
	if err != nil {
		return nil, fmt.Errorf("find_duplicates: %w", err)
	}
	defer cleanup()

	repoKey := codegraph.GraphNameFor(root)

	// Compute totalFuncs via embeddings.Store.Stats — a single COUNT(*) GROUP BY
	// query that returns the total symbol count for this repo_key. This over-counts
	// relative to countFuncs (which filters to function/method kinds only), making
	// it a conservative upper bound: if the total symbol count exceeds
	// semhealthMaxFuncs (5000), there are definitely too many functions to self-join
	// safely. The cost is a possible false skip on repos with 5000 total symbols but
	// fewer than 5000 functions; the benefit is avoiding an expensive full parse.
	//
	// compare.BuildSnapshot + countFuncs would be more accurate but adds 30-90s of
	// AST parsing per call — unacceptable for an operator-invoked MCP tool.
	totalSymbols := 0
	if stats, statsErr := deps.Store.Stats(ctx); statsErr == nil {
		totalSymbols = stats[repoKey]
	} else {
		slog.Warn("find_duplicates: Stats failed, using 1 as conservative default",
			slog.String("repo_key", repoKey),
			slog.Any("error", statsErr),
		)
		// Use 1 as a non-zero sentinel so AnalyzeTriage is not gated by totalFuncs==0.
		// The semhealthMaxFuncs guard inside AnalyzeTriage handles large repos.
		totalSymbols = 1
	}

	// Typed-nil guard: a nil *embeddings.Expander assigned to the GraphPairFilter
	// interface creates a non-nil interface wrapping a nil pointer, which would panic
	// inside the AGE graph filters. Explicit nil preserves graceful-degradation
	// (filter is a no-op when gf is nil). Mirror of collectSemanticDupGroups in
	// tool_code_health_stages.go.
	var gf semhealth.GraphPairFilter
	if deps.Expander != nil {
		gf = deps.Expander
	}

	res := semhealth.AnalyzeTriage(
		ctx,
		deps.Store,
		gf,
		repoKey, // graphName == repoKey for AGE
		repoKey,
		totalSymbols,
		semhealth.TriageOpts{IncludeSameFile: in.IncludeSameFile, Root: root},
	)

	if res == nil {
		return textResult(fmt.Sprintf(
			"find_duplicates: repo not indexed or no embeddings for %q — run semantic indexing first",
			in.Repo,
		)), nil
	}

	// Post-filters (issue #568, 7x+ demand): language / path / threshold /
	// min_lines narrow the triage result. Applied AFTER AnalyzeTriage so the
	// candidate+filter-drop counts in the header still reflect the full scan,
	// and the post-filter only affects which groups are reported.
	res.Groups = filterDupGroups(res.Groups, dupFilterOpts{
		Language:  in.Language,
		Path:      in.Path,
		Threshold: in.Threshold,
		MinLines:  in.MinLines,
		Root:      root,
	})

	formatted := formatTriage(res, in.Tier, in.Offset, limit)
	// Apply per-call budget override when max_bytes is set; the addTool
	// wrapper applies the default budget otherwise. A byte-cut is NOT a page
	// boundary — an unknown number of this page's groups were rendered but
	// cut, so the hint must not advance offset (skipping cut groups loses
	// data); it suggests re-reading the same page with more room instead.
	if in.MaxBytes > 0 {
		formatted = mcpmeta.ShapeWithHint(formatted, budgetOverride(in.MaxBytes),
			fmt.Sprintf("byte-cut mid-page — retry offset=%d with a larger max_bytes, or narrow with language=/path=", in.Offset))
	}
	return textResult(formatted), nil
}

// formatTriage renders a TriageResult as a human-readable triage report.
//
// Format:
//
//	candidates=N reported=M (exact=a very-close=b related=c)  filtered: tests=x same_file=y kind=z calls_edge=w interface_sibling=v
//
//	=== exact ===
//	symA (file:line, kind) ↔ symB (file:line, kind)  [exact]  sim=1.00
//
//	=== very-close ===
//	...
//
// When res.TimedOut is true, a warning line is prepended:
//
//	⚠ search incomplete (some queries timed out) — results may be partial
//
// tierFilter, when non-empty, restricts output to groups of that tier.
// offset skips the first N groups (after tier filtering) for pagination.
// limit caps the total number of groups rendered.
// When offset > 0, a pagination header is prepended: "showing N-M of T".
//
// The function is pure (no side effects) to make it unit-testable without
// a live DB or context.
func formatTriage(res *semhealth.TriageResult, tierFilter string, offset, limit int) string {
	if res == nil || (len(res.Groups) == 0 && res.Candidates == 0) {
		if res != nil && res.TimedOut {
			return "⚠ search incomplete (some queries timed out) — results may be partial\nno semantic duplicates found in partial result set"
		}
		return "no semantic duplicates found (no candidates returned by pgvector)"
	}

	groups, totalAfterFilter := filterOffsetAndLimitGroups(res.Groups, tierFilter, offset, limit)

	var sb strings.Builder
	if res.TimedOut {
		fmt.Fprint(&sb, "⚠ search incomplete (some queries timed out) — results may be partial\n")
	}
	if len(groups) == 0 {
		if offset > 0 && totalAfterFilter > 0 {
			fmt.Fprintf(&sb, "no more groups at offset=%d (total=%d after tier filter) — use a smaller offset", offset, totalAfterFilter)
		} else {
			fmt.Fprintf(&sb, "no semantic duplicates found in %q tier (candidates=%d before filtering)", tierFilter, res.Candidates)
		}
		return sb.String()
	}

	// Pagination header when offsetting.
	if offset > 0 {
		fmt.Fprintf(&sb, "showing %d-%d of %d groups (offset=%d)\n", offset+1, offset+len(groups), totalAfterFilter, offset)
	}

	fmt.Fprint(&sb, formatTriageSummary(res, tierFilter))
	fmt.Fprint(&sb, formatTriageTiers(groups))

	// Continuation footer when there are more groups beyond this page.
	if offset+len(groups) < totalAfterFilter {
		next := offset + len(groups)
		fmt.Fprintf(&sb, "\n[truncated: %d more groups — pass offset=%d to continue]", totalAfterFilter-next, next)
	}
	return sb.String()
}

// filterOffsetAndLimitGroups applies the tier filter, then offset, then limit
// cap to a group slice. Returns the page slice and the total count after
// tier filtering (before offset/limit) so the caller can emit a pagination
// footer.
func filterOffsetAndLimitGroups(groups []semhealth.DupGroup, tierFilter string, offset, limit int) ([]semhealth.DupGroup, int) {
	if tierFilter != "" {
		filtered := groups[:0:0]
		for _, g := range groups {
			if g.Tier == tierFilter {
				filtered = append(filtered, g)
			}
		}
		groups = filtered
	}
	total := len(groups)

	if limit <= 0 {
		limit = defaultDupLimit
	}
	if offset > 0 {
		if offset >= len(groups) {
			return nil, total
		}
		groups = groups[offset:]
	}
	if len(groups) > limit {
		groups = groups[:limit]
	}
	return groups, total
}

// formatTriageSummary renders the header line with candidate and filter-drop counts.
func formatTriageSummary(res *semhealth.TriageResult, tierFilter string) string {
	reported := countReported(res.Groups, tierFilter)
	exact := res.ReportedByTier[dupTierExact]
	veryClose := res.ReportedByTier[dupTierVeryClose]
	related := res.ReportedByTier[dupTierRelated]

	filterParts := buildFilterParts(res.Dropped)

	return fmt.Sprintf(
		"candidates=%d reported=%d (exact=%d very-close=%d related=%d)  filtered: %s\n",
		res.Candidates, reported, exact, veryClose, related,
		strings.Join(filterParts, " "),
	)
}

// countReported returns the number of groups matching tierFilter, or all groups
// when tierFilter is empty.
func countReported(groups []semhealth.DupGroup, tierFilter string) int {
	if tierFilter == "" {
		return len(groups)
	}
	n := 0
	for _, g := range groups {
		if g.Tier == tierFilter {
			n++
		}
	}
	return n
}

// buildFilterParts converts the Dropped map to sorted "key=value" strings.
func buildFilterParts(dropped map[string]int) []string {
	keys := make([]string, 0, len(dropped))
	for k := range dropped {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, dropped[k]))
	}
	return parts
}

// formatTriageTiers renders group lines organised by tier in canonical order.
func formatTriageTiers(groups []semhealth.DupGroup) string {
	byTier := make(map[string][]semhealth.DupGroup, 3) //nolint:mnd
	for _, g := range groups {
		byTier[g.Tier] = append(byTier[g.Tier], g)
	}

	var sb strings.Builder
	for _, tier := range tierOrder {
		tierGroups := byTier[tier]
		if len(tierGroups) == 0 {
			continue
		}
		fmt.Fprintf(&sb, "\n=== %s ===\n", tier)
		for _, g := range tierGroups {
			fmt.Fprint(&sb, formatDupGroupLine(g))
		}
	}
	return sb.String()
}

// formatDupGroupLine renders a single DupGroup as a triage line.
//
// For a 2-symbol group (the common case) the format is:
//
//	symA (file:line, kind) ↔ symB (file:line, kind)  [tier]  sim=0.93
//
// For groups with more than 2 symbols, each symbol is listed on its own
// indented line followed by the shared tier and similarity.
func formatDupGroupLine(g semhealth.DupGroup) string {
	sim := fmt.Sprintf("%.2f", g.AvgSimilarity)

	if len(g.Symbols) == 2 { //nolint:mnd
		a, b := g.Symbols[0], g.Symbols[1]
		return fmt.Sprintf(
			"  %s (%s:%d, %s) ↔ %s (%s:%d, %s)  [%s]  sim=%s\n",
			a.Name, a.File, a.Line, a.Kind,
			b.Name, b.File, b.Line, b.Kind,
			g.Tier, sim,
		)
	}

	// Multi-member group (3+ symbols via union-find chain).
	var sb strings.Builder
	fmt.Fprintf(&sb, "  [%s] sim=%s group (%d symbols):\n", g.Tier, sim, len(g.Symbols))
	for _, s := range g.Symbols {
		fmt.Fprintf(&sb, "    %s (%s:%d, %s)\n", s.Name, s.File, s.Line, s.Kind)
	}
	return sb.String()
}
