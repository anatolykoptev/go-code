package main

import (
	"os"
	"path/filepath"
	"testing"
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
