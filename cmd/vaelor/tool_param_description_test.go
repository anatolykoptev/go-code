package main

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
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

// TestAllRegisteredToolsHaveParamDescriptions is Guard A for issue #684: every
// property of EVERY registered tool must carry a non-empty description in the
// JSON schema generated from its input struct.
//
// The tool set is data-driven off registeredToolInputs — the slice addTool
// populates as a side effect of registerTools — NOT a hardcoded tool list, so
// this guard cannot rot the moment someone adds a tool. registerTools is run
// in-process with a zero Config (no DATABASE_URL / EMBED_URL / LLM_API_KEY):
// every optional subsystem no-ops, no goroutines are spawned, no os.Exit path
// is reachable, so the only durable side effect is tool registration.
//
// The schema is generated via jsonschema.ForType — the SAME path the MCP SDK
// uses internally (mcpserver.AddTool → jsonschema.ForType) — so this reflects
// what an agent sees in tools/list. A failure names the offending tool + param.
func TestAllRegisteredToolsHaveParamDescriptions(t *testing.T) {
	registeredToolInputs = nil // reset in case an earlier test registered tools
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	// Zero Config with the cheap URL/allowlist fields set so the four
	// config-gated tools (debug_investigate, resolve_frame, site_analyze,
	// site_crawl) register too. No DB/LLM/embed env → every optional subsystem
	// no-ops, no goroutines spawn, no os.Exit path is reachable; the only
	// durable side effect is tool registration. SourcemapRateLimit is left 0
	// so resolve_frame does not start its rate-limiter cleanup goroutine.
	registerTools(context.Background(), server, Config{
		PrometheusURL:         "http://127.0.0.1:1",
		JaegerURL:             "http://127.0.0.1:1",
		OxBrowserURL:          "http://127.0.0.1:1",
		SourcemapAllowedHosts: []string{"example.com"},
	}, nil)

	if len(registeredToolInputs) == 0 {
		t.Fatal("registerTools registered zero tools — registeredToolInputs is empty; the guard cannot run")
	}

	// dbGatedTools only register when a live DATABASE_URL (+ SPARSE_EMBED_URL /
	// AGE) is present, which a unit test cannot provide. They are covered here
	// directly via their input struct types so the guard still asserts their
	// param descriptions. Guard B (dead-tag scan) covers them too. A future
	// DB-gated tool that early-returns from its registerXxx when the store is
	// nil must be added to this list — this comment is the contract.
	dbGatedTools := []struct {
		name string
		in   reflect.Type
	}{
		{"code_graph", reflect.TypeFor[CodeGraphInput]()},
		{"find_duplicates", reflect.TypeFor[FindDuplicatesInput]()},
		{"orphan_sweep", reflect.TypeFor[OrphanSweepInput]()},
		{"list_flows", reflect.TypeFor[ListFlowsInput]()},
		{"sparse_backfill", reflect.TypeFor[SparseBackfillInput]()},
		{"remember_graph_insights", reflect.TypeFor[RememberGraphInsightsInput]()},
	}

	var (
		toolsSeen  int
		paramsSeen int
		missing    []string
	)
	check := func(name string, in reflect.Type) {
		toolsSeen++
		schema, err := jsonschema.ForType(in, &jsonschema.ForOptions{})
		if err != nil {
			t.Errorf("%s: jsonschema.ForType: %v", name, err)
			return
		}
		if schema.Properties == nil {
			return // no properties (e.g. a tool that takes no input)
		}
		for _, prop := range schema.PropertyOrder {
			ps := schema.Properties[prop]
			if ps == nil {
				missing = append(missing, name+"."+prop+" (nil schema)")
				continue
			}
			paramsSeen++
			if ps.Description == "" {
				missing = append(missing, name+"."+prop)
			}
		}
	}
	for _, rt := range registeredToolInputs {
		check(rt.Name, rt.In)
	}
	for _, rt := range dbGatedTools {
		check(rt.name, rt.in)
	}
	t.Logf("Guard A coverage: %d tools, %d params", toolsSeen, paramsSeen)
	if len(missing) > 0 {
		t.Errorf("params with empty/nil descriptions (%d):\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
}

// TestNoDeadJsonschemaDescriptionTag is Guard B for issue #684: the legacy
// description tag name (assembled below as needle) must not appear anywhere
// under cmd/ or internal/ (excluding vendor/). That tag name is the specific
// landmine: it looks correct but the vendored generator reads ONLY "jsonschema",
// so a contributor copying an old line gets a silent no-op. This test turns
// that into a red test instead.
//
// The needle is assembled at runtime ("jsonschema" + "_description") so the
// guard's own source file does not contain the contiguous literal and does
// not trip itself.
func TestNoDeadJsonschemaDescriptionTag(t *testing.T) {
	needle := "jsonschema" + "_description"

	// Resolve the repo root from this test file's location.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed — cannot locate repo root")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile))) // .../cmd/vaelor/<this> → repo root

	// Directories to scan, relative to repo root.
	scanDirs := []string{"cmd", "internal"}
	var hits []string
	for _, dir := range scanDirs {
		root := filepath.Join(repoRoot, dir)
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "vendor" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if strings.Contains(string(b), needle) {
				rel, _ := filepath.Rel(repoRoot, path)
				hits = append(hits, rel)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	sort.Strings(hits)
	if len(hits) > 0 {
		t.Errorf("dead tag %q found in %d file(s) — rename to %q:\n  %s",
			needle, len(hits), "jsonschema", strings.Join(hits, "\n  "))
	}
}
