package main

import (
	"reflect"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

// TestBatchAParamDescriptions is the acceptance gate for issue #684 batch A:
// every exposed parameter of the 10 highest-arity tools must carry a non-empty
// description in the JSON schema generated from its input struct.
//
// The schema is generated via jsonschema.ForType — the SAME path the MCP SDK
// uses internally (modelcontextprotocol/go-sdk/mcp/server.go:setSchema) — so
// this test reflects what an agent sees in tools/list, not a hand-maintained
// copy. The param set is data-driven off the real schema Properties map; the
// only hardcoded part is the 10-tool scope (batch A).
func TestBatchAParamDescriptions(t *testing.T) {
	// Each entry is one of the 10 highest-arity tools (issue #684 batch A).
	// The reflect.Type is the tool's input struct — the schema source of truth.
	tools := []struct {
		name string
		in   reflect.Type
	}{
		{"code_search", reflect.TypeFor[CodeSearchInput]()},
		{"github_code_search", reflect.TypeFor[GithubCodeSearchInput]()},
		{"repo_analyze", reflect.TypeFor[RepoAnalyzeInput]()},
		{"code_research", reflect.TypeFor[CodeResearchInput]()},
		{"find_duplicates", reflect.TypeFor[FindDuplicatesInput]()},
		{"call_trace", reflect.TypeFor[CallTraceInput]()},
		{"dataflow_analyze", reflect.TypeFor[DataflowInput]()},
		{"debug_investigate", reflect.TypeFor[DebugInvestigateInput]()},
		{"dep_graph", reflect.TypeFor[DepGraphInput]()},
		{"rewrite", reflect.TypeFor[RewriteInput]()},
	}

	for _, tt := range tools {
		t.Run(tt.name, func(t *testing.T) {
			schema, err := jsonschema.ForType(tt.in, &jsonschema.ForOptions{})
			if err != nil {
				t.Fatalf("jsonschema.ForType(%s): %v", tt.name, err)
			}
			if schema.Properties == nil {
				t.Fatalf("%s: schema has no properties", tt.name)
			}
			if len(schema.Properties) == 0 {
				t.Fatalf("%s: schema has zero properties", tt.name)
			}
			for _, prop := range schema.PropertyOrder {
				ps := schema.Properties[prop]
				if ps == nil {
					t.Errorf("%s: property %q has nil schema", tt.name, prop)
					continue
				}
				if ps.Description == "" {
					t.Errorf("%s: property %q has empty description", tt.name, prop)
				}
			}
		})
	}
}

// TestCodeSearchRequiredMatchesHandler asserts that code_search's schema-level
// required[] matches what the handler actually enforces. The handler
// (handleCodeSearch → resolveOrInferRepo) tolerates a missing repo by inferring
// it from the path argument, so "repo" must NOT be schema-required. Similarly,
// "pattern" cannot be schema-required because "query" is an alias — the handler
// normalizes query→pattern (normalizeCodeSearchInput), so a call with only
// "query" (no "pattern") is valid and must not be rejected by schema validation.
func TestCodeSearchRequiredMatchesHandler(t *testing.T) {
	schema, err := jsonschema.ForType(reflect.TypeFor[CodeSearchInput](), &jsonschema.ForOptions{})
	if err != nil {
		t.Fatalf("jsonschema.ForType: %v", err)
	}
	for _, r := range schema.Required {
		if r == "repo" {
			t.Errorf("code_search: \"repo\" must not be schema-required — handler infers it from path (resolveOrInferRepo), schema-required would break inference")
		}
		if r == "pattern" {
			t.Errorf("code_search: \"pattern\" must not be schema-required — \"query\" is an alias (normalizeCodeSearchInput), schema-required would reject valid query-only calls")
		}
	}
}
