//go:build !linux

package ingest

// atomicDirectorySwap replaces finalDest with tmpDest using a two-step rename.
// On non-Linux platforms we cannot use renameat2(RENAME_EXCHANGE), so we accept
// a brief window where finalDest is absent between the two renames. This
// fallback is only used on macOS/Windows dev environments; production runs on
// Linux.
//
// Delegates to twoStepSwap (clone_swap.go), which handles the finalDest-absent
// (first clone / concurrent-removal) case by skipping the first rename.
func atomicDirectorySwap(tmpDest, finalDest string) error {
	return twoStepSwap(tmpDest, finalDest)
}
