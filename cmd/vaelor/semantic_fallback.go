package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"log/slog"

	"github.com/anatolykoptev/vaelor/internal/codegraph"
	"github.com/anatolykoptev/vaelor/internal/embeddings"
)

const (
	semanticFallbackTopK = 5
)

// symbolNameSearcher is the minimal interface that semanticSuggest needs from
// the embedding store. *embeddings.Store satisfies it via SearchBySymbolName.
// Extracted here so tests can wire a spy without a live Postgres pool.
//
// Why an interface here rather than on SemanticDeps.Store: Store is a concrete
// type used across many callers; narrowing to an interface at the consumer
// (semantic_fallback.go) avoids touching the embedding package contract and
// mirrors the SimilarPairFinder pattern from internal/semhealth/semhealth.go.
type symbolNameSearcher interface {
	SearchBySymbolName(ctx context.Context, repoKey string, keywords []string, language string, limit int) ([]embeddings.SearchResult, error)
}

// Compile-time assertion: *embeddings.Store satisfies symbolNameSearcher.
// Catches signature drift on Store.SearchBySymbolName at build time rather
// than at runtime when the trigram fallback path first fires.
var _ symbolNameSearcher = (*embeddings.Store)(nil)

// bm25Searcher is the minimal interface that runKeywordArm needs from the
// embedding store when KeywordArm == "bm25f". *embeddings.Store satisfies it
// via BM25Search (BM25F P3). Extracted to allow test spies without a live pool.
type bm25Searcher interface {
	BM25Search(ctx context.Context, repoKey, queryText, language string, topK int) ([]embeddings.KeywordHit, error)
}

// Compile-time assertion: *embeddings.Store satisfies bm25Searcher.
var _ bm25Searcher = (*embeddings.Store)(nil)

// vectorSearcher is the minimal interface for Store.Search used by
// handleSemanticSearch. *embeddings.Store satisfies it via Search.
// Extracted as a seam so tests inject fakes without a live Postgres pool —
// production leaves storeSearcherSeam nil and the handler falls back to
// deps.Store, yielding byte-identical production behavior.
type vectorSearcher interface {
	Search(ctx context.Context, query []float32, opts embeddings.SearchOpts) ([]embeddings.SearchResult, error)
}

// Compile-time assertion: *embeddings.Store satisfies vectorSearcher.
var _ vectorSearcher = (*embeddings.Store)(nil)

// modelChecker is the minimal interface that the stale-hit guard in
// handleSemanticSearch needs. Production wires (*embeddings.Store).GetStoredModel
// for the store side and (*embeddings.Pipeline).EmbedModel + InvalidateIfModelChanged
// for the pipeline side. Extracted as a seam so tests inject fakes without live Postgres.
type modelChecker interface {
	// GetStoredModel returns the embed_model stored in code_repo_state for the
	// given repo_key, or "" on miss/error.
	GetStoredModel(ctx context.Context, repoKey string) string
}

// perRowModelChecker is an optional extension of modelChecker that reads the
// embed_model directly from code_embeddings rows rather than from code_repo_state.
// It fires when GetStoredModel returns "" (no state row — e.g. orphan vectors from
// a removed checkout that were never purged). *embeddings.Store satisfies both
// interfaces; test fakes that only set up modelChecker are unchanged.
type perRowModelChecker interface {
	// GetEmbedModelForRepo returns the embed_model of any stored embedding row for
	// the given repo_key, or "" when no rows exist or on error.
	GetEmbedModelForRepo(ctx context.Context, repoKey string) string
}

// pipelineInvalidator is the subset of *embeddings.Pipeline needed by the
// stale-hit guard: reading the active model name and triggering a purge+reindex.
type pipelineInvalidator interface {
	EmbedModel() string
	InvalidateIfModelChanged(ctx context.Context, repoKey string) bool
	IsIndexing(repoKey string) bool
	IndexRepoAsyncWithTool(tool, repoKey, root string) bool
	IndexProgress(repoKey string) (done, total int, running bool)
}

// indexedStateReader is the subset of *embeddings.Store needed by the
// no-results branch of handleSemanticSearch to decide whether an empty result
// set means "genuinely no match" (repo is indexed) vs "not indexed yet"
// (#709). It reuses the same state-reading helpers the freshness wrap and
// the same-SHA fast-path already use — GetRepoState (code_repo_state.head_sha),
// GetStoredModel (code_repo_state.embed_model), CountEmbeddings
// (code_embeddings row count) — never a second way to read that state.
// *embeddings.Store satisfies it; tests inject a fake via indexedStateSeam.
type indexedStateReader interface {
	GetRepoState(ctx context.Context, repoKey string) (string, error)
	GetStoredModel(ctx context.Context, repoKey string) string
	CountEmbeddings(ctx context.Context, repoKey string) (int, error)
}

// Compile-time assertions: concrete types satisfy the seam interfaces.
var _ modelChecker = (*embeddings.Store)(nil)
var _ perRowModelChecker = (*embeddings.Store)(nil)
var _ pipelineInvalidator = (*embeddings.Pipeline)(nil)
var _ indexedStateReader = (*embeddings.Store)(nil)

// semanticSuggest runs a trigram fuzzy name match as fallback when the primary
// tool found no symbol. Uses pg_trgm on code_embeddings.symbol_name (GIN
// trigram index) — independent of the embed-server / jina worker availability.
//
// Why pg_trgm and not embedding: callers here typo on symbol names ("render_coturn"
// missing → suggest "render_widget", "render_caddy"). Substring/typo similarity is
// the correct primitive; semantic embedding is overkill and was the source of
// 5-90s queue-overflow hangs on a saturated jina worker (incident 2026-05-27).
//
// Returns formatted XML suggestions, or empty string on no result / nil deps /
// unindexed repo (degrades silently, same as before).
func semanticSuggest(ctx context.Context, sem *SemanticDeps, root, query, language string) string {
	if sem == nil {
		return ""
	}

	// Resolve the searcher: prefer the test-injected spy, fall back to the
	// concrete Store. Nil on both paths → unindexed / unconfigured → degrade.
	var searcher symbolNameSearcher
	if sem.storeSearcher != nil {
		searcher = sem.storeSearcher
	} else if sem.Store != nil {
		searcher = sem.Store
	}
	if searcher == nil {
		return ""
	}

	repoKey := codegraph.GraphNameFor(root)

	results, err := searcher.SearchBySymbolName(ctx, repoKey, []string{query}, language, semanticFallbackTopK)
	if err != nil {
		slog.Debug("trigram fallback: search failed", slog.Any("error", err))
		return ""
	}
	if len(results) == 0 {
		return ""
	}
	return formatSemanticSuggestions(results)
}

// ---- semantic_suggestions XML types ----
//
// Migrated from a hand-rolled strings.Builder formatter onto encoding/xml.Marshal
// so well-formedness is correct by construction. The distance attribute is
// pre-formatted to a string (%.4f) because raw xml.Marshal of a float32 renders
// via strconv and drops trailing zeros (0.2500 -> "0.25"), breaking attribute
// equivalence.

const semanticSuggestHint = "No exact matches found. These symbols are semantically similar to your query:"

type semanticSuggestionsXML struct {
	XMLName xml.Name           `xml:"semantic_suggestions"`
	Hint    string             `xml:"hint"`
	Items   []semSuggestionXML `xml:"suggestion"`
}

type semSuggestionXML struct {
	Rank     int           `xml:"rank,attr"`
	Distance string        `xml:"distance,attr"`
	Symbol   semSymbolXML  `xml:"symbol"`
	File     semSymFileXML `xml:"file"`
}

type semSymbolXML struct {
	Kind string `xml:"kind,attr"`
	Name string `xml:",chardata"`
}

type semSymFileXML struct {
	Line int    `xml:"line,attr"`
	Path string `xml:",chardata"`
}

// formatSemanticSuggestions formats semantic search results as an XML
// suggestions block (a fragment embedded verbatim into the tool error/no-match
// responses via ,innerxml).
func formatSemanticSuggestions(results []embeddings.SearchResult) string {
	resp := semanticSuggestionsXML{
		Hint:  semanticSuggestHint,
		Items: make([]semSuggestionXML, 0, len(results)),
	}
	for i, r := range results {
		resp.Items = append(resp.Items, semSuggestionXML{
			Rank:     i + 1,
			Distance: fmt.Sprintf("%.4f", r.Distance),
			Symbol:   semSymbolXML{Kind: r.SymbolKind, Name: r.SymbolName},
			File:     semSymFileXML{Line: r.StartLine, Path: r.FilePath},
		})
	}
	return xmlMarshalFragment(resp)
}
