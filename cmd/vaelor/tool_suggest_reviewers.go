package main

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/compare"
	"github.com/anatolykoptev/vaelor/internal/mcpmeta"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	// suggestReviewersWeightDirect is the score weight for direct authorship commits.
	suggestReviewersWeightDirect = 1.0
	// suggestReviewersWeightCoChange is the score weight for co-change partner authorship.
	suggestReviewersWeightCoChange = 0.5
	// suggestReviewersWeightRecency is the score weight for authorship within the recency window.
	suggestReviewersWeightRecency = 0.4
	// suggestReviewersMaxPerFile is the maximum number of reviewer suggestions per file.
	suggestReviewersMaxPerFile = 5
	// suggestReviewersRecencyDays is the window in days for the recency bonus.
	suggestReviewersRecencyDays = 90
	// suggestReviewersMinCoChanges is the minimum co-change count passed to CollectCoupling.
	suggestReviewersMinCoChanges = 2
	// suggestReviewersGitTimeout is the timeout for individual git log invocations.
	suggestReviewersGitTimeout = 15 * time.Second
)

// SuggestReviewersArgs is the input schema for the suggest_reviewers tool.
type SuggestReviewersArgs struct {
	Repo  string   `json:"repo"  jsonschema:"Repository path (canonicalized via resolveRoot)"`
	Paths []string `json:"paths" jsonschema:"PR file paths to rank reviewers for"`
}

// Suggestion is a single reviewer candidate with score and signal breakdown.
type Suggestion struct {
	Name            string  `json:"name"`
	Score           float64 `json:"score"`
	SignalBreakdown string  `json:"signal_breakdown"`
}

// PerFileSuggestions holds the ranked reviewer suggestions for one file path.
type PerFileSuggestions struct {
	Path        string       `json:"path"`
	Suggestions []Suggestion `json:"suggestions"`
	Error       string       `json:"error,omitempty"` // per-file failure (non-empty when fileAuthors failed; replaces the "<error>" sentinel)
}

// SuggestReviewersResult is the JSON payload returned by the suggest_reviewers tool.
type SuggestReviewersResult struct {
	Files []PerFileSuggestions `json:"files"`
	Meta  mcpmeta.Envelope     `json:"_meta"`
}

// fileAuthors runs `git log` on the given path and returns:
//   - counts: map of author name → commit count touching the file
//   - recent: set of authors with a commit within suggestReviewersRecencyDays
//
// A 15-second timeout is applied per invocation. Errors are propagated to the caller
// so the per-file batch can degrade gracefully without aborting.
func fileAuthors(ctx context.Context, root, path string) (counts map[string]int, recent map[string]bool, err error) {
	tctx, cancel := context.WithTimeout(ctx, suggestReviewersGitTimeout)
	defer cancel()

	//nolint:gosec // root and path are trusted: root from resolveRoot, path from caller-supplied PR file list
	cmd := exec.CommandContext(tctx, "git", "-C", root,
		"log", "--pretty=format:%an|%ct", "--", path)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, nil, fmt.Errorf("git log %s: %w (stderr: %s)", path, err, strings.TrimSpace(stderr.String()))
	}

	counts = make(map[string]int)
	recent = make(map[string]bool)
	now := time.Now().Unix()
	threshold := now - int64(suggestReviewersRecencyDays)*86400

	for _, line := range strings.Split(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		idx := strings.LastIndex(line, "|")
		if idx < 0 {
			continue
		}
		author := strings.TrimSpace(line[:idx])
		tsStr := strings.TrimSpace(line[idx+1:])
		if author == "" {
			continue
		}
		counts[author]++
		if ts, parseErr := strconv.ParseInt(tsStr, 10, 64); parseErr == nil && ts >= threshold {
			recent[author] = true
		}
	}
	return counts, recent, nil
}

// couplingPartners returns the set of file paths most coupled with path,
// drawn from either side of each CoupledPair.
func couplingPartners(path string, pairs []compare.CoupledPair) []string {
	var partners []string
	for _, p := range pairs {
		switch {
		case p.FileA == path:
			partners = append(partners, p.FileB)
		case p.FileB == path:
			partners = append(partners, p.FileA)
		}
	}
	return partners
}

// partnerAuthorCounts aggregates commit counts across a set of co-change partner files.
// Errors fetching individual partners are non-fatal and silently skipped.
func partnerAuthorCounts(ctx context.Context, root string, partners []string) map[string]int {
	totals := make(map[string]int)
	for _, partner := range partners {
		pcounts, _, perr := fileAuthors(ctx, root, partner)
		if perr != nil {
			continue
		}
		for a, n := range pcounts {
			totals[a] += n
		}
	}
	return totals
}

// scoreFileReviewers computes ranked reviewer suggestions for a single file.
// Returns a per-file error entry (not nil) on git failure so the batch can continue.
func scoreFileReviewers(ctx context.Context, root, path string, coupling []compare.CoupledPair) PerFileSuggestions {
	entry := PerFileSuggestions{Path: path}

	authors, recentAuthors, authErr := fileAuthors(ctx, root, path)
	if authErr != nil {
		entry.Error = authErr.Error()
		entry.Suggestions = nil
		return entry
	}

	partnerCounts := partnerAuthorCounts(ctx, root, couplingPartners(path, coupling))

	// Accumulate scores per candidate.
	scores := make(map[string]float64)
	for a, n := range authors {
		scores[a] += suggestReviewersWeightDirect * float64(n)
	}
	for a, n := range partnerCounts {
		scores[a] += suggestReviewersWeightCoChange * float64(n)
	}
	for a := range recentAuthors {
		scores[a] += suggestReviewersWeightRecency
	}

	suggestions := buildSuggestions(scores, authors, partnerCounts, recentAuthors)
	entry.Suggestions = suggestions
	return entry
}

// buildSuggestions converts the per-author score maps into a sorted, capped []Suggestion.
func buildSuggestions(
	scores map[string]float64,
	authors, partnerCounts map[string]int,
	recent map[string]bool,
) []Suggestion {
	suggestions := make([]Suggestion, 0, len(scores))
	for a, score := range scores {
		sig := fmt.Sprintf(
			"direct=%d co-change=%d recent=%v",
			authors[a], partnerCounts[a], recent[a],
		)
		suggestions = append(suggestions, Suggestion{Name: a, Score: score, SignalBreakdown: sig})
	}
	sort.SliceStable(suggestions, func(i, j int) bool {
		if suggestions[i].Score != suggestions[j].Score {
			return suggestions[i].Score > suggestions[j].Score
		}
		return suggestions[i].Name < suggestions[j].Name
	})
	if len(suggestions) > suggestReviewersMaxPerFile {
		suggestions = suggestions[:suggestReviewersMaxPerFile]
	}
	return suggestions
}

// handleSuggestReviewersCore is the testable core of the suggest_reviewers tool.
func handleSuggestReviewersCore(ctx context.Context, args SuggestReviewersArgs, deps analyze.Deps) (*mcp.CallToolResult, error) {
	if args.Repo == "" {
		return errResult("repo is required"), nil
	}
	if len(args.Paths) == 0 {
		return errResult("paths: at least one required"), nil
	}

	root, cleanup, err := resolveRoot(ctx, args.Repo, "", deps)
	if err != nil {
		return errResult(fmt.Sprintf("resolve repo: %s", err)), nil
	}
	defer cleanup()

	t0 := time.Now()

	coupling := compare.CollectCoupling(ctx, root, suggestReviewersMinCoChanges)

	out := SuggestReviewersResult{}
	for _, path := range args.Paths {
		out.Files = append(out.Files, scoreFileReviewers(ctx, root, path, coupling))
	}

	out.Meta = mcpmeta.Wrap(time.Since(t0), "")

	return jsonMarshalResult(out), nil
}

// registerSuggestReviewers registers the suggest_reviewers tool on the MCP server.
func registerSuggestReviewers(server *mcp.Server, cfg Config, deps analyze.Deps) {
	addTool(server, &mcp.Tool{
		Name:        "suggest_reviewers",
		Description: "Rank candidate reviewers for a list of PR file paths using direct authorship, co-change coupling, and recency. Returns up to 5 distinct authors per file (fewer when the repo has fewer contributors with relevant history). Co-change signal only fires when a partner file pair has at least 2 joint commits — recently-introduced couplings won't show until they're exercised twice.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args SuggestReviewersArgs) (*mcp.CallToolResult, error) {
		return handleSuggestReviewersCore(ctx, args, deps)
	})
	_ = cfg // cfg reserved for future use (e.g. WorkspaceDir override)
}
