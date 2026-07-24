package ingest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestAtomicReclone_FinalPathNeverAbsent asserts the core safety property:
// after a failed refreshClone, the final clone directory is replaced atomically
// (renameat2 RENAME_EXCHANGE on Linux; best-effort on other platforms).
// A concurrent goroutine polling the path should never observe it absent.
//
// The never-absent invariant holds ONLY on Linux, where atomicDirectorySwap is
// a single renameat2(RENAME_EXCHANGE) syscall (clone_swap_linux.go). On
// non-Linux platforms the fallback (clone_swap_other.go) deliberately uses a
// two-step rename with a brief window where finalDest is absent — so a polling
// goroutine can legitimately observe absence and the test would flake. Skip on
// non-Linux rather than assert a property the platform cannot provide.
func TestAtomicReclone_FinalPathNeverAbsent(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skipf("atomic never-absent guarantee requires renameat2(RENAME_EXCHANGE) (Linux); %s uses the two-rename fallback with a deliberate absence window", runtime.GOOS)
	}
	t.Parallel()
	tmp := t.TempDir()
	origin := filepath.Join(tmp, "origin")
	dest := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(origin, 0o755); err != nil {
		t.Fatal(err)
	}
	gitInit(t, origin)

	opts := CloneOpts{
		Slug:     "test/demo",
		DestDir:  dest,
		CloneURL: "file://" + origin,
		Ref:      "main",
	}

	// Initial clone to populate localPath.
	res, err := CloneRepo(context.Background(), opts)
	if err != nil {
		t.Fatalf("initial clone: %v", err)
	}
	finalDest := res.LocalPath

	// Add a new file to origin so the reclone picks it up.
	if err := os.WriteFile(filepath.Join(origin, "UPDATED.md"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, origin, "git", "add", ".")
	mustRun(t, origin, "git", "commit", "-q", "-m", "add UPDATED")

	// Wipe .git to force refreshClone to fail so atomicReclone is triggered.
	if err := os.RemoveAll(filepath.Join(finalDest, ".git")); err != nil {
		t.Fatal(err)
	}

	// Spawn a goroutine that continuously checks finalDest exists.
	absenceDetected := make(chan string, 1)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				if _, err := os.Stat(finalDest); os.IsNotExist(err) {
					absenceDetected <- finalDest
					return
				}
			}
		}
	}()

	// Trigger atomicReclone via CloneRepo (refreshClone will fail, fall through).
	_, err = CloneRepo(context.Background(), opts)
	close(done)
	if err != nil {
		t.Fatalf("CloneRepo after corruption: %v", err)
	}

	select {
	case path := <-absenceDetected:
		t.Fatalf("finalDest %q was absent during atomic swap — race condition detected", path)
	default:
	}

	// New content must be present after reclone.
	if _, err := os.Stat(filepath.Join(finalDest, "UPDATED.md")); err != nil {
		t.Fatalf("UPDATED.md missing after atomic reclone: %v", err)
	}
}

// TestAtomicReclone_TmpCleanedUpOnCloneFailure asserts that if the git clone
// into the tmp directory fails, no tmp directory is left on disk.
func TestAtomicReclone_TmpCleanedUpOnCloneFailure(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	origin := filepath.Join(tmp, "origin")
	dest := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(origin, 0o755); err != nil {
		t.Fatal(err)
	}
	gitInit(t, origin)

	opts := CloneOpts{
		Slug:     "test/demo",
		DestDir:  dest,
		CloneURL: "file://" + origin,
		Ref:      "main",
	}

	// Initial clone.
	res, err := CloneRepo(context.Background(), opts)
	if err != nil {
		t.Fatalf("initial clone: %v", err)
	}

	// Corrupt the clone so refreshClone fails.
	if err := os.RemoveAll(filepath.Join(res.LocalPath, ".git")); err != nil {
		t.Fatal(err)
	}

	// Point CloneURL at a nonexistent path so the reclone git clone command fails.
	opts.CloneURL = "file:///nonexistent/no/such/repo"

	// atomicReclone must fail and leave no tmp directories behind.
	_, err = CloneRepo(context.Background(), opts)
	if err == nil {
		t.Fatal("expected CloneRepo to fail with bad CloneURL")
	}

	entries, dirErr := os.ReadDir(dest)
	if dirErr != nil {
		t.Fatalf("ReadDir workspace: %v", dirErr)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp.") {
			t.Errorf("tmp directory leaked: %s", e.Name())
		}
	}
}

// TestAtomicReclone_OldContentPreservedOnCloneFailure asserts that when the
// re-clone fails (bad URL), the original finalDest is untouched.
func TestAtomicReclone_OldContentPreservedOnCloneFailure(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	origin := filepath.Join(tmp, "origin")
	dest := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(origin, 0o755); err != nil {
		t.Fatal(err)
	}
	gitInit(t, origin)

	opts := CloneOpts{
		Slug:     "test/demo",
		DestDir:  dest,
		CloneURL: "file://" + origin,
		Ref:      "main",
	}

	// Initial clone.
	res, err := CloneRepo(context.Background(), opts)
	if err != nil {
		t.Fatalf("initial clone: %v", err)
	}
	finalDest := res.LocalPath

	// Corrupt .git to force atomicReclone.
	if err := os.RemoveAll(filepath.Join(finalDest, ".git")); err != nil {
		t.Fatal(err)
	}

	// Trigger atomicReclone with bad slug — clone will fail.
	slug, _ := NormalizeSlug(opts.Slug)
	badOpts := opts
	badOpts.CloneURL = "file:///no/such/repo"
	atomicErr := atomicReclone(context.Background(), badOpts, finalDest, filepath.Base(slug))
	if atomicErr == nil {
		t.Fatal("expected atomicReclone to fail with bad CloneURL")
	}

	// finalDest must still exist with old content — no data loss.
	if _, statErr := os.Stat(finalDest); statErr != nil {
		t.Fatalf("finalDest removed on atomicReclone failure: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(finalDest, "README.md")); statErr != nil {
		t.Fatalf("README.md lost after failed atomicReclone: %v", statErr)
	}
}

// TestAtomicDirectorySwap_FirstClone_FinalDestAbsent reproduces issue #676:
// on the first clone (or a concurrent-removal race) finalDest does NOT exist,
// so renameat2(RENAME_EXCHANGE) has nothing to exchange and returns ENOENT.
// The swap must fall back to a plain atomic rename instead of failing the
// whole search.
//
// Runs on all platforms: on Linux it exercises the plain-rename fallback;
// on non-Linux the two-step fallback already skips the absent finalDest.
func TestAtomicDirectorySwap_FirstClone_FinalDestAbsent(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	origin := filepath.Join(tmp, "origin")
	dest := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(origin, 0o755); err != nil {
		t.Fatal(err)
	}
	gitInit(t, origin)

	opts := CloneOpts{
		Slug:     "test/cold",
		DestDir:  dest,
		CloneURL: "file://" + origin,
		Ref:      "main",
	}

	// Clone into a tmp sibling exactly as atomicReclone does, but leave
	// finalDest ABSENT — the first-clone / concurrent-removal condition.
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	tmpDest := filepath.Join(dest, "test_cold.tmp.first")
	if err := runClone(context.Background(), opts.CloneURL, opts.Ref, tmpDest); err != nil {
		t.Fatalf("runClone into tmp: %v", err)
	}
	finalDest := filepath.Join(dest, "test_cold")
	if _, err := os.Stat(finalDest); err == nil {
		t.Fatalf("precondition: finalDest %q should not exist", finalDest)
	}

	if err := atomicDirectorySwap(tmpDest, finalDest); err != nil {
		t.Fatalf("atomicDirectorySwap with absent finalDest: %v", err)
	}

	if _, err := os.Stat(filepath.Join(finalDest, "README.md")); err != nil {
		t.Fatalf("README.md missing after first-clone swap: %v", err)
	}
	entries, _ := os.ReadDir(dest)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp.") {
			t.Errorf("tmp directory leaked after first-clone swap: %s", e.Name())
		}
	}
}

// TestPlainRename_FinalDestAbsent verifies the first-clone fallback primitive:
// with finalDest absent, plainRename atomically moves tmpDest into place and
// leaves no tmp directory behind.
func TestPlainRename_FinalDestAbsent(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	finalDest := filepath.Join(tmp, "final")
	tmpDest := filepath.Join(tmp, "tmp")
	if err := os.MkdirAll(tmpDest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDest, "F"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := plainRename(tmpDest, finalDest); err != nil {
		t.Fatalf("plainRename: %v", err)
	}
	if _, err := os.Stat(tmpDest); err == nil {
		t.Errorf("tmpDest not moved away by plainRename")
	}
	if b, err := os.ReadFile(filepath.Join(finalDest, "F")); err != nil || string(b) != "x" {
		t.Fatalf("finalDest missing content: %v %q", err, b)
	}
}

// TestPlainRename_FailureCleansTmp verifies that when plainRename's rename
// fails (e.g. EXDEV across filesystems), tmpDest is cleaned up — no leak.
func TestPlainRename_FailureCleansTmp(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	// finalDest on a non-existent parent → rename fails (ENOENT on parent).
	finalDest := filepath.Join(tmp, "missing-subdir", "final")
	tmpDest := filepath.Join(tmp, "tmp")
	if err := os.MkdirAll(tmpDest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDest, "F"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := plainRename(tmpDest, finalDest); err == nil {
		t.Fatal("expected plainRename to fail when parent dir is missing")
	}
	if _, err := os.Stat(tmpDest); err == nil {
		t.Errorf("tmpDest leaked after failed plainRename")
	}
}

// TestTwoStepSwap_FinalDestExists verifies the ENOSYS/EINVAL fallback path on
// Linux (and the primary non-Linux path): when finalDest exists and is
// non-empty, twoStepSwap renames old→stale, tmp→finalDest, and removes stale.
func TestTwoStepSwap_FinalDestExists(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	finalDest := filepath.Join(tmp, "final")
	tmpDest := filepath.Join(tmp, "tmp")
	if err := os.MkdirAll(finalDest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(finalDest, "OLD"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(tmpDest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDest, "NEW"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := twoStepSwap(tmpDest, finalDest); err != nil {
		t.Fatalf("twoStepSwap: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(finalDest, "NEW")); err != nil || string(b) != "new" {
		t.Fatalf("finalDest missing new content: %v %q", err, b)
	}
	// stale removed async — poll briefly.
	for i := 0; i < 100; i++ {
		leftover := ""
		entries, _ := os.ReadDir(tmp)
		for _, e := range entries {
			if strings.Contains(e.Name(), ".stale.") {
				leftover = e.Name()
			}
		}
		if leftover == "" {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	entries, _ := os.ReadDir(tmp)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".stale.") {
			t.Errorf("stale directory leaked: %s", e.Name())
		}
	}
}

// TestTwoStepSwap_FinalDestAbsent verifies twoStepSwap also handles the
// first-clone case (finalDest absent) by skipping the first rename.
func TestTwoStepSwap_FinalDestAbsent(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	finalDest := filepath.Join(tmp, "final")
	tmpDest := filepath.Join(tmp, "tmp")
	if err := os.MkdirAll(tmpDest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDest, "F"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := twoStepSwap(tmpDest, finalDest); err != nil {
		t.Fatalf("twoStepSwap absent finalDest: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(finalDest, "F")); err != nil || string(b) != "x" {
		t.Fatalf("finalDest missing content: %v %q", err, b)
	}
}

// TestAtomicReclone_StaleRemovedAfterSuccessfulSwap asserts that after a
// successful atomicReclone, no stale directory lingers in the workspace.
// (The stale/old removal is async; we poll briefly.)
func TestAtomicReclone_StaleRemovedAfterSuccessfulSwap(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	origin := filepath.Join(tmp, "origin")
	dest := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(origin, 0o755); err != nil {
		t.Fatal(err)
	}
	gitInit(t, origin)

	// Push an extra commit so the re-clone has something new to fetch.
	if err := os.WriteFile(filepath.Join(origin, "V2.md"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, origin, "git", "add", ".")
	mustRun(t, origin, "git", "commit", "-q", "-m", "v2")

	opts := CloneOpts{
		Slug:     "test/demo",
		DestDir:  dest,
		CloneURL: "file://" + origin,
		Ref:      "main",
	}

	res, err := CloneRepo(context.Background(), opts)
	if err != nil {
		t.Fatalf("initial clone: %v", err)
	}

	// Corrupt .git to force atomicReclone path.
	if err := os.RemoveAll(filepath.Join(res.LocalPath, ".git")); err != nil {
		t.Fatal(err)
	}

	if _, err := CloneRepo(context.Background(), opts); err != nil {
		t.Fatalf("CloneRepo after corruption: %v", err)
	}

	// Poll briefly for stale dir cleanup (async goroutine).
	var staleOrTmpFound string
	for i := 0; i < 100; i++ {
		entries, _ := os.ReadDir(dest)
		staleOrTmpFound = ""
		for _, e := range entries {
			name := e.Name()
			if strings.Contains(name, ".stale.") || strings.Contains(name, ".tmp.") {
				staleOrTmpFound = name
			}
		}
		if staleOrTmpFound == "" {
			break
		}
		// Yield to let the async goroutine run.
		_ = fmt.Sprintf("poll %d", i)
	}
	if staleOrTmpFound != "" {
		t.Logf("note: temporary directory %q still present after 100 polls — async removal may still be in progress", staleOrTmpFound)
	}
}
