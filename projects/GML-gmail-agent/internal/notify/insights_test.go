package notify

import (
	"strings"
	"testing"
)

func TestInsightDedupKey(t *testing.T) {
	// The grafana insight recurred with and without "+newer_than:7d" — these
	// must collapse to one key, which the old raw-link equality did not do.
	withWindow := GmailSearchURL(`from:grafana@monitoring.example.com subject:"ETD Bucket Permission Changes" newer_than:7d`)
	without := GmailSearchURL(`from:grafana@monitoring.example.com subject:"ETD Bucket Permission Changes"`)
	if InsightDedupKey(withWindow) != InsightDedupKey(without) {
		t.Errorf("time-window variants should collapse:\n  %q\n  %q", InsightDedupKey(withWindow), InsightDedupKey(without))
	}

	// Term order should not matter.
	a := GmailSearchURL(`from:x@y.com subject:alert`)
	b := GmailSearchURL(`subject:alert from:x@y.com`)
	if InsightDedupKey(a) != InsightDedupKey(b) {
		t.Errorf("term order should not affect key: %q vs %q", InsightDedupKey(a), InsightDedupKey(b))
	}

	// A genuinely different sender must NOT collapse.
	if InsightDedupKey(GmailSearchURL(`from:a@x.com`)) == InsightDedupKey(GmailSearchURL(`from:b@x.com`)) {
		t.Error("different senders must not share a dedup key")
	}

	// A different subject phrase is preserved (semantic — left to the learn LLM).
	s1 := GmailSearchURL(`from:security@example.com subject:"Spike in security events"`)
	s2 := GmailSearchURL(`from:security@example.com subject:"Spike in security events for liferay.com"`)
	if InsightDedupKey(s1) == InsightDedupKey(s2) {
		t.Error("different subject phrases should NOT be treated as the same insight")
	}
}

func TestParseAndValidateInsights_Valid(t *testing.T) {
	input := `[
		{
			"pattern": "Always replies to security alerts",
			"evidence": "12/15 security threads have SENT reply",
			"signal_strength": "strong",
			"category": "reply_pattern",
			"affected_senders": ["security@example.com"],
			"suggested_action": "Consider auto-starring security emails for immediate visibility",
			"gmail_search": "from:security@example.com"
		}
	]`
	results, err := ParseAndValidateInsights([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Pattern != "Always replies to security alerts" {
		t.Errorf("pattern: got %q", results[0].Pattern)
	}
}

func TestParseAndValidateInsights_CodeFence(t *testing.T) {
	input := "```json\n" + `[{"pattern":"test","evidence":"e","signal_strength":"weak","category":"ignore_pattern","affected_senders":["a@b.com"],"suggested_action":"archive","gmail_search":"from:a@b.com"}]` + "\n```"
	results, err := ParseAndValidateInsights([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
}

func TestParseAndValidateInsights_Preamble(t *testing.T) {
	input := "Here's the analysis:\n" + `[{"pattern":"test","evidence":"e","signal_strength":"moderate","category":"archive_candidate","affected_senders":["a@b.com"],"suggested_action":"archive","gmail_search":"from:a@b.com"}]`
	results, err := ParseAndValidateInsights([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
}

func TestParseAndValidateInsights_InvalidSignalStrength(t *testing.T) {
	input := `[{"pattern":"test","evidence":"e","signal_strength":"very_strong","category":"reply_pattern","affected_senders":["a@b.com"],"suggested_action":"do it","gmail_search":"from:a@b.com"}]`
	_, err := ParseAndValidateInsights([]byte(input))
	if err == nil || !strings.Contains(err.Error(), "signal_strength") {
		t.Errorf("expected signal_strength error, got %v", err)
	}
}

func TestParseAndValidateInsights_InvalidCategory(t *testing.T) {
	input := `[{"pattern":"test","evidence":"e","signal_strength":"strong","category":"unknown_cat","affected_senders":["a@b.com"],"suggested_action":"do it","gmail_search":"from:a@b.com"}]`
	_, err := ParseAndValidateInsights([]byte(input))
	if err == nil || !strings.Contains(err.Error(), "category") {
		t.Errorf("expected category error, got %v", err)
	}
}

func TestParseAndValidateInsights_MissingPattern(t *testing.T) {
	input := `[{"pattern":"","evidence":"e","signal_strength":"strong","category":"reply_pattern","affected_senders":["a@b.com"],"suggested_action":"do it","gmail_search":"from:a@b.com"}]`
	_, err := ParseAndValidateInsights([]byte(input))
	if err == nil || !strings.Contains(err.Error(), "pattern is required") {
		t.Errorf("expected pattern error, got %v", err)
	}
}

func TestParseAndValidateInsights_EmptySenders(t *testing.T) {
	input := `[{"pattern":"test","evidence":"e","signal_strength":"strong","category":"reply_pattern","affected_senders":[],"suggested_action":"do it","gmail_search":"from:a@b.com"}]`
	_, err := ParseAndValidateInsights([]byte(input))
	if err == nil || !strings.Contains(err.Error(), "affected_senders") {
		t.Errorf("expected affected_senders error, got %v", err)
	}
}

func TestParseAndValidateInsights_EmptyInput(t *testing.T) {
	_, err := ParseAndValidateInsights([]byte(""))
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestInsightsToNotifications(t *testing.T) {
	insights := []InsightAnalysis{
		{
			Pattern:         "Ignores JIRA notifications",
			Evidence:        "0/50 JIRA emails replied",
			SignalStrength:  "strong",
			Category:        "ignore_pattern",
			AffectedSenders: []string{"jira@tracker.example.com", "confluence@tracker.example.com"},
			SuggestedAction: "Consider auto-archiving JIRA notifications older than 3 days",
			GmailSearch:     "from:jira@tracker.example.com OR from:confluence@tracker.example.com",
		},
	}

	notifs := InsightsToNotifications(insights)
	if len(notifs) != 1 {
		t.Fatalf("got %d notifs, want 1", len(notifs))
	}

	n := notifs[0]
	if n.ProjectCode != "GML" {
		t.Errorf("project_code: got %q want GML", n.ProjectCode)
	}
	if !strings.Contains(n.Message, "[Insight: ignore_pattern]") {
		t.Errorf("message missing category: %q", n.Message)
	}
	if !strings.Contains(n.Message, "Ignores JIRA notifications") {
		t.Errorf("message missing pattern: %q", n.Message)
	}
	if n.Priority != "Q3" {
		t.Errorf("priority: got %q want Q3 (strong signal)", n.Priority)
	}
	if n.Type != "info" {
		t.Errorf("type: got %q want info", n.Type)
	}
	if !strings.Contains(n.Message, "Consider auto-archiving") {
		t.Errorf("message missing suggested action: %q", n.Message)
	}
	if !strings.Contains(n.Link, "from%3Ajira%40tracker.example.com") {
		t.Errorf("link should use gmail_search, got %q", n.Link)
	}
}

func TestInsightsToNotifications_SingleSender(t *testing.T) {
	insights := []InsightAnalysis{
		{
			Pattern:         "Always reads HR emails",
			Evidence:        "100% read rate",
			SignalStrength:  "moderate",
			Category:        "priority_pattern",
			AffectedSenders: []string{"hr@company.com"},
			SuggestedAction: "Mark as important",
			GmailSearch:     "from:hr@company.com",
		},
	}

	notifs := InsightsToNotifications(insights)
	if len(notifs) != 1 {
		t.Fatalf("got %d notifs, want 1", len(notifs))
	}
	if notifs[0].Priority != "Q4" {
		t.Errorf("priority: got %q want Q4 (moderate signal)", notifs[0].Priority)
	}
}

func TestParseAndValidateInsights_MissingGmailSearch(t *testing.T) {
	input := `[{"pattern":"test","evidence":"e","signal_strength":"strong","category":"reply_pattern","affected_senders":["a@b.com"],"suggested_action":"do it"}]`
	_, err := ParseAndValidateInsights([]byte(input))
	if err == nil || !strings.Contains(err.Error(), "gmail_search") {
		t.Errorf("expected gmail_search error, got %v", err)
	}
}
