package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-kit/llm"
	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/codegraph"
	"github.com/anatolykoptev/vaelor/internal/investigate"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

// TestWarmingSignal_AllTools_SurfaceInOutput is the red-on-revert gate for the
// go/types warming note across every tool that reaches callgraph.BuildFromRepo.
// Each row drives the REAL handler (or handler sub-function for
// debug_investigate / dataflow) with a seam-injected CallGraph carrying
// Warming=true, then asserts the response carries the warming+retry note.
//
// Falsification: remove any single tool's surfacing → exactly that tool's
// subtest goes RED; the others stay GREEN. A row whose assertion still passes
// after its surfacing is removed is a non-test and must be rewritten.
func TestWarmingSignal_AllTools_SurfaceInOutput(t *testing.T) {
	const target = "Foo"

	// makeWarmingCG builds a minimal CallGraph with Warming=true and a single
	// target symbol so impact/understand/prepare_change can find it.
	makeWarmingCG := func(root string) *callgraph.CallGraph {
		return &callgraph.CallGraph{
			Symbols: []*parser.Symbol{
				{Name: target, Kind: parser.KindFunction, File: filepath.Join(root, "foo.go"), StartLine: 1},
			},
			Tier:    "basic",
			Warming: true,
		}
	}

	// makeWarmingTraceResult builds a minimal TraceResult with Warming=true for
	// call_trace's AGE seam.
	makeWarmingTraceResult := func() *callgraph.TraceResult {
		root := &parser.Symbol{Name: target, Kind: parser.KindFunction, File: "foo.go", StartLine: 1}
		return &callgraph.TraceResult{
			Root:       root,
			Tree:       []callgraph.CallChainNode{{Symbol: root}},
			TotalNodes: 1,
			MaxDepth:   0,
			Resolved:   1,
			Warming:    true,
		}
	}

	cases := []struct {
		name string
		run  func(t *testing.T) string
	}{
		{
			name: "understand",
			run: func(t *testing.T) string {
				orig := understandBuildFromRepo
				defer func() { understandBuildFromRepo = orig }()
				understandBuildFromRepo = func(_ context.Context, in callgraph.TraceRepoInput) (*callgraph.CallGraph, error) {
					return makeWarmingCG(in.Root), nil
				}
				root := t.TempDir()
				deps := analyze.Deps{}
				res, err := handleUnderstand(context.Background(), UnderstandInput{Repo: root, Symbol: target}, deps, nil, nil, "")
				if err != nil {
					t.Fatalf("handleUnderstand: %v", err)
				}
				return textContentOf(t, res)
			},
		},
		{
			name: "impact_analysis",
			run: func(t *testing.T) string {
				orig := impactBuildFromRepo
				defer func() { impactBuildFromRepo = orig }()
				impactBuildFromRepo = func(_ context.Context, in callgraph.TraceRepoInput) (*callgraph.CallGraph, error) {
					return makeWarmingCG(in.Root), nil
				}
				root := t.TempDir()
				deps := analyze.Deps{LLM: llm.NoOp{}, LLMHasKey: false}
				res, err := handleImpact(context.Background(), ImpactInput{Repo: root, Symbol: target}, deps, nil, "")
				if err != nil {
					t.Fatalf("handleImpact: %v", err)
				}
				return textContentOf(t, res)
			},
		},
		{
			name: "call_trace",
			run: func(t *testing.T) string {
				orig := callTraceTraceFromAGE
				defer func() { callTraceTraceFromAGE = orig }()
				callTraceTraceFromAGE = func(context.Context, *codegraph.Store, string, string, string, int) (*callgraph.TraceResult, error) {
					return makeWarmingTraceResult(), nil
				}
				root := t.TempDir()
				deps := analyze.Deps{LLM: llm.NoOp{}, LLMHasKey: false}
				store := &codegraph.Store{}
				res, err := handleCallTrace(context.Background(), CallTraceInput{Repo: root, Symbol: target, Compact: true}, deps, nil, "", store)
				if err != nil {
					t.Fatalf("handleCallTrace: %v", err)
				}
				return textContentOf(t, res)
			},
		},
		{
			name: "prepare_change",
			run: func(t *testing.T) string {
				orig := prepareChangeBuildFromRepo
				defer func() { prepareChangeBuildFromRepo = orig }()
				prepareChangeBuildFromRepo = func(_ context.Context, in callgraph.TraceRepoInput) (*callgraph.CallGraph, error) {
					return makeWarmingCG(in.Root), nil
				}
				root := t.TempDir()
				deps := analyze.Deps{}
				res, err := handlePrepareChange(context.Background(), PrepareChangeInput{Repo: root, Symbol: target}, deps, nil)
				if err != nil {
					t.Fatalf("handlePrepareChange: %v", err)
				}
				return textContentOf(t, res)
			},
		},
		{
			name: "dead_code",
			run: func(t *testing.T) string {
				orig := deadCodeBuildFromRepo
				defer func() { deadCodeBuildFromRepo = orig }()
				deadCodeBuildFromRepo = func(_ context.Context, in callgraph.TraceRepoInput) (*callgraph.CallGraph, error) {
					return makeWarmingCG(in.Root), nil
				}
				root := t.TempDir()
				deps := analyze.Deps{}
				res, err := handleDeadCode(context.Background(), DeadCodeInput{Repo: root}, deps, "", nil)
				if err != nil {
					t.Fatalf("handleDeadCode: %v", err)
				}
				return textContentOf(t, res)
			},
		},
		{
			name: "dataflow_analyze",
			run: func(t *testing.T) string {
				orig := dataflowDeadBuildFromRepo
				defer func() { dataflowDeadBuildFromRepo = orig }()
				dataflowDeadBuildFromRepo = func(_ context.Context, in callgraph.TraceRepoInput) (*callgraph.CallGraph, error) {
					return makeWarmingCG(in.Root), nil
				}
				root := t.TempDir()
				// runDeadFunctionAnalysis is the handler sub-function that
				// builds the callgraph and renders xmlDfDeadFuncs. OxCodes is
				// optional (nil → second-pass string scan skipped, non-fatal).
				df := runDeadFunctionAnalysis(context.Background(), root, "go", analyze.Deps{})
				if df == nil {
					t.Fatal("runDeadFunctionAnalysis returned nil")
				}
				data, err := xml.Marshal(df.xmlDfDeadFuncs)
				if err != nil {
					t.Fatalf("marshal xmlDfDeadFuncs: %v", err)
				}
				return string(data)
			},
		},
		{
			name: "debug_investigate_symbols",
			run: func(t *testing.T) string {
				orig := debugInvestigateBuildFromRepo
				defer func() { debugInvestigateBuildFromRepo = orig }()
				debugInvestigateBuildFromRepo = func(_ context.Context, in callgraph.TraceRepoInput) (*callgraph.CallGraph, error) {
					return makeWarmingCG(in.Root), nil
				}
				root := t.TempDir()
				deps := analyze.Deps{}
				res := &investigate.InvestigationResult{}
				// runSymbolsPhase is the handler sub-function that builds the
				// callgraph and appends the warming note to Diagnostics.Warnings.
				// Empty traces → no ops → no hypotheses, but the callgraph build
				// and warming check still run (gated on input.Repo != "").
				runSymbolsPhase(context.Background(), deps, DebugInvestigateInput{Repo: root}, nil, 0, res)
				if len(res.Diagnostics.Warnings) == 0 {
					t.Fatal("expected at least one diagnostic warning")
				}
				return strings.Join(res.Diagnostics.Warnings, "\n")
			},
		},
		{
			name: "code_research",
			run: func(t *testing.T) string {
				orig := codeResearchBuildFromRepo
				defer func() { codeResearchBuildFromRepo = orig }()
				codeResearchBuildFromRepo = func(_ context.Context, in callgraph.TraceRepoInput) (*callgraph.CallGraph, error) {
					return makeWarmingCG(in.Root), nil
				}
				root := t.TempDir()
				// Create a minimal .go file so research.Run has something to
				// parse and produce a non-empty result.
				writeFile(t, filepath.Join(root, "main.go"), "package main\n\nfunc Foo() {}\n")
				deps := analyze.Deps{}
				res, err := handleCodeResearch(context.Background(), CodeResearchInput{
					Repo:             root,
					Query:            "Foo",
					Language:         "go",
					IncludeCallGraph: true,
				}, deps, nil)
				if err != nil {
					t.Fatalf("handleCodeResearch: %v", err)
				}
				return textContentOf(t, res)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.run(t)
			if !strings.Contains(got, "type-aware enrichment is warming") {
				t.Errorf("%s: expected warming note in output, got (first 400):\n%s", tc.name, truncForLog(got, 400))
			}
			if !strings.Contains(got, "retry") {
				t.Errorf("%s: expected retry hint in output, got (first 400):\n%s", tc.name, truncForLog(got, 400))
			}
		})
	}
}

// TestWarmingSignal_WarmPath_NoNote verifies that when Warming=false, none of
// the tools emit the warming note. This catches a stuck-on bug where the note
// is always emitted regardless of the Warming flag.
func TestWarmingSignal_WarmPath_NoNote(t *testing.T) {
	const target = "Foo"

	makeWarmCG := func(root string) *callgraph.CallGraph {
		return &callgraph.CallGraph{
			Symbols: []*parser.Symbol{
				{Name: target, Kind: parser.KindFunction, File: filepath.Join(root, "foo.go"), StartLine: 1},
			},
			Tier:    "enhanced",
			Warming: false,
		}
	}

	cases := []struct {
		name string
		run  func(t *testing.T) string
	}{
		{
			name: "understand",
			run: func(t *testing.T) string {
				orig := understandBuildFromRepo
				defer func() { understandBuildFromRepo = orig }()
				understandBuildFromRepo = func(_ context.Context, in callgraph.TraceRepoInput) (*callgraph.CallGraph, error) {
					return makeWarmCG(in.Root), nil
				}
				root := t.TempDir()
				res, err := handleUnderstand(context.Background(), UnderstandInput{Repo: root, Symbol: target}, analyze.Deps{}, nil, nil, "")
				if err != nil {
					t.Fatalf("handleUnderstand: %v", err)
				}
				return textContentOf(t, res)
			},
		},
		{
			name: "dead_code",
			run: func(t *testing.T) string {
				orig := deadCodeBuildFromRepo
				defer func() { deadCodeBuildFromRepo = orig }()
				deadCodeBuildFromRepo = func(_ context.Context, in callgraph.TraceRepoInput) (*callgraph.CallGraph, error) {
					return makeWarmCG(in.Root), nil
				}
				root := t.TempDir()
				res, err := handleDeadCode(context.Background(), DeadCodeInput{Repo: root}, analyze.Deps{}, "", nil)
				if err != nil {
					t.Fatalf("handleDeadCode: %v", err)
				}
				return textContentOf(t, res)
			},
		},
		{
			name: "prepare_change",
			run: func(t *testing.T) string {
				orig := prepareChangeBuildFromRepo
				defer func() { prepareChangeBuildFromRepo = orig }()
				prepareChangeBuildFromRepo = func(_ context.Context, in callgraph.TraceRepoInput) (*callgraph.CallGraph, error) {
					return makeWarmCG(in.Root), nil
				}
				root := t.TempDir()
				res, err := handlePrepareChange(context.Background(), PrepareChangeInput{Repo: root, Symbol: target}, analyze.Deps{}, nil)
				if err != nil {
					t.Fatalf("handlePrepareChange: %v", err)
				}
				return textContentOf(t, res)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.run(t)
			if strings.Contains(got, "type-aware enrichment is warming") {
				t.Errorf("%s: warm path must NOT carry warming note, got (first 400):\n%s", tc.name, truncForLog(got, 400))
			}
		})
	}
}

// TestCallTrace_Rung3Counts_CarriesWarmingNote verifies Finding 2: the
// warming note survives into the counts rung (rung 3) of call_trace's
// result-shortening ladder. Rungs 1 and 2 already carry it; rung 3 must
// match.
//
// RED-on-revert: remove the Warming field from xmlTraceCounts (or the
// warmingAttr call in the counts rung closure) → this test goes RED.
func TestCallTrace_Rung3Counts_CarriesWarmingNote(t *testing.T) {
	orig := callTraceTraceFromAGE
	defer func() { callTraceTraceFromAGE = orig }()

	// Build a tree large enough that BOTH the full rendering (rung 1) and the
	// depth-1 rendering (rung 2) overflow DefaultBudget. The counts rung
	// (rung 3) is the last rung — PickFitting returns it (possibly truncated,
	// but the opening <trace> tag with the warming attr is at the start and
	// survives truncation).
	root := &parser.Symbol{Name: "HandleRequest", Kind: parser.KindFunction, File: "main.go", StartLine: 1, EndLine: 10}
	children := make([]callgraph.CallChainNode, 200)
	for i := 0; i < 200; i++ {
		children[i] = callgraph.CallChainNode{
			Symbol: &parser.Symbol{
				Name:      fmt.Sprintf("caller_%03d_with_a_long_name_to_inflate_json_output_beyond_budget", i),
				Kind:      parser.KindFunction,
				File:      fmt.Sprintf("src/pkg/very/deeply/nested/path/that/is/long/enough/to/overflow/the/budget/when/many/callers/carry/it/file_%03d.go", i),
				StartLine: uint32(i*10 + 1),
				EndLine:   uint32(i*10 + 5),
			},
		}
	}

	callTraceTraceFromAGE = func(context.Context, *codegraph.Store, string, string, string, int) (*callgraph.TraceResult, error) {
		return &callgraph.TraceResult{
			Root:       root,
			Tree:       []callgraph.CallChainNode{{Symbol: root, Children: children}},
			TotalNodes: 201,
			MaxDepth:   1,
			Resolved:   201,
			Warming:    true,
		}, nil
	}

	rootDir := t.TempDir()
	deps := analyze.Deps{LLM: llm.NoOp{}, LLMHasKey: false}
	store := &codegraph.Store{}
	res, err := handleCallTrace(context.Background(), CallTraceInput{Repo: rootDir, Symbol: "HandleRequest", Compact: true}, deps, nil, "", store)
	if err != nil {
		t.Fatalf("handleCallTrace: %v", err)
	}
	text := textContentOf(t, res)

	// The counts rung uses total=" and files=" (not totalNodes=" which is
	// rung 1/2). Their presence proves rung 3 was chosen.
	if !strings.Contains(text, `total="`) {
		t.Fatalf("expected counts rung (total= attr), got (first 400):\n%s", truncForLog(text, 400))
	}
	if !strings.Contains(text, `files="`) {
		t.Fatalf("expected counts rung (files= attr), got (first 400):\n%s", truncForLog(text, 400))
	}
	if strings.Contains(text, `totalNodes="`) {
		t.Fatalf("expected counts rung, but got rung 1/2 (totalNodes= attr present), got (first 400):\n%s", truncForLog(text, 400))
	}
	// The warming attr must be present in the counts rung opening tag.
	if !strings.Contains(text, `warming="type-aware`) {
		t.Fatalf("counts rung must carry the warming attr, got (first 400):\n%s", truncForLog(text, 400))
	}
}

// writeFile is a test helper that writes content to path, failing the test on
// error.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
