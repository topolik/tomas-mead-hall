package prompt

import (
	"strings"
	"testing"
)

func TestBuildDistill_WithData(t *testing.T) {
	dismissed := "[2026-05-29 dismissed:2026-05-30] Q3 [Insight: ignore_pattern] Ignores JIRA\n  Link: https://mail.google.com/mail/u/0/#search/from%3Ajira%40example.com\n  Comment: \"yes, archive these\"\n"
	existing := "- [confirmed] Old pattern (gmail: from:old@example.com)\n"

	result := BuildDistill(dismissed, existing)

	checks := []string{
		"<dismissed_insights>",
		"</dismissed_insights>",
		"<existing_knowledge>",
		"Old pattern",
		"</existing_knowledge>",
		"Link:",
		"\"patterns\":",
		"\"todos\":",
	}
	for _, c := range checks {
		if !strings.Contains(result, c) {
			t.Errorf("missing %q in output", c)
		}
	}
	if !strings.Contains(result, "") {
		t.Error("dismissed insights should be datamarked")
	}
}

func TestBuildDistill_Empty(t *testing.T) {
	result := BuildDistill("", "")

	if !strings.Contains(result, "No dismissed insights") {
		t.Error("missing empty dismissed message")
	}
	if !strings.Contains(result, "No existing knowledge yet") {
		t.Error("missing empty knowledge message")
	}
}

func TestBuildDistill_ActionItemInstructions(t *testing.T) {
	result := BuildDistill("test", "")

	if !strings.Contains(result, "ACTION ITEM") {
		t.Error("prompt should explain action items vs pattern feedback")
	}
	if !strings.Contains(result, "project_code") {
		t.Error("prompt should include project_code field for todos")
	}
}

func TestBuildDistill_GmailOperators(t *testing.T) {
	result := BuildDistill("test", "")

	if !strings.Contains(result, "GMAIL OPERATORS REFERENCE") {
		t.Error("prompt should include Gmail operators reference")
	}
	if !strings.Contains(result, "filter") {
		t.Error("prompt should document filter field")
	}
	if !strings.Contains(result, "require_reply") {
		t.Error("prompt should document require_reply field")
	}
}
