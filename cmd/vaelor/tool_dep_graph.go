package main

import (
	"context"
	"encoding/xml"
	"fmt"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type xmlDepGraphResponse struct {
	XMLName  xml.Name    `xml:"response"`
	DepGraph xmlDepGraph `xml:"depGraph"`
}

type xmlDepGraph struct {
	Format  string   `xml:"format,attr,omitempty"`
	Content xmlCDATA `xml:"content"`
}

// DepGraphInput is the input schema for the dep_graph tool.
type DepGraphInput struct {
	// Repo is the GitHub repo slug (owner/repo) or local filesystem path.
	Repo string `json:"repo" jsonschema:"GitHub repo slug (owner/repo), full GitHub URL, or absolute local host path (e.g. /home/user/src/project)"`

	// Type selects what to graph: imports (file-level), packages, modules, or calls (function call graph).
	Type string `json:"type,omitempty" jsonschema:"Graph type: imports | packages | modules | calls (default: packages)"`

	// Format controls output: json (adjacency list), dot (Graphviz), mermaid, or summary.
	Format string `json:"format,omitempty" jsonschema:"Output format: json | dot | mermaid | summary (default: mermaid)"`

	// Focus limits the graph to a specific package or module.
	Focus string `json:"focus,omitempty" jsonschema:"Package or subdirectory to focus on (e.g. internal/auth), or space-separated keywords (e.g. 'auth handler')"`

	// Depth limits graph traversal depth from focused node.
	Depth int `json:"depth,omitempty" jsonschema:"Max traversal depth from focus node (default: 3, 0=unlimited)"`

	// MaxDepth is a deprecated alias for Depth.
	MaxDepth int `json:"max_depth,omitempty" jsonschema:"Deprecated: use depth instead"`

	// IncludeStdlib includes Go standard library imports in the graph.
	IncludeStdlib bool `json:"include_stdlib,omitempty" jsonschema:"Include standard library imports in graph. Default false (stdlib excluded)."`

	// CrossLanguage includes cross-language API route connections between layers.
	// Cross-language dependencies are available via code_graph polyglot_overview and layer_deps templates.
	CrossLanguage bool `json:"cross_language,omitempty" jsonschema:"Include cross-language API route connections between layers"`
}

// registerDepGraph registers the dep_graph MCP tool.
// Builds and visualizes the dependency graph of a repository.
func registerDepGraph(server *mcp.Server, cfg Config, deps analyze.Deps) {
	outputDir := cfg.OutputDir

	addTool(server, &mcp.Tool{
		Name: "dep_graph",
		Description: "Build and visualize the dependency graph of a repository. " +
			"Parses import/require/use statements across all source files using tree-sitter, " +
			"then constructs a directed graph of package or module dependencies. " +
			"Supports output as Mermaid diagrams, Graphviz DOT, or JSON adjacency lists. " +
			"Can detect cycles, hotspots, and layering violations. " +
			"Set cross_language=true to add HTTP Route edges connecting frontend callers to backend handlers.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DepGraphInput) (*mcp.CallToolResult, error) {
		if input.Repo == "" {
			return errResult("repo is required"), nil
		}

		root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps)
		if err != nil {
			return errResult(fmt.Sprintf("resolve repo: %s", err)), nil
		}
		defer cleanup()

		// Support deprecated max_depth alias.
		if input.MaxDepth > 0 && input.Depth == 0 {
			input.Depth = input.MaxDepth
		}

		graph, err := analyze.BuildDepGraph(ctx, analyze.DepGraphInput{
			Root:          root,
			Type:          input.Type,
			Format:        input.Format,
			Focus:         input.Focus,
			MaxDepth:      input.Depth,
			IncludeStdlib: input.IncludeStdlib,
			CrossLanguage: input.CrossLanguage,
		})
		if err != nil {
			return errResult(fmt.Sprintf("build dep graph: %s", err)), nil
		}

		resp := xmlDepGraphResponse{
			DepGraph: xmlDepGraph{
				Format:  input.Format,
				Content: xmlCDATA{Inner: wrapCDATA(graph)},
			},
		}
		return xmlMarshalResult(resp, "dep_graph", outputDir), nil
	})
}
