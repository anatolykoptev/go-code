package mcpmeta

import (
	"strings"
	"testing"
)

// TestPickFitting_FirstRungFits_ReturnsFirst verifies that when the budget
// fits the first (fullest) rung, that rung's rendering is returned verbatim
// with NO condensation note. RED-on-revert: if PickFitting always drops to
// the cheapest rung, the first rung's distinctive content is missing.
func TestPickFitting_FirstRungFits_ReturnsFirst(t *testing.T) {
	t.Parallel()
	called := make(map[int]bool)
	l := Ladder{
		{Name: "full", Render: func() string { called[0] = true; return "FULL-RENDER-CONTENT" }},
		{Name: "no-context", Render: func() string { called[1] = true; return "NC" }},
		{Name: "counts", Render: func() string { called[2] = true; return "C" }},
	}
	got := PickFitting(l, 1000)
	if !strings.Contains(got, "FULL-RENDER-CONTENT") {
		t.Fatalf("first rung must be returned when it fits, got %q", got)
	}
	if strings.Contains(got, condensationNotePrefix) {
		t.Fatalf("first rung must NOT carry a condensation note, got %q", got)
	}
	if called[1] || called[2] {
		t.Fatalf("cheaper rungs must not be rendered when first fits: called=%v", called)
	}
}

// TestPickFitting_OnlyThirdRungFits_ReturnsThird verifies that a budget that
// fits only the third (cheapest) rung yields the third rung's rendering.
// RED-on-revert: if PickFitting returns the first rung regardless, the result
// exceeds the budget.
func TestPickFitting_OnlyThirdRungFits_ReturnsThird(t *testing.T) {
	t.Parallel()
	l := Ladder{
		{Name: "full", Render: func() string { return strings.Repeat("X", 300) }},
		{Name: "no-context", Render: func() string { return strings.Repeat("Y", 200) }},
		{Name: "counts", Render: func() string { return "47 matches across 6 files" }},
	}
	// Budget fits only rung 3 (counts) + its condensation note.
	got := PickFitting(l, 80)
	if !strings.Contains(got, "47 matches across 6 files") {
		t.Fatalf("third rung must be returned when only it fits, got %q", got)
	}
	if !strings.Contains(got, condensationNotePrefix) {
		t.Fatalf("rung below the first must carry a condensation note, got %q", got)
	}
	if !strings.Contains(got, "counts") {
		t.Fatalf("condensation note must name the chosen rung, got %q", got)
	}
	if len(got) > 80 {
		t.Fatalf("result must fit the budget: got %d, budget 80", len(got))
	}
}

// TestPickFitting_UnreachedRungClosureNeverCalled proves laziness: a rung
// past the chosen one has its Render closure NEVER invoked. RED-on-revert:
// if PickFitting eagerly renders all rungs, the sentinel closure fires.
func TestPickFitting_UnreachedRungClosureNeverCalled(t *testing.T) {
	t.Parallel()
	reached3 := false
	l := Ladder{
		{Name: "full", Render: func() string { return strings.Repeat("A", 300) }},
		{Name: "no-context", Render: func() string { return "fits-here" }},
		{Name: "counts", Render: func() string { reached3 = true; return "should-not-render" }},
	}
	// Budget fits rung 2 (no-context) but not rung 1 (full).
	_ = PickFitting(l, 100)
	if reached3 {
		t.Fatal("rung 3 closure must NEVER be called when rung 2 fits (laziness)")
	}
}

// TestPickFitting_CondensationNoteAbsentOnFirstRung_PresentBelow verifies
// the condensation note is absent on the first rung and present on every
// rung below it. RED-on-revert: if the note is added to the first rung too,
// the first-rung assertion fails; if missing below, the below assertion fails.
func TestPickFitting_CondensationNoteAbsentOnFirst_PresentBelow(t *testing.T) {
	t.Parallel()
	check := func(t *testing.T, budget int, wantNote bool, wantName string) {
		t.Helper()
		l := Ladder{
			{Name: "full", Render: func() string { return strings.Repeat("F", 150) }},
			{Name: "no-context", Render: func() string { return strings.Repeat("N", 80) }},
			{Name: "counts", Render: func() string { return "counts-body" }},
		}
		got := PickFitting(l, budget)
		hasNote := strings.Contains(got, condensationNotePrefix)
		if hasNote != wantNote {
			t.Fatalf("budget=%d: note present=%v, want %v, got %q", budget, hasNote, wantNote, got)
		}
		if wantNote && !strings.Contains(got, wantName) {
			t.Fatalf("note must name rung %q, got %q", wantName, got)
		}
	}
	// Budget fits rung 1 → no note.
	check(t, 200, false, "")
	// Budget fits rung 2 only → note naming "no-context".
	check(t, 120, true, "no-context")
	// Budget fits rung 3 only → note naming "counts".
	check(t, 60, true, "counts")
}

// TestPickFitting_NoRungFits_EmitsCutMarker verifies the last-resort path:
// when even the cheapest rung does not fit, the cheapest rendering is
// truncated and an explicit cut marker is appended. The result must fit
// the budget. RED-on-revert: if no cut marker is emitted, the assertion
// fails; if the result exceeds the budget, the size assertion fails.
func TestPickFitting_NoRungFits_EmitsCutMarker(t *testing.T) {
	t.Parallel()
	l := Ladder{
		{Name: "full", Render: func() string { return strings.Repeat("F", 300) }},
		{Name: "no-context", Render: func() string { return strings.Repeat("N", 200) }},
		{Name: "counts", Render: func() string { return strings.Repeat("C", 100) }},
	}
	budget := 80
	got := PickFitting(l, budget)
	if !strings.Contains(got, cutMarkerPrefix) {
		t.Fatalf("last-resort must emit cut marker, got %q", got)
	}
	if len(got) > budget {
		t.Fatalf("last-resort result must fit budget: got %d, budget %d", len(got), budget)
	}
	if !strings.Contains(got, "counts") {
		t.Fatalf("cut marker must name the cheapest rung, got %q", got)
	}
}

// TestPickFitting_SingleRungFits_ReturnsItNoNote verifies a one-rung ladder
// that fits returns the rendering with no condensation note (the first rung
// is also the last — no condensation).
func TestPickFitting_SingleRungFits_ReturnsItNoNote(t *testing.T) {
	t.Parallel()
	l := Ladder{{Name: "only", Render: func() string { return "only-render" }}}
	got := PickFitting(l, 100)
	if got != "only-render" {
		t.Fatalf("single fitting rung must be returned verbatim, got %q", got)
	}
}

// TestPickFitting_SingleRungDoesNotFit_EmitsCutMarker verifies a one-rung
// ladder that does not fit truncates and emits the cut marker.
func TestPickFitting_SingleRungDoesNotFit_EmitsCutMarker(t *testing.T) {
	t.Parallel()
	l := Ladder{{Name: "only", Render: func() string { return strings.Repeat("Z", 200) }}}
	got := PickFitting(l, 80)
	if !strings.Contains(got, cutMarkerPrefix) {
		t.Fatalf("oversized single rung must emit cut marker, got %q", got)
	}
	if len(got) > 80 {
		t.Fatalf("result must fit budget: got %d, budget 80", len(got))
	}
}

// TestPickFitting_EmptyLadder_ReturnsEmpty verifies the empty-ladder edge.
func TestPickFitting_EmptyLadder_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	if got := PickFitting(Ladder{}, 100); got != "" {
		t.Fatalf("empty ladder must return empty string, got %q", got)
	}
}

// TestPickFitting_ZeroBudget_ReturnsEmpty verifies budget <= 0 returns empty.
func TestPickFitting_ZeroBudget_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	l := Ladder{{Name: "only", Render: func() string { return "x" }}}
	if got := PickFitting(l, 0); got != "" {
		t.Fatalf("zero budget must return empty, got %q", got)
	}
	if got := PickFitting(l, -1); got != "" {
		t.Fatalf("negative budget must return empty, got %q", got)
	}
}
