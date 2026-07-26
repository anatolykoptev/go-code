package main

import "testing"

// TestResolveBuildSHAFrom pins the resolution order for gocode_build_info's
// git_sha label:
//
//	ldflags-injected SHA (Docker path) → vcs.revision (local go build/test)
//	→ release version var → "unknown"
//
// Row "ldflags wins over vcs.revision" is the discriminating row: against the
// PRE-fix precedence (vcs.revision first) it returns the vcs value, so the
// assertion fails and the test proves the reorder rather than merely
// describing it.
func TestResolveBuildSHAFrom(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		ldflagsSHA  string
		vcsRevision string
		version     string
		want        string
	}{
		{
			name:        "ldflags wins over vcs.revision (Docker path, .git excluded)",
			ldflagsSHA:  "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
			vcsRevision: "cafebabecafebabecafebabecafebabecafebabe",
			version:     "v1.61.0",
			want:        "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		},
		{
			name:        "ldflags unknown placeholder falls through to vcs.revision",
			ldflagsSHA:  "unknown",
			vcsRevision: "cafebabecafebabecafebabecafebabecafebabe",
			version:     "v1.61.0",
			want:        "cafebabecafebabecafebabecafebabecafebabe",
		},
		{
			name:        "empty ldflags falls through to vcs.revision (local go build)",
			ldflagsSHA:  "",
			vcsRevision: "cafebabecafebabecafebabecafebabecafebabe",
			version:     "v1.61.0",
			want:        "cafebabecafebabecafebabecafebabecafebabe",
		},
		{
			name:        "ldflags set, no vcs, no version (Docker path, .git excluded)",
			ldflagsSHA:  "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
			vcsRevision: "",
			version:     "",
			want:        "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		},
		{
			name:        "no ldflags, no vcs, release version present",
			ldflagsSHA:  "",
			vcsRevision: "",
			version:     "v1.61.0",
			want:        "v1.61.0",
		},
		{
			name:        "no ldflags, no vcs, version=dev rejected",
			ldflagsSHA:  "",
			vcsRevision: "",
			version:     "dev",
			want:        "unknown",
		},
		{
			name:        "all empty",
			ldflagsSHA:  "",
			vcsRevision: "",
			version:     "",
			want:        "unknown",
		},
		{
			name:        "ldflags unknown + no vcs + version dev",
			ldflagsSHA:  "unknown",
			vcsRevision: "",
			version:     "dev",
			want:        "unknown",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := resolveBuildSHAFrom(tc.ldflagsSHA, tc.vcsRevision, tc.version)
			if got != tc.want {
				t.Fatalf("resolveBuildSHAFrom(%q, %q, %q) = %q, want %q",
					tc.ldflagsSHA, tc.vcsRevision, tc.version, got, tc.want)
			}
		})
	}
}
