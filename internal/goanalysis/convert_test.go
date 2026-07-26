package goanalysis_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/goanalysis"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

func TestConvertToCallGraph(t *testing.T) {
	dir := t.TempDir()

	gomod := "module example.com/convert\n\ngo 1.21\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600); err != nil {
		t.Fatal(err)
	}

	mainFile := filepath.Join(dir, "main.go")
	src := `package main

func helper() {}

func main() {
	helper()
}
`
	if err := os.WriteFile(mainFile, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := goanalysis.LoadPackages(context.Background(), dir, goanalysis.LoadOpts{})
	if err != nil {
		t.Fatalf("LoadPackages: %v", err)
	}
	typedEdges := goanalysis.Resolve(result.Packages)

	tsSymbols := []*parser.Symbol{
		{Name: "main", Kind: parser.KindFunction, File: mainFile, StartLine: 5, EndLine: 7},
		{Name: "helper", Kind: parser.KindFunction, File: mainFile, StartLine: 3, EndLine: 3},
	}

	cg := callgraph.ConvertToCallGraph(typedEdges, tsSymbols)

	if cg == nil {
		t.Fatal("expected non-nil CallGraph")
	}
	if len(cg.Symbols) != len(tsSymbols) {
		t.Errorf("expected %d symbols, got %d", len(tsSymbols), len(cg.Symbols))
	}

	// Check that at least one edge from main -> helper was produced.
	hasMainToHelper := slices.ContainsFunc(cg.Edges, func(e callgraph.CallEdge) bool {
		return e.CalleeName == "helper" && e.Caller != nil && e.Caller.Name == "main"
	})
	if !hasMainToHelper {
		t.Errorf("expected main->helper edge; got %d edges", len(cg.Edges))
	}
}

func TestConvertToCallGraph_EmptyEdges(t *testing.T) {
	syms := []*parser.Symbol{
		{Name: "Foo", Kind: parser.KindFunction, File: "/a/foo.go"},
	}
	cg := callgraph.ConvertToCallGraph(nil, syms)
	if cg == nil {
		t.Fatal("expected non-nil CallGraph for empty edges")
	}
	if len(cg.Edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(cg.Edges))
	}
	if len(cg.Symbols) != 1 {
		t.Errorf("expected 1 symbol, got %d", len(cg.Symbols))
	}
}

func TestMergeCallGraphs(t *testing.T) {
	symA := &parser.Symbol{Name: "A", Kind: parser.KindFunction, File: "/pkg/a.go"}
	symB := &parser.Symbol{Name: "B", Kind: parser.KindFunction, File: "/pkg/b.go"}
	symC := &parser.Symbol{Name: "C", Kind: parser.KindFunction, File: "/pkg/c.go"}

	// tsGraph: A->B edge + A->C edge
	tsGraph := &callgraph.CallGraph{
		Edges: []callgraph.CallEdge{
			{Caller: symA, CalleeName: "B"},
			{Caller: symA, CalleeName: "C"},
		},
		Symbols:       []*parser.Symbol{symA, symB},
		HookCallbacks: []string{"myHook"},
	}

	// typedGraph: A->B edge (overlaps ts) + B->C edge (new)
	typedGraph := &callgraph.CallGraph{
		Edges: []callgraph.CallEdge{
			{Caller: symA, CalleeName: "B", Line: 10},
			{Caller: symB, CalleeName: "C", Line: 20},
		},
		Symbols: []*parser.Symbol{symA, symB, symC},
	}

	merged := callgraph.MergeCallGraphs(tsGraph, typedGraph)

	if merged == nil {
		t.Fatal("expected non-nil merged graph")
	}

	// A->B and B->C from typed, plus A->C from ts (not in typed).
	if len(merged.Edges) != 3 {
		t.Errorf("expected 3 edges, got %d", len(merged.Edges))
	}

	// A->B must come from typed (Line==10).
	hasTypedAB := slices.ContainsFunc(merged.Edges, func(e callgraph.CallEdge) bool {
		return e.Caller != nil && e.Caller.Name == "A" && e.CalleeName == "B" && e.Line == 10
	})
	if !hasTypedAB {
		t.Error("expected typed A->B edge (line 10) to take priority")
	}

	// B->C from typed should be present.
	hasBC := slices.ContainsFunc(merged.Edges, func(e callgraph.CallEdge) bool {
		return e.Caller != nil && e.Caller.Name == "B" && e.CalleeName == "C"
	})
	if !hasBC {
		t.Error("expected B->C edge from typed graph")
	}

	// A->C from ts (not in typed) should be present.
	hasAC := slices.ContainsFunc(merged.Edges, func(e callgraph.CallEdge) bool {
		return e.Caller != nil && e.Caller.Name == "A" && e.CalleeName == "C"
	})
	if !hasAC {
		t.Error("expected A->C edge from ts graph")
	}

	// HookCallbacks come from tsGraph.
	if len(merged.HookCallbacks) != 1 || merged.HookCallbacks[0] != "myHook" {
		t.Errorf("expected HookCallbacks from ts graph, got %v", merged.HookCallbacks)
	}

	// Symbols deduped: A, B, C (3 unique).
	if len(merged.Symbols) != 3 {
		t.Errorf("expected 3 merged symbols, got %d", len(merged.Symbols))
	}
}

func TestMergeCallGraphs_PreservesWarming(t *testing.T) {
	tsGraph := &callgraph.CallGraph{
		Edges:   []callgraph.CallEdge{{CalleeName: "foo"}},
		Symbols: []*parser.Symbol{{Name: "A", Kind: parser.KindFunction, File: "/a.go"}},
		Warming: true,
	}
	typedGraph := &callgraph.CallGraph{
		Edges:   []callgraph.CallEdge{{CalleeName: "bar"}},
		Symbols: []*parser.Symbol{{Name: "B", Kind: parser.KindFunction, File: "/b.go"}},
	}

	merged := callgraph.MergeCallGraphs(tsGraph, typedGraph)

	if !merged.Warming {
		t.Error("expected Warming=true carried from tsGraph, got false")
	}
}

func TestMergeCallGraphs_PreservesTypeRels(t *testing.T) {
	tsRels := []parser.TypeRelationship{
		{Subject: "Hello", Target: "Greeter", Kind: parser.RelImplements, File: "/a.go", Line: 5},
	}
	tsGraph := &callgraph.CallGraph{
		Edges:    []callgraph.CallEdge{{CalleeName: "foo"}},
		Symbols:  []*parser.Symbol{{Name: "A", Kind: parser.KindFunction, File: "/a.go"}},
		TypeRels: tsRels,
	}
	typedGraph := &callgraph.CallGraph{
		Edges:   []callgraph.CallEdge{{CalleeName: "bar"}},
		Symbols: []*parser.Symbol{{Name: "B", Kind: parser.KindFunction, File: "/b.go"}},
	}

	merged := callgraph.MergeCallGraphs(tsGraph, typedGraph)

	if len(merged.TypeRels) != 1 {
		t.Fatalf("expected 1 TypeRel carried from tsGraph, got %d: %+v", len(merged.TypeRels), merged.TypeRels)
	}
	if merged.TypeRels[0].Subject != "Hello" || merged.TypeRels[0].Target != "Greeter" {
		t.Errorf("expected Hello->Greeter TypeRel, got %+v", merged.TypeRels[0])
	}
}

func TestMergeCallGraphs_PreservesUsesIndex(t *testing.T) {
	tsUses := map[string][]string{
		"components/Foo.astro": {"pages/index.astro"},
	}
	tsGraph := &callgraph.CallGraph{
		Edges:     []callgraph.CallEdge{{CalleeName: "foo"}},
		Symbols:   []*parser.Symbol{{Name: "A", Kind: parser.KindFunction, File: "/a.go"}},
		UsesIndex: tsUses,
	}
	typedGraph := &callgraph.CallGraph{
		Edges:   []callgraph.CallEdge{{CalleeName: "bar"}},
		Symbols: []*parser.Symbol{{Name: "B", Kind: parser.KindFunction, File: "/b.go"}},
	}

	merged := callgraph.MergeCallGraphs(tsGraph, typedGraph)

	if len(merged.UsesIndex) != 1 {
		t.Fatalf("expected UsesIndex with 1 entry carried from tsGraph, got %d entries", len(merged.UsesIndex))
	}
	users, ok := merged.UsesIndex["components/Foo.astro"]
	if !ok || len(users) != 1 || users[0] != "pages/index.astro" {
		t.Errorf("expected UsesIndex[components/Foo.astro]=[pages/index.astro], got %+v", merged.UsesIndex)
	}
}

func TestMergeCallGraphs_NilHandling(t *testing.T) {
	cg := &callgraph.CallGraph{Edges: []callgraph.CallEdge{{CalleeName: "Foo"}}}

	if got := callgraph.MergeCallGraphs(nil, cg); got != cg {
		t.Error("MergeCallGraphs(nil, cg) should return cg")
	}
	if got := callgraph.MergeCallGraphs(cg, nil); got != cg {
		t.Error("MergeCallGraphs(cg, nil) should return cg")
	}
	if got := callgraph.MergeCallGraphs(nil, nil); got != nil {
		t.Error("MergeCallGraphs(nil, nil) should return nil")
	}
}
