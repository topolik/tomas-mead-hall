package eval

import (
	"strings"
	"testing"

	"github.com/topolik/mnd-mind-model/internal/moment"
)

func moms(ids ...string) []moment.Moment {
	var ms []moment.Moment
	for _, id := range ids {
		ms = append(ms, moment.Moment{ID: id, Context: "ctx " + id, Text: "tomas " + id})
	}
	return ms
}

// T51: sampling is deterministic and bounded.
func TestSampleDeterministic(t *testing.T) {
	ms := moms("e", "a", "d", "b", "c") // unsorted on purpose
	a := Sample(ms, 3)
	b := Sample(ms, 3)
	if len(a) != 3 {
		t.Fatalf("want 3, got %d", len(a))
	}
	for i := range a {
		if a[i].ID != b[i].ID {
			t.Fatalf("non-deterministic: %v vs %v", a, b)
		}
	}
	// id-sorted stride: first pick is the smallest id
	if a[0].ID != "a" {
		t.Fatalf("expected id-sorted sample, got first=%s", a[0].ID)
	}
	if got := Sample(ms, 99); len(got) != 5 {
		t.Fatalf("n>=len returns all, got %d", len(got))
	}
}

// T50: case parsing — skip/empty/unknown-category/unknown-id dropped.
func TestParseCases(t *testing.T) {
	known := map[string]moment.Moment{"m1": {}, "m2": {}, "m3": {}, "m4": {}}
	resp := `prefix {"cases":[
	  {"moment_id":"m1","skip":false,"category":"tech_preference","situation":"Postgres or flat files?","gold":"Flat files, KISS."},
	  {"moment_id":"m2","skip":true,"category":"tech_preference","situation":"x","gold":"y"},
	  {"moment_id":"m3","skip":false,"category":"bogus","situation":"x","gold":"y"},
	  {"moment_id":"m4","skip":false,"category":"tech_preference","situation":"x","gold":""},
	  {"moment_id":"ghost","skip":false,"category":"tech_preference","situation":"x","gold":"y"}
	]}`
	cases, dropped, err := ParseCases(resp, known)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 1 || cases[0].ID != "m1" {
		t.Fatalf("only m1 should survive, got %+v", cases)
	}
	if len(dropped) != 4 {
		t.Fatalf("4 should drop (skip/category/empty/ghost), got %v", dropped)
	}
	if _, _, err := ParseCases("no json", known); err == nil {
		t.Fatal("missing JSON must error")
	}
}

// T52: the blind question never contains the gold decision.
func TestAskQuestionNoLeak(t *testing.T) {
	c := Case{Situation: "Should the new service use Postgres or flat files?", Gold: "Flat files. KISS — no DB until it outgrows them."}
	q := AskQuestion(c)
	if !strings.Contains(q, c.Situation) {
		t.Fatal("question must contain the situation")
	}
	if strings.Contains(q, "Flat files") || strings.Contains(q, c.Gold) {
		t.Fatalf("LEAK: gold decision appears in the blind question: %q", q)
	}
}

// T53: judge parsing — junk/missing verdict ⇒ disagree (conservative).
func TestParseJudge(t *testing.T) {
	answered := []Answered{
		{Case: Case{ID: "m1", Category: "tech_preference"}, Confidence: "high"},
		{Case: Case{ID: "m2", Category: "tech_preference"}, Confidence: "low"},
		{Case: Case{ID: "m3", Category: "tech_preference"}, Confidence: "medium"},
	}
	resp := `{"scores":[
	  {"id":"m1","verdict":"agree","reason":"same call"},
	  {"id":"m2","verdict":"nonsense","reason":"?"}
	]}`
	scored, dropped, err := ParseJudge(resp, answered)
	if err != nil {
		t.Fatal(err)
	}
	if len(scored) != 3 {
		t.Fatalf("want 3 scored, got %d", len(scored))
	}
	m := map[string]string{}
	for _, s := range scored {
		m[s.ID] = s.Verdict
	}
	if m["m1"] != "agree" || m["m2"] != "disagree" || m["m3"] != "disagree" {
		t.Fatalf("junk⇒disagree, unscored⇒disagree: %v", m)
	}
	if len(dropped) != 1 { // m3 unscored
		t.Fatalf("m3 unscored should be reported, got %v", dropped)
	}
}

// T54: report aggregation — score math, category + calibration buckets, disagreements.
func TestAggregateAndReport(t *testing.T) {
	scored := []Scored{
		{Answered: Answered{Case: Case{ID: "1", Category: "tech_preference", Situation: "s1", Gold: "g1"}, Answer: "a1", Confidence: "high"}, Verdict: "agree"},
		{Answered: Answered{Case: Case{ID: "2", Category: "tech_preference", Situation: "s2", Gold: "g2"}, Answer: "a2", Confidence: "high"}, Verdict: "partial"},
		{Answered: Answered{Case: Case{ID: "3", Category: "decision_heuristic", Situation: "s3", Gold: "g3"}, Answer: "a3", Confidence: "low"}, Verdict: "disagree", Reason: "opposite"},
	}
	st := Aggregate(scored, "in-sample")
	// (1 + 0.5 + 0)/3 = 50%
	if st.Total != 3 || st.Agree != 1 || st.Partial != 1 || st.Disagree != 1 {
		t.Fatalf("counts wrong: %+v", st)
	}
	if st.FidelityPct < 49.9 || st.FidelityPct > 50.1 {
		t.Fatalf("fidelity should be 50%%, got %.1f", st.FidelityPct)
	}
	if st.ByConfidence["high"].FidelityPct != 75 { // (1+0.5)/2
		t.Fatalf("high-confidence bucket should be 75%%, got %.1f", st.ByConfidence["high"].FidelityPct)
	}
	if st.ByCategory["decision_heuristic"].FidelityPct != 0 {
		t.Fatalf("decision_heuristic bucket should be 0%%, got %.1f", st.ByCategory["decision_heuristic"].FidelityPct)
	}

	md := Report(scored, "in-sample", "2026-06-14")
	for _, want := range []string{"fidelity: 50%", "in-sample", "Confidence calibration", "opposite", "Disagreements"} {
		if !strings.Contains(md, want) {
			t.Fatalf("report missing %q", want)
		}
	}
	// agreeing case must NOT appear in the disagreement list
	if strings.Contains(md[strings.Index(md, "Disagreements"):], "s1") {
		t.Fatal("agreeing case leaked into the disagreement list")
	}
}

// T57: degenerate confidence (all one bucket) is flagged — the gate can't discriminate.
func TestReportDegenerateConfidence(t *testing.T) {
	var scored []Scored
	for i := 0; i < 10; i++ {
		v := "agree"
		if i >= 6 {
			v = "disagree"
		}
		scored = append(scored, Scored{Answered: Answered{Case: Case{ID: string(rune('a' + i)), Category: "tech_preference"}, Confidence: "high"}, Verdict: v})
	}
	md := Report(scored, "in-sample", "t")
	if !strings.Contains(md, "confidence is not discriminating") {
		t.Fatal("all-high confidence should trigger the discrimination warning")
	}
	// a varied set must NOT trigger it
	scored[0].Confidence = "low"
	scored[1].Confidence = "low"
	scored[2].Confidence = "medium"
	scored[3].Confidence = "medium"
	if strings.Contains(Report(scored, "in-sample", "t"), "confidence is not discriminating") {
		t.Fatal("varied confidence should not trigger the warning")
	}
}

// T55: report header states provenance.
func TestReportProvenance(t *testing.T) {
	heldout := Report(nil, "held-out", "2026-06-14")
	if !strings.Contains(heldout, "tested against an eval-brain") {
		t.Fatal("held-out provenance not labeled")
	}
	insample := Report(nil, "in-sample", "2026-06-14")
	if !strings.Contains(insample, "optimistic upper bound") {
		t.Fatal("in-sample caveat missing")
	}
}
