package mcpmeta

import "testing"

// A translated path is the whole reason this field exists: the caller named a
// path on their own machine and got an answer computed from the server's
// checkout. Staying silent here is the bug.
func TestWithSourcePath_TranslatedPathSpeaks(t *testing.T) {
	t.Parallel()
	env := WithSourcePath(Wrap(1, ""), "/Users/dev/Developer/acme", "/host/src/acme")
	if env.SourcePath != "/host/src/acme" {
		t.Fatalf("aliased path must report the server root, got %q", env.SourcePath)
	}
}

// The caller already named the path that was read — nothing to misattribute.
func TestWithSourcePath_SamePathSilent(t *testing.T) {
	t.Parallel()
	env := WithSourcePath(Wrap(1, ""), "/host/src/acme", "/host/src/acme")
	if env.SourcePath != "" {
		t.Fatalf("un-translated path must be silent, got %q", env.SourcePath)
	}
}

// Trailing slashes and "." segments are the same directory, not a translation.
func TestWithSourcePath_EquivalentSpellingSilent(t *testing.T) {
	t.Parallel()
	env := WithSourcePath(Wrap(1, ""), "/host/src/acme/", "/host/src/acme")
	if env.SourcePath != "" {
		t.Fatalf("equivalent spelling must be silent, got %q", env.SourcePath)
	}
}

// A slug makes no claim about the caller's filesystem, so pointing it at a
// clone dir cannot mislead anyone — reporting it would be pure noise.
func TestWithSourcePath_SlugSilent(t *testing.T) {
	t.Parallel()
	env := WithSourcePath(Wrap(1, ""), "acme/web", "/tmp/workspace/acme_web")
	if env.SourcePath != "" {
		t.Fatalf("slug request must be silent, got %q", env.SourcePath)
	}
}

func TestWithSourcePath_EmptyInputsSilent(t *testing.T) {
	t.Parallel()
	if env := WithSourcePath(Wrap(1, ""), "", "/host/src/acme"); env.SourcePath != "" {
		t.Fatalf("empty request must be silent, got %q", env.SourcePath)
	}
	if env := WithSourcePath(Wrap(1, ""), "/Users/dev/acme", ""); env.SourcePath != "" {
		t.Fatalf("empty resolved must be silent, got %q", env.SourcePath)
	}
}

// HasSignal is the single gate both response-footer renderers consult. A field
// that carries actionable information but is missing from HasSignal is written
// and never rendered — the footer is suppressed and the caller sees nothing.
func TestHasSignal_EachActionableFieldCounts(t *testing.T) {
	t.Parallel()
	for name, env := range map[string]Envelope{
		"hint":         {Hint: "h"},
		"stale":        {StaleWarning: "w"},
		"source_path":  {SourcePath: "/host/src/acme"},
		"checkout_lag": {CheckoutLag: "behind"},
		"graph_age":    {GraphStaleAgeS: 1},
	} {
		if !env.HasSignal() {
			t.Errorf("%s alone must count as signal, else its footer is suppressed", name)
		}
	}
}

// A bare duration is telemetry the calling agent cannot act on; rendering it
// would put a footer on every single response.
func TestHasSignal_DurationAloneIsNotSignal(t *testing.T) {
	t.Parallel()
	if Wrap(42, "").HasSignal() {
		t.Fatal("duration alone must not trigger a footer")
	}
}
