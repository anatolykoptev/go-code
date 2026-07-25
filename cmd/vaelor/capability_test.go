package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/embeddings"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/prometheus/client_golang/prometheus"
)

// capabilityGaugeValue reads the value of gocode_capability_enabled for the
// given capability label from the default Prometheus registry. Returns false
// and ok=false when the labelled series does not exist (so tests can assert
// the pre-touch guarantee: every known capability has a series from boot).
func capabilityGaugeValue(t *testing.T, capability string) (val float64, ok bool) {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != "gocode_capability_enabled" {
			continue
		}
		for _, m := range mf.GetMetric() {
			var label string
			for _, l := range m.GetLabel() {
				if l.GetName() == "capability" {
					label = l.GetValue()
				}
			}
			if label == capability {
				return m.GetGauge().GetValue(), true
			}
		}
	}
	return 0, false
}

// --- Pre-touch: every known capability series exists from a cold start ------
//
// Falsification: remove a capability from knownCapabilities (or delete the
// init() pre-touch) and the series vanishes → ok=false → RED.

// expectedCapabilitySeries is the hardcoded set of capabilities that MUST
// have a pre-touched series from a cold start. It is intentionally SEPARATE
// from knownCapabilities so the pre-touch test is not tautological: if a
// capability is dropped from knownCapabilities, the series vanishes and this
// list (which still expects it) goes RED.
var expectedCapabilitySeries = []string{
	"semantic_search", "sparse_retrieval", "learnings_store",
	"github_app_auth", "code_graph", "find_duplicates",
	"sparse_backfill", "design_search", "resolve_frame",
	"dataflow_analyze", "web_search", "llm", "graph_signals",
}

func TestCapabilityGauge_PreTouchAllSeriesExist(t *testing.T) {
	// Not parallel: process-global gauge.
	for _, c := range expectedCapabilitySeries {
		t.Run(c, func(t *testing.T) {
			val, ok := capabilityGaugeValue(t, c)
			if !ok {
				t.Fatalf("gocode_capability_enabled{capability=%q} series missing — pre-touch failed", c)
			}
			if val != 0 {
				t.Errorf("gocode_capability_enabled{capability=%q} = %v, want 0 (pre-touched default)", c, val)
			}
		})
	}
}

// --- #602: sparse_retrieval enabled when SPARSE_EMBED_URL set ---------------
//
// Drives the REAL wiring path (wireSparse). Falsification: remove the
// setCapability("sparse_retrieval", true) call in wireSparse and the gauge
// stays 0 (pre-touch default) → RED.

func TestCapabilityGauge_SparseRetrievalEnabled(t *testing.T) {
	cfg := Config{SparseEmbedURL: "http://127.0.0.1:9999/embed_sparse", SparseEmbedModel: "splade-v3-distilbert", SparseEmbedMaxArray: 32}
	sc, _ := wireSparse(cfg, zeroRRFWeights())
	if sc == nil {
		t.Fatal("wireSparse with set URL should return non-nil embedder")
	}
	val, ok := capabilityGaugeValue(t, "sparse_retrieval")
	if !ok {
		t.Fatal("sparse_retrieval series missing")
	}
	if val != 1 {
		t.Errorf("gocode_capability_enabled{capability=\"sparse_retrieval\"} = %v, want 1 (enabled)", val)
	}
}

func TestCapabilityGauge_SparseRetrievalDisabled(t *testing.T) {
	cfg := Config{SparseEmbedURL: "", SparseEmbedModel: "splade-v3-distilbert", SparseEmbedMaxArray: 32}
	sc, _ := wireSparse(cfg, zeroRRFWeights())
	if sc != nil {
		t.Fatal("wireSparse with empty URL should return nil embedder")
	}
	val, ok := capabilityGaugeValue(t, "sparse_retrieval")
	if !ok {
		t.Fatal("sparse_retrieval series missing")
	}
	if val != 0 {
		t.Errorf("gocode_capability_enabled{capability=\"sparse_retrieval\"} = %v, want 0 (disabled)", val)
	}
}

// --- #603: github_app_auth enabled when all three fields valid --------------
//
// Drives the REAL wiring path (loadGithubAppConfig). Falsification: remove the
// setCapability("github_app_auth", ...) call in loadGithubAppConfig and the
// gauge stays 0 → RED.

func TestCapabilityGauge_GitHubAppAuthEnabled(t *testing.T) {
	validKey := filepath.Join(t.TempDir(), "key.pem")
	if err := os.WriteFile(validKey, []byte("dummy-pem"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VAELOR_GITHUB_APP_ID", "123")
	t.Setenv("VAELOR_GITHUB_APP_INSTALLATION_ID", "456")
	t.Setenv("VAELOR_GITHUB_APP_KEY_PATH", validKey)

	cfg := loadGithubAppConfig()
	if !cfg.IsConfigured() {
		t.Fatal("expected App auth configured with all three fields valid")
	}
	val, ok := capabilityGaugeValue(t, "github_app_auth")
	if !ok {
		t.Fatal("github_app_auth series missing")
	}
	if val != 1 {
		t.Errorf("gocode_capability_enabled{capability=\"github_app_auth\"} = %v, want 1 (enabled)", val)
	}
}

func TestCapabilityGauge_GitHubAppAuthDisabledPartial(t *testing.T) {
	// Partial config (APP_ID set, INSTALLATION_ID missing) → disabled, gauge 0.
	t.Setenv("VAELOR_GITHUB_APP_ID", "123")
	t.Setenv("VAELOR_GITHUB_APP_INSTALLATION_ID", "")
	t.Setenv("VAELOR_GITHUB_APP_KEY_PATH", "/nonexistent/key.pem")

	cfg := loadGithubAppConfig()
	if cfg.IsConfigured() {
		t.Fatal("expected App auth NOT configured on partial config")
	}
	val, ok := capabilityGaugeValue(t, "github_app_auth")
	if !ok {
		t.Fatal("github_app_auth series missing")
	}
	if val != 0 {
		t.Errorf("gocode_capability_enabled{capability=\"github_app_auth\"} = %v, want 0 (disabled)", val)
	}
}

// --- #601: learnings_store disabled when DSN unset --------------------------
//
// Drives the REAL wiring path (buildLearningsStore). The disabled path is the
// pre-touch default (0); the WARN is the falsifiable disabled-signal (asserted
// here alongside the gauge so the contract is one test).

func TestCapabilityGauge_LearningsStoreDisabled(t *testing.T) {
	th, restore := captureSlog(t)
	defer restore()

	cfg := Config{LearningsDSN: ""}
	store := buildLearningsStore(cfg)
	if store != nil {
		t.Fatal("expected nil store when LearningsDSN is empty")
	}
	val, ok := capabilityGaugeValue(t, "learnings_store")
	if !ok {
		t.Fatal("learnings_store series missing")
	}
	if val != 0 {
		t.Errorf("gocode_capability_enabled{capability=\"learnings_store\"} = %v, want 0 (disabled)", val)
	}
	if !warnContainsAttr(th.records, "learnings", "env_var", "LEARNINGS_DATABASE_URL") {
		t.Error("expected WARN about LEARNINGS_DATABASE_URL unset, got none")
	}
}

// --- #600: semantic_search disabled when EMBED_URL unset --------------------
//
// Drives the REAL wiring path (newSemanticDeps). Asserts gauge 0 + WARN +
// call-time honesty (the tool response carries a disabled marker naming the
// env var, not an empty result that reads as "nothing found").

func TestCapabilityGauge_SemanticSearchDisabled(t *testing.T) {
	th, restore := captureSlog(t)
	defer restore()

	cfg := Config{EmbedURL: ""}
	deps := newSemanticDeps(cfg, analyze.Deps{}, nil, nil, nil, embeddings.RRFWeights{})
	if deps.Client != nil {
		t.Error("expected nil Client when EMBED_URL unset")
	}
	val, ok := capabilityGaugeValue(t, "semantic_search")
	if !ok {
		t.Fatal("semantic_search series missing")
	}
	if val != 0 {
		t.Errorf("gocode_capability_enabled{capability=\"semantic_search\"} = %v, want 0 (disabled)", val)
	}
	if !warnContainsAttr(th.records, "semantic_search", "env_var", "EMBED_URL") {
		t.Error("expected WARN about EMBED_URL unset, got none")
	}
}

// TestCapabilityGauge_SemanticSearchCallTimeHonesty asserts that a disabled
// semantic_search returns a response carrying a "disabled" marker naming the
// env var — so a dead subsystem is distinguishable from an empty result.
// Falsification: revert the disabled-response in handleSemanticSearch to an
// empty result and the marker vanishes → RED.
func TestCapabilityGauge_SemanticSearchCallTimeHonesty(t *testing.T) {
	deps := SemanticDeps{} // all nil — semantic_search disabled
	res, err := handleSemanticSearch(context.Background(), SemanticSearchInput{
		Repo:  "owner/repo",
		Query: "jwt validation",
	}, deps, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError && len(res.Content) == 0 {
		t.Fatal("expected a non-empty response for disabled semantic_search, got empty")
	}
	var text string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text += tc.Text
		}
	}
	if !strings.Contains(strings.ToLower(text), "disabled") {
		t.Errorf("disabled semantic_search response must carry a 'disabled' marker; got: %s", text)
	}
	if !strings.Contains(text, "EMBED_URL") {
		t.Errorf("disabled semantic_search response must name EMBED_URL; got: %s", text)
	}
}
