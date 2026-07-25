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
	got, _, _ := PickFitting(l, 1000)
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
	got, _, _ := PickFitting(l, 80)
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
	_, _, _ = PickFitting(l, 100)
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
		got, _, _ := PickFitting(l, budget)
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
	// (130: rung 2 = 80 + 42-byte comment note = 122 < 130 < 150 rung 1.)
	check(t, 130, true, "no-context")
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
	// Budget 90: no rung fits (300/200/100 all > 90), and the 82-byte cut
	// marker fits (< 90) so the full marker with the rung name is emitted.
	budget := 90
	got, _, _ := PickFitting(l, budget)
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
	got, _, _ := PickFitting(l, 100)
	if got != "only-render" {
		t.Fatalf("single fitting rung must be returned verbatim, got %q", got)
	}
}

// TestPickFitting_SingleRungDoesNotFit_EmitsCutMarker verifies a one-rung
// ladder that does not fit truncates and emits the cut marker.
func TestPickFitting_SingleRungDoesNotFit_EmitsCutMarker(t *testing.T) {
	t.Parallel()
	l := Ladder{{Name: "only", Render: func() string { return strings.Repeat("Z", 200) }}}
	got, _, _ := PickFitting(l, 80)
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
	if got, _, _ := PickFitting(Ladder{}, 100); got != "" {
		t.Fatalf("empty ladder must return empty string, got %q", got)
	}
}

// TestPickFitting_FullReturnIsRung1Rendering verifies that the `full` return
// value is always the rung-1 (fullest) rendering, regardless of which rung
// was chosen as `body`. RED-on-revert: if PickFitting stops returning the
// rung-1 rendering as `full`, the assertion fails.
func TestPickFitting_FullReturnIsRung1Rendering(t *testing.T) {
	t.Parallel()
	l := Ladder{
		{Name: "full", Render: func() string { return "FULL-RENDER-CONTENT" }},
		{Name: "no-context", Render: func() string { return "NC" }},
		{Name: "counts", Render: func() string { return "C" }},
	}
	// Budget forces a cheaper rung as body, but full must be rung 1.
	body, full, _ := PickFitting(l, 10)
	if full != "FULL-RENDER-CONTENT" {
		t.Fatalf("full must be rung-1 rendering, got %q (body=%q)", full, body)
	}
	// When rung 1 fits, body == full.
	body, full, _ = PickFitting(l, 1000)
	if full != "FULL-RENDER-CONTENT" || body != "FULL-RENDER-CONTENT" {
		t.Fatalf("when rung 1 fits, body and full must both be rung 1, got body=%q full=%q", body, full)
	}
}

// TestPickFitting_Rung1RenderedAtMostOnce verifies the at-most-once guarantee:
// rung 1's Render closure is invoked exactly once, even when a cheaper rung
// is chosen as `body`. RED-on-revert: if PickFitting re-renders rung 1 to
// produce `full`, the counter exceeds 1.
func TestPickFitting_Rung1RenderedAtMostOnce(t *testing.T) {
	t.Parallel()
	rung1Calls := 0
	l := Ladder{
		{Name: "full", Render: func() string { rung1Calls++; return strings.Repeat("X", 300) }},
		{Name: "no-context", Render: func() string { return "fits-here" }},
		{Name: "counts", Render: func() string { return "C" }},
	}
	_, _, _ = PickFitting(l, 100)
	if rung1Calls != 1 {
		t.Fatalf("rung 1 Render must be called exactly once, got %d", rung1Calls)
	}
}

// TestPickFitting_ZeroBudget_ReturnsEmpty verifies budget <= 0 returns empty.
func TestPickFitting_ZeroBudget_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	l := Ladder{{Name: "only", Render: func() string { return "x" }}}
	if got, _, _ := PickFitting(l, 0); got != "" {
		t.Fatalf("zero budget must return empty, got %q", got)
	}
	if got, _, _ := PickFitting(l, -1); got != "" {
		t.Fatalf("negative budget must return empty, got %q", got)
	}
}

// TestPickFitting_RungIndex_ReturnedCorrectly verifies the 1-based rung
// index return value: rung 1 when the first fits, rung 2 when the second is
// the first that fits, rung n on the cut path. RED-on-revert: if the index
// is wrong, the handler's condensation gate (rung > 1) misfires.
func TestPickFitting_RungIndex_ReturnedCorrectly(t *testing.T) {
	t.Parallel()
	l := Ladder{
		{Name: "full", Render: func() string { return strings.Repeat("F", 300) }},
		{Name: "no-context", Render: func() string { return strings.Repeat("N", 80) }},
		{Name: "counts", Render: func() string { return strings.Repeat("C", 10) }},
	}
	// Budget fits rung 1 → rung == 1.
	if _, _, rung := PickFitting(l, 1000); rung != 1 {
		t.Fatalf("rung 1 fits: want rung=1, got %d", rung)
	}
	// Budget fits rung 2 only → rung == 2.
	if _, _, rung := PickFitting(l, 130); rung != 2 {
		t.Fatalf("rung 2 fits: want rung=2, got %d", rung)
	}
	// Budget fits rung 3 only → rung == 3.
	if _, _, rung := PickFitting(l, 60); rung != 3 {
		t.Fatalf("rung 3 fits: want rung=3, got %d", rung)
	}
	// No rung fits (cut path) → rung == n (last rung).
	if _, _, rung := PickFitting(l, 5); rung != 3 {
		t.Fatalf("cut path: want rung=3 (last), got %d", rung)
	}
}

// TestPickFitting_ReserveKeepsBodyPlusPointerUnderBudget is THE ceiling
// test for finding 4: when the caller charges a `reserve` against the budget
// (to leave headroom for a file-save pointer appended after PickFitting),
// the chosen body + a pointer of length <= reserve MUST stay <= budget.
//
// Constructs a ladder where rung 2 WOULD fit the full budget but NOT
// budget-reserve — so without the reserve, rung 2 is chosen and body+pointer
// overflows the budget (the exact #685 bug reintroduced at the margin); with
// the reserve, a cheaper rung is chosen and body+pointer stays under budget.
//
// Anti-vacuity is built in: the same ladder without the reserve picks rung 2
// and body+pointer OVERFLOWS the budget — proving the reserve is what holds
// the ceiling, not coincidence.
func TestPickFitting_ReserveKeepsBodyPlusPointerUnderBudget(t *testing.T) {
	t.Parallel()
	const budget = 1000
	const reserve = 100 // simulated pointer upper bound
	effective := budget - reserve

	noteLen := len(condensationNote(2, 3, "no-context"))
	// rung 2 body: chosen so rung 2 + note fits the FULL budget but NOT the
	// effective budget. Without the reserve, rung 2 is chosen and
	// body+pointer overflows. With the reserve, rung 2 doesn't fit and the
	// ladder steps down to rung 3.
	rung2Body := budget - noteLen - 38 // 920: 920+42=962 <= 1000, 962 > 900
	l := Ladder{
		{Name: "full", Render: func() string { return strings.Repeat("F", 1200) }},
		{Name: "no-context", Render: func() string { return strings.Repeat("N", rung2Body) }},
		{Name: "counts", Render: func() string { return strings.Repeat("C", 50) }},
	}

	// WITH reserve: rung 3 chosen, body+pointer <= budget.
	body, _, rung := PickFitting(l, effective)
	if rung != 3 {
		t.Fatalf("reserve must force a cheaper rung: got rung %d, want 3", rung)
	}
	simulatedPointer := strings.Repeat("P", reserve)
	if len(body)+len(simulatedPointer) > budget {
		t.Fatalf("with reserve: body(%d)+pointer(%d) must be <= budget(%d), got %d",
			len(body), reserve, budget, len(body)+len(simulatedPointer))
	}

	// WITHOUT reserve (full budget): rung 2 chosen, body+pointer OVERFLOWS.
	// This is the anti-vacuity proof — the reserve is load-bearing.
	bodyNoReserve, _, rungNoReserve := PickFitting(l, budget)
	if rungNoReserve != 2 {
		t.Fatalf("without reserve: rung 2 must be chosen, got rung %d", rungNoReserve)
	}
	if len(bodyNoReserve)+len(simulatedPointer) <= budget {
		t.Fatalf("without reserve: body(%d)+pointer(%d) must OVERFLOW budget(%d), got %d",
			len(bodyNoReserve), reserve, budget, len(bodyNoReserve)+len(simulatedPointer))
	}
}
