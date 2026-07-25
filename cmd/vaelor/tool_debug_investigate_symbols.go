// cmd/go-code/tool_debug_investigate_symbols.go
package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/compound"
	"github.com/anatolykoptev/vaelor/internal/deadcode"
	"github.com/anatolykoptev/vaelor/internal/investigate"
	"github.com/anatolykoptev/vaelor/internal/jaegerclient"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

// debugInvestigateBuildFromRepo is the production seam for callgraph.BuildFromRepo;
// handler-level tests can override it to avoid heavy parsing.
var debugInvestigateBuildFromRepo = callgraph.BuildFromRepo

// isLibraryPath reports whether p refers to a dependency or toolchain path
// rather than application source code. Used by Phase 3 to detect spans where
// code.* tags point to library internals (e.g. tower-http's DefaultMakeSpan),
// which are constant and useless for handler identification.
//
// Recognised patterns:
//   - /cargo/registry/ — Rust crates fetched via Cargo
//   - /.rustup/        — Rust toolchain source
//   - /go/pkg/mod/     — Go module cache
//   - /usr/local/go/src/ — Go standard library
func isLibraryPath(p string) bool {
	return strings.Contains(p, "/cargo/registry/") ||
		strings.Contains(p, "/.cargo/registry/") ||
		strings.Contains(p, "/.rustup/") ||
		strings.Contains(p, "/go/pkg/mod/") ||
		strings.Contains(p, "/usr/local/go/src/")
}

// buildOpsMap aggregates span data from traces into a map from operation name
// to OperationInfo. For each operation, the Count is incremented per span seen.
// OTEL semantic-convention tags (http.route, http.method, code.filepath,
// code.lineno, code.namespace) are captured with first-seen-wins semantics for
// stability across trace variations.
//
// Both the legacy (pre-v0.32) and renamed (v0.32+) OTEL attribute names are
// recognised:
//   - code.filepath  / code.file.path
//   - code.lineno    / code.line.number
//   - code.namespace / code.module.name
//   - code.function  / code.function.name
//
// code.lineno / code.line.number are handled defensively: the JSON wire format
// may deliver them as float64 (standard JSON numbers), int64 (rare), or string.
func buildOpsMap(traces []jaegerclient.Trace) map[string]*investigate.OperationInfo {
	ops := make(map[string]*investigate.OperationInfo)
	for _, tr := range traces {
		for _, sp := range tr.Spans {
			info, ok := ops[sp.OperationName]
			if !ok {
				info = &investigate.OperationInfo{Operation: sp.OperationName}
				ops[sp.OperationName] = info
			}
			info.Count++

			// First-seen wins for tag values — avoid per-trace noise.
			for _, tag := range sp.Tags {
				if v, ok := tag.Value.(string); ok {
					switch tag.Key {
					case "http.route":
						if info.HTTPRoute == "" {
							info.HTTPRoute = v
						}
					case "http.method":
						if info.HTTPMethod == "" {
							info.HTTPMethod = v
						}
					case "code.filepath", "code.file.path":
						if info.CodeFilepath == "" {
							info.CodeFilepath = v
						}
					case "code.namespace", "code.module.name":
						if info.CodeNamespace == "" {
							info.CodeNamespace = v
						}
					case "code.function", "code.function.name":
						if info.CodeFunction == "" {
							info.CodeFunction = v
						}
					}
				}
				// code.lineno / code.line.number may arrive as float64/int64/string.
				if (tag.Key == "code.lineno" || tag.Key == "code.line.number") && info.CodeLineno == 0 {
					switch v := tag.Value.(type) {
					case float64:
						info.CodeLineno = int(v)
					case int64:
						info.CodeLineno = int(v)
					case string:
						if n, err := strconv.Atoi(v); err == nil {
							info.CodeLineno = n
						}
					}
				}
			}
		}
	}
	return ops
}

// runSymbolsPhase executes Phase 3: span→operation→symbol correlation.
//
// Phase 3 symbol resolution is best-effort and depends on what tags the service
// emits. Resolution proceeds through tiers in order:
//
//   - Tier-1 (code.*): requires #[tracing::instrument] (Rust) or equivalent OTEL
//     annotations on handler functions — without it, code.* will point to library
//     internals (e.g. tower-http for axum services) and Phase 3 falls to Tier-2.
//   - Tier-2 (http.route): used when code.* tags are absent or point to library
//     internals (detected by isLibraryPath). Generates a code_search next_check
//     for the axum Router::route pattern so the operator can locate the handler.
//   - Tier-3 (OperationToFuncName): legacy / non-OTEL-instrumented services.
//     Requires a callgraph built from the repo.
//
// See issue #74 for full discussion of service-side instrumentation requirements.
//
// It aggregates unique operations across all spans via buildOpsMap, then for
// each operation attempts one of the above strategies.
//
// The resulting Hypotheses are ranked by investigate.RankHypotheses before
// being stored in res.
//
// Phase γ.B enrichments (applied in order after ranking):
//  1. Dead-code filter: drops hypotheses resolved to dead symbols.
//  2. Impact phase: enriches top-3 with blast radius.
//  3. Symbol body: enriches top-1 with compound.AnalyzeBody (requires OxCodes).
//
// Returns ops — the operation→OperationInfo map — so Phase 5 can include the
// operations in the LLM prompt.
//
// Note: resolveRoot returns a cleanup func that is deferred inside this
// function. The defer fires when runSymbolsPhase returns (before Phase 4/5
// execute). Nothing after this call reads the repo path, so the earlier
// cleanup is correct.
func runSymbolsPhase(
	ctx context.Context,
	deps analyze.Deps,
	input DebugInvestigateInput,
	traces []jaegerclient.Trace,
	anomalyScore float64,
	res *investigate.InvestigationResult,
) (map[string]*investigate.OperationInfo, *callgraph.CallGraph) {
	// Aggregate unique operations and their OTEL tags across all spans.
	ops := buildOpsMap(traces)
	for _, tr := range traces {
		res.Diagnostics.SpansAnalyzed += len(tr.Spans)
	}

	// symMap tracks the parser.Symbol for each hypothesis index so
	// Phase γ.B.3 (AnalyzeBody) can access the full symbol struct.
	symMap := make(map[int]*parser.Symbol)

	// retCG is the callgraph built in PASS 2 (nil for Tier-1/Tier-2 paths).
	// Returned to the orchestrator so runUpstreamPhase can walk callers.
	var retCG *callgraph.CallGraph

	// resolvedFromCodeTags tracks operations resolved via code.* or http.route
	// so the callgraph fallback (PASS 2) skips them.
	resolvedFromCodeTags := make(map[string]bool, len(ops))

	// PASS 1: Preferred path — OTEL code.* tags give file:line directly (Tier-1),
	// or http.route gives a discriminator when code.* are absent/library-internal
	// (Tier-2). Runs regardless of whether a repo was provided.
	for op, info := range ops {
		// Tier-1: code.* present and pointing to application code (not a library).
		if info.CodeFilepath != "" && info.CodeLineno > 0 && !isLibraryPath(info.CodeFilepath) {
			h := investigate.Hypothesis{
				Subject:       fmt.Sprintf("operation %q", op),
				SpanCount:     info.Count,
				AnomalyScore:  anomalyScore,
				EvidenceLinks: []string{fmt.Sprintf("operation=%s; spans=%d", op, info.Count)},
			}
			h.File = reverseToHost(info.CodeFilepath, deps.PathMappings)
			h.Line = info.CodeLineno
			symbol := joinSymbol(info.CodeNamespace, info.CodeFunction)
			if symbol == "" {
				symbol = op
			}
			if info.HTTPRoute != "" {
				h.Subject = fmt.Sprintf("%s %s in %s:%d",
					info.HTTPMethod, info.HTTPRoute, h.File, info.CodeLineno)
			} else {
				h.Subject = fmt.Sprintf("%s in %s:%d", symbol, h.File, info.CodeLineno)
			}
			h.NextChecks = append(h.NextChecks, investigate.NextCheck{
				Tool: "understand",
				Args: map[string]string{
					"file": h.File,
					"line": fmt.Sprintf("%d", h.Line),
					"repo": input.Repo,
				},
			})
			res.Diagnostics.SymbolsTouched++
			res.Hypotheses = append(res.Hypotheses, h)
			resolvedFromCodeTags[op] = true
			continue
		}

		// Tier-2: route-based discriminator. Used when code.* tags are absent or
		// point to library internals (e.g. tower-http's DefaultMakeSpan span where
		// code.filepath = tower-http-X.Y.Z/src/trace/make_span.rs).
		// Generates a code_search next_check for the axum Router::route pattern so
		// the operator/Claude can locate the handler in source:
		//   Router::new().route("/api/x", get(handler_fn))
		if info.HTTPRoute != "" {
			h := investigate.Hypothesis{
				Subject:       fmt.Sprintf("%s %s", info.HTTPMethod, info.HTTPRoute),
				SpanCount:     info.Count,
				AnomalyScore:  anomalyScore,
				EvidenceLinks: []string{fmt.Sprintf("operation=%s; spans=%d; route=%s", op, info.Count, info.HTTPRoute)},
			}
			args := map[string]string{
				"pattern": fmt.Sprintf(`route("%s"`, info.HTTPRoute),
			}
			if input.Repo != "" {
				args["repo"] = input.Repo
			}
			h.NextChecks = append(h.NextChecks, investigate.NextCheck{
				Tool: "code_search",
				Args: args,
			})
			res.Diagnostics.SymbolsTouched++ // gives operator something to act on
			res.Hypotheses = append(res.Hypotheses, h)
			resolvedFromCodeTags[op] = true // prevent PASS 2 double-hypothesizing
			continue
		}
	}

	// PASS 2: Fallback — OperationToFuncName + callgraph for Go services
	// without code.* tags. Requires a repo path to be provided.
	repo := input.Repo
	if repo != "" {
		resolvedRoot, cleanup, resolveErr := resolveRoot(ctx, repo, "", deps)
		if resolveErr != nil {
			res.Diagnostics.Warnings = append(res.Diagnostics.Warnings,
				fmt.Sprintf("resolve root %q: %v", repo, resolveErr))
		} else {
			defer cleanup()
			cg, cgErr := debugInvestigateBuildFromRepo(ctx, callgraph.TraceRepoInput{
				Root:     resolvedRoot,
				Language: "go",
			})
			if cgErr != nil {
				res.Diagnostics.Warnings = append(res.Diagnostics.Warnings,
					fmt.Sprintf("build callgraph: %v", cgErr))
			}
			// Expose cg to the orchestrator regardless of build error — it may be
			// partially populated and still usable for upstream walking.
			retCG = cg

			// Surface the warming note when go/types enrichment was skipped on a
			// cold cache (issue #735). The background warm is running; a retry
			// will return the enhanced tier with type-aware call resolution.
			if cg != nil && cg.Warming {
				res.Diagnostics.Warnings = append(res.Diagnostics.Warnings,
					"type-aware enrichment is warming in the background; retry for the enhanced tier (go/types interface dispatch resolution)")
			}

			for op, info := range ops {
				if resolvedFromCodeTags[op] {
					continue // already resolved via code.* or http.route path
				}
				h := investigate.Hypothesis{
					Subject:       fmt.Sprintf("operation %q", op),
					SpanCount:     info.Count,
					AnomalyScore:  anomalyScore,
					EvidenceLinks: []string{fmt.Sprintf("operation=%s; spans=%d", op, info.Count)},
				}
				funcName := investigate.OperationToFuncName(op)
				if cg != nil && funcName != "" {
					matches := compound.FindSymbol(cg.Symbols, funcName)
					if len(matches) > 0 {
						sym := matches[0]
						h.File = reverseToHost(sym.File, deps.PathMappings)
						h.Line = int(sym.StartLine)
						h.EndLine = int(sym.EndLine)
						h.Subject = fmt.Sprintf("%s in %s", funcName, h.File)
						h.NextChecks = append(h.NextChecks,
							investigate.NextCheck{
								Tool: "understand",
								Args: map[string]string{
									"symbol": funcName,
									"repo":   repo,
								},
							})
						res.Diagnostics.SymbolsTouched++
						// Invariant: key == index of hypothesis about to be appended.
						symMap[len(res.Hypotheses)] = sym
					}
				}
				res.Hypotheses = append(res.Hypotheses, h)
			}

			// γ.B.1: Dead-code filter — build dead set and drop false-positive hypotheses.
			if cg != nil {
				dcResult := deadcode.Analyze(cg, deadcode.Options{
					OxCodes:       deps.OxCodes, // second-pass string-reference scan reduces false positives
					Root:          resolvedRoot, // required for ox-codes queries
					Language:      "go",
					Relationships: cg.TypeRels, // interface-aware filtering: prevents concrete methods
					// from being marked dead when they satisfy an interface
					// with no direct callgraph edge.
					IncludeExported: false, // conservative: exported symbols are not dead by definition
					Ctx:             ctx,
				})
				deadSet := make(map[string]bool, dcResult.DeadCount)
				for _, ds := range dcResult.DeadSymbols {
					deadSet[ds.Name] = true
				}
				res.Hypotheses = filterDeadHypotheses(res.Hypotheses, deadSet, &res.Diagnostics)
			}

			res.Hypotheses = investigate.RankHypotheses(res.Hypotheses)

			// γ.B.2: Impact phase — enrich top-3 surviving hypotheses.
			if cg != nil {
				res.Hypotheses = runImpactPhase(ctx, cg, res.Hypotheses, 3)
			}

			// γ.B.3: Symbol body — enrich top-1 only (requires OxCodes).
			// Build a post-ranking symMap keyed by ranked position 0..n-1.
			// We match by Subject prefix to find the original symbol.
			rankedSymMap := buildRankedSymMap(res.Hypotheses, symMap)
			res.Hypotheses = runSymbolBodyPhase(ctx, res.Hypotheses, rankedSymMap, deps.OxCodes, resolvedRoot, &res.Diagnostics)

			// γ.C.3: Hint-driven codesearch — append hint_match hypotheses and re-rank.
			// Re-rank is mandatory: at this point existing hypotheses already have
			// Confidence set by the first RankHypotheses call above, so isRanked()
			// returns true and the final guard below would skip ranking entirely,
			// leaving hint_match hypotheses unordered at the tail.
			if input.Hint != "" {
				hintCtx, hintCancel := context.WithTimeout(ctx, 5*time.Second)
				hintMatches := runHintSearch(hintCtx, input.Hint, resolvedRoot)
				hintCancel() // synchronous — purpose-bounded scope, not deferred
				if len(hintMatches) > 0 {
					res.Hypotheses = applyHintMatches(res.Hypotheses, hintMatches)
					// Clear Confidence so RankHypotheses re-fills all entries uniformly.
					for i := range res.Hypotheses {
						res.Hypotheses[i].Confidence = ""
					}
					res.Hypotheses = investigate.RankHypotheses(res.Hypotheses)
				}
			}
		}
	}

	if len(res.Hypotheses) == 0 {
		// No symbol resolution (empty repo or no callgraph) — fall back to
		// frequency-only hypotheses so callers always get something useful.
		for op, info := range ops {
			res.Hypotheses = append(res.Hypotheses, investigate.Hypothesis{
				Subject:       fmt.Sprintf("operation %q", op),
				SpanCount:     info.Count,
				AnomalyScore:  anomalyScore,
				EvidenceLinks: []string{fmt.Sprintf("operation=%s; spans=%d", op, info.Count)},
			})
		}
	}

	if !isRanked(res.Hypotheses) {
		res.Hypotheses = investigate.RankHypotheses(res.Hypotheses)
	}
	return ops, retCG
}

// buildRankedSymMap maps ranked hypothesis position → *parser.Symbol by
// matching the ranked hypothesis Subject to the original symMap entries.
// The original symMap is keyed by the hypothesis index *before* ranking.
// After RankHypotheses re-orders, we match by reconstructing the hypothesis
// Subject for each original symMap entry.
func buildRankedSymMap(ranked []investigate.Hypothesis, origSymMap map[int]*parser.Symbol) map[int]*parser.Symbol {
	// Build subject → symbol from original map.
	subjectToSym := make(map[string]*parser.Symbol, len(origSymMap))
	for _, sym := range origSymMap {
		if sym == nil {
			continue
		}
		subjectToSym[sym.Name] = sym
	}
	result := make(map[int]*parser.Symbol, len(ranked))
	for i, h := range ranked {
		name := subjectFuncName(h.Subject)
		if sym, ok := subjectToSym[name]; ok {
			result[i] = sym
		}
	}
	return result
}

// isRanked returns true if RankHypotheses has already been called, detected
// by checking whether any hypothesis has a non-empty Confidence (ranking sets it).
func isRanked(hyps []investigate.Hypothesis) bool {
	for _, h := range hyps {
		if h.Confidence != "" {
			return true
		}
	}
	return false
}

// joinSymbol combines a code.namespace and code.function into a single
// symbol string for hypothesis subjects.
//
//   - Both empty → ""
//   - Only ns     → ns
//   - Only fn     → fn
//   - Both        → ns + "." + fn  (Go dotted form, e.g. "main.(*T).Method")
//
// For Rust spans where ns = "acme_sfu::client_ws" and fn is empty, the
// dot is not appended so the Rust module path is preserved unchanged.
func joinSymbol(ns, fn string) string {
	switch {
	case ns == "" && fn == "":
		return ""
	case fn == "":
		return ns
	case ns == "":
		return fn
	default:
		return ns + "." + fn
	}
}
