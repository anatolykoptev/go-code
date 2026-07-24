//go:build linux

package ingest

import (
	"testing"

	"golang.org/x/sys/unix"
)

// TestClassifyRenameExchangeError verifies the fallback routing for
// renameat2(RENAME_EXCHANGE) failures — the ENOSYS/EINVAL case cannot be
// triggered against a real Linux filesystem in a unit test, so the routing
// is exercised as a pure function (the seam used by atomicDirectorySwap).
//
// ENOENT (finalDest absent) → plain rename; ENOSYS/EINVAL (FS lacks
// RENAME_EXCHANGE) → two-step; anything else → no fallback (return error).
// Reverting the routing (e.g. mapping ENOENT to fallbackTwoStep) makes this RED.
func TestClassifyRenameExchangeError(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want fallbackKind
	}{
		{"ENOENT", unix.ENOENT, fallbackPlain},
		{"ENOSYS", unix.ENOSYS, fallbackTwoStep},
		{"EINVAL", unix.EINVAL, fallbackTwoStep},
		{"EPERM", unix.EPERM, fallbackNone},
		{"EXDEV", unix.EXDEV, fallbackNone},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := classifyRenameExchangeError(c.err); got != c.want {
				t.Fatalf("classifyRenameExchangeError(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}
