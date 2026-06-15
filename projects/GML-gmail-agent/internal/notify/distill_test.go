package notify

import (
	"strings"
	"testing"
)

func TestParseAndValidateDistilled_Valid(t *testing.T) {
	input := `{
		"patterns": [
			{
				"gmail_search": "from:jira@tracker.example.com",
				"pattern": "Ignores JIRA notifications",
				"status": "confirmed",
				"category": "ignore_pattern",
				"senders": ["jira@tracker.example.com"],
				"comment_summary": "Confirmed: these are noise"
			}
		],
		"todos": []
	}`
	result, _, err := ParseAndValidateDistilled([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Patterns) != 1 {
		t.Fatalf("got %d patterns, want 1", len(result.Patterns))
	}
	if result.Patterns[0].GmailSearch != "from:jira@tracker.example.com" {
		t.Errorf("gmail_search: got %q", result.Patterns[0].GmailSearch)
	}
	if result.Patterns[0].Status != "confirmed" {
		t.Errorf("status: got %q", result.Patterns[0].Status)
	}
}

func TestParseAndValidateDistilled_Refined(t *testing.T) {
	input := `{"patterns": [{
		"gmail_search": "from:hr@company.com",
		"pattern": "Reads HR emails",
		"status": "refined",
		"category": "priority_pattern",
		"senders": ["hr@company.com"],
		"comment_summary": "Yes but only benefits emails",
		"refined_action": "star benefits emails, archive rest"
	}], "todos": []}`
	result, _, err := ParseAndValidateDistilled([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if result.Patterns[0].RefinedAction != "star benefits emails, archive rest" {
		t.Errorf("refined_action: got %q", result.Patterns[0].RefinedAction)
	}
}

// Lenient: a per-item problem drops/sanitizes that item and warns — it never
// fails the whole batch (one bad LLM item must not block all distillation).
func TestParseAndValidateDistilled_DropsInvalidItems(t *testing.T) {
	cases := []struct {
		name, pattern string
		wantWarnSub   string
	}{
		{"refined missing action", `{"gmail_search":"from:hr@company.com","pattern":"test","status":"refined","category":"priority_pattern","senders":["hr@company.com"],"comment_summary":"test"}`, "refined"},
		{"invalid status", `{"gmail_search":"from:a@b.com","pattern":"test","status":"maybe","category":"ignore_pattern","senders":["a@b.com"],"comment_summary":"test"}`, "invalid status"},
		{"missing gmail_search", `{"gmail_search":"","pattern":"test","status":"confirmed","category":"ignore_pattern","senders":["a@b.com"],"comment_summary":"test"}`, "gmail_search"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result, warnings, err := ParseAndValidateDistilled([]byte(`{"patterns": [` + c.pattern + `], "todos": []}`))
			if err != nil {
				t.Fatalf("should not hard-fail, got %v", err)
			}
			if len(result.Patterns) != 0 {
				t.Errorf("expected the bad pattern dropped, got %d patterns", len(result.Patterns))
			}
			if len(warnings) == 0 || !strings.Contains(strings.Join(warnings, " "), c.wantWarnSub) {
				t.Errorf("expected a warning containing %q, got %v", c.wantWarnSub, warnings)
			}
		})
	}
}

// A genuinely invalid optional filter (a bare unknown operator) must be stripped
// and the pattern KEPT (so the batch survives and the insight is still learned),
// not sink the whole batch. (A quoted phrase with a colon like
// "principal_email: x" is valid — see gmail.TestValidateQueryValid — and is
// preserved, not stripped.)
func TestParseAndValidateDistilled_StripsInvalidFilterKeepsPattern(t *testing.T) {
	input := `{"patterns": [
		{"gmail_search":"from:logs@gserviceaccount.com","pattern":"GCP audit logs","status":"confirmed","category":"archive_candidate","senders":["logs@gserviceaccount.com"],"comment_summary":"noise","filter":"bogusop:lcp-api"},
		{"gmail_search":"from:jira@tracker.example.com","pattern":"jira","status":"confirmed","category":"ignore_pattern","senders":["jira@tracker.example.com"],"comment_summary":"noise"}
	], "todos": []}`
	result, warnings, err := ParseAndValidateDistilled([]byte(input))
	if err != nil {
		t.Fatalf("must not hard-fail on a bad filter, got %v", err)
	}
	if len(result.Patterns) != 2 {
		t.Fatalf("expected both patterns kept (batch survives), got %d", len(result.Patterns))
	}
	if result.Patterns[0].Filter != "" {
		t.Errorf("invalid filter should be stripped, got %q", result.Patterns[0].Filter)
	}
	if len(warnings) == 0 || !strings.Contains(strings.Join(warnings, " "), "stripped invalid filter") {
		t.Errorf("expected a 'stripped invalid filter' warning, got %v", warnings)
	}
}

func TestParseAndValidateDistilled_EmptyResult(t *testing.T) {
	input := `{"patterns": [], "todos": []}`
	result, _, err := ParseAndValidateDistilled([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Patterns) != 0 {
		t.Errorf("expected 0 patterns, got %d", len(result.Patterns))
	}
	if len(result.Todos) != 0 {
		t.Errorf("expected 0 todos, got %d", len(result.Todos))
	}
}

func TestParseAndValidateDistilled_CodeFence(t *testing.T) {
	input := "```json\n" + `{"patterns":[{"gmail_search":"from:a@b.com","pattern":"test","status":"confirmed","category":"ignore_pattern","senders":["a@b.com"],"comment_summary":"ok"}],"todos":[]}` + "\n```"
	result, _, err := ParseAndValidateDistilled([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Patterns) != 1 {
		t.Fatalf("got %d patterns, want 1", len(result.Patterns))
	}
}

func TestDistilledToKnowledge(t *testing.T) {
	distilled := []DistilledPattern{
		{
			GmailSearch:    "from:jira@tracker.example.com",
			Pattern:        "Ignores JIRA",
			Status:         "confirmed",
			Category:       "ignore_pattern",
			Senders:        []string{"jira@tracker.example.com"},
			CommentSummary: "noise",
		},
	}
	patterns := DistilledToKnowledge(distilled)
	if len(patterns) != 1 {
		t.Fatalf("got %d patterns, want 1", len(patterns))
	}
	p := patterns[0]
	if p.GmailSearch != "from:jira@tracker.example.com" {
		t.Errorf("gmail_search: got %q", p.GmailSearch)
	}
	if p.Status != "confirmed" {
		t.Errorf("status: got %q", p.Status)
	}
	if p.FirstSeen == "" {
		t.Error("first_seen should be set")
	}
}

func TestParseAndValidateDistilled_WithTodos(t *testing.T) {
	input := `{
		"patterns": [{
			"gmail_search": "from:jira@tracker.example.com",
			"pattern": "Ignores JIRA",
			"status": "confirmed",
			"category": "ignore_pattern",
			"senders": ["jira@tracker.example.com"],
			"comment_summary": "archive these"
		}],
		"todos": [{
			"text": "Implement JIRA notification filter in Liferay",
			"priority": "Q2",
			"project_code": "GML"
		}]
	}`
	result, _, err := ParseAndValidateDistilled([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Patterns) != 1 {
		t.Fatalf("got %d patterns, want 1", len(result.Patterns))
	}
	if len(result.Todos) != 1 {
		t.Fatalf("got %d todos, want 1", len(result.Todos))
	}
	if result.Todos[0].Text != "Implement JIRA notification filter in Liferay" {
		t.Errorf("todo text: got %q", result.Todos[0].Text)
	}
	if result.Todos[0].Priority != "Q2" {
		t.Errorf("todo priority: got %q", result.Todos[0].Priority)
	}
	if result.Todos[0].ProjectCode != "GML" {
		t.Errorf("todo project_code: got %q", result.Todos[0].ProjectCode)
	}
}

func TestParseAndValidateDistilled_TodoMissingText(t *testing.T) {
	input := `{"patterns": [], "todos": [{"text": "", "priority": "Q1"}]}`
	result, warnings, err := ParseAndValidateDistilled([]byte(input))
	if err != nil {
		t.Fatalf("should not hard-fail, got %v", err)
	}
	if len(result.Todos) != 0 {
		t.Errorf("expected the text-less todo dropped, got %d", len(result.Todos))
	}
	if len(warnings) == 0 || !strings.Contains(strings.Join(warnings, " "), "text") {
		t.Errorf("expected a text warning, got %v", warnings)
	}
}

func TestParseAndValidateDistilled_TodoInvalidPriority(t *testing.T) {
	input := `{"patterns": [], "todos": [{"text": "test", "priority": "HIGH"}]}`
	result, warnings, err := ParseAndValidateDistilled([]byte(input))
	if err != nil {
		t.Fatalf("should not hard-fail, got %v", err)
	}
	if len(result.Todos) != 1 || result.Todos[0].Priority != "" {
		t.Errorf("expected the todo kept with priority reset to default, got %+v", result.Todos)
	}
	if len(warnings) == 0 || !strings.Contains(strings.Join(warnings, " "), "priority") {
		t.Errorf("expected a priority warning, got %v", warnings)
	}
}

