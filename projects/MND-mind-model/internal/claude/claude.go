// Package claude parses Claude Code session JSONL files
// (~/.claude/projects/<project>/<session>.jsonl) into decision moments.
//
// Only human-authored turns are kept (MND-001): tool results, sidechains,
// non-external user types, slash-command echoes, and harness stdout are all
// skipped. The last assistant text before each user turn is attached as
// context.
package claude

import (
	"bufio"
	"encoding/json"
	"os"
	"regexp"
	"strings"

	"github.com/topolik/mnd-mind-model/internal/moment"
)

// record is the subset of a session JSONL line we care about.
type record struct {
	Type        string  `json:"type"`
	IsSidechain bool    `json:"isSidechain"`
	UserType    string  `json:"userType"`
	Timestamp   string  `json:"timestamp"`
	SessionID   string  `json:"sessionId"`
	Message     message `json:"message"`
}

type message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

var systemReminderRe = regexp.MustCompile(`(?s)<system-reminder>.*?</system-reminder>`)

// ParseFile extracts raw (unredacted, unfiltered-for-noise) moments from one
// session file. project is the project directory name.
// contextBudget caps the attached assistant context (tail), in characters.
func ParseFile(path, project string, contextBudget int) ([]moment.Moment, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []moment.Moment
	lastAssistantText := ""

	sc := bufio.NewScanner(f)
	// Single JSONL lines carry whole file snapshots — they can be huge.
	sc.Buffer(make([]byte, 0, 1024*1024), 64*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var r record
		if err := json.Unmarshal(line, &r); err != nil {
			continue // non-record lines (snapshots, attachments) don't matter
		}
		switch r.Type {
		case "assistant":
			if t := textOf(r.Message.Content); t != "" {
				lastAssistantText = t
			}
		case "user":
			if r.IsSidechain || (r.UserType != "" && r.UserType != "external") {
				continue
			}
			text := humanText(r.Message.Content)
			if text == "" {
				continue
			}
			out = append(out, moment.Moment{
				ID:      moment.NewID("claude", r.SessionID, r.Timestamp, text),
				Source:  "claude",
				Project: project,
				Session: r.SessionID,
				TS:      r.Timestamp,
				Context: tail(lastAssistantText, contextBudget),
				Text:    text,
			})
		}
	}
	return out, sc.Err()
}

// humanText returns the human-authored text of a user record, or "" when the
// record is not a human turn (tool results, command echoes, harness output).
func humanText(raw json.RawMessage) string {
	text := ""
	// content is either a plain string or an array of blocks
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		text = s
	} else {
		var blocks []contentBlock
		if err := json.Unmarshal(raw, &blocks); err != nil {
			return ""
		}
		var parts []string
		for _, b := range blocks {
			if b.Type == "tool_result" {
				return "" // tool results are not human turns (MND-001)
			}
			if b.Type == "text" && b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		text = strings.Join(parts, "\n")
	}

	// Slash-command echoes and harness-generated turns
	if strings.Contains(text, "<command-name>") ||
		strings.Contains(text, "<local-command-stdout>") ||
		strings.HasPrefix(text, "[Request interrupted") {
		return ""
	}
	text = systemReminderRe.ReplaceAllString(text, "")
	return strings.TrimSpace(text)
}

// textOf joins text blocks of an assistant message.
func textOf(raw json.RawMessage) string {
	var blocks []contentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

// tail returns the last n characters (rune-safe enough for budget purposes).
func tail(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	cut := s[len(s)-n:]
	// avoid splitting a UTF-8 sequence
	for len(cut) > 0 && (cut[0]&0xC0) == 0x80 {
		cut = cut[1:]
	}
	return cut
}
