package callgraph

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// gocode_callees_emitted_total counts call-graph edges by language and kind.
//
//   - kind="call" — primary call (parent is call_expression / equivalent)
//   - kind="argref_kept" — heuristic arg/struct-literal reference that resolved
//     to a known function symbol; kept as an edge.
//   - kind="argref_dropped_unresolved" — heuristic argref dropped because no
//     function symbol matches its name (e.g. `ctx`, `opts.Slug`, `dirPerm`).
//   - kind="argref_kept_legacy" — heuristic argref kept despite no resolution
//     because the caller passed IncludeFieldAccess=true (MCP field_access=true).
//
// Useful for empirically observing the noise reduction of the callees filter
// post-deploy via :8897/metrics. A high argref_dropped_unresolved:call ratio
// at deploy time, dropping after rollout, confirms the fix landed.
var calleesEmittedTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gocode_callees_emitted_total",
		Help: "Call graph edges emitted, labelled by language and kind " +
			"(call, argref_kept, argref_kept_legacy, argref_dropped_unresolved).",
	},
	[]string{"language", "kind"},
)

// gocode_callgraph_gotypes_fallback_total counts each time go/types typed resolution
// fails and the call graph degrades to tree-sitter-only edges.
//
// Labels:
//   - reason: "deadline" — packages.Load context deadline exceeded;
//     "load_error" — any other packages.Load failure.
//
// A non-zero rate means typed Go call edges are being dropped silently. Operators
// can alert on this to detect repeated cold-GOCACHE deadline misses or environment
// issues with packages.Load. Before this counter, the only signal was a WARN log.
var callgraphGotypesFallbackTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gocode_callgraph_gotypes_fallback_total",
		Help: "Times go/types typed resolution failed and the call graph fell back to tree-sitter-only edges, by reason (deadline, load_error).",
	},
	[]string{"reason"},
)

// recordGotypesFallback bumps the go/types fallback counter with the appropriate reason.
func recordGotypesFallback(err error) {
	reason := "load_error"
	if isDeadlineErr(err) {
		reason = "deadline"
	}
	callgraphGotypesFallbackTotal.WithLabelValues(reason).Inc()
}

// gocode_scip_fallback_total counts each time a SCIP indexer fails and the call
// graph stays at tree-sitter-only tier.
//
// Labels:
//   - indexer: the SCIP indexer binary name (e.g. "rust-analyzer", "scip-python").
//   - reason:  "killed" — indexer subprocess received SIGKILL (OOM or ctx deadline);
//     "indexer_error" — indexer exited with a non-zero status;
//     "read_error" — index file could not be parsed after indexer succeeded;
//     "no_edges" — index was read but contained 0 typed edges.
//
// A non-zero rate makes SCIP degradation visible, enabling operators to correlate
// kills with memory pressure or deadline budgets.
var scipFallbackTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gocode_scip_fallback_total",
		Help: "Times a SCIP indexer failed and the call graph stayed at tree-sitter tier, by indexer and reason (killed, indexer_error, read_error, no_edges).",
	},
	[]string{"indexer", "reason"},
)

// recordSCIPFallback bumps the SCIP fallback counter for a given indexer and reason.
func recordSCIPFallback(indexer, reason string) {
	scipFallbackTotal.WithLabelValues(indexer, reason).Inc()
}

// isDeadlineErr reports whether err wraps context.DeadlineExceeded.
// context.Canceled is NOT a deadline — it is a deliberate cancellation
// (e.g. caller disconnect) and should not be reported as a deadline miss.
func isDeadlineErr(err error) bool {
	return errors.Is(err, context.DeadlineExceeded)
}

// isKilledErr reports whether err indicates a subprocess received SIGKILL.
// This covers both cgroup OOM kills of child processes and exec.CommandContext
// kills on ctx-deadline expiry — both arrive as "signal: killed".
func isKilledErr(err error) bool {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			return status.Signaled() && status.Signal() == syscall.SIGKILL
		}
	}
	return false
}

// gocode_callgraph_eager_warm_total counts startup GOCACHE pre-warm outcomes.
// Eager warming is performed once per Go repo discovered under AUTO_INDEX_DIRS
// at process start. The first user request lands on a warm cache and returns
// tier=enhanced from request #1 (instead of tier=basic on cold-cache miss).
//
// Outcomes (cardinality 4, no repo label to keep cardinality bounded):
//   - started          — vendor/ present; goroutine kicked off the go build
//   - completed        — go build returned without error
//   - failed           — go build returned an error, OR vendor/ stat returned
//     a non-ENOENT IO error (broken symlink, EPERM, etc.)
//   - skipped_no_vendor — vendor/ directory absent (ENOENT); repo uses the
//     module proxy workflow; -mod=vendor would always fail with "inconsistent
//     vendoring" so no build is attempted. This is distinct from completed so
//     the started/completed ratio remains meaningful for repos that do build.
var eagerWarmTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gocode_callgraph_eager_warm_total",
		Help: "Eager startup GOCACHE pre-warm outcomes per repo, labelled by outcome (started, completed, failed, skipped_no_vendor).",
	},
	[]string{"outcome"},
)

// recordEagerWarm bumps the eager-warm counter for one outcome.
func recordEagerWarm(outcome string) {
	eagerWarmTotal.WithLabelValues(outcome).Inc()
}

// gocode_callgraph_background_warm_total counts on-demand background go/types
// warm outcomes (warmGoTypesCache in repo.go). This is the warm path that
// upgrades a cold tree-sitter-only CallGraph to the enhanced tier after the
// request has already returned the degraded result.
//
// Outcomes (cardinality 3, no repo label to keep cardinality bounded):
//   - completed — packages.Load succeeded, cache upgraded to enhanced
//   - failed    — packages.Load failed (cold GOCACHE, unbuildable deps,
//     timeout); the cache stays at basic tier
//   - skipped   — already warming (duplicate goroutine suppressed by
//     goTypesWarmingSet)
//
// Operators should alert on a sustained non-zero `failed` rate: it means the
// background warm never succeeds for a repo, so every cold request returns
// tree-sitter-only and the enhanced tier is never reached. Before this
// counter, the only signal was a WARN log swallowed as "non-fatal" — the
// pre-warm `go build` step that failed on every cgo repo (issue #735) was
// invisible to operators.
var backgroundWarmTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gocode_callgraph_background_warm_total",
		Help: "On-demand background go/types warm outcomes, labelled by outcome (completed, failed, skipped).",
	},
	[]string{"outcome"},
)

// recordBackgroundWarm bumps the background-warm counter for one outcome.
func recordBackgroundWarm(outcome string) {
	backgroundWarmTotal.WithLabelValues(outcome).Inc()
}

// gocode_parser_unresolved_alias_total counts import paths in Astro frontmatter
// that contain a path alias (~/…, @/…, or any non-relative prefix) and could
// not be resolved to a repo-relative file path after all resolution attempts.
//
// A non-zero rate signals that a tsconfig/astro.config alias map was present
// but the alias could not be matched — either the alias prefix is not in the
// map, or the target path does not exist. Operators can use this to discover
// which repos have alias-heavy imports that need tsconfig entries.
var parserUnresolvedAliasTotal = promauto.NewCounter(
	prometheus.CounterOpts{
		Name: "gocode_parser_unresolved_alias_total",
		Help: "Alias import paths (~/…, @/…, non-relative) in Astro frontmatter that could not be resolved to a repo-relative file, after consulting tsconfig paths and astro.config aliases.",
	},
)

// recordCallee bumps the counter for one parser.CallSite outcome.
func recordCallee(file, kind string) {
	calleesEmittedTotal.WithLabelValues(languageFromExt(file), kind).Inc()
}

// languageFromExt maps a file path to a coarse language label for metrics.
// Unknown extensions report "other" — callers should not introduce a new
// label without bounding cardinality.
func languageFromExt(file string) string { //nolint:cyclop // dispatch switch — complexity is inherent
	ext := strings.ToLower(filepath.Ext(file))
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx", ".cjs", ".mjs":
		return "javascript"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".c", ".h":
		return "c"
	case ".cpp", ".cc", ".cxx", ".hpp", ".hh":
		return "cpp"
	case ".rb":
		return "ruby"
	case ".cs":
		return "csharp"
	case ".php":
		return "php"
	case ".svelte":
		return "svelte"
	case ".astro":
		return "astro"
	case ".vue":
		return "vue"
	default:
		return "other"
	}
}
