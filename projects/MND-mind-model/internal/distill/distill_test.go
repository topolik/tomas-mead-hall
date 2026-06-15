package distill

import (
	"strings"
	"testing"

	"github.com/topolik/mnd-mind-model/internal/moment"
)

func sampleMoments(n int) []moment.Moment {
	ms := make([]moment.Moment, n)
	for i := range ms {
		ms[i] = moment.Moment{
			ID: moment.NewID("claude", "s1", string(rune('a'+i)), "text"), Source: "claude",
			Project: "p", Session: "s1", TS: "2026-06-01T00:00:00Z",
			Context: "assistant was proposing postgres",
			Text:    "no, keep it simple - flat yaml file",
		}
	}
	return ms
}

// T11: batching with stable IDs; prompt contains schema and datamarked moments.
func TestMakeBatchesAndPrompt(t *testing.T) {
	ms := sampleMoments(95)
	bs := MakeBatches(ms, 40)
	if len(bs) != 3 {
		t.Fatalf("want 3 batches, got %d", len(bs))
	}
	if bs[0].ID != "batch-001" || bs[2].ID != "batch-003" {
		t.Fatalf("unstable ids: %s %s", bs[0].ID, bs[2].ID)
	}
	if len(bs[2].Moments) != 15 {
		t.Fatalf("last batch size: %d", len(bs[2].Moments))
	}

	p := BuildPrompt(bs[0])
	if !strings.Contains(p, "OUTPUT SCHEMA") || !strings.Contains(p, "<moments>") {
		t.Fatal("prompt missing schema or moments block")
	}
	if !strings.Contains(p, "TOMAS: no,"+datamarker+"keep") {
		t.Fatal("moment text not datamarked")
	}
	if !strings.Contains(p, "id="+ms[0].ID) {
		t.Fatal("moment id missing from prompt")
	}
}

// T12+T14: resilient parse — prose-wrapped JSON accepted, bad items dropped
// without sinking the batch, unknown moment ids rejected.
func TestParseResponseResilient(t *testing.T) {
	ms := sampleMoments(1)
	known := map[string]moment.Moment{ms[0].ID: ms[0]}

	resp := "Here's the analysis:\n```json\n" + `{
	  "insights": [
	    {"category": "decision_heuristic", "statement": "Prefer flat files over databases for small data.", "context": "storage choices", "strength": "strong",
	     "evidence": [{"moment_id": "` + ms[0].ID + `", "quote": "keep it simple - flat yaml file"}]},
	    {"category": "bogus_category", "statement": "x", "strength": "strong", "evidence": [{"moment_id": "` + ms[0].ID + `", "quote": "q"}]},
	    {"category": "tech_preference", "statement": "", "strength": "weak", "evidence": [{"moment_id": "` + ms[0].ID + `", "quote": "q"}]},
	    {"category": "tech_preference", "statement": "Hallucinated.", "strength": "odd", "evidence": [{"moment_id": "doesnotexist", "quote": "q"}]}
	  ]
	}` + "\n```\nHope this helps!"

	insights, dropped, err := ParseResponse(resp, known)
	if err != nil {
		t.Fatal(err)
	}
	if len(insights) != 1 {
		t.Fatalf("want 1 valid insight, got %d", len(insights))
	}
	if len(dropped) != 3 {
		t.Fatalf("want 3 dropped, got %d: %v", len(dropped), dropped)
	}
	in := insights[0]
	if in.ID == "" || in.Category != "decision_heuristic" || in.Evidence[0].TS == "" {
		t.Fatalf("bad insight: %+v", in)
	}
}

// T12: a response with no JSON at all is an error (caller retries/skips batch).
func TestParseResponseNoJSON(t *testing.T) {
	if _, _, err := ParseResponse("I could not process this.", nil); err == nil {
		t.Fatal("want error for JSON-free response")
	}
}

// T13: identity-keyed merge — same statement folds, evidence appended once,
// strength upgraded, distinct statements kept.
func TestMerge(t *testing.T) {
	a := Insight{ID: IdentityKey("tech_preference", "Prefer Go for agent services."),
		Category: "tech_preference", Statement: "Prefer Go for agent services.",
		Strength: "moderate", Occurrences: 1, Evidence: []Evidence{{Moment: "m1"}}}
	b := a
	b.Strength = "strong"
	b.Evidence = []Evidence{{Moment: "m1"}, {Moment: "m2"}}
	c := Insight{ID: IdentityKey("decision_heuristic", "Run it or it doesn't count."),
		Category: "decision_heuristic", Statement: "Run it or it doesn't count.",
		Strength: "strong", Occurrences: 1, Evidence: []Evidence{{Moment: "m3"}}}

	merged := Merge([]Insight{a}, []Insight{b, c})
	if len(merged) != 2 {
		t.Fatalf("want 2 insights, got %d", len(merged))
	}
	got := merged[0]
	if got.Occurrences != 2 || len(got.Evidence) != 2 || got.Strength != "strong" {
		t.Fatalf("merge wrong: %+v", got)
	}
	// identity key normalizes case/whitespace/trailing period
	if IdentityKey("x", "Prefer  Go for agent services") != IdentityKey("x", "prefer go for agent services.") {
		t.Fatal("identity key not normalized")
	}
}
