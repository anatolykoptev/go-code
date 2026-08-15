package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/forge"
	"github.com/anatolykoptev/vaelor/internal/ingest"
	"github.com/anatolykoptev/vaelor/internal/workspace"
)

// rewritePath applies path mappings, translating external prefixes to internal ones.
// Kept for callers in this package (tool_file_parse.go, tool_debug_investigate_body.go).
func rewritePath(path string, mappings []analyze.PathMapping) string {
	return workspace.RewritePath(path, mappings)
}

// makePathRewrite returns a func that applies mappings to a path, or nil when
// mappings is empty. The nil case signals callers that no rewriting is needed
// so they can skip worktree .git file parsing entirely.
func makePathRewrite(mappings []analyze.PathMapping) func(string) string {
	if len(mappings) == 0 {
		return nil
	}
	return func(path string) string {
		return workspace.RewritePath(path, mappings)
	}
}

// resolveRoot returns the local root path for the given repo input.
// For remote repos, it clones into the workspace (at the given ref) and returns
// the clone dir along with a cleanup function. For local paths, it validates the
// directory exists and returns a no-op cleanup.
//
// Input shape dispatch:
//   - WordPress plugin slug  → workspace.WPSource (download from wordpress.org)
//   - Remote owner/repo slug → workspace.RemoteSource (clone)
//   - Local path             → workspace.LocalSource (PATH_MAPPINGS applied)
func resolveRoot(ctx context.Context, repo, ref string, deps analyze.Deps) (root string, cleanup func(), err error) {
	// Strip common prefix mistakes from callers.
	repo = strings.TrimPrefix(repo, "path=")
	repo = strings.TrimPrefix(repo, "local:")

	var src workspace.Source
	outcome := resolveOutcomeAbsolute
	switch {
	case ingest.IsWordPressPlugin(repo):
		src = workspace.WPSource{Slug: repo, DestDir: deps.WorkspaceDir}
		outcome = resolveOutcomeWP

	case forge.IsRemote(repo):
		slug, ok := forge.ExtractSlug(repo)
		if !ok {
			repoResolveTotal.WithLabelValues(resolveOutcomeMiss).Inc()
			return "", nil, fmt.Errorf("invalid repo: cannot extract slug from %q", repo)
		}
		// Prefer a local checkout (e.g. /host/src/<name>) whose origin remote
		// matches the slug — the autoindexer already maintains it, and cloning a
		// private repo without a configured GitHub App yields an empty tree.
		if local := localCheckoutFor(ctx, slug, deps.LocalRepoDirs); local != "" {
			src = workspace.LocalSource{Path: local, Mappings: deps.PathMappings}
		} else {
			src = workspace.RemoteSource{
				Slug:        slug,
				RepoInput:   repo,
				Ref:         ref,
				DestDir:     deps.WorkspaceDir,
				TokenFunc:   deps.CloneTokenFunc,
				StaticToken: deps.GithubToken,
			}
		}
		outcome = resolveOutcomeRemote

	default:
		// Bare name (no slash → not a forge slug, not absolute → not a local
		// path). Before stat'ing it CWD-relative (which always misses inside the
		// container), resolve it against the indexed-repo registry: a checkout
		// whose basename matches under LocalRepoDirs (e.g. /host/src/<name>).
		// This is the path every subagent + the go-code cheatsheet exercises
		// with bare repo names like "acme-web".
		if local := bareNameCheckoutFor(repo, deps.LocalRepoDirs); local != "" {
			src = workspace.LocalSource{Path: local, Mappings: deps.PathMappings}
			outcome = resolveOutcomeBareRoot
		} else {
			// No registry match — fall back to the original CWD-relative stat so
			// callers that pass a real relative path still work and unknown bare
			// names error loudly. outcome stays hit_absolute on success; the
			// error path below re-labels a stat miss to "miss".
			src = workspace.LocalSource{Path: repo, Mappings: deps.PathMappings}
		}
	}

	root, cleanup, err = src.Root(ctx)
	if err != nil {
		repoResolveTotal.WithLabelValues(resolveOutcomeMiss).Inc()
		return root, cleanup, err
	}
	// Record the (requested, resolved) pair into the provenance slot on the
	// context. The addTool wrapper reads this after the handler returns to
	// attach the provenance footer centrally — after budget shaping, so it
	// survives truncation. Write-once: first writer wins, later calls dropped.
	recordProvenance(ctx, repo, root)
	repoResolveTotal.WithLabelValues(outcome).Inc()
	return root, cleanup, nil
}

// bareNameCheckoutFor resolves a bare repo basename (no slash, no scheme) to a
// checkout directory under one of dirs (e.g. /host/src/<name>). Unlike
// localCheckoutFor it does NOT require the checkout's origin remote to resolve
// to a forge slug — a bare local name carries no slug to compare against, so a
// directory match (with a .git entry to confirm it is a real checkout) is the
// correct signal. Returns "" when no match exists, leaving the caller to fall
// back to the original CWD-relative stat (preserving pre-fix error behavior).
func bareNameCheckoutFor(name string, dirs []string) string {
	if name == "" || len(dirs) == 0 {
		return ""
	}
	// Reject anything that is not a plain basename: a path separator or a
	// parent-traversal segment would let a caller escape the registry dirs.
	if strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return ""
	}
	for _, dir := range dirs {
		candidate := filepath.Join(dir, name)
		// Require a .git entry (dir or file, for worktrees) so we only match a
		// real checkout, not an arbitrary same-named directory.
		if _, err := os.Stat(filepath.Join(candidate, ".git")); err != nil {
			continue
		}
		return candidate
	}
	return ""
}

// localCheckoutFor returns the path to a local git checkout of slug under one of
// dirs (e.g. /host/src/<repo>) when one exists and its origin remote resolves to
// the same slug. Returns "" when none matches, signalling the caller to clone.
// Preferring a local checkout avoids a redundant clone — which yields an empty
// tree for private repos when no GitHub App is configured.
func localCheckoutFor(ctx context.Context, slug string, dirs []string) string {
	if len(dirs) == 0 {
		return ""
	}
	name := slug
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	if name == "" {
		return ""
	}
	for _, dir := range dirs {
		candidate := filepath.Join(dir, name)
		if _, err := os.Stat(filepath.Join(candidate, ".git")); err != nil {
			continue // not a git checkout
		}
		out, err := exec.CommandContext(ctx, "git", "-C", candidate, "remote", "get-url", "origin").Output()
		if err != nil {
			continue
		}
		if s, ok := forge.ExtractSlug(strings.TrimSpace(string(out))); ok && strings.EqualFold(s, slug) {
			return candidate
		}
	}
	return ""
}
