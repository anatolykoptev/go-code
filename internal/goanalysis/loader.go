package goanalysis

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/tools/go/packages"
)

const defaultTimeout = 10 * time.Minute

// LoadOpts configures package loading.
type LoadOpts struct {
	Patterns []string      // package patterns to load (default: "./...")
	Timeout  time.Duration // override default 60s timeout
}

// LoadResult contains loaded packages with full type information.
type LoadResult struct {
	Packages []*packages.Package
	Errors   []string // non-fatal errors
}

// HasGoModule checks if dir contains a go.mod file.
func HasGoModule(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "go.mod"))
	return err == nil
}

// LoadPackages loads Go packages from dir with full type info.
// Returns error if go.mod missing or context expires.

// goEnv returns env vars for go/packages.Load.
// Uses -mod=vendor when vendor/ dir exists (read-only mounts), else -mod=mod.
func goEnv(dir string) []string {
	flag := "-mod=mod"
	if _, err := os.Stat(filepath.Join(dir, "vendor")); err == nil {
		flag = "-mod=vendor"
	}
	return append(os.Environ(), "GOFLAGS="+flag, "GONOSUMCHECK=*", "GONOSUMDB=*", "GOCACHE=/tmp/go-build-cache", "GOPATH=/tmp/gopath", "GOWORK=off")
}

func LoadPackages(ctx context.Context, dir string, opts LoadOpts) (*LoadResult, error) {
	if !HasGoModule(dir) {
		return nil, fmt.Errorf("no go.mod found in %s", dir)
	}

	timeout := defaultTimeout
	if opts.Timeout > 0 {
		timeout = opts.Timeout
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	patterns := opts.Patterns
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}

	cfg := &packages.Config{
		Mode: packages.NeedName |
			packages.NeedTypes |
			packages.NeedSyntax |
			packages.NeedTypesInfo |
			packages.NeedImports |
			packages.NeedDeps,
		Dir:     dir,
		Context: ctx,
		Env:     goEnv(dir),
	}

	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, fmt.Errorf("packages.Load: %w", err)
	}

	// packages.Load materialises the ENTIRE types.Info for every package it
	// touches: NeedTypesInfo is all-or-nothing, so asking for the Defs, Uses
	// and Selections the resolver reads also builds Types, Implicits, Scopes,
	// Instances, InitOrder and FileVersions, which nothing here reads. Types
	// is the largest by far — one entry per expression in the graph.
	//
	// Release them before returning, or they stay reachable for as long as the
	// caller holds the LoadResult.
	releaseUnreadTypeInfo(pkgs)

	result := &LoadResult{}
	for _, pkg := range pkgs {
		for _, e := range pkg.Errors {
			result.Errors = append(result.Errors, e.Error())
		}
		if pkg.TypesInfo != nil {
			result.Packages = append(result.Packages, pkg)
		}
	}

	if len(result.Packages) == 0 {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("package loading timed out or cancelled: %w", ctx.Err())
		}
		return nil, fmt.Errorf("no packages with type information loaded from %s", dir)
	}

	return result, nil
}

// releaseUnreadTypeInfo drops the types.Info maps this repo never reads, across
// the whole package graph.
//
// The kept set is Defs, Uses and Selections — the three the resolver consumes
// (internal/goanalysis/resolver.go, resolver_dispatch.go). Everything else is
// a by-product of NeedTypesInfo being a single all-or-nothing Mode flag.
//
// It walks with packages.Visit rather than ranging over the roots, because
// NeedDeps gives every DEPENDENCY its own fully-populated types.Info too. The
// roots are a small fraction of the retained bytes; a roots-only loop looks
// like it works and frees almost nothing, which is why
// TestLoadPackages_ReleasesAcrossDependencyGraph asserts on an imported
// package rather than on the root.
//
// A dropped map reads as empty, not as a panic: indexing a nil Go map returns
// the zero value. So a future caller that starts reading Types would get a
// silent "no entry" for every expression rather than a crash — which is why
// the kept set is pinned by a test instead of left to a comment.
func releaseUnreadTypeInfo(pkgs []*packages.Package) {
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		if p.TypesInfo == nil {
			return
		}
		p.TypesInfo.Types = nil
		p.TypesInfo.Implicits = nil
		p.TypesInfo.Scopes = nil
		p.TypesInfo.Instances = nil
		p.TypesInfo.InitOrder = nil
		p.TypesInfo.FileVersions = nil
	})
}
