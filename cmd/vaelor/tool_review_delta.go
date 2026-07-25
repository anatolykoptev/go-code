package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"sort"
	"strings"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/codegraph"
	"github.com/anatolykoptev/vaelor/internal/review"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ReviewDeltaInput struct {
	Repo            string `json:"repo" jsonschema:"Repository: GitHub slug (owner/repo), full URL, or absolute local host path"`
	Base            string `json:"base,omitempty" jsonschema:"Base ref to diff against (commit SHA, branch, tag, HEAD~N). Default: HEAD~1"`
	Head            string `json:"head,omitempty" jsonschema:"Head ref for the diff (commit SHA, branch, tag). Default: HEAD. The DIFF is computed base..head; when head differs from the current checkout, impacted-symbol analysis runs against an isolated worktree at head so it reflects head tree (falls back to the working tree with a note if the worktree cannot be created)."`
	Depth           int    `json:"depth,omitempty" jsonschema:"Impact traversal depth (default 2, max 5)"`
	Language        string `json:"language,omitempty" jsonschema:"Limit to files of this language (e.g. go, python)"`
	ExcludeSnippets bool   `json:"exclude_snippets,omitempty" jsonschema:"Set true to omit source code snippets (included by default)"`
	FullImpact      bool   `json:"full_impact,omitempty" jsonschema:"Set true to return the COMPLETE impacted_symbols list, uncapped. Default caps to the top maxReviewImpacted entries (ranked by impact distance ascending, then confidence descending) and marks the response truncated=true with the true total when more exist."`
}

const (
	defaultReviewDepth   = 2
	maxReviewDepth       = 5
	maxReviewOutputChars = 40_000 // ~10K tokens; backstop that drops snippets if still oversized
	maxReviewImpacted    = 50     // default cap on impacted_symbols entries (see capImpactedSymbols)
)

func registerReviewDelta(server *mcp.Server, _ Config, deps analyze.Deps, graphStore *codegraph.Store) {
	addTool(server, &mcp.Tool{
		Name: "review_delta",
		Description: "Analyze changes between two git refs and compute differential impact. " +
			"Returns changed files, changed symbols, impacted downstream symbols, " +
			"untested changes, and risk guidance. " +
			"Ideal for pre-merge review: shows blast radius of a branch's changes. " +
			"Set head= to shape the DIFF as base..head; when head differs from the " +
			"current checkout, impacted-symbol analysis runs against an isolated " +
			"worktree at head so it reflects head's tree (falls back to the working " +
			"tree with a note if the worktree can't be created). " +
			"impacted_symbols is capped to the top " + fmt.Sprint(maxReviewImpacted) +
			" entries by default (ranked by impact distance then confidence); " +
			"set full_impact=true for the complete list.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ReviewDeltaInput) (*mcp.CallToolResult, error) {
		return handleReviewDelta(ctx, input, deps, graphStore)
	})
}

type xmlDeltaResponse struct {
	XMLName xml.Name `xml:"response"`
	Tool    string   `xml:"tool,attr"`
	Tier    string   `xml:"tier,attr,omitempty"`

	ChangedFiles    []xmlChangedFile   `xml:"changed_files>file"`
	ChangedSymbols  []xmlChangedSymbol `xml:"changed_symbols>symbol"`
	ImpactedSymbols xmlImpactedList    `xml:"impacted_symbols"`
	Untested        []string           `xml:"untested>symbol,omitempty"`
	Snippets        []xmlSnippet       `xml:"snippets>snippet,omitempty"`
	Risk            xmlRisk            `xml:"risk"`
	Quality         *xmlQualitySignals `xml:"quality,omitempty"`
	// Verdict is populated only by review_pr (via deriveVerdict); review_delta
	// leaves it nil so omitempty suppresses the element for that tool.
	Verdict *xmlVerdict `xml:"verdict,omitempty"`
}

type xmlChangedFile struct {
	Path    string `xml:"path,attr"`
	Added   int    `xml:"added,attr"`
	Removed int    `xml:"removed,attr"`
}

type xmlChangedSymbol struct {
	Name          string       `xml:"name,attr"`
	Kind          string       `xml:"kind,attr"`
	File          string       `xml:"file,attr"`
	ChangeType    string       `xml:"change,attr"`
	Flag          *xmlFlagHint `xml:"flag,omitempty"`
	DeadCodeScore float32      `xml:"deadCodeScore,attr,omitempty"`
	DeadCodeNote  string       `xml:"deadCodeNote,attr,omitempty"`
}

type xmlImpacted struct {
	Name       string  `xml:"name,attr"`
	File       string  `xml:"file,attr"`
	Distance   int     `xml:"distance,attr"`
	ChangedBy  string  `xml:"changed_by,attr"`
	Confidence float64 `xml:"confidence,attr"`
}

// xmlImpactedList wraps the impacted_symbols entries with an explicit count so
// a capped default can never be mistaken for a complete list. Total is the
// true number of impacted symbols the analysis found; Shown is how many are
// serialized below; Truncated is set only when Shown < Total. See
// capImpactedSymbols for how the default cap is applied.
type xmlImpactedList struct {
	Total     int           `xml:"total,attr"`
	Shown     int           `xml:"shown,attr"`
	Truncated bool          `xml:"truncated,attr,omitempty"`
	Symbols   []xmlImpacted `xml:"symbol"`
}

type xmlRisk struct {
	Level       string   `xml:"level,attr"`
	Score       float64  `xml:"score,attr"`
	Flags       []string `xml:"flag,omitempty"`
	Suggestions []string `xml:"suggestion,omitempty"`
}

type xmlSnippet struct {
	File   string   `xml:"file,attr"`
	Symbol string   `xml:"symbol,attr"`
	Start  int      `xml:"start,attr"`
	End    int      `xml:"end,attr"`
	Code   xmlCDATA `xml:"code"`
}

func handleReviewDelta(ctx context.Context, input ReviewDeltaInput, deps analyze.Deps, graphStore *codegraph.Store) (*mcp.CallToolResult, error) {
	if input.Repo == "" {
		return errResult("repo is required"), nil
	}

	root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps)
	if err != nil {
		return errResult(fmt.Sprintf("resolve repo: %s", err)), nil
	}
	defer cleanup()

	depth := input.Depth
	if depth <= 0 {
		depth = defaultReviewDepth
	}
	if depth > maxReviewDepth {
		depth = maxReviewDepth
	}

	// Tree-walking root for the call-graph/impact stage. When head is set and
	// points at a different tree than the current checkout, build an isolated
	// worktree at head (mirror review_pr's CreatePRWorktree) so impacted
	// symbols reflect head's tree, not the warm checkout. The base..head diff
	// stays against `root` (it resolves git refs, not files). Falls back to the
	// live checkout with an honest note when the worktree can't be created —
	// degrade, don't crash.
	analysisRoot := root
	var fallbackNote string
	if headRef := input.Head; headRef != "" && !strings.EqualFold(headRef, "HEAD") {
		wt, ok, note := tryHeadWorktree(ctx, root, headRef)
		if ok {
			defer wt.Cleanup()
			analysisRoot = wt.Path
		} else if note != "" {
			fallbackNote = note
		}
	}

	result, err := review.DeltaReview(ctx, review.DeltaInput{
		Root:            root,
		AnalysisRoot:    analysisRoot,
		Base:            input.Base,
		Head:            input.Head,
		Depth:           depth,
		Language:        input.Language,
		IncludeSnippets: !input.ExcludeSnippets,
		OxCodes:         deps.OxCodes,
		PathRewrite:     makePathRewrite(deps.PathMappings),
	})
	if err != nil {
		return errResult(fmt.Sprintf("delta review: %s", err)), nil
	}

	findings := applyPolicy(ctx, root, result)
	for _, f := range findings {
		result.Risk.Flags = append(result.Risk.Flags, fmt.Sprintf("policy:%s %s:%d %s", f.Rule, f.Path, f.Line, f.Message))
	}

	// Annotate removed symbols with dead_code_score when available.
	if graphStore != nil {
		for i := range result.ChangedSymbols {
			s := &result.ChangedSymbols[i]
			if s.ChangeType != review.ChangeRemoved {
				continue
			}
			if s.Symbol == nil {
				continue
			}
			score, ok := graphStore.LoadDeadCodeScore(ctx, root, s.Symbol.Name, s.Symbol.File)
			if ok && score > 0.25 {
				s.DeadCodeScore = score
				s.DeadCodeNote = fmt.Sprintf("CE dead-code probability %.0f%% — likely safe to remove", float64(score)*100)
			}
		}
	}

	resp := buildDeltaXML(result)
	resp.Quality = collectQualitySignals(ctx, root, input.Language, deps.OxCodes)

	// Compact-by-default: cap impacted_symbols to the top maxReviewImpacted
	// entries (ranked, not a source-order prefix) unless the caller explicitly
	// asked for the full list. This is the primary size lever — a large multi-
	// day delta's response is dominated by hundreds of impacted_symbols
	// entries, not by risk/summary data (#391).
	resp.ImpactedSymbols = capImpactedSymbols(resp.ImpactedSymbols.Symbols, maxReviewImpacted, input.FullImpact)

	data, err := xml.Marshal(resp)
	if err != nil {
		return errResult(fmt.Sprintf("marshal: %s", err)), nil
	}
	out := string(data)

	// Defensive backstop: impacted_symbols is already bounded above, but a
	// caller-requested full_impact=true on a huge changeset — combined with
	// source snippets — can still overflow the token ceiling. Dropping
	// snippets is the last lever; it no longer needs to touch impacted_symbols,
	// which always carries an honest total/shown/truncated.
	if len(out) > maxReviewOutputChars && len(resp.Snippets) > 0 {
		resp.Snippets = nil
		data, err = xml.Marshal(resp)
		if err != nil {
			return errResult(fmt.Sprintf("marshal: %s", err)), nil
		}
		out = string(data)
	}

	// When head differed from the checkout AND we could not build a worktree
	// at head, impacted symbols reflect the live working tree (not head's).
	// Surface that honestly so an agent never mistakes the blast radius for
	// head's tree. When the worktree succeeded (or head equals the checkout,
	// or head was unset), no note is needed — the impact stage already
	// reflects head's tree.
	if fallbackNote != "" {
		out += "\n" + fallbackNote
	}

	return textResult(out), nil
}

// tryHeadWorktree resolves headRef to a SHA, compares it to the current
// checkout HEAD, and — when they differ — creates an isolated worktree at
// headRef (mirroring review_pr's CreatePRWorktree) so tree-walking analysis
// sees head's tree instead of the warm checkout.
//
// Returns:
//   - (wt, true, "")  on success — caller MUST defer wt.Cleanup().
//   - (nil, false, "")  when head equals the checkout (fast path: no worktree
//     needed, impacted symbols reflect head's tree via the live checkout).
//   - (nil, false, note)  when the worktree could not be created (unknown ref,
//     fetch failed, worktree add failed) — caller appends `note` to the
//     response; impacted symbols fall back to the working tree.
func tryHeadWorktree(ctx context.Context, root, headRef string) (wt *review.PRWorktree, ok bool, note string) {
	headSHA, err := resolveRef(ctx, root, headRef)
	if err != nil {
		return nil, false, "note: could not resolve head=" + headRef + " for worktree (" + err.Error() +
			"); impacted symbols reflect the current working tree — use review_pr for a full no-checkout branch review"
	}
	curSHA, err := review.GitExec(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		return nil, false, "note: could not resolve checkout HEAD (" + err.Error() +
			"); impacted symbols reflect the current working tree — use review_pr for a full no-checkout branch review"
	}
	if strings.TrimSpace(headSHA) == strings.TrimSpace(curSHA) {
		// Checkout already at head — impacted symbols reflect head's tree via
		// the live checkout. No worktree, no note.
		return nil, false, ""
	}
	wt, err = review.CreatePRWorktree(ctx, root, strings.TrimSpace(headSHA))
	if err != nil {
		return nil, false, "note: head=" + headRef + " worktree creation failed (" + err.Error() +
			"); impacted symbols reflect the current working tree — use review_pr for a full no-checkout branch review"
	}
	return wt, true, ""
}

// resolveRef resolves a git ref to a SHA, fetching it from origin first if it
// is unknown locally (e.g. a remote branch that has not been fetched yet).
func resolveRef(ctx context.Context, root, ref string) (string, error) {
	out, err := review.GitExec(ctx, root, "rev-parse", ref)
	if err == nil {
		return out, nil
	}
	// Ref unknown locally — try fetching it from origin, then resolve again.
	if _, ferr := review.GitExec(ctx, root, "fetch", "origin", ref); ferr != nil {
		return "", fmt.Errorf("rev-parse %q: %w (fetch: %v)", ref, err, ferr)
	}
	out, err = review.GitExec(ctx, root, "rev-parse", ref)
	if err != nil {
		return "", fmt.Errorf("rev-parse %q after fetch: %w", ref, err)
	}
	return out, nil
}

// capImpactedSymbols bounds the impacted_symbols list returned to the caller.
// Entries are ranked by impact distance ascending (closer callers are more
// actionable) then confidence descending, so a capped default keeps the most
// meaningful entries rather than an arbitrary source-order prefix. full
// disables capping entirely — the caller explicitly asked for the complete
// list. The result always reports the true Total and Symbols/Shown length, and
// Truncated is set whenever Shown < Total, so a caller can never mistake a
// capped response for a complete one.
func capImpactedSymbols(all []xmlImpacted, limit int, full bool) xmlImpactedList {
	ranked := make([]xmlImpacted, len(all))
	copy(ranked, all)
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Distance != ranked[j].Distance {
			return ranked[i].Distance < ranked[j].Distance
		}
		return ranked[i].Confidence > ranked[j].Confidence
	})

	total := len(ranked)
	if full || total <= limit {
		return xmlImpactedList{Total: total, Shown: total, Symbols: ranked}
	}
	return xmlImpactedList{
		Total:     total,
		Shown:     limit,
		Truncated: true,
		Symbols:   ranked[:limit],
	}
}

func buildDeltaXML(r *review.DeltaResult) xmlDeltaResponse {
	resp := xmlDeltaResponse{
		Tool: "review_delta",
		Tier: r.Tier,
	}

	for _, f := range r.ChangedFiles {
		resp.ChangedFiles = append(resp.ChangedFiles, xmlChangedFile{
			Path: f.Path, Added: f.Added, Removed: f.Removed,
		})
	}
	for _, cs := range r.ChangedSymbols {
		xcs := xmlChangedSymbol{
			Name: cs.Symbol.Name, Kind: string(cs.Symbol.Kind),
			File: cs.FileDiff.Path, ChangeType: string(cs.ChangeType),
		}
		if cs.Flag != "" {
			xcs.Flag = &xmlFlagHint{Kind: cs.Flag, Note: cs.Note}
		}
		if cs.DeadCodeScore > 0 {
			xcs.DeadCodeScore = cs.DeadCodeScore
			xcs.DeadCodeNote = cs.DeadCodeNote
		}
		resp.ChangedSymbols = append(resp.ChangedSymbols, xcs)
	}
	var impacted []xmlImpacted
	for _, is := range r.ImpactedSymbols {
		impacted = append(impacted, xmlImpacted{
			Name: is.Name, File: is.File, Distance: is.Distance,
			ChangedBy: is.ChangedBy, Confidence: is.Confidence,
		})
	}
	// Full, unranked, uncapped baseline — review_pr's dry-run path marshals
	// this as-is (it has its own consumers and isn't in scope for #391's
	// default-cap change); review_delta's handler re-caps it via
	// capImpactedSymbols before marshaling its own response.
	resp.ImpactedSymbols = xmlImpactedList{
		Total:   len(impacted),
		Shown:   len(impacted),
		Symbols: impacted,
	}
	for _, s := range r.Snippets {
		resp.Snippets = append(resp.Snippets, xmlSnippet{
			File: s.File, Symbol: s.Symbol,
			Start: s.StartLine, End: s.EndLine,
			Code: xmlCDATA{Inner: wrapCDATA(s.Code)},
		})
	}
	resp.Untested = r.UntestedSymbols
	resp.Risk = xmlRisk{
		Level: r.Risk.RiskLevel, Score: r.Risk.RiskScore,
		Flags: r.Risk.Flags, Suggestions: r.Risk.Suggestions,
	}

	return resp
}
