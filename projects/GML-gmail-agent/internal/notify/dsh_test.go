package notify

import (
	"strings"
	"testing"
)

func TestFormatDismissedNotifications_IncludesLink(t *testing.T) {
	notifs := []PreviousNotification{
		{
			Message:     "🔵 Q3 [Insight: ignore_pattern] Ignores JIRA notifications",
			Priority:    "Q3",
			Link:        "https://mail.google.com/mail/u/0/#search/from%3Ajira%40example.com",
			Comment:     "yes, archive these",
			CreatedAt:   "2026-05-29T10:00:00Z",
			DismissedAt: "2026-05-30T08:00:00Z",
		},
	}
	result := FormatDismissedNotifications(notifs)

	if !strings.Contains(result, "Link: https://mail.google.com/mail/u/0/#search/from%3Ajira%40example.com") {
		t.Errorf("missing Link line in output:\n%s", result)
	}
	if !strings.Contains(result, "Comment:") {
		t.Errorf("missing Comment line in output:\n%s", result)
	}
}

func TestFormatDismissedNotifications_NoLink(t *testing.T) {
	notifs := []PreviousNotification{
		{
			Message:     "🔵 Q3 [Insight: ignore_pattern] Old insight",
			Priority:    "Q3",
			Comment:     "ok",
			CreatedAt:   "2026-05-28T10:00:00Z",
			DismissedAt: "2026-05-29T08:00:00Z",
		},
	}
	result := FormatDismissedNotifications(notifs)

	if strings.Contains(result, "Link:") {
		t.Errorf("should not have Link line when link is empty:\n%s", result)
	}
}

func TestFormatDismissedNotifications_Empty(t *testing.T) {
	result := FormatDismissedNotifications(nil)
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}
