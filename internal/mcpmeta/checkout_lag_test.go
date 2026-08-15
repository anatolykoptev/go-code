package mcpmeta

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeRef plants a loose ref, the same file git itself would write.
func writeRef(t *testing.T, dir, ref, sha string) {
	t.Helper()
	full := filepath.Join(dir, ".git", filepath.FromSlash(ref))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(sha+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

const (
	shaA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	shaB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestMainBranchRefs_LooseRefs(t *testing.T) {
	t.Parallel()
	dir := mkRepo(t)
	writeRef(t, dir, "refs/remotes/origin/main", shaB)
	branch, local, origin, err := mainBranchRefs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if branch != "main" {
		t.Fatalf("branch: got %q, want main", branch)
	}
	if len(local) != 40 {
		t.Fatalf("local: got %q, want a 40-hex sha", local)
	}
	if origin != shaB {
		t.Fatalf("origin: got %q, want %q", origin, shaB)
	}
}

// Remote-tracking refs are packed on most real checkouts; a reader that only
// handles loose files reports "no upstream" almost everywhere.
func TestMainBranchRefs_PackedRemoteRef(t *testing.T) {
	t.Parallel()
	dir := mkRepo(t)
	if err := os.WriteFile(
		filepath.Join(dir, ".git", "packed-refs"),
		[]byte("# pack-refs with: peeled fully-peeled sorted\n"+shaB+" refs/remotes/origin/main\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	_, _, origin, err := mainBranchRefs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if origin != shaB {
		t.Fatalf("packed remote ref: got %q, want %q", origin, shaB)
	}
}

// THE regression this pairing exists for. A repo migrated master -> main that
// still carries a stale refs/remotes/origin/master must NOT have its local main
// compared against that unrelated remote branch. Reading the two sides through
// independent main-then-master searches produces exactly that cross-branch
// comparison and reports a confident, wrong lag.
func TestMainBranchRefs_DoesNotCrossBranches(t *testing.T) {
	t.Parallel()
	dir := mkRepo(t) // creates refs/heads/main
	writeRef(t, dir, "refs/remotes/origin/master", shaB)

	branch, local, origin, err := mainBranchRefs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if branch != "main" {
		t.Fatalf("branch: got %q, want main (the branch that exists locally)", branch)
	}
	if origin != "" {
		t.Fatalf("origin/main is absent, so origin must be empty; got %q "+
			"(origin/master leaked across the branch boundary)", origin)
	}
	if local == origin {
		t.Fatal("local must not collapse to the cross-branch value")
	}
}

func TestWithCheckoutLag_CrossBranchStaysSilent(t *testing.T) {
	t.Parallel()
	dir := mkRepo(t)
	writeRef(t, dir, "refs/remotes/origin/master", shaB)
	env := WithCheckoutLag(Wrap(1, ""), dir)
	if env.CheckoutLag != "" {
		t.Fatalf("a stale origin/master must not produce a lag claim about main, got %q", env.CheckoutLag)
	}
}

// The reason this exists: the index can be perfectly fresh against a checkout
// that itself has not been pulled.
func TestWithCheckoutLag_BehindOriginSpeaks(t *testing.T) {
	t.Parallel()
	dir := mkRepo(t)
	writeRef(t, dir, "refs/remotes/origin/main", shaB)
	env := WithCheckoutLag(Wrap(1, ""), dir)
	if env.CheckoutLag == "" {
		t.Fatal("checkout behind origin/main must populate checkout_lag")
	}
	if env.OriginSHA != shaB {
		t.Fatalf("origin_sha: got %q, want %q", env.OriginSHA, shaB)
	}
	if len(env.CheckoutSHA) != 40 {
		t.Fatalf("checkout_sha: got %q, want a 40-hex sha", env.CheckoutSHA)
	}
}

// The message must name the branch it actually compared, not a hardcoded one.
func TestWithCheckoutLag_MessageNamesTheRealBranch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeRef(t, dir, "refs/heads/master", shaA)
	writeRef(t, dir, "refs/remotes/origin/master", shaB)
	if err := os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("ref: refs/heads/master\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := WithCheckoutLag(Wrap(1, ""), dir)
	if env.CheckoutLag == "" {
		t.Fatal("master-only repo behind origin must speak")
	}
	if !strings.Contains(env.CheckoutLag, "origin/master") {
		t.Fatalf("message must name the compared branch, got %q", env.CheckoutLag)
	}
}

func TestWithCheckoutLag_UpToDateSilent(t *testing.T) {
	t.Parallel()
	dir := mkRepo(t)
	_, local, _, err := mainBranchRefs(dir)
	if err != nil {
		t.Fatal(err)
	}
	writeRef(t, dir, "refs/remotes/origin/main", local)
	env := WithCheckoutLag(Wrap(1, ""), dir)
	if env.CheckoutLag != "" {
		t.Fatalf("checkout level with origin must be silent, got %q", env.CheckoutLag)
	}
}

func TestWithCheckoutLag_NoOriginSilent(t *testing.T) {
	t.Parallel()
	dir := mkRepo(t)
	env := WithCheckoutLag(Wrap(1, ""), dir)
	if env.CheckoutLag != "" {
		t.Fatalf("repo without origin must be silent, got %q", env.CheckoutLag)
	}
}

func TestWithCheckoutLag_NonRepoSilent(t *testing.T) {
	t.Parallel()
	env := WithCheckoutLag(Wrap(1, ""), t.TempDir())
	if env.CheckoutLag != "" {
		t.Fatalf("non-repo must be silent, got %q", env.CheckoutLag)
	}
}
