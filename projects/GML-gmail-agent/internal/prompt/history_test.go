package prompt

import (
	"strings"
	"testing"

	"github.com/topolik/gml-gmail-agent/internal/behavior"
)

func TestBuildHistory_WithData(t *testing.T) {
	senders := []behavior.SenderBehavior{
		{Email: "alice@example.com", TotalEmails: 10, ReadCount: 9, ReadRate: 0.9, ThreadCount: 5, RepliedThreads: 4, ReplyRate: 0.8},
		{Email: "bot@example.com", TotalEmails: 50, ReadCount: 5, ReadRate: 0.1, ThreadCount: 50, RepliedThreads: 0, ReplyRate: 0.0},
	}
	dismissed := "[2026-05-20 dismissed:2026-05-21] Q2 Security alert\n  Comment: \"handled\"\n"
	rules := "- archive_by_sender: notifications@github.com\n"
	prevInsights := "[Insight: ignore_pattern] test insight\n"

	knowledge := "- gmail_search: from:jira@tracker.example.com\n  status: confirmed\n"

	result := BuildHistory(senders, dismissed, rules, prevInsights, knowledge)

	checks := []string{
		"<behavior_data>",
		"alice@example.com",
		"bot@example.com",
		"</behavior_data>",
		"<notification_history>",
		"</notification_history>",
		"<active_rules>",
		"notifications@github.com",
		"</active_rules>",
		"<previous_insights>",
		"test insight",
		"</previous_insights>",
		"<knowledge>",
		"from:jira@tracker.example.com",
		"</knowledge>",
		"90%",
		"10%",
	}
	for _, c := range checks {
		if !strings.Contains(result, c) {
			t.Errorf("missing %q in output", c)
		}
	}
}

func TestBuildHistory_EmptyData(t *testing.T) {
	result := BuildHistory(nil, "", "", "", "")

	checks := []string{
		"No sender data available",
		"No dismissed notifications",
		"No active rules configured",
		"No previous insights available",
		"No knowledge base yet",
	}
	for _, c := range checks {
		if !strings.Contains(result, c) {
			t.Errorf("missing %q in output", c)
		}
	}
}

func TestBuildHistory_DatamarksComments(t *testing.T) {
	dismissed := "test comment here"
	result := BuildHistory(nil, dismissed, "", "", "")

	if !strings.Contains(result, "") {
		t.Error("dismissed notification comments should be datamarked")
	}
}
