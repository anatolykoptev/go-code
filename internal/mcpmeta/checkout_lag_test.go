package mcpmeta

import (
	"os"
	"path/filepath"
	"testing"
)

// writeOriginMain plants a refs/remotes/origin/main loose ref, the same file
// `git fetch` would write.
func writeOriginMain(t *testing.T, dir, sha string) {
	t.Helper()
	refDir := filepath.Join(dir, ".git", "refs", "remotes", "origin")
	if err := os.MkdirAll(refDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(refDir, "main"), []byte(sha+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestOriginMainSHA_LooseRef(t *testing.T) {
	t.Parallel()
	dir := mkRepo(t)
	want := "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	writeOriginMain(t, dir, want)
	got, err := OriginMainSHA(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("loose ref: got %q, want %q", got, want)
	}
}

// Remote-tracking refs are commonly packed rather than loose; a reader that
// only handles loose files reports "no upstream" on most real checkouts.
func TestOriginMainSHA_PackedRefs(t *testing.T) {
	t.Parallel()
	dir := mkRepo(t)
	want := "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	if err := os.WriteFile(
		filepath.Join(dir, ".git", "packed-refs"),
		[]byte("# pack-refs with: peeled fully-peeled sorted\n"+want+" refs/remotes/origin/main\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	got, err := OriginMainSHA(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("packed-refs: got %q, want %q", got, want)
	}
}

// No origin remote is not "behind" — it has no upstream to be behind of.
func TestOriginMainSHA_NoRemoteIsNotAnError(t *testing.T) {
	t.Parallel()
	dir := mkRepo(t)
	got, err := OriginMainSHA(dir)
	if err != nil {
		t.Fatalf("absent origin must not error: %v", err)
	}
	if got != "" {
		t.Fatalf("absent origin must return empty, got %q", got)
	}
}

// The reason this exists: the index can be perfectly fresh against a checkout
// that itself has not been pulled. Staying silent here is the failure the
// caller cannot otherwise see.
func TestWithCheckoutLag_BehindOriginSpeaks(t *testing.T) {
	t.Parallel()
	dir := mkRepo(t)
	writeOriginMain(t, dir, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	env := WithCheckoutLag(Wrap(1, ""), dir)
	if env.CheckoutLag == "" {
		t.Fatal("checkout behind origin/main must populate checkout_lag")
	}
}

func TestWithCheckoutLag_UpToDateSilent(t *testing.T) {
	t.Parallel()
	dir := mkRepo(t)
	sha, err := MainBranchHeadSHA(dir)
	if err != nil {
		t.Fatal(err)
	}
	writeOriginMain(t, dir, sha)
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
