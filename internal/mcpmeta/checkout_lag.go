package mcpmeta

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// OriginMainSHA returns the SHA of the repo's origin/main remote-tracking ref
// (refs/remotes/origin/main, then .../master), without spawning git and
// without touching the network.
//
// The remote-tracking ref is an ordinary local file that `git fetch` maintains;
// on this fleet an hourly ff-only sync unit refreshes it. Reading it answers
// "where was origin when we last fetched", which is exactly what a staleness
// report needs. Fetching here instead would be both impossible and wrong: the
// source tree is mounted read-only, and a write to .git on every tool call
// would race the host's syncer.
//
// Returns ("", nil) when the repo has no origin remote — a checkout that was
// never cloned from anywhere is not "behind", it simply has no upstream.
// Returns ("", err) only when repoRoot is not a git repository at all.
func OriginMainSHA(repoRoot string) (string, error) {
	gd, err := gitDir(repoRoot)
	if err != nil {
		return "", err
	}
	cd := commonDir(gd)
	for _, branch := range []string{"main", "master"} {
		ref := "refs/remotes/origin/" + branch
		// Loose ref first.
		loose := filepath.Join(cd, filepath.FromSlash(ref))
		if data, err := os.ReadFile(loose); err == nil { //nolint:gosec
			sha := strings.TrimSpace(string(data))
			if len(sha) == 40 && isHex(sha) {
				return sha, nil
			}
		}
		// packed-refs fallback — remote-tracking refs are commonly packed.
		if sha := packedRefSHA(cd, ref); sha != "" {
			return sha, nil
		}
	}
	return "", nil
}

// WithCheckoutLag annotates an envelope when the server-side checkout's main
// branch differs from its origin/main remote-tracking ref — that is, when the
// tree vaelor indexed is not what the forge currently holds.
//
// This is a DIFFERENT axis from WithFreshness, and both must be green before
// an answer is current:
//
//   - WithFreshness asks "has the INDEX kept up with the CHECKOUT?"
//   - WithCheckoutLag asks "has the CHECKOUT kept up with ORIGIN?"
//
// An index built against a checkout nobody has pulled for a week is perfectly
// fresh by the first question and badly stale by the second, which is the gap
// this closes.
//
// It reports only. Updating the checkout belongs to the host's ff-only sync
// unit, which holds a safety contract (never switches branch, skips dirty
// trees, respects worktrees and index.lock) that a per-call `git pull` from
// inside the container would not — and could not, the mount being read-only.
//
// Silent when the refs match, when either is unavailable, or when the repo has
// no origin remote.
func WithCheckoutLag(env Envelope, repoRoot string) Envelope {
	local, err := MainBranchHeadSHA(repoRoot)
	if err != nil {
		slog.Debug("mcpmeta.MainBranchHeadSHA failed",
			"repo_root", repoRoot,
			"err", err,
		)
		return env
	}
	origin, err := OriginMainSHA(repoRoot)
	if err != nil {
		slog.Debug("mcpmeta.OriginMainSHA failed",
			"repo_root", repoRoot,
			"err", err,
		)
		return env
	}
	if local == "" || origin == "" || local == origin {
		return env
	}
	env.CheckoutLag = fmt.Sprintf(
		"server checkout main is %s, origin/main is %s -- the indexed tree is not the forge's current tip; the host ff-syncs hourly",
		short(local), short(origin),
	)
	return env
}
