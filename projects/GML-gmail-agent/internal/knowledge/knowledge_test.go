package knowledge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSaveRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "knowledge.yaml")

	kf := &KnowledgeFile{
		LastDistilledAt: "2026-05-30T12:00:00Z",
		Patterns: []Pattern{
			{
				GmailSearch:    "from:jira@tracker.example.com",
				Pattern:        "Ignores JIRA notifications",
				Status:         "confirmed",
				Category:       "ignore_pattern",
				Senders:        []string{"jira@tracker.example.com"},
				FirstSeen:      "2026-05-29",
				LastUpdated:    "2026-05-30",
				CommentSummary: "Confirmed: these are noise",
			},
		},
	}

	if err := Save(path, kf); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(loaded.Patterns) != 1 {
		t.Fatalf("got %d patterns, want 1", len(loaded.Patterns))
	}
	p := loaded.Patterns[0]
	if p.GmailSearch != "from:jira@tracker.example.com" {
		t.Errorf("gmail_search: got %q", p.GmailSearch)
	}
	if p.Status != "confirmed" {
		t.Errorf("status: got %q", p.Status)
	}
	if p.CommentSummary != "Confirmed: these are noise" {
		t.Errorf("comment_summary: got %q", p.CommentSummary)
	}
	if loaded.LastDistilledAt != "2026-05-30T12:00:00Z" {
		t.Errorf("last_distilled_at: got %q", loaded.LastDistilledAt)
	}
}

func TestLoadNonexistent(t *testing.T) {
	kf, err := Load("/nonexistent/knowledge.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(kf.Patterns) != 0 {
		t.Errorf("expected empty patterns for nonexistent file, got %d", len(kf.Patterns))
	}
}

func TestUpsertNew(t *testing.T) {
	kf := &KnowledgeFile{}
	kf.Upsert(Pattern{
		GmailSearch: "from:bot@example.com",
		Pattern:     "Ignores bot emails",
		Status:      "confirmed",
		FirstSeen:   "2026-05-30",
		LastUpdated: "2026-05-30",
	})

	if len(kf.Patterns) != 1 {
		t.Fatalf("got %d patterns, want 1", len(kf.Patterns))
	}
	if kf.Patterns[0].GmailSearch != "from:bot@example.com" {
		t.Errorf("gmail_search: got %q", kf.Patterns[0].GmailSearch)
	}
}

func TestUpsertExisting(t *testing.T) {
	kf := &KnowledgeFile{
		Patterns: []Pattern{
			{
				GmailSearch:    "from:bot@example.com",
				Pattern:        "Ignores bot emails",
				Status:         "confirmed",
				FirstSeen:      "2026-05-28",
				LastUpdated:    "2026-05-29",
				CommentSummary: "old comment",
			},
		},
	}

	kf.Upsert(Pattern{
		GmailSearch:    "from:bot@example.com",
		Pattern:        "Ignores bot emails — updated",
		Status:         "refined",
		FirstSeen:      "2026-05-30",
		LastUpdated:    "2026-05-30",
		CommentSummary: "new comment",
		RefinedAction:  "auto-archive after 1 day",
	})

	if len(kf.Patterns) != 1 {
		t.Fatalf("got %d patterns, want 1 (should update, not add)", len(kf.Patterns))
	}
	p := kf.Patterns[0]
	if p.Pattern != "Ignores bot emails — updated" {
		t.Errorf("pattern not updated: got %q", p.Pattern)
	}
	if p.FirstSeen != "2026-05-28" {
		t.Errorf("first_seen should be preserved: got %q, want 2026-05-28", p.FirstSeen)
	}
	if p.CommentSummary != "new comment" {
		t.Errorf("comment_summary not updated: got %q", p.CommentSummary)
	}
	if p.RefinedAction != "auto-archive after 1 day" {
		t.Errorf("refined_action: got %q", p.RefinedAction)
	}
}

func TestValidatePattern(t *testing.T) {
	valid := Pattern{
		GmailSearch: "from:a@b.com",
		Pattern:     "test",
		Status:      "confirmed",
	}
	if err := ValidatePattern(valid); err != nil {
		t.Errorf("expected valid, got %v", err)
	}

	noSearch := Pattern{Pattern: "test", Status: "confirmed"}
	if err := ValidatePattern(noSearch); err == nil || !strings.Contains(err.Error(), "gmail_search") {
		t.Errorf("expected gmail_search error, got %v", err)
	}

	badStatus := Pattern{GmailSearch: "from:a@b.com", Pattern: "test", Status: "unknown"}
	if err := ValidatePattern(badStatus); err == nil || !strings.Contains(err.Error(), "status") {
		t.Errorf("expected status error, got %v", err)
	}

	refinedNoAction := Pattern{GmailSearch: "from:a@b.com", Pattern: "test", Status: "refined"}
	if err := ValidatePattern(refinedNoAction); err == nil || !strings.Contains(err.Error(), "refined_action") {
		t.Errorf("expected refined_action error, got %v", err)
	}
}

func TestFormat(t *testing.T) {
	kf := &KnowledgeFile{
		Patterns: []Pattern{
			{
				GmailSearch:    "from:jira@tracker.example.com",
				Pattern:        "Ignores JIRA",
				Status:         "confirmed",
				CommentSummary: "noise",
			},
			{
				GmailSearch:   "from:hr@company.com",
				Pattern:       "Reads HR emails",
				Status:        "refined",
				RefinedAction: "star instead of archive",
			},
		},
	}

	result := Format(kf)
	if !strings.Contains(result, "[confirmed]") {
		t.Error("missing [confirmed]")
	}
	if !strings.Contains(result, "[refined]") {
		t.Error("missing [refined]")
	}
	if !strings.Contains(result, "from:jira@tracker.example.com") {
		t.Error("missing gmail_search")
	}
	if !strings.Contains(result, "Refined action:") {
		t.Error("missing refined action")
	}
}

func TestFormatEmpty(t *testing.T) {
	kf := &KnowledgeFile{}
	if Format(kf) != "" {
		t.Error("expected empty string for empty knowledge")
	}
}

func TestSaveCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "knowledge.yaml")

	kf := &KnowledgeFile{Patterns: []Pattern{{
		GmailSearch: "from:test@example.com",
		Pattern:     "test",
		Status:      "confirmed",
	}}}

	if err := Save(path, kf); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("file not created: %v", err)
	}
}
