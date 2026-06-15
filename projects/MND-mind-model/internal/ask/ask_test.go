package ask

import (
	"strings"
	"testing"

	"github.com/topolik/mnd-mind-model/internal/distill"
)

func corpus() []distill.Insight {
	return []distill.Insight{
		{ID: "go1", Category: "tech_preference", Strength: "strong", Occurrences: 4,
			Statement: "Prefer Go for new agent services.",
			Evidence:  []distill.Evidence{{Moment: "m1", Quote: "write it in go like the others"}}},
		{ID: "em1", Category: "direction_pattern", Strength: "moderate", Occurrences: 2,
			Statement: "Escalate external security reports unanswered for 7 days.",
			Evidence:  []distill.Evidence{{Moment: "m2", Quote: "7-day no-response escalation"}}},
		{ID: "ki1", Category: "decision_heuristic", Strength: "strong", Occurrences: 6,
			Statement: "Choose the simplest solution that works; polish later.",
			Evidence:  []distill.Evidence{{Moment: "m3", Quote: "KISS"}}},
	}
}

// T16: relevant insight outranks unrelated ones.
func TestBM25Ranking(t *testing.T) {
	idx := NewIndex(corpus())
	top := idx.Top("which language should the new agent service use", 2)
	if len(top) == 0 || top[0].ID != "go1" {
		t.Fatalf("want go1 first, got %+v", top)
	}
	for _, in := range top {
		if in.ID == "em1" {
			t.Fatalf("escalation insight should not match a language question: %+v", top)
		}
	}
	if got := idx.Top("zebra xylophone", 3); len(got) != 0 {
		t.Fatalf("no-match query must return empty, got %+v", got)
	}
}

// T17: prompt carries profiles whole, evidence, and the question.
func TestBuildPrompt(t *testing.T) {
	p := BuildPrompt("## decision-making.md\n\nRun it.", corpus()[:1], "Postgres or flat file?")
	for _, want := range []string{"<profiles>", "Run it.", "id=go1", `"write it in go like the others"`, "<question>\nPostgres or flat file?"} {
		if !strings.Contains(p, want) {
			t.Fatalf("prompt missing %q", want)
		}
	}
}

// T17: answer JSON parses; junk confidence coerced; prose tolerated.
func TestParseAnswer(t *testing.T) {
	a, err := ParseAnswer(`Here you go: {"answer": "Flat yaml file. KISS — no DB until the data outgrows it.", "confidence": "high", "citations": ["ki1"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if a.Confidence != "high" || len(a.Citations) != 1 || !strings.Contains(a.Answer, "KISS") {
		t.Fatalf("bad answer: %+v", a)
	}

	a, err = ParseAnswer(`{"answer": "x", "confidence": "very sure"}`)
	if err != nil || a.Confidence != "low" {
		t.Fatalf("junk confidence not coerced: %+v %v", a, err)
	}

	if _, err := ParseAnswer("no json at all"); err == nil {
		t.Fatal("want error")
	}
	if _, err := ParseAnswer(`{"answer": "  ", "confidence": "high"}`); err == nil {
		t.Fatal("empty answer must error")
	}
}

// T35: pending question|none passes through; missing or junk coerces to
// "question" (old responses keep old behavior — confidence still gates).
func TestParseAnswerPending(t *testing.T) {
	for resp, want := range map[string]string{
		`{"answer": "x", "confidence": "high", "pending": "question"}`: "question",
		`{"answer": "x", "confidence": "high", "pending": "none"}`:     "none",
		`{"answer": "x", "confidence": "high"}`:                        "question",
		`{"answer": "x", "confidence": "high", "pending": "maybe"}`:    "question",
	} {
		a, err := ParseAnswer(resp)
		if err != nil {
			t.Fatalf("%s: %v", resp, err)
		}
		if a.Pending != want {
			t.Fatalf("%s: pending = %q, want %q", resp, a.Pending, want)
		}
	}
}

// T36: the prompt instructs the pending field and its tail-only semantics.
func TestBuildPromptPendingInstruction(t *testing.T) {
	p := BuildPrompt("profiles", nil, "q")
	for _, want := range []string{`"pending"`, "terminal tail", "no pending question"} {
		if !strings.Contains(p, want) {
			t.Fatalf("prompt missing %q", want)
		}
	}
}
