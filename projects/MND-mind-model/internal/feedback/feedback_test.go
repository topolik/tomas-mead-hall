package feedback

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/topolik/mnd-mind-model/internal/dsh"
	"github.com/topolik/mnd-mind-model/internal/exclude"
)

// T30: escalation message carries the qhash marker and clipped content;
// marker round-trips; same question → same hash; FindActive matches only
// active notifications.
func TestEscalationFormatAndIdentity(t *testing.T) {
	q := "Should the new agent service use Postgres or flat files for execution logs?"
	msg := FormatEscalation(q, strings.Repeat("long answer ", 100), "projects/MND-mind-model/data/escalations/x.txt")

	h := MarkerHash(msg)
	if h == "" || h != QHash(q) {
		t.Fatalf("marker round-trip failed: %q vs %q", h, QHash(q))
	}
	// Tight enough for the DSH UI (Tomas's feedback), no "Brain" wording,
	// and a pointer to the full text.
	if !strings.Contains(msg, "confidence: low") || len(msg) > 1100 {
		t.Fatalf("message format wrong (len %d): %.120s", len(msg), msg)
	}
	if strings.Contains(msg, "Brain") || !strings.Contains(msg, "Proposed direction:") {
		t.Fatalf("wording wrong: %.200s", msg)
	}
	if !strings.Contains(msg, "Full text: projects/MND-mind-model/data/escalations/x.txt") {
		t.Fatal("missing full-text pointer")
	}
	if QHash(q) != QHash("should the  new agent service use postgres or flat files for execution logs?") {
		t.Fatal("qhash must be normalization-stable")
	}

	notifs := []dsh.Previous{
		{ID: 1, Message: FormatEscalation(q, "x", "p"), DismissedAt: "2026-06-12"},
		{ID: 2, Message: FormatEscalation(q, "x", "p")},
		{ID: 3, Message: "[GML] unrelated"},
	}
	a := FindActive(notifs, QHash(q))
	if a == nil || a.ID != 2 {
		t.Fatalf("FindActive wrong: %+v", a)
	}
}

// T31: learn prompt marks comments authoritative and datamarks content;
// the whole prompt self-marks as pipeline content (exclusion bonus).
func TestBuildLearnPrompt(t *testing.T) {
	p := BuildLearnPrompt([]dsh.Previous{{ID: 7, Message: "[MND ask abc] q", Comment: "flat files, and stay on task", DismissedAt: "2026-06-12"}})
	for _, want := range []string{"id=7", "COMMENT (authoritative)", "OUTPUT SCHEMA", "<feedback>"} {
		if !strings.Contains(p, want) {
			t.Fatalf("prompt missing %q", want)
		}
	}
	if strings.Contains(p, "flat files, and stay on task") {
		t.Fatal("comment not datamarked")
	}
	if !exclude.IsPipelineContent(p) {
		t.Fatal("learn prompt must self-mark as pipeline content")
	}
}

// T32: learn response parsing — strong/feedback insights with dsh: evidence,
// per-item resilience, unknown notification ids rejected.
func TestParseLearnResponse(t *testing.T) {
	known := map[int64]dsh.Previous{7: {ID: 7, Comment: "flat files; also stay on the current task", DismissedAt: "2026-06-12T14:00:00Z"}}
	resp := `Sure: {"insights": [
	  {"category": "direction_pattern", "statement": "Finish the task at hand before fixing adjacent broken tooling.", "context": "mid-task discoveries", "notification_id": 7, "comment_quote": "stay on the current task"},
	  {"category": "bogus", "statement": "x", "notification_id": 7},
	  {"category": "tech_preference", "statement": "Hallucinated.", "notification_id": 99}
	]}`
	insights, dropped, err := ParseLearnResponse(resp, known)
	if err != nil {
		t.Fatal(err)
	}
	if len(insights) != 1 || len(dropped) != 2 {
		t.Fatalf("want 1 insight / 2 dropped, got %d/%d", len(insights), len(dropped))
	}
	in := insights[0]
	if in.Source != "feedback" || in.Strength != "strong" || in.Evidence[0].Moment != "dsh:7" || in.Evidence[0].TS == "" {
		t.Fatalf("provenance wrong: %+v", in)
	}

	if _, _, err := ParseLearnResponse("nope", known); err == nil {
		t.Fatal("want error for JSON-free response")
	}
}

// Ingest ledger round-trip.
func TestLedger(t *testing.T) {
	path := filepath.Join(t.TempDir(), "feedback-ledger.yaml")
	if done, err := LoadLedger(path); err != nil || len(done) != 0 {
		t.Fatalf("missing ledger must be empty: %v %v", err, done)
	}
	if err := AppendLedger(path, []int64{7, 9}); err != nil {
		t.Fatal(err)
	}
	if err := AppendLedger(path, []int64{9, 11}); err != nil {
		t.Fatal(err)
	}
	done, err := LoadLedger(path)
	if err != nil || len(done) != 3 || !done[7] || !done[11] {
		t.Fatalf("ledger wrong: %v %v", err, done)
	}
}
