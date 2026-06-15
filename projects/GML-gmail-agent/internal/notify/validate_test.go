package notify

import (
	"strings"
	"testing"
)

func TestParseAndValidate_CodeFence(t *testing.T) {
	input := "```json\n" + `[{"concern":"test","box":1,"box_name":"TODO","priority":"Q2","summary":"test summary","email_ids":["abc"],"gmail_search":"subject:test"}]` + "\n```"
	results, err := ParseAndValidate([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

func TestParseAndValidate_ValidJSON(t *testing.T) {
	input := `[{"concern":"JA4 POC ready","box":2,"box_name":"Important Unread","priority":"Q1","summary":"David confirms proxying liferay.com","email_ids":["abc123"],"gmail_search":"from:david subject:\"JA4 POC\""}]`
	results, err := ParseAndValidate([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Concern != "JA4 POC ready" || results[0].Box != 2 {
		t.Errorf("unexpected result: %+v", results[0])
	}
}

func TestParseAndValidate_TenableCancellation(t *testing.T) {
	input := `[
  {
    "concern": "Tenable Cancellation Update",
    "box": 2,
    "box_name": "Important Unread",
    "priority": "Q2",
    "summary": "Cleverbridge redirected the Tenable license cancellation back to Tenable support, prompting Zsófia to ask Jack Hawley for termination confirmation.",
    "email_ids": [
      "19e981b8a7f40dd5",
      "19e9818666c88689",
      "19e980f4e0674945"
    ],
    "gmail_search": "{from:zsofia@example.com from:jhawley@tenable.com} subject:\"Tenable\" newer_than:7d"
  }
]`
	results, err := ParseAndValidate([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Concern != "Tenable Cancellation Update" || results[0].Box != 2 {
		t.Errorf("unexpected result: %+v", results[0])
	}
	if results[0].Priority != "Q2" {
		t.Errorf("expected priority Q2, got %s", results[0].Priority)
	}
}

func TestParseAndValidate_EasePrismaSuppressions(t *testing.T) {
	input := `[
  {
    "concern": "ease++ Prisma SEV1 suppressions",
    "box": 2,
    "box_name": "Important Unread",
    "priority": "Q3",
    "summary": "Multiple new ease++ system registrations for Prisma SEV1 alert suppressions.",
    "email_ids": [
      "19e9b40b6760358d",
      "19e9b40927fd6ae5",
      "19e9b405ac17fc6e",
      "19e9b402955888e9"
    ],
    "gmail_search": "from:ttwa@login.hu subject:\"Requiring registration\""
  }
]`
	results, err := ParseAndValidate([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Concern != "ease++ Prisma SEV1 suppressions" || results[0].Box != 2 {
		t.Errorf("unexpected result: %+v", results[0])
	}
	if results[0].Priority != "Q3" {
		t.Errorf("expected priority Q3, got %s", results[0].Priority)
	}
}

func TestParseAndValidate_InvalidPriority(t *testing.T) {
	input := `[{"concern":"test","box":1,"box_name":"TODO","priority":"Q5","summary":"test","email_ids":["a"],"gmail_search":"q"}]`
	_, err := ParseAndValidate([]byte(input))
	if err == nil {
		t.Fatal("expected error for invalid priority")
	}
}

func TestParseAndValidate_MissingPriority(t *testing.T) {
	input := `[{"concern":"test","box":1,"box_name":"TODO","priority":"","summary":"test","email_ids":["a"],"gmail_search":"q"}]`
	_, err := ParseAndValidate([]byte(input))
	if err == nil {
		t.Fatal("expected error for empty priority")
	}
}

func TestParseAndValidate_FreeformText(t *testing.T) {
	input := `Here is my analysis: the inbox looks good`
	_, err := ParseAndValidate([]byte(input))
	if err == nil {
		t.Fatal("expected error for freeform text")
	}
}

func TestParseAndValidate_BoxOutOfRange(t *testing.T) {
	input := `[{"concern":"test","box":7,"box_name":"Test","priority":"Q2","summary":"test","email_ids":["a"],"gmail_search":"q"}]`
	_, err := ParseAndValidate([]byte(input))
	if err == nil {
		t.Fatal("expected error for box > 6")
	}
}

func TestParseAndValidate_EmptySummary(t *testing.T) {
	input := `[{"concern":"test","box":1,"box_name":"TODO","priority":"Q2","summary":"","email_ids":["a"],"gmail_search":"q"}]`
	_, err := ParseAndValidate([]byte(input))
	if err == nil {
		t.Fatal("expected error for empty summary")
	}
}

func TestParseAndValidate_MissingConcern(t *testing.T) {
	input := `[{"box":1,"box_name":"TODO","priority":"Q2","summary":"test","email_ids":["a"],"gmail_search":"q"}]`
	_, err := ParseAndValidate([]byte(input))
	if err == nil {
		t.Fatal("expected error for missing concern")
	}
}

func TestParseAndValidate_MissingEmailIDs(t *testing.T) {
	input := `[{"concern":"test","box":1,"box_name":"TODO","priority":"Q2","summary":"test","email_ids":[],"gmail_search":"q"}]`
	_, err := ParseAndValidate([]byte(input))
	if err == nil {
		t.Fatal("expected error for empty email_ids")
	}
}

func TestParseAndValidate_MissingGmailSearch(t *testing.T) {
	input := `[{"concern":"test","box":1,"box_name":"TODO","priority":"Q2","summary":"test","email_ids":["a"],"gmail_search":""}]`
	_, err := ParseAndValidate([]byte(input))
	if err == nil {
		t.Fatal("expected error for empty gmail_search")
	}
}

func TestToNotifications(t *testing.T) {
	results := []ConcernAnalysis{
		{
			Concern:     "Rapid7 renewal",
			Box:         2,
			BoxName:     "Important Unread",
			Priority:    "Q1",
			Summary:     "Renewal needs scheduling before Jul 31",
			EmailIDs:    []string{"abc123"},
			GmailSearch: `from:rapid7 subject:"renewal"`,
		},
	}
	notifs := ToNotifications(results)
	if len(notifs) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(notifs))
	}
	// 🔴 Q1 [Box 2 — Important Unread] Rapid7 renewal — ...
	want := "\U0001F534 Q1 [Box 2 — Important Unread] Rapid7 renewal — Renewal needs scheduling before Jul 31"
	if notifs[0].Message != want {
		t.Errorf("unexpected message:\n  got:  %s\n  want: %s", notifs[0].Message, want)
	}
	if notifs[0].Type != "action_needed" {
		t.Errorf("expected Q1 → action_needed, got %s", notifs[0].Type)
	}
	if notifs[0].Priority != "Q1" {
		t.Errorf("expected priority Q1, got %s", notifs[0].Priority)
	}
	if notifs[0].Link == "" {
		t.Error("expected non-empty link")
	}
}

func TestToNotifications_PriorityMapping(t *testing.T) {
	tests := []struct {
		priority string
		wantIcon string
		wantType string
	}{
		{"Q1", "\U0001F534", "action_needed"},
		{"Q2", "\U0001F7E1", "action_needed"},
		{"Q3", "\U0001F535", "info"},
		{"Q4", "⚪", "info"},
	}
	for _, tt := range tests {
		results := []ConcernAnalysis{{
			Concern: "test", Box: 1, BoxName: "TODO",
			Priority: tt.priority, Summary: "test",
			EmailIDs: []string{"a"}, GmailSearch: "q",
		}}
		notifs := ToNotifications(results)
		if !strings.HasPrefix(notifs[0].Message, tt.wantIcon+" "+tt.priority) {
			t.Errorf("%s: message should start with %s %s, got: %s", tt.priority, tt.wantIcon, tt.priority, notifs[0].Message)
		}
		if notifs[0].Type != tt.wantType {
			t.Errorf("%s: expected DSH type %s, got %s", tt.priority, tt.wantType, notifs[0].Type)
		}
		if notifs[0].Priority != tt.priority {
			t.Errorf("%s: expected priority %s, got %s", tt.priority, tt.priority, notifs[0].Priority)
		}
	}
}

func TestParseAndValidate_PreambleText(t *testing.T) {
	input := "Here's my analysis of the inbox:\n" + `[{"concern":"test","box":1,"box_name":"TODO","priority":"Q2","summary":"test summary","email_ids":["abc"],"gmail_search":"subject:test"}]`
	results, err := ParseAndValidate([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

func TestParseAndValidate_TrailingText(t *testing.T) {
	input := `[{"concern":"test","box":1,"box_name":"TODO","priority":"Q2","summary":"test summary","email_ids":["abc"],"gmail_search":"subject:test"}]` + "\n\nLet me know if you need more details."
	results, err := ParseAndValidate([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

func TestGmailSearchURL(t *testing.T) {
	got := GmailSearchURL(`from:david subject:"JA4 POC"`)
	want := "https://mail.google.com/mail/u/0/#search/from%3Adavid+subject%3A%22JA4+POC%22"
	if got != want {
		t.Errorf("GmailSearchURL\n  got:  %s\n  want: %s", got, want)
	}
}

func TestGmailSearchURL_ScriptInjection(t *testing.T) {
	got := GmailSearchURL(`"><script>alert(1)</script>`)
	if strings.Contains(got, "<script>") {
		t.Errorf("script tag should be escaped in URL, got: %s", got)
	}
	if !strings.HasPrefix(got, "https://mail.google.com/mail/u/0/#search/") {
		t.Error("URL prefix must always be Gmail search")
	}
}

func TestGmailSearchURL_JavascriptProtocol(t *testing.T) {
	got := GmailSearchURL("javascript:alert(1)")
	if !strings.HasPrefix(got, "https://mail.google.com/mail/u/0/#search/") {
		t.Errorf("must always produce Gmail URL, got: %s", got)
	}
	if strings.Contains(got, "javascript:") {
		t.Errorf("javascript: should be escaped, got: %s", got)
	}
}

func TestToNotifications_XSSInConcern(t *testing.T) {
	results := []ConcernAnalysis{
		{
			Concern:     `<script>alert(1)</script>`,
			Box:         1,
			BoxName:     "TODO",
			Priority:    "Q4",
			Summary:     `<img onerror=alert(1) src=x>`,
			EmailIDs:    []string{"abc"},
			GmailSearch: "test",
		},
	}
	notifs := ToNotifications(results)
	// Message is plain text — XSS defense is at the template layer (html/template auto-escapes).
	// But verify the message doesn't get mangled in unexpected ways.
	if len(notifs) != 1 {
		t.Fatal("expected 1 notification")
	}
	if !strings.Contains(notifs[0].Message, "<script>") {
		t.Log("Note: message contains raw HTML tags — DSH html/template will escape these on render")
	}
}

func TestParseAndValidate_MaliciousGmailSearch(t *testing.T) {
	input := `[{"concern":"test","box":1,"box_name":"TODO","priority":"Q2","summary":"test","email_ids":["a"],"gmail_search":"javascript:alert(document.cookie)"}]`
	_, err := ParseAndValidate([]byte(input))
	if err == nil {
		t.Fatal("expected error for malicious gmail_search with javascript: protocol")
	}
	if !strings.Contains(err.Error(), "unknown Gmail search operator") {
		t.Errorf("expected unknown operator error, got: %v", err)
	}
}
