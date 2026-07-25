package mcpmeta

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// run is a local test helper that executes a git command in dir and fatals on error.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func mkRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "t@t.t"},
		{"config", "user.name", "t"},
		{"commit", "--allow-empty", "-m", "x"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func TestLiveHead_DirectRef(t *testing.T) {
	t.Parallel()
	dir := mkRepo(t)
	sha, err := LiveHead(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(sha) != 40 {
		t.Fatalf("sha: got %q (len=%d), want 40-hex", sha, len(sha))
	}
}

func TestLiveHead_PackedRefs(t *testing.T) {
	t.Parallel()
	dir := mkRepo(t)
	refPath := filepath.Join(dir, ".git", "refs", "heads", "main")
	loose, err := os.ReadFile(refPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(dir, ".git", "packed-refs"),
		[]byte("# pack-refs with: peeled fully-peeled sorted\n"+string(loose)[:40]+" refs/heads/main\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(refPath); err != nil {
		t.Fatal(err)
	}
	sha, err := LiveHead(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(sha) != 40 {
		t.Fatalf("packed-refs path: got %q", sha)
	}
}

func TestWithFreshness_MatchSilent(t *testing.T) {
	t.Parallel()
	dir := mkRepo(t)
	sha, _ := LiveHead(dir)
	env := WithFreshness(Wrap(1, ""), dir, sha)
	if env.StaleWarning != "" {
		t.Fatalf("match must be silent, got: %q", env.StaleWarning)
	}
	if env.IndexedSHA != "" || env.LiveSHA != "" {
		t.Fatalf("match must not surface SHAs")
	}
}

func TestWithFreshness_MismatchSpeaks(t *testing.T) {
	t.Parallel()
	dir := mkRepo(t)
	env := WithFreshness(Wrap(1, ""), dir, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	if env.StaleWarning == "" {
		t.Fatalf("mismatch must populate stale_warning")
	}
	if env.IndexedSHA == "" || env.LiveSHA == "" {
		t.Fatalf("mismatch must surface both SHAs")
	}
}

func TestWithFreshness_EmptyIndexedSilent(t *testing.T) {
	t.Parallel()
	dir := mkRepo(t)
	env := WithFreshness(Wrap(1, ""), dir, "")
	if env.StaleWarning != "" {
		t.Fatalf("empty indexedSHA must be silent, got %q", env.StaleWarning)
	}
	if env.IndexedSHA != "" || env.LiveSHA != "" {
		t.Fatalf("empty indexedSHA must not surface SHAs")
	}
}

// TestWithFreshness_FeatureBranchSilentWhenMainMatches guards the bug where
// comparing against the working-tree HEAD (not main) produced permanent false
// warnings on feature branches. Here the index is current with main, the
// worktree is on a feature commit ahead of main, and freshness must stay
// SILENT (main hasn't moved past the index).
//
// RED reasoning: pre-fix, WithFreshness called LiveHead which returns the
// feature commit SHA. That != mainSHA, so StaleWarning was populated —
// a false alarm on every feature-branch checkout. Post-fix, MainBranchHeadSHA
// returns the main-branch tip == indexedSHA → silent.
func TestWithFreshness_FeatureBranchSilentWhenMainMatches(t *testing.T) {
	t.Parallel()
	dir := mkRepo(t) // one commit on main
	mainSHA, err := MainBranchHeadSHA(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Move to a feature branch with a new commit AHEAD of main.
	runGit(t, dir, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "new.txt")
	runGit(t, dir, "commit", "-m", "feature work")

	// Index is current with main (indexedSHA == mainSHA). On the feature
	// branch, HEAD != main, but main itself hasn't moved → must be SILENT.
	env := WithFreshness(Wrap(1, ""), dir, mainSHA)
	if env.StaleWarning != "" {
		t.Fatalf("feature-branch checkout with current main-index must be silent, got: %q", env.StaleWarning)
	}
}

func TestLiveHead_LinkedWorktree(t *testing.T) {
	t.Parallel()
	// Create a primary repo, then add a linked worktree, then verify
	// LiveHead resolves the worktree's HEAD by following the gitdir
	// pointer file.
	primary := mkRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	cmd := exec.Command("git", "-C", primary, "worktree", "add", "-b", "feature", wt)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	sha, err := LiveHead(wt)
	if err != nil {
		t.Fatalf("LiveHead on worktree: %v", err)
	}
	if len(sha) != 40 {
		t.Fatalf("LiveHead worktree SHA: got %q (len=%d), want 40-hex", sha, len(sha))
	}
}
