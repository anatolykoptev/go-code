package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"strings"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/codegraph"
	"github.com/anatolykoptev/vaelor/internal/learnings"
	"github.com/anatolykoptev/vaelor/internal/review"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ReviewPRInput is the unified input for the review_pr tool.
// When DryRun is true (the default) the review is returned without
// posting to GitHub or persisting learnings. Set DryRun=false to post
// the review as a GitHub PR review and persist per-symbol learnings.
type ReviewPRInput struct {
	Repo  string `json:"repo" jsonschema:"Repository: GitHub slug (owner/repo) or full URL"`
	PR    int    `json:"pr" jsonschema:"Pull request number"`
	Depth int    `json:"depth,omitempty" jsonschema:"Impact traversal depth (default 2, max 5)"`
	// Language limits analysis to files of the given language (dry-run path only).
	Language string `json:"language,omitempty" jsonschema:"Limit to files of this language"`
	// DryRun controls whether the review is posted. When true (default) the
	// review XML/body is returned without any side effects. Set false to post
	// to GitHub and write learnings.
	DryRun bool `json:"dry_run,omitempty" jsonschema:"When true (default), returns the review without posting or persisting. Set false to post to GitHub + write learnings."`
	// Event is the GitHub review event. Required when DryRun=false.
	// Accepted values: APPROVE | COMMENT | REQUEST_CHANGES.
	Event string `json:"event,omitempty" jsonschema:"Required when dry_run=false: APPROVE | COMMENT | REQUEST_CHANGES."`
}

func registerReviewPR(server *mcp.Server, _ Config, deps analyze.Deps, graphStore *codegraph.Store) {
	addTool(server, &mcp.Tool{
		Name: "review_pr",
		Description: "Review a pull request: fetches PR metadata and diff, " +
			"then runs differential impact analysis on all changes. " +
			"Returns changed symbols, blast radius, untested code, and risk guidance. " +
			"When dry_run=false (requires event=APPROVE|COMMENT|REQUEST_CHANGES), " +
			"posts the review to GitHub and persists per-symbol learnings. " +
			"Requires GITHUB_TOKEN when dry_run=false.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ReviewPRInput) (*mcp.CallToolResult, error) {
		return handleReviewPR(ctx, input, deps, graphStore)
	})
}

func handleReviewPR(ctx context.Context, input ReviewPRInput, deps analyze.Deps, graphStore *codegraph.Store) (*mcp.CallToolResult, error) {
	if input.Repo == "" {
		return errResult("repo is required"), nil
	}
	if input.PR <= 0 {
		return errResult("pr number is required"), nil
	}

	// Backward-compat semantics:
	//   Event absent  → always dry-run (matches pre-consolidation review_pr behaviour).
	//   Event present → post unless DryRun=true (matches pre-consolidation review_pr_post).
	// Users who want to preview what would be posted can set both Event and DryRun=true.
	dryRun := input.DryRun || input.Event == ""

	root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps)
	if err != nil {
		return errResult(fmt.Sprintf("resolve repo: %s", err)), nil
	}
	defer cleanup()

	base, head, err := fetchPRBase(ctx, root, input.PR)
	if err != nil {
		base = "origin/main"
		head = "HEAD"
	}

	// Worktree-isolated checkout at FETCH_HEAD. Without this the call graph
	// is built from the warm clone's working tree (usually main), so impact
	// analysis on PR-only symbols (new functions in the PR) misses callers.
	// Falls back gracefully to the warm clone if worktree creation fails —
	// diff still works (it operates on git refs, not files), only call
	// graph signal is reduced.
	analysisRoot := root
	if head == "FETCH_HEAD" {
		if wt, wtErr := review.CreatePRWorktree(ctx, root, head); wtErr == nil {
			defer wt.Cleanup()
			analysisRoot = wt.Path
			// Inside the worktree, FETCH_HEAD is now the working tree —
			// downstream diff should compare base..HEAD.
			head = "HEAD"
		} else {
			// Stay on warm clone; diff will use FETCH_HEAD as before.
			fmt.Fprintf(os.Stderr, "review_pr: worktree fallback (warm clone): %s\n", wtErr)
		}
	}

	depth := input.Depth
	if depth <= 0 {
		depth = defaultReviewDepth
	}
	if depth > maxReviewDepth {
		depth = maxReviewDepth
	}

	result, err := review.DeltaReview(ctx, review.DeltaInput{
		Root:     analysisRoot,
		Base:     base,
		Head:     head,
		Depth:    depth,
		Language: input.Language,
		OxCodes:  deps.OxCodes,
	})
	if err != nil {
		return errResult(fmt.Sprintf("review: %s", err)), nil
	}

	findings := applyPolicy(ctx, root, result)
	for _, f := range findings {
		result.Risk.Flags = append(result.Risk.Flags, fmt.Sprintf("policy:%s %s:%d %s", f.Rule, f.Path, f.Line, f.Message))
	}

	// Enrich changed symbols with graph signals (community_move / high_surprise).
	review.ApplyGraphFlags(ctx, deps.Graph, root, result.ChangedSymbols, nil)

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
				s.DeadCodeNote = fmt.Sprintf("CE dead-code probability %.0f%% - likely safe to remove", float64(score)*100)
			}
		}
	}

	if dryRun {
		return reviewPRDryRun(ctx, input, result)
	}
	return reviewPRPost(ctx, input, deps, root, result, findings)
}

// reviewPRDryRun executes the dry-run path: persists risk-level learnings
// (best-effort) and returns the review as XML. Byte-identical to the former
// standalone review_pr behaviour.
func reviewPRDryRun(ctx context.Context, input ReviewPRInput, result *review.DeltaResult) (*mcp.CallToolResult, error) {
	// Persist learnings and look up prior findings.
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		store, err := learnings.New(ctx, dsn, nil)
		if err == nil {
			defer store.Close()
			slug := input.Repo
			for _, cs := range result.ChangedSymbols {
				// Lookup hints.
				prior, _ := store.Nearest(ctx, slug, cs.Symbol.Name, 3)
				for _, p := range prior {
					result.Risk.Suggestions = append(result.Risk.Suggestions,
						fmt.Sprintf("prior review on %s: %s (%s)", p.Symbol, p.Flag, p.PRURL))
				}
				// Record current risk level from impact analysis.
				_ = store.Upsert(ctx, learnings.Record{
					Repo: slug, Symbol: cs.Symbol.Name,
					RiskLevel: result.Risk.RiskLevel,
					Flag:      strings.Join(result.Risk.Flags, ";"),
					PRURL:     fmt.Sprintf("https://github.com/%s/pull/%d", slug, input.PR),
				})
			}
		}
	}

	resp := buildDeltaXML(result)
	resp.Tool = "review_pr"
	resp.Verdict = deriveVerdict(result)
	data, err := xml.Marshal(resp)
	if err != nil {
		return errResult(fmt.Sprintf("marshal: %s", err)), nil
	}

	return textResult(string(data)), nil
}

// deriveVerdict maps a delta review's risk guidance into a structured merge
// verdict. It recognises the three levels produced by review.classifyRisk
// ("low", "medium", "high"); any future level falls through to nil so the
// verdict is simply omitted rather than emitting a misleading default.
func deriveVerdict(r *review.DeltaResult) *xmlVerdict {
	if r == nil {
		return nil
	}
	level := r.Risk.RiskLevel
	switch level {
	case "low":
		return &xmlVerdict{
			CanReplace: "yes",
			Reason:     "low risk",
		}
	case "medium":
		reason := "medium risk"
		if len(r.Risk.Flags) > 0 {
			reason = r.Risk.Flags[0]
		}
		return &xmlVerdict{
			CanReplace: "partial",
			Reason:     reason,
		}
	case "high":
		return &xmlVerdict{
			CanReplace: "no",
			Reason:     "high risk",
			Blockers:   r.Risk.Flags,
		}
	default:
		return nil
	}
}

// fetchPRBase fetches PR head into FETCH_HEAD and computes the merge-base
// with main/master. Returns (base, head). Head is always "FETCH_HEAD" on
// success — callers must pass it through to ChangedFiles, otherwise diffs
// fall back to the warm-clone HEAD (which is main/master, not the PR) and
// review_pr returns a stale or empty diff.
func fetchPRBase(ctx context.Context, root string, prNumber int) (base, head string, err error) {
	ref := fmt.Sprintf("pull/%d/head", prNumber)
	if _, err := review.GitExec(ctx, root, "fetch", "origin", ref); err != nil {
		return "", "", fmt.Errorf("fetch PR ref: %w", err)
	}
	out, err := review.GitExec(ctx, root, "merge-base", "FETCH_HEAD", "origin/main")
	if err != nil {
		out, err = review.GitExec(ctx, root, "merge-base", "FETCH_HEAD", "origin/master")
		if err != nil {
			return "", "", fmt.Errorf("merge-base: %w", err)
		}
	}
	return strings.TrimSpace(out), "FETCH_HEAD", nil
}
