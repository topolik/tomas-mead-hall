package brain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/topolik/mnd-mind-model/internal/distill"
)

func fixtureInsights() []distill.Insight {
	return []distill.Insight{
		{ID: "aaa111", Category: "decision_heuristic", Statement: "Run it or it doesn't count.",
			Strength: "strong", Occurrences: 5, Evidence: []distill.Evidence{{Moment: "m1", Quote: "actually run the solution"}}},
		{ID: "bbb222", Category: "tech_preference", Statement: "Prefer Go for agent services.",
			Strength: "moderate", Occurrences: 2, Evidence: []distill.Evidence{{Moment: "m2"}}},
	}
}

// insights.yaml round-trip.
func TestSaveLoadInsights(t *testing.T) {
	path := filepath.Join(t.TempDir(), "insights.yaml")
	if err := SaveInsights(path, fixtureInsights()); err != nil {
		t.Fatal(err)
	}
	f, err := LoadInsights(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Insights) != 2 || f.Updated == "" {
		t.Fatalf("round-trip lost data: %+v", f)
	}
	if f.Insights[0].Statement != "Run it or it doesn't count." {
		t.Fatalf("order/content wrong: %+v", f.Insights[0])
	}
}

// Missing file = empty brain, not an error.
func TestLoadMissingInsights(t *testing.T) {
	f, err := LoadInsights(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil || len(f.Insights) != 0 {
		t.Fatalf("missing file should be empty brain: %v %+v", err, f)
	}
}

// T26: processed ledger round-trip, append merges, missing file = empty.
func TestProcessedLedger(t *testing.T) {
	path := filepath.Join(t.TempDir(), "processed.yaml")
	if done, err := LoadProcessed(path); err != nil || len(done) != 0 {
		t.Fatalf("missing ledger must be empty: %v %v", err, done)
	}
	if err := AppendProcessed(path, []string{"m1", "m2"}); err != nil {
		t.Fatal(err)
	}
	if err := AppendProcessed(path, []string{"m2", "m3"}); err != nil {
		t.Fatal(err)
	}
	done, err := LoadProcessed(path)
	if err != nil || len(done) != 3 || !done["m1"] || !done["m3"] {
		t.Fatalf("ledger wrong after appends: %v %v", err, done)
	}
}

// T15: profile prompt groups by category and carries ids + quotes.
func TestBuildProfilePrompt(t *testing.T) {
	p := BuildProfilePrompt(fixtureInsights())
	for _, want := range []string{"id=aaa111", "id=bbb222", "Run it or it doesn't count.", `"actually run the solution"`, "<insights>"} {
		if !strings.Contains(p, want) {
			t.Fatalf("prompt missing %q", want)
		}
	}
}

// T15: profile response written to three files; empty profile is an error.
func TestWriteProfiles(t *testing.T) {
	dir := t.TempDir()
	resp := `Sure! {"decision_making": "# Decisions\nRun it. (aaa111)", "technical_preferences": "# Tech\nGo. (bbb222)", "direction_style": "# Direction\nQ2 default."}`
	files, err := WriteProfiles(resp, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Fatalf("want 3 files, got %v", files)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "decision-making.md"))
	if !strings.Contains(string(data), "Run it. (aaa111)") {
		t.Fatalf("content wrong: %s", data)
	}

	if _, err := WriteProfiles(`{"decision_making": "x", "technical_preferences": "", "direction_style": "y"}`, t.TempDir()); err == nil {
		t.Fatal("empty profile must error")
	}

	combined, err := LoadProfiles(dir)
	if err != nil || !strings.Contains(combined, "technical-preferences.md") {
		t.Fatalf("LoadProfiles: %v", err)
	}
}

// T48: gemini emits RAW newlines inside JSON string values (invalid JSON);
// WriteProfiles must repair and parse them instead of failing the retrain.
func TestWriteProfilesRawNewlines(t *testing.T) {
	dir := t.TempDir()
	// real 0x0A newlines inside each string value (double-quoted Go string)
	resp := "{\"decision_making\": \"# Decisions\n\nRun it.\tNow. (aaa111)\", " +
		"\"technical_preferences\": \"# Tech\nGo.\", " +
		"\"direction_style\": \"# Direction\nQ2 default.\"}"
	files, err := WriteProfiles(resp, dir)
	if err != nil {
		t.Fatalf("raw-newline JSON should be repaired and parsed, got: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("want 3 files, got %v", files)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "decision-making.md"))
	if !strings.Contains(string(data), "Run it.") || !strings.Contains(string(data), "(aaa111)") {
		t.Fatalf("repaired content wrong: %s", data)
	}
	// the repaired newline must be a real line break in the written file
	if !strings.Contains(string(data), "# Decisions\n") {
		t.Fatalf("escaped newline should round-trip to a real newline in the .md: %q", string(data))
	}
}
