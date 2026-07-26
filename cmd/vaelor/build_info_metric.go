package main

import (
	"runtime/debug"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// buildInfoGitSHA is the git SHA reported by gocode_build_info. Populated once
// in init() from resolveBuildSHAFrom so it is set before any test or main
// startup.
var buildInfoGitSHA string

// buildSHA is the ldflags-injected git SHA for the Docker build path. Set via
// `-X main.buildSHA=<sha>` from the OXPULSE_GIT_SHA build arg (dozor injects
// it on every compose build). Empty for local `make build` / `go test`, where
// vcs.revision is available instead. Distinct from buildInfoGitSHA: init()
// overwrites buildInfoGitSHA, so injecting into it would be silently undone.
var buildSHA string

func init() {
	vcsRevision := ""
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" && s.Value != "" {
				vcsRevision = s.Value
				break
			}
		}
	}
	buildInfoGitSHA = resolveBuildSHAFrom(buildSHA, vcsRevision, version)
	// gocode_build_info exposes the running binary's git SHA as a Prometheus
	// gauge (value always 1). Labelled by git_sha for deploy provenance:
	// Grafana / alertmanager rules can correlate metric gaps with specific SHAs
	// without parsing dozor logs.
	//
	// Set once at startup; never changes during the process lifetime.
	promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "gocode_build_info",
		Help: "Always 1. Labels carry build provenance (git_sha). Set once at startup.",
	}, []string{"git_sha"}).WithLabelValues(buildInfoGitSHA).Set(1)
}

// resolveBuildSHAFrom picks the git SHA to report in gocode_build_info.
//
// Order: ldflags-injected SHA (Docker path) → vcs.revision (local go
// build/test) → release version var → "unknown".
//
// ldflags outranks vcs.revision because the two never compete in practice:
// in a Docker build .git is excluded (.dockerignore) so vcs.revision is empty,
// and in a local build the ldflags var is empty (Makefile leaves it to
// vcs.revision unless GIT_SHA is explicitly passed). Ranking ldflags first
// means the deploy provenance dozor injects is authoritative when present,
// and the local-build path is unchanged when it is absent.
func resolveBuildSHAFrom(ldflagsSHA, vcsRevision, version string) string {
	if ldflagsSHA != "" && ldflagsSHA != "unknown" {
		return ldflagsSHA
	}
	if vcsRevision != "" {
		return vcsRevision
	}
	if version != "" && version != "dev" {
		return version
	}
	return "unknown"
}
