package mcpmeta

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// refSHA reads one fully-qualified ref (e.g. "refs/remotes/origin/main") from a
// repo's common dir, loose file first and packed-refs second. Returns "" when
// the ref does not exist or does not hold a 40-hex SHA.
//
// Remote-tracking refs are packed on most real checkouts, so the packed
// fallback is the common path rather than the exotic one.
func refSHA(commonDir, ref string) string {
	loose := filepath.Join(commonDir, filepath.FromSlash(ref))
	if data, err := os.ReadFile(loose); err == nil { //nolint:gosec
		sha := strings.TrimSpace(string(data))
		if len(sha) == 40 && isHex(sha) {
			return sha
		}
	}
	return packedRefSHA(commonDir, ref)
}

// mainBranchRefs resolves the repo's main branch ONCE and returns its name
// alongside the local tip and the tip of that SAME branch on origin.
//
// Resolving the name once is the whole point. Reading "the local main branch"
// and "the origin main branch" through two independent main-then-master
// searches lets them land on DIFFERENT branches: a repo migrated master → main
// that still carries a stale refs/remotes/origin/master would match local
// `main` and remote `master`, and comparing those two unrelated histories
// produces a confident, wrong staleness claim — the exact failure this whole
// signal exists to prevent.
//
// The local branch is what defines the pair: a branch with no local ref is not
// this repo's main branch no matter what origin carries. Returns ("", "", "",
// nil) when neither main nor master exists locally — unidentifiable, so callers
// stay silent. origin is "" when the repo has no matching remote-tracking ref,
// which means "no upstream", not "behind".
//
// Reads files only: no git subprocess, no network, no write. The remote ref is
// whatever the last fetch left on disk, which is precisely the question a
// staleness report asks.
func mainBranchRefs(repoRoot string) (branch, local, origin string, err error) {
	gd, err := gitDir(repoRoot)
	if err != nil {
		return "", "", "", err
	}
	cd := commonDir(gd)
	for _, b := range []string{"main", "master"} {
		localSHA := refSHA(cd, "refs/heads/"+b)
		if localSHA == "" {
			continue
		}
		return b, localSHA, refSHA(cd, "refs/remotes/origin/"+b), nil
	}
	return "", "", "", nil
}

// WithCheckoutLag annotates an envelope when the server-side checkout's main
// branch differs from the same branch on origin — that is, when the tree
// vaelor indexed is not what the forge currently holds.
//
// This is a DIFFERENT axis from WithFreshness, and both must be clean before an
// answer is current:
//
//   - WithFreshness asks "has the INDEX kept up with the CHECKOUT?"
//   - WithCheckoutLag asks "has the CHECKOUT kept up with ORIGIN?"
//
// An index built against a checkout nobody has pulled for a week is perfectly
// fresh by the first question and badly stale by the second, which is the gap
// this closes.
//
// It reports; it never updates. Pulling belongs to whatever syncs the checkout,
// which can hold safety properties (never switching branch, skipping dirty
// trees, respecting worktrees and index.lock) that a per-call `git pull` from a
// read-only mount could not.
//
// Silent when the two tips match, when either is unavailable, or when the repo
// has no origin remote. Note this reads the main-branch refs a second time
// after WithFreshness already read them; the duplicate cost is a few file
// stats, and sharing the read would couple two checks that are deliberately
// independent.
func WithCheckoutLag(env Envelope, repoRoot string) Envelope {
	branch, local, origin, err := mainBranchRefs(repoRoot)
	if err != nil {
		slog.Debug("mcpmeta.mainBranchRefs failed",
			"repo_root", repoRoot,
			"err", err,
		)
		return env
	}
	if local == "" || origin == "" || local == origin {
		return env
	}
	env.CheckoutLag = fmt.Sprintf(
		"server checkout %s is %s, origin/%s is %s -- the indexed tree is not the forge's current tip",
		branch, short(local), branch, short(origin),
	)
	env.CheckoutSHA = local
	env.OriginSHA = origin
	return env
}
