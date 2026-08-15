package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/mcpmeta"
)

// mkLaggingRepo writes the minimum git plumbing both freshness readers need: a
// main branch and an origin/main that disagree. No git binary is spawned —
// the readers parse these files directly, so planting them is the whole setup.
func mkLaggingRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeLaggingGitDir(t, dir)
	return dir
}

// writeLaggingGitDir plants the lagging plumbing into an EXISTING directory so
// a caller can put real source files beside it.
func writeLaggingGitDir(t *testing.T, dir string) {
	t.Helper()
	gitDir := filepath.Join(dir, ".git")
	for _, sub := range []string{
		filepath.Join("refs", "heads"),
		filepath.Join("refs", "remotes", "origin"),
	} {
		if err := os.MkdirAll(filepath.Join(gitDir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(rel, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(gitDir, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("HEAD", "ref: refs/heads/main\n")
	write(filepath.Join("refs", "heads", "main"), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n")
	write(filepath.Join("refs", "remotes", "origin", "main"), "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n")
}

// The unit tests prove each annotation works in isolation; this proves the
// tools actually get them. Dropping either call from annotateEnv leaves every
// mcpmeta test green while every real response loses the signal — the
// wired-but-dark failure that #701 was filed about.
func TestAnnotateEnv_AttachesBothNewSignals(t *testing.T) {
	t.Parallel()
	root := mkLaggingRepo(t)

	env := annotateEnv(mcpmeta.Wrap(1, ""), "/Users/dev/Developer/acme", root, "")

	if env.SourcePath != root {
		t.Errorf("aliased request must carry the server root: got %q, want %q", env.SourcePath, root)
	}
	if env.CheckoutLag == "" {
		t.Error("checkout behind origin/main must carry checkout_lag")
	}
	if !env.HasSignal() {
		t.Error("an envelope carrying provenance must render a footer")
	}
}

// The quiet path must stay quiet: same path in and out, checkout level with
// origin, no index staleness — no footer at all.
func TestAnnotateEnv_QuietWhenNothingToReport(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git", "refs", "heads")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "main"), []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	env := annotateEnv(mcpmeta.Wrap(1, ""), dir, dir, "")

	if env.HasSignal() {
		t.Errorf("nothing to report must render no footer, got %+v", env)
	}
}
