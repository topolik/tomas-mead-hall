package route

import (
	"testing"

	"github.com/topolik/mnd-mind-model/internal/eval"
)

func sc(id, category, verdict string) eval.Scored {
	return eval.Scored{
		Answered: eval.Answered{Case: eval.Case{ID: id, Category: category, Situation: "s" + id}},
		Verdict:  verdict,
	}
}

// T61: ParseClassify — unknown category -> other, unknown id dropped, known id
// the model skipped -> other (escalate-by-default).
func TestParseClassify(t *testing.T) {
	known := map[string]bool{"a": true, "b": true, "c": true}
	resp := `noise {"labels":[
	  {"id":"a","category":"tech_preference"},
	  {"id":"b","category":"banana"},
	  {"id":"ghost","category":"tech_preference"}
	]} trailing`
	labels, dropped, err := ParseClassify(resp, known)
	if err != nil {
		t.Fatal(err)
	}
	if labels["a"] != "tech_preference" {
		t.Fatalf("a should be tech_preference, got %q", labels["a"])
	}
	if labels["b"] != CategoryOther {
		t.Fatalf("unknown category should map to other, got %q", labels["b"])
	}
	if labels["c"] != CategoryOther {
		t.Fatalf("skipped known id should default to other, got %q", labels["c"])
	}
	if _, ok := labels["ghost"]; ok {
		t.Fatalf("unknown id must not appear in labels")
	}
	if len(dropped) != 1 {
		t.Fatalf("one drop (ghost) expected, got %v", dropped)
	}
}

// T62: Simulate math — delivered fidelity over the auto subset, classifier
// accuracy, leak detection, blanket baseline. Hand-computed expectations.
func TestSimulate(t *testing.T) {
	scored := []eval.Scored{
		sc("1", "tech_preference", "agree"),       // tech, correct, auto
		sc("2", "tech_preference", "disagree"),    // tech, mislabeled judg below
		sc("3", "decision_heuristic", "disagree"), // judg, correct, escalate
		sc("4", "decision_heuristic", "agree"),    // judg mislabeled tech -> LEAK (auto, agree)
	}
	predicted := map[string]string{
		"1": "tech_preference",    // correct
		"2": "tech_preference",    // correct
		"3": "decision_heuristic", // correct
		"4": "tech_preference",    // WRONG (gold judg) -> leaks into auto
	}
	o := Simulate(scored, predicted, PolicyOf("tech_preference"))

	// blanket: agree,disagree,disagree,agree = (1+0+0+1)/4 = 50%
	if o.BlanketFidelity != 50 {
		t.Fatalf("blanket want 50, got %v", o.BlanketFidelity)
	}
	// classifier accuracy: 1,2,3 correct; 4 wrong = 75%
	if o.ClassifierAccuracy != 75 {
		t.Fatalf("classifier acc want 75, got %v", o.ClassifierAccuracy)
	}
	// auto (pred==tech): ids 1,2,4 -> scores 1,0,1 -> 2/3 = 66.67%
	if o.AutoPred != 3 || o.EscalatedPred != 1 {
		t.Fatalf("auto/escalated want 3/1, got %d/%d", o.AutoPred, o.EscalatedPred)
	}
	if d := o.DeliveredFidelityPred; d < 66 || d > 67 {
		t.Fatalf("delivered(pred) want ~66.7, got %v", d)
	}
	// oracle (gold==tech): ids 1,2 -> 1,0 -> 1/2 = 50%
	if o.AutoOracle != 2 || o.DeliveredFidelityOracle != 50 {
		t.Fatalf("oracle want auto=2 fid=50, got auto=%d fid=%v", o.AutoOracle, o.DeliveredFidelityOracle)
	}
	// leak: id 4 (gold judg -> escalate, but auto-answered), fidelity 100%
	if o.LeakedToAuto != 1 || o.LeakedAutoFidelity != 100 {
		t.Fatalf("leak want 1 @ 100%%, got %d @ %v", o.LeakedToAuto, o.LeakedAutoFidelity)
	}
}

// T63: auto-everything ⇒ delivered == blanket, nothing escalated/leaked.
func TestSimulateAutoAll(t *testing.T) {
	scored := []eval.Scored{sc("1", "tech_preference", "agree"), sc("2", "decision_heuristic", "disagree")}
	predicted := map[string]string{"1": "tech_preference", "2": "decision_heuristic"}
	o := Simulate(scored, predicted, PolicyOf(Categories...))
	if o.EscalatedPred != 0 || o.LeakedToAuto != 0 {
		t.Fatalf("auto-all should escalate/leak nothing, got esc=%d leak=%d", o.EscalatedPred, o.LeakedToAuto)
	}
	if o.DeliveredFidelityPred != o.BlanketFidelity {
		t.Fatalf("auto-all delivered (%v) must equal blanket (%v)", o.DeliveredFidelityPred, o.BlanketFidelity)
	}
}

// T64: escalate-everything ⇒ no coverage, delivered guarded to 0 (no div-by-zero).
func TestSimulateEscalateAll(t *testing.T) {
	scored := []eval.Scored{sc("1", "tech_preference", "agree")}
	o := Simulate(scored, map[string]string{"1": "tech_preference"}, PolicyOf()) // empty policy
	if o.AutoPred != 0 || o.CoveragePred != 0 || o.DeliveredFidelityPred != 0 {
		t.Fatalf("escalate-all want auto=0 cov=0 delivered=0, got %d/%v/%v", o.AutoPred, o.CoveragePred, o.DeliveredFidelityPred)
	}
	if o.EscalatedPred != 1 {
		t.Fatalf("escalate-all want escalated=1, got %d", o.EscalatedPred)
	}
}

// T65: Sweep orders categories most-reliable-first and is cumulative.
func TestSweep(t *testing.T) {
	scored := []eval.Scored{
		sc("1", "tech_preference", "agree"),       // tech 100%
		sc("2", "decision_heuristic", "disagree"), // judg 0%
		sc("3", "correction_pattern", "agree"),    // corr 100%
		sc("4", "direction_pattern", "partial"),   // dir 50%
	}
	predicted := map[string]string{"1": "tech_preference", "2": "decision_heuristic", "3": "correction_pattern", "4": "direction_pattern"}
	s := Sweep(scored, predicted)
	if len(s) != 4 {
		t.Fatalf("want 4 cumulative policies, got %d", len(s))
	}
	// first policy = single most-reliable category; coverage grows monotonically
	if len(s[0].Policy) != 1 {
		t.Fatalf("first sweep step should be one category, got %v", s[0].Policy)
	}
	for i := 1; i < len(s); i++ {
		if s[i].CoveragePred < s[i-1].CoveragePred {
			t.Fatalf("coverage must be non-decreasing across sweep, step %d", i)
		}
	}
	// last policy covers all 4 known categories
	if s[3].AutoPred != 4 {
		t.Fatalf("final sweep step should auto-answer all 4, got %d", s[3].AutoPred)
	}
}
