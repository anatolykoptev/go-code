BINARY  = bin/vaelor
SERVICE = vaelor
COMPOSE = cd $(HOME)/deploy/example-server && docker compose

# VERSION is embedded via -ldflags into the binary. Falls back to "dev" when
# .git is absent (e.g. Docker build with .git in .dockerignore) or no tags
# are reachable. The Dockerfile passes VERSION as a build arg from CI.
VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo dev)

# GIT_SHA is the commit SHA baked into gocode_build_info's git_sha label.
# Kept in sync with the Docker path (OXPULSE_GIT_SHA build arg → main.buildSHA)
# so `make build` and the container agree. Falls back to "unknown" when .git
# is absent; resolveBuildSHAFrom rejects that placeholder and drops to
# vcs.revision / version / "unknown" as appropriate.
GIT_SHA ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
LDFLAGS  = -s -w -X main.version=$(VERSION) -X main.buildSHA=$(GIT_SHA)

.PHONY: build lint fmt-check test test-short test-race govulncheck preflight run deploy clean vendor

build:
	GOWORK=off CGO_ENABLED=1 go build -mod=vendor -ldflags="$(LDFLAGS)" -o $(BINARY) ./cmd/vaelor

fmt-check:
	@out=$$(GOWORK=off gofmt -l cmd internal eval); \
	if [ -n "$$out" ]; then \
	  echo "gofmt drift detected:" >&2; \
	  echo "$$out" >&2; \
	  exit 1; \
	fi

lint: fmt-check
	GOWORK=off golangci-lint run ./...

test:
	GOWORK=off go test -mod=vendor -timeout 20m ./...

# test-short — the fast merge-gate variant. Skips heavy integration tests
# (real-repo git / co-change / snapshot analysis in cmd/go-code and
# internal/{compare,federate,embeddings}) via testing.Short(). Those still
# run IN FULL every night via `make test` (.github/workflows/nightly.yml) —
# a cadence split, NOT skip-to-green. Cuts the gate from ~26m to ~2m.
test-short:
	GOWORK=off go test -mod=vendor -short -timeout 20m ./...

# test-race — narrow race-detector target covering ONLY the packages that
# actually carry concurrency: internal/callgraph (background warmGoTypesCache
# goroutine mutating an LRU-cached *CallGraph that concurrent BuildFromRepo
# cache hits read) and internal/embeddings (sync IndexRepo single-flight +
# schema singleflight). NOT -race on ./... — the full suite carries a
# 20-minute timeout and -race costs 2-10× CPU / 5-10× memory, so a blanket
# -race would trade an invisible bug class for a timing-out gate, which is
# its own synthetic green (issue #743). Runs -short so embeddings' heavy
# non-concurrency integration tests skip while the single-flight tests
# (which do NOT gate on testing.Short) still run. Falsifiable: reintroduce
# an in-place mutation of a cache-shared pointer and
# TestWarmGoTypesCache_ZeroEdgePath_NoRaceWithConcurrentReader goes red
# with WARNING: DATA RACE.
test-race:
	GOWORK=off go test -mod=vendor -race -short -timeout 10m ./internal/callgraph/ ./internal/embeddings/

# govulncheck — dependency + toolchain vulnerability scan (plan ADR 7,
# plans/go-code/2026-06-30-frontend-parse-parity-react-svelte-astro.md Phase
# 0a): later phases vendor third-party tree-sitter grammars and the plan
# claims those deps are "OSV-scanned" — this target is what actually performs
# that scan (internal/freshness/vuln_check.go queries the same vuln.go.dev
# database for OTHER repos as a go-code PRODUCT feature; it is not go-code's
# own CI, which is why nothing self-scanned go-code until now).
#
# -scan package (not the default -scan symbol) skips govulncheck's call-graph
# reachability analysis — package-level import scanning is enough for a CI
# gate and stays fast/scoped on the 4-core ARM prod box (~1s locally); -scan
# module was tried first but rejects package patterns entirely and govulncheck
# still insists on loading "." for module metadata, which fails on this repo's
# layout (no .go files at module root, only under cmd/internal/eval) — a
# govulncheck limitation, not a go-code one.
#
# Installs a pinned binary into GOBIN if the runner doesn't already have one
# cached — first run on a fresh runner bootstraps itself via `go install`
# through the same module proxy every other dependency uses; subsequent runs
# reuse the cached binary.
GOVULNCHECK_VERSION = v1.4.0

govulncheck:
	@GOBIN=$$(GOWORK=off go env GOPATH)/bin; \
	if [ ! -x "$$GOBIN/govulncheck" ]; then \
	  echo "==> installing golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)"; \
	  GOWORK=off go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION); \
	fi; \
	echo "==> govulncheck -scan package ./..."; \
	GOWORK=off "$$GOBIN/govulncheck" -scan package ./...

# preflight — the merge gate: gofmt + vet + build + test + govulncheck, run
# from .github/workflows/preflight.yml on the self-hosted ci-runner
# runner (see CLAUDE.md ## CI). go-code persists three pgvector stores
# (internal/embeddings, internal/designmd, internal/learnings) plus an Apache
# AGE property graph (internal/codegraph) behind DATABASE_URL /
# PR_TEST_DATABASE_URL; every DB-gated test t.Skip's cleanly when its var is
# unset (plain local `make preflight` with no DB configured stays green),
# and runs live once the workflow provisions an ephemeral
# deploy-postgres-age:17 container and exports both vars. No separate
# migration step is needed — each store bootstraps its own schema/extension
# on first use (EnsureSchema / EnsureGraph).
preflight: fmt-check
	@echo "==> go vet ./..."
	GOWORK=off go vet -mod=vendor ./...
	@echo "==> go build ./..."
	GOWORK=off CGO_ENABLED=1 go build -mod=vendor -ldflags="$(LDFLAGS)" ./...
	@echo "==> go test -short ./... (heavy integration tests run nightly)"
	$(MAKE) test-short
	$(MAKE) govulncheck

run: build
	./$(BINARY)

deploy:
	$(COMPOSE) build --no-cache $(SERVICE)
	$(COMPOSE) up -d --no-deps --force-recreate $(SERVICE)
	@echo "Deployed and restarted $(SERVICE)"

vendor:
	GOWORK=off go mod vendor
	@# go mod vendor strips C headers needed by tree-sitter CGO. Restore from
	@# GOMODCACHE — `git checkout` fallback fails when headers are absent from
	@# HEAD (recurring footgun on every migration that didn't pre-restore).
	@MOD_CACHE=$$(GOWORK=off go env GOMODCACHE); \
	HDR_DIR=vendor/github.com/smacker/go-tree-sitter/php/tree_sitter; \
	mkdir -p $$HDR_DIR && \
	cp $$MOD_CACHE/github.com/smacker/go-tree-sitter@*/php/tree_sitter/*.h $$HDR_DIR/ && \
	chmod u+w $$HDR_DIR/*.h

clean:
	rm -f $(BINARY)
