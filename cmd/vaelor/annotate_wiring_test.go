package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/mcpmeta"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

// mkLaggingSourceRepo builds a directory that is both a searchable source tree
// and a checkout whose main branch is behind origin.
func mkLaggingSourceRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeLaggingGitDir(t, dir)
	body := "package main\n\n// MARKER lives here\nfunc marked() {}\n"
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestProvenance_AllTools_SurfaceCheckoutLag is the red-on-revert gate for the
// provenance annotation across every tool whose handler can be driven directly.
// Each row builds a checkout that is BEHIND its origin, drives the REAL handler,
// and asserts the response carries checkout_lag.
//
// It exists because the annotation is a single line per tool that nothing else
// pins: a round-2 review probe deleted all of them and the whole suite stayed
// green, since the only test drove annotateEnv itself rather than any caller.
//
// Falsification: remove any single row's annotateEnv call → exactly that row
// goes RED and the others stay GREEN. A row that still passes after its call is
// removed is a non-test and must be rewritten.
//
// Not covered here, and deliberately so — each needs a live dependency this
// suite has no cheap stub for: the code_search expand path (needs an OxCodes
// client), the code_search and symbol_search semantic-fallback paths (need a
// SemanticDeps that returns suggestions), symbol_search's main path (its
// handler is a closure inside registerSymbolSearch, reachable only through a
// running MCP server), and semantic_search (needs an embedding backend).
func TestProvenance_AllTools_SurfaceCheckoutLag(t *testing.T) {
	const target = "Foo"

	// A minimal graph so the symbol-driven handlers find something to report.
	makeCG := func(root string) *callgraph.CallGraph {
		return &callgraph.CallGraph{
			Symbols: []*parser.Symbol{
				{Name: target, Kind: parser.KindFunction, File: filepath.Join(root, "foo.go"), StartLine: 1},
			},
			Tier: "basic",
		}
	}

	cases := []struct {
		name string
		run  func(t *testing.T) string
	}{
		{
			name: "code_search",
			run: func(t *testing.T) string {
				root := mkLaggingSourceRepo(t)
				res, err := handleCodeSearchInner(context.Background(), CodeSearchInput{
					Repo:       root,
					Pattern:    "MARKER",
					MaxResults: 10,
				}, analyze.Deps{}, &SemanticDeps{}, "")
				if err != nil {
					t.Fatalf("handleCodeSearchInner: %v", err)
				}
				return textContentOf(t, res)
			},
		},
		{
			name: "understand",
			run: func(t *testing.T) string {
				orig := understandBuildFromRepo
				defer func() { understandBuildFromRepo = orig }()
				understandBuildFromRepo = func(_ context.Context, in callgraph.TraceRepoInput) (*callgraph.CallGraph, error) {
					return makeCG(in.Root), nil
				}
				root := mkLaggingSourceRepo(t)
				res, err := handleUnderstand(context.Background(),
					UnderstandInput{Repo: root, Symbol: target}, analyze.Deps{}, nil, nil, "")
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
					return makeCG(in.Root), nil
				}
				root := mkLaggingSourceRepo(t)
				res, err := handleDeadCode(context.Background(),
					DeadCodeInput{Repo: root}, analyze.Deps{}, "", nil)
				if err != nil {
					t.Fatalf("handleDeadCode: %v", err)
				}
				return textContentOf(t, res)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := tc.run(t)
			if !strings.Contains(out, "checkout_lag") {
				t.Fatalf("%s: a checkout behind origin must surface checkout_lag in the response "+
					"the agent reads, got:\n%s", tc.name, truncForLog(out, 600))
			}
		})
	}
}

// The same signal must survive the file-save path. metaXMLMarshalResult used to
// fold the footer into the body BEFORE largeTextResult decided to spill that
// body to a file, so the summary the agent actually read carried no footer --
// the staleness warning went into the file nobody re-reads for it.
func TestMetaXMLMarshalResult_EnvelopeAppearsInSummaryWhenSavedToFile(t *testing.T) {
	// A payload comfortably over the inline ceiling so largeTextResult spills.
	big := struct {
		XMLName struct{} `xml:"big"`
		Blob    string   `xml:"blob"`
	}{Blob: strings.Repeat("X", maxInlineCharsDefault*2)}

	env := mcpmeta.WithSourcePath(mcpmeta.Wrap(50*time.Millisecond, ""), "/Users/dev/Developer/acme", "/host/src/acme")
	if !env.HasSignal() {
		t.Fatal("fixture must carry a signal, else this asserts nothing")
	}

	text := textContentOf(t, metaXMLMarshalResult(big, "test_tool", t.TempDir(), env))

	if !strings.Contains(text, "saved to:") {
		t.Fatalf("fixture must exercise the file-save path, got: %q", truncForLog(text, 200))
	}
	if !strings.Contains(text, "<!-- meta:") {
		t.Fatalf("footer must appear in the visible summary when the body is saved to file, got: %q",
			truncForLog(text, 400))
	}
	if !strings.Contains(text, "source_path") {
		t.Fatalf("the signal itself must survive, got: %q", truncForLog(text, 400))
	}
}
