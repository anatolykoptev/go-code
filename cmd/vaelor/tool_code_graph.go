package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"log/slog"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/codegraph"
	"github.com/anatolykoptev/vaelor/internal/ingest"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type xmlGraphResponse struct {
	XMLName xml.Name      `xml:"response"`
	Graph   xmlGraphQuery `xml:"graph"`
}

type xmlGraphQuery struct {
	Repo      string       `xml:"repo,attr"`
	Template  string       `xml:"template,attr"`
	Vertices  int          `xml:"vertices,attr"`
	Edges     int          `xml:"edges,attr"`
	Cached    bool         `xml:"cached,attr"`
	Query     string       `xml:"query"`
	Cypher    string       `xml:"cypher"`
	Results   xmlGraphRows `xml:"results"`
	Narrative string       `xml:"narrative,omitempty"`
}

type xmlGraphRows struct {
	Rows []xmlGraphRow `xml:"row"`
}

type xmlGraphRow struct {
	Cols []string `xml:"col"`
}

// codeGraphStatusXML is the building/unavailable status shape returned by
// code_graph, mirroring semanticStatusXML so the caller gets a status response
// instead of a tool error.
type codeGraphStatusXML struct {
	XMLName xml.Name `xml:"response"`
	Tool    string   `xml:"tool,attr"`
	Query   string   `xml:"query"`
	Repo    string   `xml:"repo"`
	Status  string   `xml:"status"`
	Message string   `xml:"message"`
}

// CodeGraphInput is the input schema for the code_graph tool.
type CodeGraphInput struct {
	Repo      string `json:"repo" jsonschema:"Repository: GitHub slug (owner/repo), full GitHub URL, or absolute local host path"`
	Query     string `json:"query" jsonschema:"Natural language question about the code graph (e.g. 'who calls ParseFile?', 'what depends on package store?', 'find dead code')"`
	Language  string `json:"language,omitempty" jsonschema:"Limit graph to files of this language (e.g. go, python)"`
	Refresh   bool   `json:"refresh,omitempty" jsonschema:"Force re-indexing of the graph even if cached"`
	Narrative *bool  `json:"narrative,omitempty" jsonschema:"Set to false to skip LLM narrative generation and return only raw graph rows + Cypher (faster, fewer tokens). Default: true"`
}

// registerCodeGraph registers the code_graph MCP tool.
// If store is nil the tool is not registered (DATABASE_URL not configured).
func registerCodeGraph(server *mcp.Server, cfg Config, deps analyze.Deps, store *codegraph.Store) {
	if store == nil {
		slog.Info("code_graph: DATABASE_URL not set, tool disabled")
		return
	}

	addTool(server, &mcp.Tool{
		Name: "code_graph",
		Description: "Query a persistent code knowledge graph backed by Apache AGE. " +
			"Indexes the repository as a property graph with vertices (Package, File, Symbol, Layer, Route) " +
			"and edges (CONTAINS, CALLS, INHERITS, IMPLEMENTS, IMPORTS, HANDLES, FETCHES, BELONGS_TO, TESTED_BY). " +
			"Answers natural-language questions using Cypher query templates or LLM-generated Cypher. " +
			"Lazy indexing: if the graph is not cached, it builds in the background and returns a " +
			"<status>building</status> response; retry the same query in 2-3 minutes. " +
			"Ideal for: call chains, type hierarchies, dependency analysis, dead code detection, " +
			"API route mapping, cross-language connections, coupling analysis, " +
			"community detection (Louvain clusters — 'show communities'), " +
			"surprise scoring (hidden cross-package dependencies — 'find hidden dependencies'), " +
			"and graph diff (what changed since last rebuild — 'what changed in the graph'). " +
			"Results include raw graph rows and an LLM narrative.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CodeGraphInput) (*mcp.CallToolResult, error) {
		return handleCodeGraph(ctx, input, cfg, deps, store)
	})
}

// handleCodeGraph is the extracted handler for code_graph, callable from tests.
func handleCodeGraph(ctx context.Context, input CodeGraphInput, cfg Config, deps analyze.Deps, store *codegraph.Store) (*mcp.CallToolResult, error) {
	outputDir := cfg.OutputDir

	if input.Repo == "" {
		return errResult("repo is required"), nil
	}
	if input.Query == "" {
		return errResult("query is required"), nil
	}

	// Gate: code_graph NL-query requires LLM to generate or select Cypher.
	if !deps.LLMHasKey {
		return errResult("code_graph: requires LLM_API_KEY to be set"), nil
	}

	if store != nil && !store.HasAGE(ctx) {
		return errResult("Apache AGE extension is not available in the configured PostgreSQL instance"), nil
	}

	root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps)
	if err != nil {
		return errResult(fmt.Sprintf("resolve repo: %s", err)), nil
	}
	defer cleanup()

	isRemote := ingest.IsRemote(input.Repo)
	repoKey := codegraph.GraphNameFor(root)

	if input.Refresh {
		// Snapshot current graph before forced refresh for future diffing.
		codegraph.SnapshotBeforeRebuild(ctx, store, repoKey, repoKey)
		if dropErr := store.DropGraph(ctx, repoKey, repoKey); dropErr != nil {
			slog.Warn("code_graph: drop graph failed (continuing with re-index)",
				slog.String("key", repoKey),
				slog.Any("error", dropErr))
		}
	}

	// Check whether a fresh graph is already cached.
	// If not, launch a background goroutine to build it and return immediately
	// so the MCP client is not held for the duration of the first-time build,
	// which can take several minutes for large repos.
	indexCfg := codegraph.IndexConfig{
		TTLLocal:            cfg.GraphTTLLocal,
		TTLRemote:           cfg.GraphTTLRemote,
		BatchSize:           cfg.GraphBatchSize,
		EnableSurpriseIndex: cfg.CodegraphSurpriseIndex,
		FlowsMax:            cfg.FlowsMax,
		FlowsDFSDepth:       cfg.FlowsDFSDepth,
	}

	fresh, status := ensureAgeGraphOrStatus(ctx, "code_graph", store, root, repoKey, isRemote, indexCfg, func(status, message string) *mcp.CallToolResult {
		return textResult(buildCodeGraphStatusResponse(input, status, message))
	})
	if !fresh {
		return status, nil
	}

	meta, err := codegraph.IndexRepo(ctx, store, root, isRemote, indexCfg)
	if err != nil {
		return errResult(fmt.Sprintf("index repo: %s", err)), nil
	}
	recordCodeGraphAge(codegraph.GraphNameFor(root), meta.BuiltAt)

	narrativeEnabled := true
	if input.Narrative != nil {
		narrativeEnabled = *input.Narrative
	}

	result, err := codegraph.QueryGraph(ctx, store, deps.LLM, meta.GraphName, input.Query, meta, narrativeEnabled)
	if err != nil {
		return errResult(fmt.Sprintf("query graph: %s", err)), nil
	}

	formatted, err := formatGraphXML(result)
	if err != nil {
		return errResult(fmt.Sprintf("marshal: %s", err)), nil
	}

	return largeTextResult(formatted, "code_graph", outputDir), nil
}

// formatGraphXML converts a QueryResult to XML string.
func formatGraphXML(result *codegraph.QueryResult) (string, error) {
	resp := xmlGraphResponse{
		Graph: xmlGraphQuery{
			Repo:      result.Repo,
			Template:  result.Template,
			Vertices:  result.GraphStats.Vertices,
			Edges:     result.GraphStats.Edges,
			Cached:    result.GraphStats.Cached,
			Query:     result.Query,
			Cypher:    result.Cypher,
			Narrative: result.Narrative,
		},
	}
	rows := make([]xmlGraphRow, len(result.Results))
	for i, r := range result.Results {
		rows[i] = xmlGraphRow{Cols: r}
	}
	resp.Graph.Results = xmlGraphRows{Rows: rows}

	data, err := xml.Marshal(resp)
	if err != nil {
		return "", err
	}
	return xml.Header + string(data), nil
}

// buildCodeGraphStatusResponse returns an XML status response similar to
// semantic_search's buildStatusResponse, so code_graph can signal "graph is
// building" as a normal (non-error) status and include the retry hint.
func buildCodeGraphStatusResponse(input CodeGraphInput, status, message string) string {
	return xmlMarshalFragment(codeGraphStatusXML{
		Tool:    "code_graph",
		Query:   input.Query,
		Repo:    input.Repo,
		Status:  status,
		Message: message,
	})
}
