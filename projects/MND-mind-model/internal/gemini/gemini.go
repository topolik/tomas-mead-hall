// Package gemini parses Gemini CLI session files into decision moments.
//
// Primary source: ~/.gemini/tmp/<project>/chats/session-*.json — both sides
// of the conversation. Fallback: <project>/logs.json (user messages only)
// for projects without chats/. Checkpoint files snapshot the same session
// incrementally, so callers must dedup by moment ID (extract does).
package gemini

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/topolik/mnd-mind-model/internal/moment"
)

type chatFile struct {
	SessionID string        `json:"sessionId"`
	Messages  []chatMessage `json:"messages"`
}

type chatMessage struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"` // user | gemini | info | error
	Content   json.RawMessage `json:"content"`
}

type logEntry struct {
	SessionID string `json:"sessionId"`
	Type      string `json:"type"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}

// ParseChatFile extracts moments from one chats/session-*.json file.
func ParseChatFile(path, project string, contextBudget int) ([]moment.Moment, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cf chatFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return nil, err
	}

	var out []moment.Moment
	lastGeminiText := ""
	for _, m := range cf.Messages {
		switch m.Type {
		case "gemini":
			if t := contentText(m.Content); t != "" {
				lastGeminiText = t
			}
		case "user":
			text := strings.TrimSpace(contentText(m.Content))
			if text == "" || strings.HasPrefix(text, "/") { // slash commands are CLI mechanics
				continue
			}
			out = append(out, moment.Moment{
				ID:      moment.NewID("gemini", cf.SessionID, m.Timestamp, text),
				Source:  "gemini",
				Project: project,
				Session: cf.SessionID,
				TS:      m.Timestamp,
				Context: tail(lastGeminiText, contextBudget),
				Text:    text,
			})
		}
	}
	return out, nil
}

// ParseLogsFile extracts moments from a logs.json (user side only, no context).
func ParseLogsFile(path, project string) ([]moment.Moment, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var entries []logEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}

	var out []moment.Moment
	for _, e := range entries {
		if e.Type != "user" {
			continue
		}
		text := strings.TrimSpace(e.Message)
		if text == "" || strings.HasPrefix(text, "/") {
			continue
		}
		out = append(out, moment.Moment{
			ID:      moment.NewID("gemini", e.SessionID, e.Timestamp, text),
			Source:  "gemini",
			Project: project,
			Session: e.SessionID,
			TS:      e.Timestamp,
			Text:    text,
		})
	}
	return out, nil
}

// contentText handles both content forms: a plain string, or an array of
// parts like [{"text": "..."}].
func contentText(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return ""
	}
	var b []string
	for _, p := range parts {
		if p.Text != "" {
			b = append(b, p.Text)
		}
	}
	return strings.TrimSpace(strings.Join(b, "\n"))
}

func tail(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	cut := s[len(s)-n:]
	for len(cut) > 0 && (cut[0]&0xC0) == 0x80 {
		cut = cut[1:]
	}
	return cut
}
