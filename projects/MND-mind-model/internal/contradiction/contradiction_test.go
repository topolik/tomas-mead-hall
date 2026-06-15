package contradiction

import (
	"strings"
	"testing"

	"github.com/topolik/mnd-mind-model/internal/distill"
)

func ins(id, src, ts, strength string) distill.Insight {
	return distill.Insight{
		ID: id, Category: "direction_pattern", Statement: "stmt " + id,
		Strength: strength, Source: src,
		Evidence: []distill.Evidence{{Moment: "m" + id, TS: ts}},
	}
}

// T41: feedback beats distill; then newer ts; then strength; then stable id.
func TestMoreAuthoritative(t *testing.T) {
	fb := ins("a", "feedback", "2026-06-01T00:00:00Z", "weak")
	di := ins("b", "distill", "2026-06-13T00:00:00Z", "strong")
	if !MoreAuthoritative(fb, di) {
		t.Fatal("feedback must beat distill regardless of recency/strength")
	}

	old := ins("c", "distill", "2026-01-01T00:00:00Z", "strong")
	new := ins("d", "distill", "2026-06-13T00:00:00Z", "weak")
	if !MoreAuthoritative(new, old) {
		t.Fatal("newer must beat older among same source")
	}

	weak := ins("e", "distill", "2026-06-01T00:00:00Z", "weak")
	strong := ins("f", "distill", "2026-06-01T00:00:00Z", "strong")
	if !MoreAuthoritative(strong, weak) {
		t.Fatal("stronger must beat weaker at equal source+ts")
	}

	x := ins("aaa", "distill", "2026-06-01T00:00:00Z", "weak")
	y := ins("zzz", "distill", "2026-06-01T00:00:00Z", "weak")
	if !MoreAuthoritative(x, y) || MoreAuthoritative(y, x) {
		t.Fatal("id tiebreak must be deterministic (smaller id wins)")
	}
}

// T42: contradiction — losers marked superseded, kept in slice; winner untouched.
func TestResolve(t *testing.T) {
	insights := []distill.Insight{
		ins("stale", "distill", "2026-01-01T00:00:00Z", "strong"),  // old belief
		ins("ruling", "feedback", "2026-06-12T00:00:00Z", "strong"), // Tomas correction
		ins("unrelated", "distill", "2026-05-01T00:00:00Z", "moderate"),
	}
	out, retired, scoped := Resolve(insights, []Conflict{{Verdict: VerdictContradiction, IDs: []string{"stale", "ruling"}, Reason: "push policy"}})

	if len(out) != 3 {
		t.Fatalf("superseded insights must stay in the slice (audit trail), got %d", len(out))
	}
	if len(retired) != 1 || retired[0].LoserID != "stale" || retired[0].WinnerID != "ruling" {
		t.Fatalf("expected stale retired by ruling, got %+v", retired)
	}
	if len(scoped) != 0 {
		t.Fatalf("a contradiction must not produce scopings, got %+v", scoped)
	}
	byID := map[string]distill.Insight{}
	for _, in := range out {
		byID[in.ID] = in
	}
	if byID["stale"].Status != "superseded" || byID["stale"].SupersededBy != "ruling" || byID["stale"].SupersededReason == "" {
		t.Fatalf("stale not marked correctly: %+v", byID["stale"])
	}
	if byID["ruling"].Status != "" || byID["unrelated"].Status != "" {
		t.Fatal("winner and unrelated insights must stay active")
	}
	if len(distill.Active(out)) != 2 {
		t.Fatalf("Active should drop the 1 superseded, got %d", len(distill.Active(out)))
	}
}

// T47: context_split — both stay active, contexts sharpened, nothing retired.
func TestResolveContextSplit(t *testing.T) {
	insights := []distill.Insight{
		ins("containers", "distill", "2026-05-31T00:00:00Z", "strong"),
		ins("localbuild", "distill", "2026-06-03T00:00:00Z", "strong"),
	}
	out, retired, scoped := Resolve(insights, []Conflict{{
		Verdict: VerdictContextSplit,
		IDs:     []string{"containers", "localbuild"},
		Reason:  "run vs build",
		Contexts: map[string]string{
			"containers": "when running or deploying a service",
			"localbuild": "when building or compiling artifacts",
		},
	}})
	if len(retired) != 0 {
		t.Fatalf("context_split must retire nothing, got %+v", retired)
	}
	if len(distill.Active(out)) != 2 {
		t.Fatalf("both insights must stay active, got %d", len(distill.Active(out)))
	}
	if len(scoped) != 2 {
		t.Fatalf("both contexts should be sharpened, got %+v", scoped)
	}
	byID := map[string]distill.Insight{}
	for _, in := range out {
		byID[in.ID] = in
	}
	if byID["containers"].Context != "when running or deploying a service" || byID["localbuild"].Context != "when building or compiling artifacts" {
		t.Fatalf("contexts not written: %q / %q", byID["containers"].Context, byID["localbuild"].Context)
	}

	// T49: idempotent — re-running the SAME context_split scopes nothing (both
	// already have a context), so loop-until-dry converges instead of thrashing.
	c := []Conflict{{Verdict: VerdictContextSplit, IDs: []string{"containers", "localbuild"}, Reason: "run vs build",
		Contexts: map[string]string{"containers": "REWORDED ctx", "localbuild": "REWORDED ctx"}}}
	_, _, scoped2 := Resolve(out, c)
	if len(scoped2) != 0 {
		t.Fatalf("re-scoping already-contexted insights must be a no-op, got %+v", scoped2)
	}
}

// T43: prose tolerated; unknown/inactive ids dropped; <2 active members → no-op.
func TestParseResponse(t *testing.T) {
	active := map[string]distill.Insight{
		"x": ins("x", "distill", "2026-01-01T00:00:00Z", "weak"),
		"y": ins("y", "distill", "2026-02-01T00:00:00Z", "weak"),
	}
	resp := "Here are the conflicts I found:\n{\"conflicts\":[" +
		"{\"verdict\":\"contradiction\",\"ids\":[\"x\",\"y\"],\"reason\":\"opposite\"}," +
		"{\"ids\":[\"x\",\"ghost\"],\"reason\":\"one unknown\"}," +
		"{\"ids\":[\"x\"],\"reason\":\"single\"}]}"
	conflicts, dropped, err := ParseResponse(resp, active)
	if err != nil {
		t.Fatal(err)
	}
	if len(conflicts) != 1 || len(conflicts[0].IDs) != 2 || conflicts[0].Verdict != VerdictContradiction {
		t.Fatalf("only the x,y contradiction should survive, got %+v", conflicts)
	}
	if len(dropped) != 2 {
		t.Fatalf("two malformed groups should be dropped, got %v", dropped)
	}

	// missing/unknown verdict defaults to the non-destructive context_split
	def, _, _ := ParseResponse(`{"conflicts":[{"ids":["x","y"],"reason":"r"}]}`, active)
	if len(def) != 1 || def[0].Verdict != VerdictContextSplit {
		t.Fatalf("missing verdict must default to context_split, got %+v", def)
	}
	if _, _, err := ParseResponse("no json here", active); err == nil {
		t.Fatal("missing JSON must error")
	}
}

// T45: empty conflicts → unchanged; re-resolving a resolved set adds nothing.
func TestResolveIdempotent(t *testing.T) {
	insights := []distill.Insight{
		ins("a", "feedback", "2026-06-01T00:00:00Z", "strong"),
		ins("b", "distill", "2026-01-01T00:00:00Z", "weak"),
	}
	if out, retired, scoped := Resolve(insights, nil); len(retired) != 0 || len(scoped) != 0 || len(out) != 2 {
		t.Fatal("no conflicts must change nothing")
	}
	c := []Conflict{{Verdict: VerdictContradiction, IDs: []string{"a", "b"}, Reason: "x"}}
	out, retired, _ := Resolve(insights, c)
	if len(retired) != 1 {
		t.Fatalf("first pass retires one, got %d", len(retired))
	}
	_, retired2, _ := Resolve(out, c)
	if len(retired2) != 0 {
		t.Fatalf("second pass must retire nothing (b already superseded), got %d", len(retired2))
	}
}

// T44: the sweep prompt carries only the active insights it was given.
func TestBuildPromptActiveOnly(t *testing.T) {
	active := []distill.Insight{
		ins("keep1", "distill", "2026-01-01T00:00:00Z", "strong"),
		ins("keep2", "feedback", "2026-06-01T00:00:00Z", "strong"),
	}
	p := BuildPrompt(active)
	for _, want := range []string{"id=keep1", "id=keep2", "source=feedback", "<insights>", "context_split", "contradiction"} {
		if !strings.Contains(p, want) {
			t.Fatalf("prompt missing %q", want)
		}
	}
}
