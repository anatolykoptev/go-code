package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/fleet"
	"github.com/anatolykoptev/vaelor/internal/fleet/docker"
	"github.com/anatolykoptev/vaelor/internal/fleet/ssh"
	"github.com/anatolykoptev/vaelor/internal/fleet/upstream"
	"github.com/anatolykoptev/vaelor/internal/polyglot/pinned"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// FleetVersionsInput is the input schema for the fleet_versions tool.
type FleetVersionsInput struct {
	Repo    string   `json:"repo,omitempty"    jsonschema:"Path or GitHub URL to the indexed repo. Pinned image versions are extracted from its Dockerfile and docker-compose*.yml files. Empty = runtime probe only."`
	Host    string   `json:"host,omitempty"    jsonschema:"Single probe target. Empty or 'local://' = local docker socket (default); 'ssh://[user@]host[:port]' = remote via system ssh (requires GOCODE_FLEET_SSH_ENABLE=true). Superseded by 'hosts' when both are set."`
	Hosts   []string `json:"hosts,omitempty"   jsonschema:"Multiple probe targets in one call. When present, supersedes 'host'. Each is local://, docker://, or ssh://[user@]host[:port]. Sibling-drift across hosts is reported in the sibling_drifts top-level field."`
	Service string   `json:"service,omitempty" jsonschema:"Optional filter: matches container name first, then com.docker.compose.service label. Pass empty to list all containers."`
}

// FleetVersionsOutput is the JSON-serialised response for fleet_versions.
type FleetVersionsOutput struct {
	Targets       []TargetReport          `json:"targets"`
	SiblingDrifts []fleet.SiblingDriftRow `json:"sibling_drifts,omitempty"`
	Warnings      []string                `json:"warnings,omitempty"`
}

// TargetReport is one probe target's result within FleetVersionsOutput.
type TargetReport struct {
	Target string            `json:"target"` // canonical target string as supplied by the caller
	Diffs  []fleet.ImageDiff `json:"diffs"`
	Error  string            `json:"error,omitempty"` // per-target soft-fail; probe errors land here, not at tool level
}

// TargetStr implements fleet.TargetReportLike.
func (r TargetReport) TargetStr() string { return r.Target }

// DiffsList implements fleet.TargetReportLike.
func (r TargetReport) DiffsList() []fleet.ImageDiff { return r.Diffs }

// overrideUpstreamBaseURL is a test seam that redirects the upstream GitHub client
// to a different base URL. Set by tests to point at httptest.Server.
// Empty string (default) uses the production GitHub API.
var overrideUpstreamBaseURL string

// buildFleetRegistry is the package-level seam for tests. Production code builds
// the registry from config; tests swap this var to inject fake probes.
//
// Always registers docker (canonical scheme) and ssh (driver-level gate via
// WithEnabled — returns ErrSSHDisabled immediately when FleetSSHEnable is false).
// This ensures callers see a descriptive error ("driver disabled; set
// GOCODE_FLEET_SSH_ENABLE=true") rather than a cryptic "no probe registered".
var buildFleetRegistry = func(cfg Config) *fleet.Registry {
	reg := fleet.NewRegistry()
	reg.Register(docker.New(
		docker.WithSocketPath(cfg.FleetDockerSocket),
		docker.WithTimeout(cfg.FleetTimeout),
	))
	reg.Register(ssh.New(
		ssh.WithEnabled(cfg.FleetSSHEnable),
		ssh.WithBinary(cfg.FleetSSHBinary),
		ssh.WithTimeout(cfg.FleetTimeout),
		ssh.WithSSHHome(cfg.FleetSSHHomeSrc, cfg.FleetSSHHomeDst),
	))
	return reg
}

// registerFleetVersions registers the fleet_versions MCP tool on server.
// The tool is always registered regardless of config; the handler degrades
// gracefully when no probes are reachable.
func registerFleetVersions(server *mcp.Server, cfg Config, deps analyze.Deps) {
	addTool(server, &mcp.Tool{
		Name: "fleet_versions",
		Description: "Compare pinned image versions in a repo (Dockerfile, docker-compose*.yml) " +
			"against images currently running on one or more hosts. " +
			"Default host = local docker socket — no configuration needed for the common case. " +
			"For remote hosts use 'ssh://[user@]host[:port]' (requires GOCODE_FLEET_SSH_ENABLE=true; " +
			"uses the system ssh binary so ~/.ssh/config is the single source of truth for ProxyJump, " +
			"identity, port, and known_hosts). " +
			"Multi-host: pass 'hosts' array to probe multiple targets in one call; cross-host " +
			"sibling-drift (same image running different tags) is reported in 'sibling_drifts'. " +
			"Service filter: container name is checked first, then com.docker.compose.service label. " +
			"Pass the most specific identifier available (container name preferred). " +
			"Soft failures (unreachable host, missing repo) appear in the JSON as target-level errors, " +
			"not tool-level errors, so partial results are still returned.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input FleetVersionsInput) (*mcp.CallToolResult, error) {
		return fleetVersionsHandler(ctx, cfg, deps, input)
	})
}

// fleetVersionsHandler is the testable core of the fleet_versions handler.
// Extracted so tests can call it directly with injected cfg/deps.
func fleetVersionsHandler(ctx context.Context, cfg Config, deps analyze.Deps, input FleetVersionsInput) (*mcp.CallToolResult, error) {
	// 1. Validate service filter early (fast-fail; drivers do their own check too).
	if !fleet.IsValidFilter(input.Service) {
		return errResult(fmt.Sprintf("invalid service filter: service name %q contains invalid characters", input.Service)), nil
	}

	// 2. Resolve effective hosts list.
	//    Priority: Hosts > Host > []string{"local://"}.
	var warnings []string
	var effectiveHosts []string
	if len(input.Hosts) > 0 {
		if input.Host != "" {
			warnings = append(warnings, "'host' is ignored when 'hosts' is set; using 'hosts' list")
		}
		effectiveHosts = input.Hosts
	} else if input.Host != "" {
		effectiveHosts = []string{input.Host}
	} else {
		effectiveHosts = []string{"local://"}
	}

	// 3. Validate all host strings up-front (fast-fail before any I/O).
	type parsedHost struct {
		raw    string
		target fleet.Target
	}
	parsedHosts := make([]parsedHost, 0, len(effectiveHosts))
	for _, h := range effectiveHosts {
		t, err := fleet.ParseTarget(h)
		if err != nil {
			return errResult(fmt.Sprintf("invalid host %q: %s", h, err)), nil
		}
		// Option A alias: "local" → "docker" for registry dispatch.
		// Target.Raw is preserved for the report's Target field.
		if t.Scheme == "local" {
			t.Scheme = "docker"
		}
		parsedHosts = append(parsedHosts, parsedHost{raw: h, target: t})
	}

	// 4. Resolve repo and collect pinned images ONCE (shared across all hosts).
	var pinnedImgs []pinned.PinnedImage
	var repoWarn string
	if input.Repo != "" {
		root, cleanup, resolveErr := resolveRoot(ctx, input.Repo, "", deps)
		if resolveErr != nil {
			// Warn but continue — probes still run against empty pinned set.
			repoWarn = fmt.Sprintf("repo resolve: %s", resolveErr)
		} else {
			defer cleanup()
			pinnedImgs, _ = pinned.Collect(root) // best-effort; ignore error per spec
		}
	}
	if repoWarn != "" {
		warnings = append(warnings, repoWarn)
	}

	// 5. Build (or get injected) registry — built ONCE for all hosts.
	reg := buildFleetRegistry(cfg)

	// 6. Probe all hosts in parallel. Per-host failures go into TargetReport.Error.
	reports := make([]TargetReport, len(parsedHosts))
	var wg sync.WaitGroup
	wg.Add(len(parsedHosts))
	for i, ph := range parsedHosts {
		i, ph := i, ph // capture for goroutine
		go func() {
			defer wg.Done()
			report := probeOneHost(ctx, ph.raw, ph.target, pinnedImgs, input.Service, reg)
			reports[i] = report
		}()
	}
	wg.Wait()

	// 6b. Upstream changelog enrichment: populate Changelog on TagDrift rows.
	// Skipped when FleetUpstreamDisable=true or no GITHUB_TOKEN configured.
	if !cfg.FleetUpstreamDisable && cfg.GithubToken != "" {
		upstreamOpts := []upstream.Option{
			upstream.WithToken(cfg.GithubToken),
			upstream.WithTimeout(8 * time.Second),
		}
		if overrideUpstreamBaseURL != "" {
			upstreamOpts = append(upstreamOpts, upstream.WithBaseURL(overrideUpstreamBaseURL))
		}
		upstreamClient := upstream.New(upstreamOpts...)
		for i := range reports {
			reports[i].Diffs = upstream.Enrich(ctx, upstreamClient, reports[i].Diffs, 30)
		}
	}

	// 7. Compute sibling drift across all probed targets (nil when < 2 targets).
	var siblingDrifts []fleet.SiblingDriftRow
	if len(reports) >= 2 {
		likes := make([]fleet.TargetReportLike, len(reports))
		for i := range reports {
			likes[i] = reports[i]
		}
		siblingDrifts = fleet.SiblingDiff(likes)
	}

	// 8. Warn when the whole call returns nothing useful.
	if len(pinnedImgs) == 0 && allEmpty(reports) && len(warnings) == 0 {
		warnings = append(warnings,
			"no pinned images parsed and no runtime containers found; check that the repo contains a Dockerfile or docker-compose*.yml and that the docker socket is accessible")
	}

	out := FleetVersionsOutput{
		Targets:       reports,
		SiblingDrifts: siblingDrifts,
		Warnings:      warnings,
	}

	return jsonMarshalResult(out), nil
}

// probeOneHost runs the probe for a single host and returns its TargetReport.
// All errors are soft (per-target) — this function never returns an error itself.
func probeOneHost(
	ctx context.Context,
	hostRaw string,
	t fleet.Target,
	pinnedImgs []pinned.PinnedImage,
	service string,
	reg *fleet.Registry,
) TargetReport {
	probe, probeErr := reg.Get(t)

	var runtimeImgs []fleet.RuntimeImage
	var targetErr string

	if probeErr != nil {
		targetErr = probeErr.Error()
	} else {
		var err error
		runtimeImgs, err = probe.List(ctx, t, fleet.Filter{Service: service})
		if err != nil {
			targetErr = err.Error()
		}
	}

	// If probe failed, runtimeImgs stays nil → Diff produces OnlySource rows.
	diffs := fleet.Diff(pinnedImgs, runtimeImgs)

	return TargetReport{
		Target: hostRaw,
		Diffs:  diffs,
		Error:  targetErr,
	}
}

// allEmpty reports whether all TargetReports have empty Diffs and no Error.
func allEmpty(reports []TargetReport) bool {
	for _, r := range reports {
		if len(r.Diffs) > 0 || r.Error != "" {
			return false
		}
	}
	return true
}
