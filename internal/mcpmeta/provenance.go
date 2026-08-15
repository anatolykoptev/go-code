package mcpmeta

import "path/filepath"

// WithSourcePath annotates an envelope with the server-side repository root
// the answer was actually computed from, but only when the caller named the
// repo by a path that is not where it lives.
//
// PATH_MAPPINGS lets a caller on another host spell a repo by its local path:
// /Users/<me>/Developer/<repo> resolves to /host/src/<repo> inside the
// container. The answer is then computed from the SERVER's checkout, never
// from a file on the caller's disk — no file is transferred and no local edit
// is visible. That distinction is invisible in the response, and a reader who
// does not know the alias exists will read the answer as being about their own
// working tree.
//
// Silence is the calibrated signal (see Envelope). When requested and resolved
// name the same directory there is nothing a reader could misattribute, so the
// field stays empty and the footer is suppressed. A non-absolute request (a
// GitHub slug or a bare repo name) is likewise silent — nobody reads
// "owner/repo" as a path on their own disk.
func WithSourcePath(env Envelope, requested, resolved string) Envelope {
	if requested == "" || resolved == "" {
		return env
	}
	// A slug ("owner/repo") or bare name carries no claim about the caller's
	// filesystem, so resolving it elsewhere cannot mislead anyone.
	if !filepath.IsAbs(requested) {
		return env
	}
	if filepath.Clean(requested) == filepath.Clean(resolved) {
		return env
	}
	env.SourcePath = resolved
	return env
}
