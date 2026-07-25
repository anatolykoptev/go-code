package main

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/codegraph"
	"github.com/anatolykoptev/vaelor/internal/embeddings"
	"github.com/anatolykoptev/vaelor/internal/research"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// CodeResearchInput is the input schema for the code_research tool.
type CodeResearchInput struct {
	Repo             string `json:"repo,omitempty" jsonschema:"GitHub repo (owner/repo) or local path. Required in practice — omitting it returns a short error naming recently-indexed repos."`
	Query            string `json:"query" jsonschema:"Natural language query describing what you're looking for (e.g. 'DAG parallel executor implementation', 'how retry logic works')"`
	Language         string `json:"language,omitempty" jsonschema:"Filter by language (e.g. go, python, typescript). Optional."`
	MaxTokens        int    `json:"max_tokens,omitempty" jsonschema:"Token budget for the output map (default 8000). Higher = more context, more tokens."`
	ExpandHops       int    `json:"expand_hops,omitempty" jsonschema:"Import-graph expansion hops from seed files (default 2). Higher = wider context."`
	IncludeBody      bool   `json:"include_body,omitempty" jsonschema:"Include full function bodies in the output (default false). Significantly increases token usage."`
	FileGlob         string `json:"file_glob,omitempty" jsonschema:"Restrict analysis to files matching this glob (e.g. 'internal/**', 'pkg/foo/*.go'). Optional."`
	IncludeTests     bool   `json:"include_tests,omitempty" jsonschema:"Include *_test.go / test files in retrieval (default false). Useful for 'how is X tested' queries."`
	IncludeCallGraph bool   `json:"include_call_graph,omitempty" jsonschema:"Expand retrieval via call-graph edges (callers + callees) in addition to imports. Slower but higher precision for 'what calls X' queries."`
	Compact          bool   `json:"compact,omitempty" jsonschema:"If true, return only the stats header and rendered map (skip <seeds>/<graph>). ~20% token savings."`
}

// registerCodeResearch registers the code_research MCP tool.
func registerCodeResearch(server *mcp.Server, _ Config, deps analyze.Deps, semDeps *SemanticDeps) {
	addTool(server, &mcp.Tool{
		Name: "code_research",
		Description: "Deep code research for large repositories. " +
			"Combines keyword (BM25F with doc-comment indexing), semantic (embeddings), " +
			"import-graph DAG expansion, and optional call-graph BFS (callers+callees) " +
			"to find relevant code and produce a compact, LLM-ready map. " +
			"Features: file_glob scoping, include_tests/include_call_graph opt-ins, " +
			"compact output mode. " +
			"Better than repo_analyze for targeted questions: 'how does X work?', " +
			"'find the implementation of Y', 'what calls Z'. " +
			"Returns seed symbols (direct matches) + linked files (import/call-graph neighbours) " +
			"+ Aider-style compact map within a token budget.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CodeResearchInput) (*mcp.CallToolResult, error) {
		return handleCodeResearch(ctx, input, deps, semDeps)
	})
}

func handleCodeResearch(
	ctx context.Context, input CodeResearchInput,
	analyzeDeps analyze.Deps, semDeps *SemanticDeps,
) (*mcp.CallToolResult, error) {
	if input.Repo == "" {
		return errResult(shortMissingRepoMsg(ctx, semStore(semDeps), analyzeDeps.LocalRepoDirs)), nil
	}
	if input.Query == "" {
		return errResult("query is required"), nil
	}

	root, cleanup, err := resolveRoot(ctx, input.Repo, "", analyzeDeps)
	if err != nil {
		return errResult(fmt.Sprintf("resolve repo: %s", err)), nil
	}
	defer cleanup()

	resDeps := research.Deps{
		AnalyzeDeps: analyzeDeps,
		BuildCallGraph: func(ctx context.Context, root string) (*callgraph.CallGraph, error) {
			cgCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			return callgraph.BuildFromRepo(cgCtx, callgraph.TraceRepoInput{Root: root})
		},
	}
	if semDeps != nil && semDeps.Client != nil && semDeps.QueryClient != nil && semDeps.Store != nil {
		// Use QueryClient (not Client) so model-specific prefixes are applied
		// on the query path. The research package's EmbedClient interface only
		// calls EmbedQuery, never Embed, so QueryEmbedder satisfies it.
		resDeps.EmbedClient = semDeps.QueryClient
		resDeps.EmbedStore = semDeps.Store
		resDeps.RepoKey = codegraph.GraphNameFor(root)
		// Wire pg_trgm symbol search for Step 3.5 augmentation.
		resDeps.SymbolSearcher = semDeps.Store
		// Trigger background re-index for freshness.
		if semDeps.Pipeline != nil {
			semDeps.Pipeline.IndexRepoAsyncWithTool("code_research", resDeps.RepoKey, root)
		}
	}
	if analyzeDeps.Graph != nil {
		resDeps.Graph = analyzeDeps.Graph
		resDeps.GraphRepoKey = root
	}

	result, err := research.Run(ctx, research.Input{
		Root:             root,
		Query:            input.Query,
		Language:         input.Language,
		MaxTokens:        input.MaxTokens,
		ExpandHops:       input.ExpandHops,
		IncludeBody:      input.IncludeBody,
		FileGlob:         input.FileGlob,
		IncludeTests:     input.IncludeTests,
		IncludeCallGraph: input.IncludeCallGraph,
	}, resDeps)
	if err != nil {
		return errResult(fmt.Sprintf("code_research: %s", err)), nil
	}

	return textResult(formatResearchResult(input, root, result)), nil
}

const (
	// maxSeedsOutput caps the number of seed files in the XML output.
	maxSeedsOutput = 30
	// maxGraphOutput caps the number of graph files in the XML output.
	maxGraphOutput = 50
)

func formatResearchResult(input CodeResearchInput, root string, r *research.Result) string {
	// Strip workspace prefix from paths for cleaner output.
	stripRoot := root + "/"

	resp := researchRespXML{
		Tool:  "code_research",
		Query: input.Query,
		Repo:  input.Repo,
		Mode:  r.Mode,
		Stats: researchStatsXML{
			Seeds:           len(r.Seeds),
			GraphFiles:      len(r.Graph),
			Pruned:          r.PrunedFiles,
			EstimatedTokens: r.EstimatedTokens,
		},
	}

	// Compact map — the primary LLM-consumable output, carried verbatim in a
	// CDATA section (byte-neutral vs entity-escaping every <-chan / Vec<T> / &x).
	// A nil pointer omits the element, matching the prior `if r.Map != ""` guard.
	//
	// Emitted BEFORE Seeds/Graph (#571): the map is the verdict/summary the
	// agent needs most; Seeds and Graph are detail sections that get cut off
	// first when the response hits the client truncation ceiling. Putting the
	// map first ensures it survives budget shaping.
	if r.Map != "" {
		resp.Map = &xmlCDATA{Inner: wrapCDATA(r.Map)}
	}

	if !input.Compact {
		// Seeds section — top N by score.
		if seeds := sortedSeeds(r.Seeds, maxSeedsOutput); len(seeds) > 0 {
			resp.Seeds = buildResearchSeeds(seeds, stripRoot)
		}
		// Graph section — top N by score, skip files with no symbols.
		if graph := sortedGraph(r.Graph, maxGraphOutput); len(graph) > 0 {
			resp.Graph = buildResearchGraph(graph, stripRoot)
		}
	}

	return xmlMarshalFragment(resp)
}

func sortedSeeds(seeds []research.SeedSymbol, limit int) []research.SeedSymbol {
	if len(seeds) <= limit {
		out := make([]research.SeedSymbol, len(seeds))
		copy(out, seeds)
		sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
		return out
	}
	out := make([]research.SeedSymbol, len(seeds))
	copy(out, seeds)
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out[:limit]
}

func sortedGraph(graph []research.LinkedFile, limit int) []research.LinkedFile {
	if len(graph) <= limit {
		return graph
	}
	out := make([]research.LinkedFile, len(graph))
	copy(out, graph)
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out[:limit]
}

// embeddings.SearchOpts needs to be accessible — re-export via alias is not needed
// since we use it only through semDeps which is already typed.
var _ = embeddings.SearchOpts{}
