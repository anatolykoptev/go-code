// cmd/go-code/tool_debug_investigate.go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/dozorclient"
	"github.com/anatolykoptev/vaelor/internal/gitutil"
	"github.com/anatolykoptev/vaelor/internal/investigate"
	"github.com/anatolykoptev/vaelor/internal/jaegerclient"
	"github.com/anatolykoptev/vaelor/internal/promclient"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// debugInvestigateTraceLimit caps the number of traces fetched per investigation.
const debugInvestigateTraceLimit = 20

// investigateTopN is the number of top hypotheses passed to body extraction
// and the LLM phase. Declared once to keep both call sites in sync.
const investigateTopN = 5

// Anomaly score buckets — highest to lowest.
const (
	scoreCritical      = 1.0 // ratio > 5x baseline
	scoreElevated      = 0.8 // ratio > 2x baseline
	scoreBaselineEmpty = 0.7 // baseline empty, window has errors
	scoreMild          = 0.6 // ratio > 1.2x baseline
	scoreDefault       = 0.5 // metric data missing or both queries failed
	scoreNominal       = 0.3 // ratio close to baseline (default healthy)
)

// Anomaly ratio thresholds — Prometheus window/baseline comparisons.
const (
	ratioCritical = 5.0
	ratioElevated = 2.0
	ratioMild     = 1.2
)

// Latency spike score buckets — used by computeLatencySpikes.
const (
	scoreLatencyCritical = 0.9 // p99 ratio > 2.0x baseline
	scoreLatencyElevated = 0.7 // p99 ratio > 1.5x baseline
)

// Latency ratio thresholds.
const (
	ratioLatencyCritical = 2.0
	ratioLatencyElevated = 1.5
)

// Saturation ratio thresholds.
const (
	ratioSatCritical = 5.0
	ratioSatElevated = 2.0
	ratioSatMild     = 1.3
)

// HintKind disambiguates investigation focus when caller knows the bug class.
// Empty value preserves current "auto-detect everything" behavior.
type HintKind string

const (
	HintKindAuto                  HintKind = ""
	HintKindFrontendReactiveCycle HintKind = "frontend_reactive_cycle"
	HintKindPanicAtHandler        HintKind = "panic_at_handler"
	HintKindMetricSpikeUnknown    HintKind = "metric_spike_unknown_source"
	HintKindLatencySpike          HintKind = "latency_spike"
)

// IsValid reports whether k is a known HintKind value.
func (k HintKind) IsValid() bool {
	switch k {
	case HintKindAuto, HintKindFrontendReactiveCycle, HintKindPanicAtHandler, HintKindMetricSpikeUnknown, HintKindLatencySpike:
		return true
	}
	return false
}

// DebugInvestigateInput is the user-facing tool input.
type DebugInvestigateInput struct {
	Service   string   `json:"service" jsonschema:"Service name as known to Jaeger (e.g. 'go-code', 'acme-web')."`
	StartUnix int64    `json:"start_unix" jsonschema:"Investigation window start, unix seconds. If 0, defaults to now-15m."`
	EndUnix   int64    `json:"end_unix" jsonschema:"Investigation window end, unix seconds. If 0, defaults to now."`
	Hint      string   `json:"hint,omitempty" jsonschema:"Optional free-text hint about the suspected behaviour."`
	HintKind  HintKind `json:"hint_kind,omitempty" jsonschema:"Optional structured hint kind: frontend_reactive_cycle | panic_at_handler | metric_spike_unknown_source | latency_spike. Empty = auto-detect (default)."`
	Repo      string   `json:"repo,omitempty" jsonschema:"Repo path for symbol lookup. Defaults to the service's resolved repo when known."`
	// Host, when set, enables Phase 7 (fleet_versions). Forms accepted:
	//   "local://"           - local docker socket (default if Host == "")
	//   "ssh://user@host[:port]" - via system ssh (requires GOCODE_FLEET_SSH_ENABLE=true)
	//
	// When unset AND no compose/Dockerfile is detected in the repo, Phase 7
	// is silently skipped. When unset AND a compose/Dockerfile IS detected,
	// Phase 7 records one Diagnostics.Warning explaining how to enable it,
	// then skips.
	Host string `json:"host,omitempty"  jsonschema:"Optional probe target for runtime-binary drift analysis (Phase 7). 'local://' for local docker, 'ssh://...' for remote (requires GOCODE_FLEET_SSH_ENABLE=true)."`
	// Hosts enables multi-host Phase 7. Supersedes Host when both are set.
	Hosts []string `json:"hosts,omitempty" jsonschema:"Multiple probe targets for Phase 7 fleet diff. Supersedes 'host' when set. Each is local://, docker://, or ssh://[user@]host[:port]."`
	// MaxBytes is the per-call response budget override (default 8192).
	MaxBytes int `json:"max_bytes,omitempty" jsonschema:"Response budget in bytes (default 8192). When the response exceeds this, the ranked head is returned with a continuation footer."`
}

// debugInvestigateStore is module-scoped — survives across calls in the same process.
var debugInvestigateStore = investigate.NewInvestigationStore()

func registerDebugInvestigate(server *mcp.Server, cfg Config, deps analyze.Deps) {
	if cfg.PrometheusURL == "" || cfg.JaegerURL == "" {
		slog.Warn("debug_investigate: not registering — PROMETHEUS_URL or JAEGER_URL empty")
		return
	}

	prom := promclient.NewClient(cfg.PrometheusURL, 30*time.Second)
	jaeger := jaegerclient.NewClient(cfg.JaegerURL, 30*time.Second)
	var dozor *dozorclient.Client
	if cfg.DozorURL != "" {
		dozor = dozorclient.NewClient(cfg.DozorURL, cfg.DozorAPIToken, 10*time.Second)
	}

	addTool(server, &mcp.Tool{
		Name:        "debug_investigate",
		Description: "Correlate Prometheus metrics + Jaeger failed traces + code symbols to suggest the likely buggy file:function for the given service+window. Long-running (5min budget); poll same input to fetch result.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DebugInvestigateInput) (*mcp.CallToolResult, error) {
		return handleDebugInvestigate(ctx, input, cfg, deps, prom, jaeger, dozor)
	})
}

func handleDebugInvestigate(ctx context.Context, input DebugInvestigateInput, cfg Config, deps analyze.Deps, prom *promclient.Client, jaeger *jaegerclient.Client, dozor *dozorclient.Client) (*mcp.CallToolResult, error) {

	if !input.HintKind.IsValid() {
		return errResult(fmt.Sprintf("invalid hint_kind %q; expected one of: frontend_reactive_cycle, panic_at_handler, metric_spike_unknown_source, latency_spike", input.HintKind)), nil
	}

	if input.Service == "" {
		return errResult("service is required"), nil
	}

	now := time.Now()
	start := time.Unix(input.StartUnix, 0)
	end := time.Unix(input.EndUnix, 0)
	if input.StartUnix == 0 {
		start = now.Add(-15 * time.Minute)
	}
	if input.EndUnix == 0 {
		end = now
	}
	if !end.After(start) {
		return errResult("end must be after start"), nil
	}

	// Lifecycle dedup. repo is part of the key so that re-running with a
	// corrected repo arg busts the cache instead of returning a stale result.
	st, fresh := debugInvestigateStore.Start(input.Service, start, end, input.Repo)
	if !fresh {
		switch st.Status() {
		case investigate.StatusRunning:
			return textResult(fmt.Sprintf("Investigation in progress for %q (started %s). Re-run this call in 5s to fetch the result.",
				input.Service, st.StartedAt().Format(time.RFC3339))), nil
		case investigate.StatusDone:
			return textResult(formatInvestigationResult(st.Result())), nil
		case investigate.StatusFailed:
			return errResult(fmt.Sprintf("Previous investigation failed: %s", st.Error())), nil
		default:
			return errResult(fmt.Sprintf("unknown status: %v", st.Status())), nil
		}
	}

	// Fresh — kick off background goroutine.
	go runInvestigation(input, cfg, deps, prom, jaeger, dozor, start, end)

	return textResult(fmt.Sprintf("Investigation started for service=%q range=[%s, %s]. Re-run this call in 5s to fetch the result.",
		input.Service, start.Format(time.RFC3339), end.Format(time.RFC3339))), nil
}

func runInvestigation(input DebugInvestigateInput, cfg Config, deps analyze.Deps, prom *promclient.Client, jaeger *jaegerclient.Client, dozor *dozorclient.Client, start, end time.Time) {
	finished := false
	defer func() {
		if r := recover(); r != nil {
			slog.Error("debug_investigate panic", "service", input.Service, "recover", r)
			debugInvestigateStore.Fail(input.Service, start, end, input.Repo, fmt.Sprintf("panic: %v", r))
			return
		}
		if !finished {
			// Covers any future early-return that misses Finish/Fail.
			debugInvestigateStore.Fail(input.Service, start, end, input.Repo, "investigation did not complete")
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// TODO(phase-beta): route to specialized data sources per hint_kind
	res := &investigate.InvestigationResult{
		Service:   input.Service,
		Range:     investigate.TimeRange{Start: start, End: end},
		StartedAt: time.Now(),
		HintKind:  string(input.HintKind),
	}

	// Phase 1 + 2: list Jaeger services and fetch failed traces.
	services, traces, err := runTracesPhase(ctx, jaeger, input, start, end, res)
	if err != nil {
		debugInvestigateStore.Fail(input.Service, start, end, input.Repo, err.Error())
		finished = true
		return
	}

	// γ.C.2: Retrieve historical incidents for this service before analysis begins.
	// Best-effort: errors append a warning and return empty slice.
	retrieveHistoricalIncidents(ctx, deps.Learnings, input.Service, input.Hint, res)

	// Phase 4 pre-fetch: fetch the full metric name list once. All discover*
	// functions are pure filters over this slice — avoids 3 redundant Prometheus
	// /api/v1/label/__name__/values round-trips in Phase 4. Also reused in Phase 5
	// (LLM system prompt ground truth). Non-fatal: empty slice falls through
	// gracefully; the warning is appended to diagnostics.
	metricNames, mnErr := prom.MetricNames(ctx)
	if mnErr != nil {
		res.Diagnostics.Warnings = append(res.Diagnostics.Warnings,
			fmt.Sprintf("metric names: %v", mnErr))
		// metricNames is nil — discover* return empty; legacy fallback runs.
	}
	res.Diagnostics.MetricsQueried++ // count the single __name__ fetch

	// Phase 4: query Prometheus for the error-rate ratio between the
	// investigation window and a baseline (same duration, 1h earlier).
	// The composite anomaly score weights metric-confirmed operations higher.
	anomalyScore, spikes := computeAnomalyScore(ctx, prom, input.Service, metricNames, start, end, &res.Diagnostics)

	// Phase 4.5: query Prometheus /api/v1/alerts for firing alerts.
	// Captures constant-state invariant violations that Phase 4 misses because
	// there is no delta when the metric has been broken continuously.
	alertSpikes, violations := runAlertsPhase(ctx, prom, input.Service, &res.Diagnostics)
	res.AlertViolations = violations

	// Phase 4.5: separate budgets — alerts are guaranteed surface-able regardless
	// of metric spike count, so they get their own top-K slot. Merging into a
	// single ranked list would let 5 alerts at score=1.0 displace all metric spikes,
	// masking the actual anomaly signal that Phase 3 symbol weighting relies on.
	//
	// anomalyScore drives Phase 3 symbol weighting. Keep it driven by metric
	// signals only (alerts have their own weight via AlertViolations + invariant
	// spikes). Fall back to top alert score only when no metric spikes exist.
	const (
		topAlertsK  = 3
		topMetricsK = 5
	)
	rankedAlerts := rankSpikes(alertSpikes, topAlertsK)
	rankedMetrics := rankSpikes(spikes, topMetricsK)
	res.MetricSpikes = append(rankedMetrics, rankedAlerts...)

	if len(rankedMetrics) > 0 {
		anomalyScore = rankedMetrics[0].Score
	} else if len(rankedAlerts) > 0 {
		anomalyScore = rankedAlerts[0].Score
	}

	// Phase 3: span → symbol correlation.
	ops, investigateCG := runSymbolsPhase(ctx, deps, input, traces, anomalyScore, res)

	// Phase γ.D: multi-signal fusion + recent diff embedding.
	// Best-effort: runs only when there are hypotheses to rank; gitutil calls
	// are timeout-bounded (CommitsSince=5s, FileDiffSince=10s) and return
	// empty on error so they never abort the investigation.
	if len(res.Hypotheses) > 0 {
		repoRoot := input.Repo
		var recentCommits map[string]int
		if repoRoot != "" {
			recentCommits = gitutil.CommitsSince(ctx, repoRoot, 30*24*time.Hour)
		}
		historicalSubjects := historicalSubjectsFromIncidents(res.HistoricalIncidents)
		// Sprint B2: walk upstream callers for top-3 span hypotheses before fusion rank.
		// Best-effort: investigateCG=nil for Tier-1/Tier-2 paths — no-op.
		res.Hypotheses = runUpstreamPhase(ctx, investigateCG, res.Hypotheses, 3, 2)
		// Sprint B4: walk callees of top-1 hypothesis before fusion rank.
		// Symmetric to B2 but walks DOWN: when the symptom site is a dispatcher
		// or event-loop, the buggy logic lives in something it calls.
		// Best-effort: investigateCG=nil for Tier-1/Tier-2 paths — no-op.
		// Note: if runUpstreamPhase displaced hyps[0] from its original Tier-3
		// hypothesis, runDownstreamPhase walks a non-Span hypothesis and the
		// Source != "" && Source != HypothesisSourceSpan guard makes it silently no-op.
		res.Hypotheses = runDownstreamPhase(ctx, investigateCG, res.Hypotheses, 2)
		res.Hypotheses = runFusionRank(res.Hypotheses, recentCommits, historicalSubjects)

		// Embed recent diff for top-1 hypothesis.
		if repoRoot != "" && len(res.Hypotheses) > 0 && res.Hypotheses[0].File != "" {
			diff := gitutil.FileDiffSince(ctx, repoRoot, res.Hypotheses[0].File, 30*24*time.Hour, 60)
			res.Hypotheses[0].RecentChange = recentChangeForHypothesis(res.Hypotheses[0].File, diff)
		}

		// Sprint B1/B5: populate BodySource for top-5 hypotheses.
		// Runs after FusionRank so we know the definitive ranking.
		// Bumped from top-3 to top-5 (Sprint B5) so downstream callee hypotheses
		// added by B4 also get BodySource when they rank in the top-5.
		// Files are read via host-side paths (Hypothesis.File is already reversed
		// to host by reverseToHost in Tier-1/Tier-3 symbol resolution). Inside the
		// container, host paths are accessible under /host via PATH_MAPPINGS mount.
		// rewritePath translates host → container for the disk read.
		res.Hypotheses = runBodyExtractionPhaseWithMappings(res.Hypotheses, investigateTopN, input.Service, input.Repo, deps.PathMappings, &res.Diagnostics)
	}

	// Phase 7: fleet versions — diff pinned image refs in repo against deployed
	// container images on the target host. Populates res.RuntimeVersions in
	// place. Soft failures append Diagnostics.Warnings; phase never aborts the
	// investigation.
	runFleetVersionsPhase(ctx, input, cfg, deps, res)

	// Phase 8: LLM correlate — produce one-paragraph summary + reasoning.
	// metricNames is passed to avoid re-fetching __name__ in the LLM phase.
	runLLMPhase(ctx, deps, metricNames, input, services, ops, start, end, res)

	// Phase 9: log excerpts — fetch recent ERROR/WARN/panic lines from the dozor
	// sidecar. Supplemental evidence; errors are non-fatal (appended to warnings).
	if dozor != nil {
		res.LogExcerpts = runLogsPhase(ctx, dozor, input, start, end, &res.Diagnostics)
	}

	res.FinishedAt = time.Now()

	// γ.C.1: Persist investigation outcome. Best-effort: nil store and errors
	// append a warning but do not abort. Must run before Finish so
	// LearningsPersisted is visible in the polled result.
	runHistoryPersist(ctx, deps.Learnings, input.Service, topAnomalyScore(res), res)

	debugInvestigateStore.Finish(input.Service, start, end, input.Repo, res)
	finished = true
}
