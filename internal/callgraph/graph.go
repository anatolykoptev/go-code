// Package callgraph builds and queries call relationships between functions.
package callgraph

import (
	"path/filepath"

	"github.com/anatolykoptev/vaelor/internal/parser"

	"github.com/anatolykoptev/vaelor/internal/strutil"
)

// CallEdge is a resolved (or unresolved) call from one function to another.
type CallEdge struct {
	Caller      *parser.Symbol // function containing the call
	Callee      *parser.Symbol // target function (nil if unresolved)
	CalleeName  string         // original name from source
	Receiver    string         // qualifier if method call
	Line        uint32         // 1-based call site line
	IsInterface bool           // true when resolved via interface dispatch (go/types or SCIP)
}

// CallGraph holds all call relationships for a repository.
type CallGraph struct {
	Edges         []CallEdge
	Symbols       []*parser.Symbol
	TypeRels      []parser.TypeRelationship // interface/extends/embeds relationships
	HookCallbacks []string                  // function names registered as hook callbacks
	Tier          string                    // "basic" (tree-sitter), "enhanced" (go/types merged), "full" (future)
	Backend       string                    // resolution backend: "tree-sitter", "tree-sitter+go/types", "tree-sitter+scip"
	// Warming is set when go/types enrichment was skipped because the
	// go/packages load was cold (tryGoTypesResolution returned nil). The
	// background warm (warmGoTypesCache) is running and will upgrade the
	// cached entry; a retry will return the enhanced tier. Callers should
	// surface a "type-aware enrichment is warming, retry for the enhanced
	// tier" note so the agent knows to retry instead of treating the
	// tree-sitter-only result as final.
	Warming bool
	// UsesIndex maps a target file's relative path to a list of relative paths
	// of Astro files that render it as a component (<Foo />). Populated by
	// ResolveTemplateRefs during BuildFromRepo. Enables impact_analysis to
	// report file-level USES callers for Astro components.
	UsesIndex map[string][]string
}

// BuildOpts controls how parser.CallSite entries are converted into call
// graph edges.
type BuildOpts struct {
	// IncludeFieldAccess keeps heuristic argref/field-access call sites even
	// when they don't resolve to a known function symbol. Default false —
	// unresolved CallSite.IsArgRef entries are dropped to avoid reporting
	// vars (`ctx`, `localPath`) and member access (`opts.Slug`) as callees.
	IncludeFieldAccess bool
}

// BuildCallGraph resolves call sites against the symbol table.
// Resolution: same-file -> same-package (directory) -> global name match.
//
// Heuristic argref sites (parser.CallSite.IsArgRef==true) are dropped when
// they don't resolve to a function/method symbol — this filters noise like
// member access (`opts.Slug`) and local variable references (`ctx`,
// `localPath`) that the parser captures inside argument lists.
func BuildCallGraph(symbols []*parser.Symbol, calls []parser.CallSite) *CallGraph {
	return BuildCallGraphWithOpts(symbols, calls, BuildOpts{})
}

// BuildCallGraphWithOpts is BuildCallGraph with explicit options.
func BuildCallGraphWithOpts(symbols []*parser.Symbol, calls []parser.CallSite, opts BuildOpts) *CallGraph {
	byName := indexByName(symbols)
	byFile := indexByFile(symbols)
	byDir := indexByDir(symbols)

	edges := make([]CallEdge, 0, len(calls))
	for i := range calls {
		cs := &calls[i]
		caller := findCaller(byFile[cs.File], cs.Line)
		callee := resolveCall(cs, byFile, byDir, byName)

		if cs.IsArgRef {
			if callee != nil {
				recordCallee(cs.File, "argref_kept")
			} else if opts.IncludeFieldAccess {
				recordCallee(cs.File, "argref_kept_legacy")
			} else {
				// Unresolved argref — drop to avoid reporting member access
				// and locals as callees. This is the noise filter.
				recordCallee(cs.File, "argref_dropped_unresolved")
				continue
			}
		} else {
			recordCallee(cs.File, "call")
		}

		edges = append(edges, CallEdge{
			Caller:     caller,
			Callee:     callee,
			CalleeName: cs.Name,
			Receiver:   cs.Receiver,
			Line:       cs.Line,
		})
	}
	return &CallGraph{Edges: edges, Symbols: symbols}
}

// findCaller returns the narrowest function/method containing the given line.
func findCaller(fileSymbols []*parser.Symbol, line uint32) *parser.Symbol {
	var best *parser.Symbol
	for _, sym := range fileSymbols {
		if sym.Kind != parser.KindFunction && sym.Kind != parser.KindMethod {
			continue
		}
		if line >= sym.StartLine && line <= sym.EndLine {
			if best == nil || (sym.EndLine-sym.StartLine) < (best.EndLine-best.StartLine) {
				best = sym
			}
		}
	}
	return best
}

// resolveCall finds the target symbol. Priority: same file -> same dir -> global.
func resolveCall(cs *parser.CallSite, byFile, byDir, byName map[string][]*parser.Symbol) *parser.Symbol {
	name := cs.Name

	if syms, ok := byFile[cs.File]; ok {
		if found := findByName(syms, name); found != nil {
			return found
		}
	}

	dir := filepath.Dir(cs.File)
	if syms, ok := byDir[dir]; ok {
		if found := findByName(syms, name); found != nil {
			return found
		}
	}

	if syms, ok := byName[name]; ok && len(syms) > 0 {
		callerDir := filepath.Dir(cs.File)
		return closestSymbol(syms, callerDir)
	}

	return nil
}

func findByName(symbols []*parser.Symbol, name string) *parser.Symbol {
	for _, sym := range symbols {
		if sym.Name == name && (sym.Kind == parser.KindFunction || sym.Kind == parser.KindMethod) {
			return sym
		}
	}
	return nil
}

func closestSymbol(symbols []*parser.Symbol, dir string) *parser.Symbol {
	if len(symbols) == 0 {
		return nil
	}
	best := symbols[0]
	bestLen := strutil.CommonPrefixLen(filepath.Dir(best.File), dir)
	for _, sym := range symbols[1:] {
		cl := strutil.CommonPrefixLen(filepath.Dir(sym.File), dir)
		if cl > bestLen {
			bestLen = cl
			best = sym
		}
	}
	return best
}
