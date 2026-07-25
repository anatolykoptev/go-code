package mcpmeta

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// gitDir returns the absolute path to the real git directory for repoRoot.
//
// In a primary checkout, <repoRoot>/.git is a directory and is returned as-is.
// In a linked worktree created by "git worktree add", <repoRoot>/.git is a
// regular file whose single line is "gitdir: <path>" — absolute or relative to
// repoRoot. gitDir resolves <path> to an absolute path and returns it.
func gitDir(repoRoot string) (string, error) {
	gitPath := filepath.Join(repoRoot, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return "", fmt.Errorf("stat .git: %w", err)
	}
	if info.IsDir() {
		// Primary checkout — gitdir is the .git directory itself.
		return gitPath, nil
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf(".git is neither file nor directory")
	}

	// Linked worktree: .git is a pointer file "gitdir: <path>".
	data, err := os.ReadFile(gitPath)
	if err != nil {
		return "", fmt.Errorf("read .git pointer: %w", err)
	}
	line := strings.TrimSpace(string(data))
	const prefix = "gitdir: "
	if !strings.HasPrefix(line, prefix) {
		return "", fmt.Errorf(".git pointer: unexpected format %q", line)
	}
	gd := strings.TrimPrefix(line, prefix)
	if !filepath.IsAbs(gd) {
		gd = filepath.Join(repoRoot, gd)
	}
	return gd, nil
}

// commonDir returns the directory that holds shared refs (packed-refs, etc.)
// for the given gitdir. In a linked worktree, git writes a "commondir" file
// inside gitdir that points (one relative path) to the main repo's .git.
// If no commondir file is present, gitdir itself is the common dir.
func commonDir(gd string) string {
	cdPath := filepath.Join(gd, "commondir")
	data, err := os.ReadFile(cdPath)
	if err != nil {
		// No commondir — this is the main repo's .git or a standalone gitdir.
		return gd
	}
	rel := strings.TrimSpace(string(data))
	if filepath.IsAbs(rel) {
		return rel
	}
	return filepath.Join(gd, rel)
}

// packedRefSHA looks up a fully-qualified ref (e.g. "refs/heads/main") in
// <commonDir>/packed-refs, returning "" if absent or unparseable.
func packedRefSHA(commonDir, ref string) string {
	f, err := os.Open(filepath.Join(commonDir, "packed-refs")) //nolint:gosec
	if err != nil {
		return ""
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, "^") {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		if len(parts) == 2 && parts[1] == ref && len(parts[0]) == 40 && isHex(parts[0]) {
			return parts[0]
		}
	}
	return ""
}

// MainBranchHeadSHA returns the SHA of the repo's main branch
// (refs/heads/main → refs/heads/master → HEAD fallback), without spawning
// git. This mirrors the embed pipeline's repoMainBranchSHA fingerprint:
// the index is keyed on the main branch, so freshness must compare against
// main's tip, not the working-tree HEAD (which on a feature branch would
// never match the main-keyed index → permanent false stale warning).
//
// Returns ("", error) when the path is not a git repo or no ref resolves —
// callers treat this as "cannot confirm indexed" and fall through to the
// indexing path rather than declaring a genuine empty result (#709).
func MainBranchHeadSHA(repoRoot string) (string, error) {
	gd, err := gitDir(repoRoot)
	if err != nil {
		return "", err
	}
	cd := commonDir(gd)
	for _, branch := range []string{"main", "master"} {
		// Loose ref first.
		loose := filepath.Join(cd, "refs", "heads", branch)
		if data, err := os.ReadFile(loose); err == nil { //nolint:gosec
			sha := strings.TrimSpace(string(data))
			if len(sha) == 40 && isHex(sha) {
				return sha, nil
			}
		}
		// packed-refs fallback.
		if sha := packedRefSHA(cd, "refs/heads/"+branch); sha != "" {
			return sha, nil
		}
	}
	// No main/master — fall back to current HEAD (single-branch repo).
	return LiveHead(repoRoot)
}

// LiveHead returns the on-disk HEAD SHA for a git repo without spawning git.
//
// It handles both primary checkouts (where .git is a directory) and linked
// worktrees created by "git worktree add" (where .git is a file containing a
// "gitdir: <path>" pointer). The algorithm:
//
//  1. Resolve the real gitdir via gitDir (file or directory).
//  2. Read <gitdir>/HEAD — if detached, return the SHA directly.
//  3. If symbolic ("ref: refs/…"), look up the loose ref file under gitdir.
//  4. Fall back to packed-refs from commonDir(gitdir), since worktrees share
//     the main repo's packed refs via the commondir pointer.
//
// Returns "" + error if none of the above resolves.
func LiveHead(repoRoot string) (string, error) {
	gd, err := gitDir(repoRoot)
	if err != nil {
		return "", fmt.Errorf("resolve gitdir: %w", err)
	}

	headPath := filepath.Join(gd, "HEAD")
	headBytes, err := os.ReadFile(headPath)
	if err != nil {
		return "", fmt.Errorf("read HEAD: %w", err)
	}
	head := strings.TrimSpace(string(headBytes))

	if len(head) == 40 && isHex(head) {
		return head, nil
	}

	if !strings.HasPrefix(head, "ref: ") {
		return "", fmt.Errorf("unrecognised HEAD: %q", head)
	}
	ref := strings.TrimPrefix(head, "ref: ")

	// Loose ref lives under gitdir (worktree-specific refs are in the worktree's
	// gitdir, shared refs like heads/tags are under commonDir).
	loose := filepath.Join(gd, filepath.FromSlash(ref))
	if data, err := os.ReadFile(loose); err == nil {
		sha := strings.TrimSpace(string(data))
		if len(sha) == 40 && isHex(sha) {
			return sha, nil
		}
	}

	// Loose ref not found — also check under commonDir (shared refs).
	cd := commonDir(gd)
	if cd != gd {
		sharedLoose := filepath.Join(cd, filepath.FromSlash(ref))
		if data, err := os.ReadFile(sharedLoose); err == nil {
			sha := strings.TrimSpace(string(data))
			if len(sha) == 40 && isHex(sha) {
				return sha, nil
			}
		}
	}

	// packed-refs lives under commonDir (shared with main repo).
	if sha := packedRefSHA(cd, ref); sha != "" {
		return sha, nil
	}
	return "", fmt.Errorf("ref %q not in packed-refs", ref)
}

func isHex(s string) bool {
	for _, c := range s {
		if c >= '0' && c <= '9' || c >= 'a' && c <= 'f' {
			continue
		}
		return false
	}
	return true
}

// WithFreshness annotates an envelope with a staleness warning when the
// caller-supplied indexedSHA (the SHA the embed pipeline indexed, which is
// the MAIN-BRANCH tip) no longer matches the repo's current main-branch
// tip. Freshness means "has main advanced past the index?", NOT "does your
// working tree differ from the index" — the index is keyed on main, so
// comparing against the working-tree HEAD would false-warn on every feature
// branch. Silent on match.
func WithFreshness(env Envelope, repoRoot, indexedSHA string) Envelope {
	live, err := MainBranchHeadSHA(repoRoot)
	if err != nil {
		slog.Debug("mcpmeta.MainBranchHeadSHA failed",
			"repo_root", repoRoot,
			"err", err,
		)
		return env
	}
	if live == "" || indexedSHA == "" {
		return env
	}
	if live == indexedSHA {
		return env
	}
	env.StaleWarning = fmt.Sprintf(
		"index built against main %s, main is now %s -- live-read tools (code_search/symbol_search/understand) read working-tree files and are current; this marker advances only on a service re-index, NOT code_graph refresh=true (which rebuilds the AGE graph layer only and leaves this marker unchanged)",
		short(indexedSHA), short(live),
	)
	env.IndexedSHA = indexedSHA
	env.LiveSHA = live
	return env
}

func short(sha string) string {
	if len(sha) < 7 {
		return sha
	}
	return sha[:7]
}
