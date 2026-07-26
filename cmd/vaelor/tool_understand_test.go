package main

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/codegraph"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

func makeTestSym(name, file string) *parser.Symbol {
	return &parser.Symbol{Name: name, Kind: parser.KindFunction, File: file, StartLine: 1}
}

func TestFilterByFocus_EmptyReturnsAll(t *testing.T) {
	syms := []*parser.Symbol{
		makeTestSym("a", "/repo/src/a.go"),
		makeTestSym("b", "/repo/src/b.go"),
	}
	got := filterByFocus(syms, "")
	if len(got) != 2 {
		t.Fatalf("empty focus: want 2, got %d", len(got))
	}
}

func TestFilterByFocus_SuffixMatch(t *testing.T) {
	// Regression: focus="ThemeToggle.svelte" must match a file in a deep subdirectory.
	// This test documents the critical bug: if focus were passed to ingest as a path
	// prefix, the file would be excluded before this filter runs, yielding 0 results.
	syms := []*parser.Symbol{
		makeTestSym("toggle", "/host/src/acme-guide/frontend/src/components/ThemeToggle.svelte"),
		makeTestSym("other", "/host/src/other/util.ts"),
	}
	got := filterByFocus(syms, "ThemeToggle.svelte")
	if len(got) != 1 {
		t.Fatalf("suffix focus: want 1, got %d", len(got))
	}
	if got[0].Name != "toggle" {
		t.Errorf("suffix focus: want toggle, got %s", got[0].Name)
	}
}

func TestFilterByFocus_SubstringMatch(t *testing.T) {
	syms := []*parser.Symbol{
		makeTestSym("filter", "/host/src/acme-guide/frontend/src/components/Filters.svelte"),
		makeTestSym("other", "/host/src/other/util.ts"),
	}
	got := filterByFocus(syms, "components/Filters")
	if len(got) != 1 {
		t.Fatalf("substring focus: want 1, got %d", len(got))
	}
	if got[0].Name != "filter" {
		t.Errorf("substring focus: want filter, got %s", got[0].Name)
	}
}

func TestFilterByFocus_ExactMatch(t *testing.T) {
	path := "/host/src/other/lib/util.ts"
	syms := []*parser.Symbol{
		makeTestSym("fn", path),
		makeTestSym("other", "/host/src/a.ts"),
	}
	got := filterByFocus(syms, path)
	if len(got) != 1 || got[0].File != path {
		t.Errorf("exact focus: want %s, got %v", path, got)
	}
}

func TestFilterByFocus_NoMatch(t *testing.T) {
	syms := []*parser.Symbol{
		makeTestSym("fn", "/repo/src/foo.go"),
	}
	got := filterByFocus(syms, "does_not_exist.go")
	if len(got) != 0 {
		t.Errorf("no-match focus: want 0, got %d", len(got))
	}
}

// TestFilterByFocus_IngestLayeringRegression is the critical regression test for Issue 1.
// It verifies that filterByFocus works correctly when symbols come from the FULL repo
// (i.e., focus is NOT passed to ingest). If focus were passed to ingest as a prefix
// filter, ingest would exclude "ThemeToggle.svelte" because it doesn't start with that
// string, and this list would be empty before filterByFocus ever runs.
// The fix: BuildFromRepo is called without Focus, so all files are ingested,
// and filterByFocus does the narrowing post-ingest.
func TestFilterByFocus_IngestLayeringRegression(t *testing.T) {
	// Simulate: full repo symbols (not filtered by ingest)
	allRepoSymbols := []*parser.Symbol{
		makeTestSym("toggle", "/host/src/app/components/ThemeToggle.svelte"),
		makeTestSym("submit", "/host/src/app/components/Form.svelte"),
		makeTestSym("fetch", "/host/src/app/lib/api.ts"),
	}

	// focus="ThemeToggle.svelte" — a bare filename, not a path prefix
	focus := "ThemeToggle.svelte"
	got := filterByFocus(allRepoSymbols, focus)

	if len(got) == 0 {
		t.Fatal("REGRESSION: filterByFocus returned 0 results for ThemeToggle.svelte — " +
			"check that BuildFromRepo is NOT called with Focus set (ingest prefix filtering would exclude this file)")
	}
	if len(got) != 1 || got[0].Name != "toggle" {
		t.Errorf("want [toggle], got %v", got)
	}
}

// TestUnderstand_ColdGraph_ReturnsBuildingStatus verifies that understand returns
// a JSON building-status response (and does not synchronously parse the repo)
// when the AGE graph is not yet fresh and the source is remote.
func TestUnderstand_ColdGraph_ReturnsBuildingStatus(t *testing.T) {
	origCacheStatus := ageGraphCacheStatus
	origIndexRepo := ageGraphIndexRepo
	origMemGuard := ageGraphMemGuardWatchdog
	defer func() {
		ageGraphCacheStatus = origCacheStatus
		ageGraphIndexRepo = origIndexRepo
		ageGraphMemGuardWatchdog = origMemGuard
	}()

	ageGraphCacheStatus = func(context.Context, *codegraph.Store, string) (bool, error) { return false, nil }
	ageGraphIndexRepo = func(context.Context, *codegraph.Store, string, bool, codegraph.IndexConfig) (*codegraph.GraphMeta, error) {
		return nil, nil
	}
	ageGraphMemGuardWatchdog = func(context.Context, context.CancelFunc) {}

	// Create a local checkout that resolveRoot will match for a remote slug.
	checkouts := t.TempDir()
	repoName := "testrepo"
	repoDir := filepath.Join(checkouts, repoName)
	if err := exec.Command("git", "init", repoDir).Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	if err := exec.Command("git", "-C", repoDir, "remote", "add", "origin", "https://github.com/acme/"+repoName+".git").Run(); err != nil {
		t.Fatalf("git remote add: %v", err)
	}

	input := UnderstandInput{Repo: "acme/testrepo", Symbol: "Foo"}
	deps := analyze.Deps{LocalRepoDirs: []string{checkouts}}
	graphStore := &codegraph.Store{}

	res, err := handleUnderstand(context.Background(), input, deps, nil, graphStore, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res == nil {
		t.Fatal("expected non-nil result")
	}
	if res.IsError {
		t.Fatalf("expected non-error status response, got error: %s", textContentOf(t, res))
	}

	text := textContentOf(t, res)
	var status understandStatusResponse
	if err := json.Unmarshal([]byte(text), &status); err != nil {
		t.Fatalf("expected JSON status, got %q: %v", text, err)
	}
	if status.Status != "building" {
		t.Errorf("expected status 'building', got %q", status.Status)
	}
	if !strings.Contains(status.Message, "retry") {
		t.Errorf("expected retry hint in message, got %q", status.Message)
	}
	if status.Repo != input.Repo {
		t.Errorf("expected repo %q, got %q", input.Repo, status.Repo)
	}
	if status.Symbol != "Foo" {
		t.Errorf("expected symbol %q, got %q", "Foo", status.Symbol)
	}
}

// TestUnderstand_ColdLocalGraph_DoesNotGate verifies that a local cold source
// does NOT short-circuit to "building" — it proceeds to the real build path.
func TestUnderstand_ColdLocalGraph_DoesNotGate(t *testing.T) {
	origCacheStatus := ageGraphCacheStatus
	origBuildFromRepo := understandBuildFromRepo
	defer func() {
		ageGraphCacheStatus = origCacheStatus
		understandBuildFromRepo = origBuildFromRepo
	}()

	ageGraphCacheStatus = func(context.Context, *codegraph.Store, string) (bool, error) { return false, nil }
	understandBuildFromRepo = func(_ context.Context, input callgraph.TraceRepoInput) (*callgraph.CallGraph, error) {
		cg := &callgraph.CallGraph{
			Symbols: []*parser.Symbol{makeTestSym("Foo", filepath.Join(input.Root, "foo.go"))},
			Tier:    "basic",
		}
		return cg, nil
	}

	root := t.TempDir()
	input := UnderstandInput{Repo: root, Symbol: "Foo"}
	deps := analyze.Deps{}
	// graphStore is nil for the local path: the AGE gate must be skipped entirely,
	// and a nil store must not be dereferenced on the BuildFromRepo path.
	var graphStore *codegraph.Store

	res, err := handleUnderstand(context.Background(), input, deps, nil, graphStore, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res == nil {
		t.Fatal("expected non-nil result")
	}
	if res.IsError {
		t.Fatalf("expected success, got error: %s", textContentOf(t, res))
	}

	text := textContentOf(t, res)
	// The success response must NOT be the building-status short-circuit.
	if strings.Contains(text, `"status":"building"`) {
		t.Fatalf("local cold source was gated: got building status response: %s", text)
	}

	var status understandStatusResponse
	if err := json.Unmarshal([]byte(text), &status); err == nil && status.Status == "building" {
		t.Fatalf("local cold source was gated: status=%q", status.Status)
	}

	var result struct {
		Symbol struct {
			Name string `json:"name"`
		} `json:"symbol"`
	}
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatalf("expected JSON result, got %q: %v", text, err)
	}
	if result.Symbol.Name != "Foo" {
		t.Errorf("expected symbol Foo, got %q", result.Symbol.Name)
	}
}

// TestUnderstand_ColdGoTypes_WarmingNoteInResponse verifies that when
// BuildFromRepo returns a CallGraph with Warming=true (go/types enrichment
// is warming in the background), the understand response carries a
// "type-aware enrichment is warming, retry" note so the agent knows to retry
// for the enhanced tier. On the warm path (Warming=false), the note must NOT
// appear.
func TestUnderstand_WarmingNote_DegradedPathOnly(t *testing.T) {
	origBuildFromRepo := understandBuildFromRepo
	defer func() { understandBuildFromRepo = origBuildFromRepo }()

	// Degraded path: Warming=true — the note MUST appear.
	understandBuildFromRepo = func(_ context.Context, input callgraph.TraceRepoInput) (*callgraph.CallGraph, error) {
		return &callgraph.CallGraph{
			Symbols: []*parser.Symbol{makeTestSym("Foo", filepath.Join(input.Root, "foo.go"))},
			Tier:    "basic",
			Warming: true,
		}, nil
	}

	root := t.TempDir()
	input := UnderstandInput{Repo: root, Symbol: "Foo"}
	deps := analyze.Deps{}

	res, err := handleUnderstand(context.Background(), input, deps, nil, nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("expected non-error result, got: %+v", res)
	}

	text := textContentOf(t, res)
	if !strings.Contains(text, "type-aware enrichment is warming") || !strings.Contains(text, "retry") {
		t.Errorf("degraded path: expected warming+retry note in response, got: %s", text)
	}

	// Warm path: Warming=false — the note must NOT appear.
	understandBuildFromRepo = func(_ context.Context, input callgraph.TraceRepoInput) (*callgraph.CallGraph, error) {
		return &callgraph.CallGraph{
			Symbols: []*parser.Symbol{makeTestSym("Foo", filepath.Join(input.Root, "foo.go"))},
			Tier:    "enhanced",
			Warming: false,
		}, nil
	}

	resWarm, err := handleUnderstand(context.Background(), input, deps, nil, nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resWarm == nil || resWarm.IsError {
		t.Fatalf("expected non-error result, got: %+v", resWarm)
	}

	textWarm := textContentOf(t, resWarm)
	if strings.Contains(textWarm, "type-aware enrichment is warming") {
		t.Errorf("warm path: expected NO warming note, got: %s", textWarm)
	}
}
