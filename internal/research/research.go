package research

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"sort"
	"time"

	"github.com/anatolykoptev/go-kit/rerank"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/codegraph"
	"github.com/anatolykoptev/vaelor/internal/embeddings"
	"github.com/anatolykoptev/vaelor/internal/graphx"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

// Deps holds optional dependencies for the research pipeline.
// All fields are optional — the pipeline degrades gracefully.
type Deps struct {
	// AnalyzeDeps provides ingest, parse, and BM25 capabilities (required).
	AnalyzeDeps analyze.Deps

	// EmbedClient enables semantic search signal. Optional.
	EmbedClient EmbedClient

	// EmbedStore provides vector similarity search. Optional.
	EmbedStore EmbedStore

	// RepoKey is the embedding store key for this repository. Required when
	// EmbedClient/EmbedStore are set.
	RepoKey string

	// BuildCallGraph is an optional hook that returns the call graph for a
	// repository root. Used when Input.IncludeCallGraph=true. Non-fatal — if
	// nil or it returns an error, import-DAG expansion still runs normally.
	BuildCallGraph func(ctx context.Context, root string) (*callgraph.CallGraph, error)

	// SymbolSearcher enables pg_trgm symbol name search.
	// Optional — nil disables symbol name augmentation.
	SymbolSearcher SymbolSearcher

	// Graph provides PageRank signals for symbol-importance file boosting.
	// Optional - nil disables PageRank file prioritization.
	Graph graphx.Analytics

	// GraphRepoKey is the repo root path passed to TopPageRank. Required when Graph is non-nil.
	GraphRepoKey string
}

// Run executes the full code-research pipeline:
//  1. BM25F keyword seeds (always)
//  2. Semantic embed seeds (if EmbedClient+EmbedStore available) + RRF fusion
//  3. Import-DAG BFS expansion (±ExpandHops hops)
//  4. Token-budget pruning with distance decay
//  5. Aider-style compact map rendering
func Run(ctx context.Context, input Input, deps Deps) (*Result, error) {
	t0 := time.Now()
	if input.Root == "" {
		return nil, fmt.Errorf("root is required")
	}
	if input.Query == "" {
		return nil, fmt.Errorf("query is required")
	}
	if input.MaxTokens <= 0 {
		input.MaxTokens = DefaultMaxTokens
	}
	if input.ExpandHops <= 0 {
		input.ExpandHops = DefaultExpandHops
	}

	// --- Step 1: parse repo + build BM25F scores ---
	t_analyze := time.Now()
	data, err := analyze.AnalyzeForResearch(ctx, input.Root, input.Query, input.Language, input.FileGlob, input.IncludeTests, input.IncludeBody, deps.AnalyzeDeps)
	if err != nil {
		return nil, fmt.Errorf("analyze: %w", err)
	}
	slog.Info("research.run: analyze done",
		slog.String("root", input.Root),
		slog.Int("files", len(data.Files)),
		slog.Duration("elapsed", time.Since(t_analyze)))

	// --- Step 2: collect seed scores from fused ranking ---
	seedScores := make(map[string]float64, len(data.FusedScores))
	for relPath, score := range data.FusedScores {
		if score > 0 {
			seedScores[relPath] = score
		}
	}
	mode := "keyword-only"

	// --- Step 3: semantic search (optional) + RRF fusion ---
	t_sem := time.Now()
	var semanticHits []embeddings.SearchResult
	if deps.EmbedClient != nil && deps.EmbedStore != nil && deps.RepoKey != "" {
		hits, semMode := runSemanticSeeds(ctx, input, deps)
		if len(hits) > 0 {
			mode = semMode
			semanticHits = hits

			// Build per-file rank for fusion (highest 1-distance score wins per file).
			semFileScores := make(map[string]float64, len(hits))
			for _, r := range hits {
				s := 1.0 - float64(r.Distance)
				if s > semFileScores[r.FilePath] {
					semFileScores[r.FilePath] = s
				}
			}
			seedScores = fuseScores(seedScores, semFileScores)
		}
	}
	slog.Info("research.run: semantic search done",
		slog.String("root", input.Root),
		slog.String("mode", mode),
		slog.Int("semantic_hits", len(semanticHits)),
		slog.Duration("elapsed", time.Since(t_sem)))

	// --- Step 3.5: pg_trgm symbol name augmentation ---
	// Finds symbols missed by vector search due to abbreviated names (init->initializes etc.)
	t_trgm := time.Now()
	if deps.SymbolSearcher != nil && deps.RepoKey != "" {
		kws := embeddings.ExtractQueryKeywords(input.Query)
		if nameHits, err := deps.SymbolSearcher.SearchBySymbolName(ctx, deps.RepoKey, kws, input.Language, 20); err == nil && len(nameHits) > 0 {
			// Build per-file scores from pg_trgm hits and fuse into seedScores.
			trgmFileScores := make(map[string]float64, len(nameHits))
			for _, r := range nameHits {
				s := float64(1.0 - r.Distance)
				if s > trgmFileScores[r.FilePath] {
					trgmFileScores[r.FilePath] = s
				}
			}
			seedScores = fuseScores(seedScores, trgmFileScores)
			// Also append to semanticHits so buildSeedList surfaces individual symbols.
			semanticHits = append(semanticHits, nameHits...)
		}
	}

	// --- Step 3.6: PageRank file boost ---
	// Files containing top-PageRank symbols get a structural importance boost.
	// Ensures architecturally central code surfaces in research context even when
	// not textually similar to the query (e.g. learnings/store.go PageRank=0.038).
	if deps.Graph != nil && deps.GraphRepoKey != "" {
		if signals, prErr := deps.Graph.TopPageRank(ctx, deps.GraphRepoKey, 20); prErr == nil {
			for _, sig := range signals {
				if sig.PageRank > 0 && sig.Symbol.File != "" {
					// Boost proportional to PageRank, max +0.3
					boost := sig.PageRank * 8.0
					if boost > 0.3 {
						boost = 0.3
					}
					seedScores[sig.Symbol.File] += boost
				}
			}
		}
	}
	slog.Info("research.run: trgm + pagerank boost done",
		slog.String("root", input.Root),
		slog.Int("seed_scores", len(seedScores)),
		slog.Duration("elapsed", time.Since(t_trgm)))

	// --- Step 4: build seed set ---
	seedFiles := make(map[string]bool)
	for relPath, score := range seedScores {
		if score > 0 {
			seedFiles[relPath] = true
		}
	}

	// --- Step 5: DAG expansion ---
	t_expand := time.Now()
	expanded := expandFromSeeds(seedFiles, data.FileImports, input.ExpandHops)

	// --- Step 5b: call-graph BFS expansion (optional) ---
	var warmingNote string
	if input.IncludeCallGraph && deps.BuildCallGraph != nil {
		cg, cgErr := deps.BuildCallGraph(ctx, input.Root)
		if cgErr != nil {
			log.Printf("research: call-graph build failed (non-fatal): %v", cgErr)
		} else if cg != nil {
			cgExpanded := expandFromCallGraph(seedFiles, cg, input.ExpandHops)
			expanded = mergeExpandResults(expanded, cgExpanded)
			if cg.Warming {
				warmingNote = "type-aware enrichment is warming in the background; retry for the enhanced tier (go/types interface dispatch resolution)"
			}
		}
	}
	slog.Info("research.run: DAG + callgraph expansion done",
		slog.String("root", input.Root),
		slog.Int("seeds", len(seedFiles)),
		slog.Int("expanded", len(expanded)),
		slog.Duration("elapsed", time.Since(t_expand)))

	// --- Step 6: filter symbols per file by query terms ---
	t_filter := time.Now()
	filteredSymbols := make(map[string][]*parser.Symbol, len(expanded))
	for _, ex := range expanded {
		syms := data.FileSymbols[ex.relPath]
		filteredSymbols[ex.relPath] = filterSymbolsByQuery(syms, data.QueryTerms)
	}

	// --- Step 7: token-budget pruning ---
	kept, pruned := pruneToTokenBudget(expanded, seedScores, filteredSymbols, input.MaxTokens, input.IncludeBody)

	// --- Step 7b: attach sibling test files (only when IncludeTests=true) ---
	if input.IncludeTests {
		allFilesMap := make(map[string]bool, len(data.Files))
		for _, f := range data.Files {
			allFilesMap[f.RelPath] = true
		}
		kept = linkTestFiles(kept, allFilesMap)

		// Populate filteredSymbols for any newly-added test files so the
		// render/graph stages don't see nil entries.
		for _, sf := range kept {
			if _, ok := filteredSymbols[sf.expand.relPath]; !ok {
				filteredSymbols[sf.expand.relPath] = filterSymbolsByQuery(
					data.FileSymbols[sf.expand.relPath], data.QueryTerms,
				)
			}
		}
	}
	slog.Info("research.run: filter + prune done",
		slog.String("root", input.Root),
		slog.Int("kept", len(kept)),
		slog.Int("pruned", pruned),
		slog.Duration("elapsed", time.Since(t_filter)))

	// --- Step 8: render map ---
	t_render := time.Now()
	codeMap := RenderMap(kept, input.IncludeBody, input.Root)
	estimatedTokens := estimateMapTokens(codeMap)

	// --- Build result ---
	seeds := buildSeedList(seedFiles, seedScores, data.FileSymbols, data.QueryTerms, semanticHits)
	graph := buildLinkedFiles(kept, data.FileSymbols, data.QueryTerms)

	// --- CE rerank seeds for better relevance ordering ---
	// Converts SeedSymbol list to SearchResult, reranks via cross-encoder, maps back.
	if len(seeds) > 0 && input.Root != "" && input.Query != "" {
		searchResults := make([]embeddings.SearchResult, len(seeds))
		for i, s := range seeds {
			searchResults[i] = embeddings.SearchResult{
				FilePath:   s.File,
				SymbolName: s.Name,
				SymbolKind: s.Kind,
				Language:   input.Language,
				StartLine:  int(s.Line),
			}
		}
		reranked := codegraph.RerankSemanticResults(ctx, input.Root, input.Query, searchResults, min(len(searchResults), 50))
		// Map reranked SearchResults back to SeedSymbols preserving all fields.
		seedByKey := make(map[string]SeedSymbol, len(seeds))
		for _, s := range seeds {
			seedByKey[s.File+":"+s.Name] = s
		}
		rerankedSeeds := make([]SeedSymbol, 0, len(reranked))
		seen := make(map[string]bool, len(reranked))
		for _, r := range reranked {
			key := r.FilePath + ":" + r.SymbolName
			if s, ok := seedByKey[key]; ok {
				rerankedSeeds = append(rerankedSeeds, s)
				seen[key] = true
			}
		}
		// Append any seeds not covered by reranker (e.g. beyond topK).
		for _, s := range seeds {
			if !seen[s.File+":"+s.Name] {
				rerankedSeeds = append(rerankedSeeds, s)
			}
		}
		seeds = rerankedSeeds
	}
	slog.Info("research.run: render + rerank done",
		slog.String("root", input.Root),
		slog.Int("seeds", len(seeds)),
		slog.Int("estimated_tokens", estimatedTokens),
		slog.Duration("elapsed", time.Since(t_render)))

	slog.Info("research.run: complete",
		slog.String("root", input.Root),
		slog.String("mode", mode),
		slog.Int("seeds", len(seeds)),
		slog.Int("kept", len(kept)),
		slog.Int("pruned", pruned),
		slog.Duration("total", time.Since(t0)))

	var warnings []string
	if warmingNote != "" {
		warnings = []string{warmingNote}
	}
	return &Result{
		Seeds:           seeds,
		Graph:           graph,
		Map:             codeMap,
		EstimatedTokens: estimatedTokens,
		PrunedFiles:     pruned,
		Mode:            mode,
		Warnings:        warnings,
	}, nil
}

const (
	semanticTopKMin = 10
	semanticTopKMax = 100
	semanticTopKDiv = 400

	// minSeedScore filters out low-relevance BM25F/RRF seeds.
	// RRF scores cluster around 0.009-0.016 for noise files; meaningful
	// matches typically score 0.02+. Semantic seeds bypass this filter.
	minSeedScore = 0.02
)

// semanticTopK scales the embedding-store TopK with the token budget.
// Formula: clamp(MaxTokens / 400, 10, 100). A budget of 8000 (default)
// yields 20 — matching the previous hardcoded value.
func semanticTopK(maxTokens int) int {
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokens
	}
	k := maxTokens / semanticTopKDiv
	if k < semanticTopKMin {
		k = semanticTopKMin
	}
	if k > semanticTopKMax {
		k = semanticTopKMax
	}
	return k
}

// runSemanticSeeds queries the embed store and returns the raw hits.
func runSemanticSeeds(ctx context.Context, input Input, deps Deps) ([]embeddings.SearchResult, string) {
	vec, err := deps.EmbedClient.EmbedQuery(ctx, input.Query)
	if err != nil {
		return nil, "keyword-only"
	}

	results, err := deps.EmbedStore.Search(ctx, vec, embeddings.SearchOpts{
		RepoKey:  deps.RepoKey,
		Language: input.Language,
		TopK:     semanticTopK(input.MaxTokens),
	})
	if err != nil || len(results) == 0 {
		return nil, "keyword-only"
	}

	return results, "full"
}

// fuseScores combines two score maps using Reciprocal Rank Fusion
// (Cormack et al. 2009). Backed by go-kit/rerank.RRF (k=60). Each input map
// is sorted by score descending into a ranked ID list before fusion.
// Accepts nil for either input.
func fuseScores(a, b map[string]float64) map[string]float64 {
	rankIDs := func(m map[string]float64) []string {
		ids := make([]string, 0, len(m))
		for id := range m {
			ids = append(ids, id)
		}
		sort.SliceStable(ids, func(i, j int) bool { return m[ids[i]] > m[ids[j]] })
		return ids
	}

	fused := rerank.RRF(60, rankIDs(a), rankIDs(b))
	out := make(map[string]float64, len(fused))
	for _, f := range fused {
		out[f.ID] = f.Score
	}
	return out
}

// estimateMapTokens estimates the token count of the rendered map string.
func estimateMapTokens(m string) int {
	return len(m) / charsPerToken
}

// buildSeedList converts seed file map into ordered SeedSymbol slice.
// semanticHits are emitted first (one per hit, preserving SymbolName/Kind/StartLine);
// keyword matches that overlap with a semantic hit on (file, name) are upgraded to "hybrid".
func buildSeedList(
	seedFiles map[string]bool,
	seedScores map[string]float64,
	fileSymbols map[string][]*parser.Symbol,
	queryTerms []string,
	semanticHits []embeddings.SearchResult,
) []SeedSymbol {
	var seeds []SeedSymbol

	// 1) Semantic-derived seeds — one entry per hit, preserves StartLine.
	type semKey struct {
		file string
		name string
	}
	semSeen := make(map[semKey]int) // maps key → index in seeds slice
	for _, h := range semanticHits {
		seeds = append(seeds, SeedSymbol{
			File:   h.FilePath,
			Name:   h.SymbolName,
			Kind:   h.SymbolKind,
			Line:   int(h.StartLine),
			Score:  1.0 - float64(h.Distance),
			Source: "semantic",
		})
		semSeen[semKey{h.FilePath, h.SymbolName}] = len(seeds) - 1
	}

	// 2) Keyword/fused-derived seeds. If the same (file, name) already exists
	//    from a semantic hit, upgrade it to "hybrid" instead of duplicating.
	// Skip zero/near-zero score seeds — they're noise.
	for relPath := range seedFiles {
		score := seedScores[relPath]
		if score < minSeedScore {
			continue
		}
		syms := filterSymbolsByQuery(fileSymbols[relPath], queryTerms)
		if len(syms) == 0 {
			// file-level seed with no resolved symbol
			key := semKey{relPath, ""}
			if _, already := semSeen[key]; !already {
				seeds = append(seeds, SeedSymbol{File: relPath, Score: score, Source: "keyword"})
			}
			continue
		}
		for _, sym := range syms {
			key := semKey{relPath, sym.Name}
			if idx, already := semSeen[key]; already {
				seeds[idx].Source = "hybrid"
				continue
			}
			seeds = append(seeds, SeedSymbol{
				File:   relPath,
				Name:   sym.Name,
				Kind:   string(sym.Kind),
				Line:   int(sym.StartLine),
				Score:  score,
				Source: "keyword",
			})
		}
	}
	return seeds
}

// buildLinkedFiles converts pruned scoredFiles into LinkedFile output.
func buildLinkedFiles(
	kept []scoredFile,
	fileSymbols map[string][]*parser.Symbol,
	queryTerms []string,
) []LinkedFile {
	out := make([]LinkedFile, 0, len(kept))
	for _, sf := range kept {
		syms := filterSymbolsByQuery(fileSymbols[sf.expand.relPath], queryTerms)
		out = append(out, LinkedFile{
			RelPath:   sf.expand.relPath,
			Distance:  sf.expand.distance,
			WhyLinked: sf.expand.whyLinked,
			Symbols:   syms,
			Score:     sf.seedScore,
		})
	}
	return out
}
