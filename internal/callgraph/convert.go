package callgraph

import (
	"path/filepath"
	"strconv"

	"github.com/anatolykoptev/vaelor/internal/goanalysis"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

// ConvertToCallGraph converts typed edges to the existing CallGraph format by
// matching callers/callees against tree-sitter symbols by name and file.
func ConvertToCallGraph(typedEdges []goanalysis.TypedEdge, tsSymbols []*parser.Symbol) *CallGraph {
	byNameFile, byName := buildConvertIndexes(tsSymbols)

	edges := make([]CallEdge, 0, len(typedEdges))
	for _, te := range typedEdges {
		caller := resolveSymbol(te.CallerName, te.CallerFile, byNameFile, byName)
		callee := resolveSymbol(te.CalleeName, te.CalleeFile, byNameFile, byName)
		edges = append(edges, CallEdge{
			Caller:      caller,
			Callee:      callee,
			CalleeName:  te.CalleeName,
			Receiver:    te.ReceiverType,
			Line:        te.Line,
			IsInterface: te.IsInterface,
		})
	}

	return &CallGraph{
		Edges:   edges,
		Symbols: tsSymbols,
	}
}

// MergeCallGraphs merges a tree-sitter call graph with a typed call graph.
// Typed edges take priority; unmatched tree-sitter edges are appended.
//
// Metadata fields (Warming, TypeRels, UsesIndex, HookCallbacks) are carried
// from tsGraph — the typed/SCIP graphs never set them. TypeRels is unioned
// with dedup in case either side carries them. Tier and Backend are left
// zero-valued; every caller re-assigns them immediately after the merge.
//
// The result is built by shallow-copying tsGraph and overriding the merged
// fields. This ensures a future field added to CallGraph is carried from
// tsGraph by default rather than silently dropped. A compile-time guard that
// fails loudly on new fields would require reflection; none exists cleanly
// in Go, so the copy-then-override shape is the next best thing.
func MergeCallGraphs(tsGraph, typedGraph *CallGraph) *CallGraph {
	if typedGraph == nil {
		return tsGraph
	}
	if tsGraph == nil {
		return typedGraph
	}

	// Build dedup key set from typed edges (typed takes priority).
	seen := make(map[string]struct{}, len(typedGraph.Edges))
	for _, e := range typedGraph.Edges {
		seen[edgeKey(e)] = struct{}{}
	}

	// Start with all typed edges; append unmatched tree-sitter edges.
	merged := make([]CallEdge, len(typedGraph.Edges), len(typedGraph.Edges)+len(tsGraph.Edges))
	copy(merged, typedGraph.Edges)
	for _, e := range tsGraph.Edges {
		if _, dup := seen[edgeKey(e)]; !dup {
			merged = append(merged, e)
		}
	}

	symbols := mergeSymbols(typedGraph.Symbols, tsGraph.Symbols)

	// Shallow-copy tsGraph to carry all metadata fields by default, then
	// override the merged fields. Tier/Backend are zeroed — callers re-assign.
	out := *tsGraph
	out.Edges = merged
	out.Symbols = symbols
	out.TypeRels = mergeTypeRels(tsGraph.TypeRels, typedGraph.TypeRels)
	out.Tier = ""
	out.Backend = ""
	return &out
}

// mergeTypeRels unions two TypeRels slices, deduplicating by
// Subject+Target+Kind+File+Line. Primary (tsGraph) entries are kept first.
func mergeTypeRels(primary, secondary []parser.TypeRelationship) []parser.TypeRelationship {
	if len(primary) == 0 {
		return secondary
	}
	if len(secondary) == 0 {
		return primary
	}
	seen := make(map[string]struct{}, len(primary)+len(secondary))
	result := make([]parser.TypeRelationship, 0, len(primary)+len(secondary))
	for _, rel := range primary {
		k := relKey(rel)
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			result = append(result, rel)
		}
	}
	for _, rel := range secondary {
		k := relKey(rel)
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			result = append(result, rel)
		}
	}
	return result
}

func relKey(r parser.TypeRelationship) string {
	return r.Subject + ":" + r.Target + ":" + string(r.Kind) + ":" + r.File + ":" + strconv.FormatUint(uint64(r.Line), 10)
}

// buildConvertIndexes creates lookup maps for symbol resolution during conversion.
func buildConvertIndexes(symbols []*parser.Symbol) (byNameFile, byName map[string]*parser.Symbol) {
	byNameFile = make(map[string]*parser.Symbol, len(symbols))
	byName = make(map[string]*parser.Symbol, len(symbols))
	for _, sym := range symbols {
		nf := sym.Name + ":" + filepath.Base(sym.File)
		if _, exists := byNameFile[nf]; !exists {
			byNameFile[nf] = sym
		}
		if _, exists := byName[sym.Name]; !exists {
			byName[sym.Name] = sym
		}
	}
	return byNameFile, byName
}

// resolveSymbol looks up a symbol by name+file, falling back to name only.
func resolveSymbol(name, file string, byNameFile, byName map[string]*parser.Symbol) *parser.Symbol {
	if name == "" {
		return nil
	}
	if file != "" {
		key := name + ":" + filepath.Base(file)
		if sym, ok := byNameFile[key]; ok {
			return sym
		}
	}
	return byName[name]
}

// edgeKey returns a deduplication key for a CallEdge.
func edgeKey(e CallEdge) string {
	callerName := ""
	if e.Caller != nil {
		callerName = e.Caller.Name
	}
	return callerName + "->" + e.CalleeName
}

// mergeSymbols merges two symbol slices, deduplicating by "name:file".
func mergeSymbols(primary, secondary []*parser.Symbol) []*parser.Symbol {
	seen := make(map[string]struct{}, len(primary)+len(secondary))
	result := make([]*parser.Symbol, 0, len(primary)+len(secondary))

	for _, sym := range primary {
		key := sym.Name + ":" + sym.File
		if _, exists := seen[key]; !exists {
			seen[key] = struct{}{}
			result = append(result, sym)
		}
	}
	for _, sym := range secondary {
		key := sym.Name + ":" + sym.File
		if _, exists := seen[key]; !exists {
			seen[key] = struct{}{}
			result = append(result, sym)
		}
	}
	return result
}
