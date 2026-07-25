package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/parser"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const outputFormatAST = "ast"

// FileParseInput is the input schema for the file_parse tool.
type FileParseInput struct {
	// Repo is optional: GitHub slug or URL. When set, path is relative to the repo root.
	Repo string `json:"repo,omitempty" jsonschema:"Repository: GitHub slug (owner/repo), full GitHub URL, or absolute local host path. When set, path is relative to repo root."`

	// Ref is the branch, tag, or commit SHA (only used with repo).
	Ref string `json:"ref,omitempty" jsonschema:"Branch, tag, or commit SHA (default: HEAD). Only used with repo."`

	// Path is the file path. Absolute for local files, or relative to repo root when repo is set.
	Path string `json:"path" jsonschema:"File path: absolute for local files, or relative to repo root when repo is set"`

	// Language overrides auto-detection.
	Language string `json:"language,omitempty" jsonschema:"Language override (go/python/typescript/rust/java/c/cpp). Auto-detected if omitted."`

	// OutputFormat controls what is returned.
	OutputFormat string `json:"output_format,omitempty" jsonschema:"Output format: ast (raw tree) | symbols (functions types vars) (default: symbols)"`
}

func registerFileParse(server *mcp.Server, cfg Config, deps analyze.Deps) {
	maxBytes := cfg.MaxFileBytes

	addTool(server, &mcp.Tool{
		Name: "file_parse",
		Description: "Parse a single source file using tree-sitter and return its AST or symbol table. " +
			"Supports Go, Python, TypeScript, JavaScript, Rust, Java, C, C++. " +
			"Accepts a repo parameter (GitHub slug or URL) to parse files from remote repositories. " +
			"Use output_format=symbols to get a structured list of functions, types, and variables. " +
			"Use output_format=ast to get the raw syntax tree for deep analysis.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input FileParseInput) (*mcp.CallToolResult, error) {
		if input.Path == "" {
			return errResult("path is required"), nil
		}

		var filePath string
		if input.Repo != "" {
			// Remote or local repo — resolve root, then join with path.
			root, cleanup, err := resolveRoot(ctx, input.Repo, input.Ref, deps)
			if err != nil {
				return errResult(fmt.Sprintf("resolve repo: %s", err)), nil
			}
			defer cleanup()
			filePath = filepath.Join(root, input.Path)
		} else {
			// Local file path — apply path mappings.
			filePath = rewritePath(input.Path, cfg.PathMappings)
		}

		fi, err := os.Stat(filePath)
		if err != nil {
			return errResult(fmt.Sprintf("stat file: %s", err)), nil
		}
		if fi.Size() > maxBytes {
			return errResult(fmt.Sprintf("file too large: %d bytes (max %d)", fi.Size(), maxBytes)), nil
		}

		source, err := os.ReadFile(filePath)
		if err != nil {
			return errResult(fmt.Sprintf("read file: %s", err)), nil
		}

		includeBody := input.OutputFormat == outputFormatAST
		pr, err := parser.ParseFile(filePath, source, parser.ParseOpts{
			Language:       input.Language,
			IncludeBody:    includeBody,
			IncludeImports: true,
		})
		if err != nil {
			return errResult(fmt.Sprintf("parse file: %s", err)), nil
		}

		return textResult(formatParseResult(pr, input.OutputFormat)), nil
	})
}

// formatParseResult formats a ParseResult according to the requested output format.
func formatParseResult(pr *parser.ParseResult, format string) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "File: %s\nLanguage: %s\n\n", pr.File, pr.Language)

	if len(pr.Imports) > 0 {
		fmt.Fprintf(&sb, "## Imports (%d)\n", len(pr.Imports))
		for _, imp := range pr.Imports {
			fmt.Fprintf(&sb, "  - %s\n", imp)
		}
		sb.WriteString("\n")
	}

	if len(pr.Symbols) == 0 {
		sb.WriteString("No symbols found.\n")
		return sb.String()
	}

	fmt.Fprintf(&sb, "## Symbols (%d)\n", len(pr.Symbols))

	if format == outputFormatAST {
		formatSymbolsAST(&sb, pr.Symbols)
	} else {
		formatSymbolsTable(&sb, pr.Symbols)
	}

	return sb.String()
}

// formatSymbolsTable formats symbols as a compact list with kind, name, and line numbers.
func formatSymbolsTable(sb *strings.Builder, symbols []*parser.Symbol) {
	for _, sym := range symbols {
		if sym.Signature != "" {
			fmt.Fprintf(sb, "  [%s] %s\n    Signature: %s\n    Lines: %d-%d\n",
				sym.Kind, sym.Name, sym.Signature, sym.StartLine, sym.EndLine)
		} else {
			fmt.Fprintf(sb, "  [%s] %s\n    Lines: %d-%d\n",
				sym.Kind, sym.Name, sym.StartLine, sym.EndLine)
		}
		if sym.Complexity > 0 {
			fmt.Fprintf(sb, "    Complexity: %d\n", sym.Complexity)
		}
		if sym.DocComment != "" {
			fmt.Fprintf(sb, "    Doc: %s\n", sym.DocComment)
		}
	}
}

// formatSymbolsAST formats symbols with their full source body included.
func formatSymbolsAST(sb *strings.Builder, symbols []*parser.Symbol) {
	for _, sym := range symbols {
		if sym.Complexity > 0 {
			fmt.Fprintf(sb, "### [%s] %s (lines %d-%d, complexity %d)\n", sym.Kind, sym.Name, sym.StartLine, sym.EndLine, sym.Complexity)
		} else {
			fmt.Fprintf(sb, "### [%s] %s (lines %d-%d)\n", sym.Kind, sym.Name, sym.StartLine, sym.EndLine)
		}
		if sym.DocComment != "" {
			fmt.Fprintf(sb, "Doc: %s\n", sym.DocComment)
		}
		if sym.Body != "" {
			fmt.Fprintf(sb, "```\n%s\n```\n\n", sym.Body)
		} else {
			sb.WriteString("\n")
		}
	}
}
