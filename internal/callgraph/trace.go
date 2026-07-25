package callgraph

import (
	"context"

	"github.com/anatolykoptev/vaelor/internal/graphx"
	"github.com/anatolykoptev/vaelor/internal/langutil"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

const (
	defaultMaxDepth = 5
	maxAllowedDepth = 10

	// CrossLanguageFetchKind is the node Kind set on synthetic nodes injected
	// by the cross-language enrichment pass (HANDLES → FETCHES traversal).
	CrossLanguageFetchKind = "cross_language_fetch"
)

// TraceOpts controls the call chain traversal.
type TraceOpts struct {
	Direction string // "callees" (default) or "callers"
	MaxDepth  int    // default 5, max 10

	// CrossRefs, when non-nil, extends the trace through HANDLES/FETCHES
	// edges at +1 depth beyond the existing call graph. Noop when nil.
	CrossRefs graphx.CrossRefs
	// Repo is the repo key used for CrossRefs lookups. Required when CrossRefs is set.
	Repo string
}

// SpeculativeCall represents a candidate callee found via text search.
type SpeculativeCall struct {
	Name       string  `json:"name"`
	File       string  `json:"file"`
	Line       int     `json:"line"`
	Confidence float64 `json:"confidence"` // 0.0-1.0
}

// CallChainNode is a single node in the call chain tree.
type CallChainNode struct {
	Symbol      *parser.Symbol    `json:"symbol"`
	Children    []CallChainNode   `json:"children,omitempty"`
	CallLine    uint32            `json:"callLine,omitempty"`
	Cycle       bool              `json:"cycle,omitempty"`
	Speculative []SpeculativeCall `json:"speculative,omitempty"` // candidates for unresolved calls
	Kind        string            `json:"kind,omitempty"`        // internal: empty = normal; CrossLanguageFetchKind for synthetic nodes
	Depth       int               `json:"depth,omitempty"`       // only set on synthetic cross-language nodes
	CallerKind  string            `json:"caller_kind,omitempty"` // production | test | example | benchmark | unresolved
}

// TraceResult holds the complete call chain traversal output.
type TraceResult struct {
	Root       *parser.Symbol  `json:"root,omitempty"`
	Tree       []CallChainNode `json:"tree"`
	MaxDepth   int             `json:"maxDepth"`
	TotalNodes int             `json:"totalNodes"`
	Resolved   int             `json:"resolved"`
	Unresolved int             `json:"unresolved"`
	Tier       string          `json:"tier,omitempty"`
	Warming    bool            `json:"warming,omitempty"`
}

// Trace walks the call graph from the named symbol, building a tree of call chains.
// Direction "callees" follows outgoing calls; "callers" follows incoming calls.
//
// When opts.CrossRefs is non-nil and opts.Repo is non-empty, a single extra pass
// runs after the main BFS/DFS: for each discovered node at depth <= MaxDepth-1
// it checks whether the symbol HANDLES an HTTP route, then queries FetchedBy for
// upstream callers. Results are appended as synthetic CallChainNode values with
// Kind == CrossLanguageFetchKind. The pass never recurses. CrossRefs errors are
// swallowed (slog.Debug) — the feature is purely opportunistic.
func Trace(ctx context.Context, g *CallGraph, symbolName string, opts TraceOpts) TraceResult {
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = defaultMaxDepth
	}
	if opts.MaxDepth > maxAllowedDepth {
		opts.MaxDepth = maxAllowedDepth
	}
	if opts.Direction == "" {
		opts.Direction = "callees"
	}

	root := findSymbol(g.Symbols, symbolName)
	if root == nil {
		return TraceResult{}
	}

	var adjacency map[*parser.Symbol][]CallEdge
	if opts.Direction == "callers" {
		adjacency = buildCallerIndex(g.Edges)
	} else {
		adjacency = buildCalleeIndex(g.Edges)
	}

	visited := make(map[*parser.Symbol]bool)
	result := TraceResult{Root: root}

	node := traceNode(root, adjacency, visited, 0, opts.MaxDepth, &result)
	result.Tree = []CallChainNode{node}

	// Cross-language enrichment pass — noop when CrossRefs is nil or Repo empty.
	if opts.CrossRefs != nil && opts.Repo != "" {
		injectCrossLangNodes(ctx, &result, opts)
	}

	return result
}

func traceNode(
	sym *parser.Symbol,
	adj map[*parser.Symbol][]CallEdge,
	visited map[*parser.Symbol]bool,
	depth, maxDepth int,
	result *TraceResult,
) CallChainNode {
	result.TotalNodes++
	if depth > result.MaxDepth {
		result.MaxDepth = depth
	}

	node := CallChainNode{Symbol: sym, CallerKind: nodeCallerKind(sym)}
	if depth >= maxDepth {
		return node
	}

	visited[sym] = true
	defer func() { visited[sym] = false }() // allow visiting from different paths

	for i := range adj[sym] {
		e := &adj[sym][i]

		// Determine target based on direction.
		var target *parser.Symbol
		if e.Caller == sym {
			target = e.Callee // forward: we are caller, target is callee
		} else {
			target = e.Caller // reverse: we are callee, target is caller
		}

		if target == nil {
			result.Unresolved++
			node.Children = append(node.Children, CallChainNode{
				Symbol:     &parser.Symbol{Name: e.CalleeName, Kind: "external"},
				CallLine:   e.Line,
				CallerKind: langutil.CallerKindUnresolved,
			})
			continue
		}

		result.Resolved++

		if visited[target] {
			node.Children = append(node.Children, CallChainNode{
				Symbol:     target,
				CallLine:   e.Line,
				Cycle:      true,
				CallerKind: nodeCallerKind(target),
			})
			continue
		}

		child := traceNode(target, adj, visited, depth+1, maxDepth, result)
		child.CallLine = e.Line
		node.Children = append(node.Children, child)
	}

	return node
}

// nodeCallerKind returns the caller kind for a resolved symbol, or
// "unresolved" when the symbol is nil, has no source file, or is explicitly
// marked as external.
func nodeCallerKind(sym *parser.Symbol) string {
	if sym == nil || sym.File == "" || sym.Kind == "external" {
		return langutil.CallerKindUnresolved
	}
	return langutil.CallerKind(sym.Name, sym.File)
}

func findSymbol(symbols []*parser.Symbol, name string) *parser.Symbol {
	for _, sym := range symbols {
		if sym.Name == name && (sym.Kind == parser.KindFunction || sym.Kind == parser.KindMethod) {
			return sym
		}
	}
	return nil
}

func buildCalleeIndex(edges []CallEdge) map[*parser.Symbol][]CallEdge {
	m := make(map[*parser.Symbol][]CallEdge)
	for _, e := range edges {
		if e.Caller != nil {
			m[e.Caller] = append(m[e.Caller], e)
		}
	}
	return m
}

func buildCallerIndex(edges []CallEdge) map[*parser.Symbol][]CallEdge {
	m := make(map[*parser.Symbol][]CallEdge)
	for _, e := range edges {
		if e.Callee != nil {
			m[e.Callee] = append(m[e.Callee], e)
		}
	}
	return m
}
