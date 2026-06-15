package notify

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/topolik/gml-gmail-agent/internal/gmail"
)

type ConcernAnalysis struct {
	Concern     string   `json:"concern"`
	Box         int      `json:"box"`
	BoxName     string   `json:"box_name"`
	Priority    string   `json:"priority"`
	Summary     string   `json:"summary"`
	EmailIDs    []string `json:"email_ids"`
	GmailSearch string   `json:"gmail_search"`
}

var validPriorities = map[string]bool{
	"Q1": true,
	"Q2": true,
	"Q3": true,
	"Q4": true,
}

var priorityIcon = map[string]string{
	"Q1": "\U0001F534", // 🔴 Do Now — urgent + important
	"Q2": "\U0001F7E1", // 🟡 Schedule — important, not urgent
	"Q3": "\U0001F535", // 🔵 Quick/Delegate — urgent, not important
	"Q4": "⚪",     // ⚪ Low — neither
}

var priorityToDSHType = map[string]string{
	"Q1": "action_needed",
	"Q2": "action_needed",
	"Q3": "info",
	"Q4": "info",
}

func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if i := strings.Index(s, "\n"); i >= 0 {
			s = s[i+1:]
		}
	}
	if strings.HasSuffix(s, "```") {
		s = s[:len(s)-3]
	}
	s = strings.TrimSpace(s)

	// Handle LLM preamble: strip lines before the first JSON delimiter.
	// Gemini sometimes prepends prose like "Here's the analysis:" before JSON.
	if strings.HasPrefix(s, "[") || strings.HasPrefix(s, "{") {
		// Already starts with JSON — no preamble to strip
	} else if i := strings.Index(s, "\n{"); i >= 0 {
		s = s[i+1:]
	} else if i := strings.Index(s, "\n["); i >= 0 {
		s = s[i+1:]
	}

	// Strip trailing text after the JSON closes
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "{") {
		if last := strings.LastIndex(s, "}"); last >= 0 && last < len(s)-1 {
			s = s[:last+1]
		}
	} else if strings.HasPrefix(s, "[") {
		if last := strings.LastIndex(s, "]"); last >= 0 && last < len(s)-1 {
			s = s[:last+1]
		}
	}
	return strings.TrimSpace(s)
}

func ParseAndValidate(data []byte) ([]ConcernAnalysis, error) {
	data = []byte(stripCodeFence(string(data)))
	if len(data) == 0 {
		return nil, fmt.Errorf("empty input")
	}

	var results []ConcernAnalysis
	if err := json.Unmarshal(data, &results); err != nil {
		preview := string(data)
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		return nil, fmt.Errorf("invalid JSON: %w\nraw output: %s", err, preview)
	}

	for i, r := range results {
		if r.Concern == "" {
			return nil, fmt.Errorf("result[%d]: concern is required", i)
		}
		if r.Box < 1 || r.Box > 6 {
			return nil, fmt.Errorf("result[%d]: box must be 1-6, got %d", i, r.Box)
		}
		if r.BoxName == "" {
			return nil, fmt.Errorf("result[%d]: box_name is required", i)
		}
		if !validPriorities[r.Priority] {
			return nil, fmt.Errorf("result[%d]: priority must be Q1/Q2/Q3/Q4, got %q", i, r.Priority)
		}
		if r.Summary == "" {
			return nil, fmt.Errorf("result[%d]: summary is required", i)
		}
		if len(r.EmailIDs) == 0 {
			return nil, fmt.Errorf("result[%d]: email_ids is required (at least one)", i)
		}
		if r.GmailSearch == "" {
			return nil, fmt.Errorf("result[%d]: gmail_search is required", i)
		}
		if err := gmail.ValidateQuery(r.GmailSearch); err != nil {
			return nil, fmt.Errorf("result[%d]: %w", i, err)
		}
	}
	return results, nil
}

func GmailSearchURL(query string) string {
	return "https://mail.google.com/mail/u/0/#search/" + url.QueryEscape(query)
}

func ToNotifications(results []ConcernAnalysis) []Notification {
	var notifs []Notification
	for _, r := range results {
		if strings.HasPrefix(r.Summary, "Unchanged:") || strings.HasPrefix(r.Summary, "Unchanged —") {
			continue
		}
		icon := priorityIcon[r.Priority]
		dshType := priorityToDSHType[r.Priority]
		notifs = append(notifs, Notification{
			ProjectCode: "GML",
			Message:     fmt.Sprintf("%s %s [Box %d — %s] %s — %s", icon, r.Priority, r.Box, r.BoxName, r.Concern, r.Summary),
			Type:        dshType,
			Priority:    r.Priority,
			Link:        GmailSearchURL(r.GmailSearch),
		})
	}
	return notifs
}
