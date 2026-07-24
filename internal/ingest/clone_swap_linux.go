//go:build linux

package ingest

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// fallbackKind classifies the recovery action when renameat2(RENAME_EXCHANGE)
// fails. Extracted as a pure function so the routing is unit-falsifiable
// without having to make a real filesystem return ENOSYS/EINVAL.
type fallbackKind int

const (
	fallbackNone    fallbackKind = iota // unrecoverable; return the error
	fallbackPlain                       // finalDest absent → plain rename
	fallbackTwoStep                     // FS lacks RENAME_EXCHANGE, finalDest exists → two-step
)

// classifyRenameExchangeError maps a renameat2(RENAME_EXCHANGE) error to a
// fallback strategy:
//   - ENOENT: finalDest is absent (first clone, or a concurrent-removal race
//     wiped it between the caller's stat and the syscall) → plain rename.
//     There is nothing to exchange, and rename(2) is atomic.
//   - ENOSYS/EINVAL: the kernel/filesystem/overlay does not support
//     RENAME_EXCHANGE, but finalDest exists and is non-empty → two-step.
//     A plain rename would hit ENOTEMPTY over a non-empty directory.
//   - anything else: unrecoverable (e.g. EXDEV, EACCES) → return the error.
func classifyRenameExchangeError(err error) fallbackKind {
	switch {
	case errors.Is(err, unix.ENOENT):
		return fallbackPlain
	case errors.Is(err, unix.ENOSYS), errors.Is(err, unix.EINVAL):
		return fallbackTwoStep
	default:
		return fallbackNone
	}
}

// atomicDirectorySwap atomically replaces finalDest with tmpDest on Linux.
//
// Refresh case (finalDest exists): renameat2(RENAME_EXCHANGE) atomically
// exchanges the two directories in a single syscall — finalDest is never
// absent, and tmpDest ends up holding the OLD content (removed async).
//
// First-clone / race case (finalDest absent): a plain rename(2) is used —
// there is nothing to exchange, and rename is atomic so finalDest is still
// never absent. This covers the concurrent-removal race (issue #676) where
// finalDest existed at the caller's os.Stat but was wiped before the swap.
//
// If RENAME_EXCHANGE is unsupported by the filesystem/overlay (ENOSYS/EINVAL)
// but finalDest exists, fall back to the two-step rename (twoStepSwap).
//
// Both paths must be on the same filesystem (same as rename(2)).
func atomicDirectorySwap(tmpDest, finalDest string) error {
	if _, err := os.Stat(finalDest); err != nil {
		if !os.IsNotExist(err) {
			_ = os.RemoveAll(tmpDest)
			return fmt.Errorf("stat final dest: %w", err)
		}
		// finalDest absent — nothing to exchange; plain rename is atomic.
		return plainRename(tmpDest, finalDest)
	}

	// finalDest exists — atomic exchange.
	err := unix.Renameat2(unix.AT_FDCWD, tmpDest, unix.AT_FDCWD, finalDest, unix.RENAME_EXCHANGE)
	if err == nil {
		// tmpDest now holds the OLD content. Remove it asynchronously.
		go func() { _ = os.RemoveAll(tmpDest) }()
		return nil
	}

	switch classifyRenameExchangeError(err) {
	case fallbackPlain:
		// finalDest vanished between the stat above and the syscall.
		return plainRename(tmpDest, finalDest)
	case fallbackTwoStep:
		// FS/overlay lacks RENAME_EXCHANGE; finalDest exists & non-empty.
		return twoStepSwap(tmpDest, finalDest)
	default:
		_ = os.RemoveAll(tmpDest)
		return fmt.Errorf("atomic directory exchange (renameat2 RENAME_EXCHANGE): %w", err)
	}
}
