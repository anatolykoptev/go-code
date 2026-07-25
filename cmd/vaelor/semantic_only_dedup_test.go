package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/vaelor/internal/embeddings"
)

// TestSemanticOnlyResult_DedupDropsDuplicate is the primary Bug A regression test.
//
// Two SearchResults share the same FilePath+SymbolName — exactly the live
// failure mode: dense-cosine arm (distance 0.5178) and trigram-name arm
// (distance 0.6825) both emit the same symbol when keyword+sparse arms are
// empty and MergeRRF is bypassed.
//
// semanticOnlyResult must collapse them to ONE entry, keeping the lower distance.
//
// Anti-tautology: removing the seen-map dedup loop from semanticOnlyResult
// causes both entries to reach finalResult → cappedResults → formatSemanticResults,
// producing count="2" and two <symbol> tags — the assertions below go RED.
func TestSemanticOnlyResult_DedupDropsDuplicate(t *testing.T) {
	ctx := context.Background()
	input := SemanticSearchInput{
		Repo:        "/tmp/dedup-test",
		Query:       "validate token",
		MaxDistance: 0.85,
	}
	// Minimal deps with the RRF identity so MergeRRF in the semantic-only path
	// does not discard the filtered results. Cold-path reranker (no URL), nil
	// graph, nil store keep the test hermetic.
	deps := SemanticDeps{RRFWeights: embeddings.DefaultRRFWeights()}

	in := []embeddings.SearchResult{
		{FilePath: "pkg/auth.go", SymbolName: "Validate", Distance: 0.5178, Source: "semantic"},
		{FilePath: "pkg/auth.go", SymbolName: "Validate", Distance: 0.6825, Source: "semantic"}, // dup
	}
	result, err := semanticOnlyResult(ctx, input, deps, "dedup-test", "/tmp/dedup-test", in, nil, 10, 0.85, 0, "", time.Now())
	if err != nil {
		t.Fatalf("semanticOnlyResult error: %v", err)
	}

	text := textContentOf(t, result)

	// Output must declare exactly 1 result.
	if !strings.Contains(text, `count="1"`) {
		t.Errorf("want count=\"1\" (dup collapsed), got text:\n%s", text)
	}
	// Only one <symbol> entry.
	if got := strings.Count(text, "<symbol"); got != 1 {
		t.Errorf("want 1 <symbol> element, got %d:\n%s", got, text)
	}
	// Winning entry is the lower distance (0.5178).
	if !strings.Contains(text, "0.5178") {
		t.Errorf("want distance 0.5178 (lower/better match) in output, got:\n%s", text)
	}
}

// TestSemanticOnlyResult_NoDupUnchanged verifies that distinct symbols are
// never collapsed by the dedup path.
//
// Anti-tautology: a dedup that incorrectly matches on FilePath alone (not
// FilePath+SymbolName) would collapse "a.go:Foo" and "a.go:Bar" here,
// producing count="1" — the assertion fails.
func TestSemanticOnlyResult_NoDupUnchanged(t *testing.T) {
	ctx := context.Background()
	input := SemanticSearchInput{
		Repo:        "/tmp/dedup-test",
		Query:       "something",
		MaxDistance: 0.85,
	}
	deps := SemanticDeps{RRFWeights: embeddings.DefaultRRFWeights()}

	in := []embeddings.SearchResult{
		{FilePath: "pkg/a.go", SymbolName: "Foo", Distance: 0.30, Source: "semantic"},
		{FilePath: "pkg/a.go", SymbolName: "Bar", Distance: 0.40, Source: "semantic"}, // same file, different symbol
		{FilePath: "pkg/b.go", SymbolName: "Baz", Distance: 0.50, Source: "semantic"},
	}
	result, err := semanticOnlyResult(ctx, input, deps, "dedup-test", "/tmp/dedup-test", in, nil, 10, 0.85, 0, "", time.Now())
	if err != nil {
		t.Fatalf("semanticOnlyResult error: %v", err)
	}
	text := textContentOf(t, result)
	if !strings.Contains(text, `count="3"`) {
		t.Errorf("want count=\"3\" (no dups to collapse), got:\n%s", text)
	}
}
