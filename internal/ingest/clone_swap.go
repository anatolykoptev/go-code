package ingest

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// plainRename atomically moves tmpDest to finalDest via a single rename(2).
// Used when there is nothing to exchange — finalDest is absent (first clone,
// or a concurrent-removal race that wiped it between the caller's stat and
// the swap). rename(2) is atomic: finalDest is never observed absent.
//
// On failure tmpDest is cleaned up so no leaked .tmp.<ns> directory remains.
func plainRename(tmpDest, finalDest string) error {
	if err := os.Rename(tmpDest, finalDest); err != nil {
		_ = os.RemoveAll(tmpDest)
		return fmt.Errorf("atomic rename (first-clone fallback): %w", err)
	}
	return nil
}

// twoStepSwap replaces finalDest with tmpDest using a two-step rename when
// renameat2(RENAME_EXCHANGE) is unavailable (non-Linux platforms, or a
// Linux filesystem/overlay that lacks RENAME_EXCHANGE — ENOSYS/EINVAL).
//
// Sequence: rename(finalDest → stale) → rename(tmpDest → finalDest) → rm stale.
// If finalDest is absent the first rename is skipped (os.IsNotExist), so the
// first-clone case is handled too. There is a sub-microsecond window where
// finalDest is absent between the two renames; acceptable on dev platforms
// and on Linux only as a last-resort fallback (RENAME_EXCHANGE is preferred
// because it has no absence window).
func twoStepSwap(tmpDest, finalDest string) error {
	stale := finalDest + ".stale." + strconv.FormatInt(time.Now().UnixNano(), 36)
	if err := os.Rename(finalDest, stale); err != nil && !os.IsNotExist(err) {
		_ = os.RemoveAll(tmpDest)
		return fmt.Errorf("atomic swap (two-step): rename old: %w", err)
	}
	if err := os.Rename(tmpDest, finalDest); err != nil {
		// Attempt to restore the old directory before returning the error.
		_ = os.Rename(stale, finalDest)
		_ = os.RemoveAll(tmpDest)
		return fmt.Errorf("atomic swap (two-step): rename new into place: %w", err)
	}
	go func() { _ = os.RemoveAll(stale) }()
	return nil
}
