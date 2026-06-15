package gemini

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Modeled on real ~/.gemini/tmp/*/chats/session-*.json (surveyed 2026-06-12).
const chatFixture = `{
  "sessionId": "a7e8a9db",
  "messages": [
    {"id": "1", "timestamp": "2026-04-17T15:09:18Z", "type": "info", "content": "Gemini CLI update available!"},
    {"id": "2", "timestamp": "2026-04-17T15:10:43Z", "type": "user", "content": [{"text": "I found this project and I like how it enables long-term memory. What do you think?"}]},
    {"id": "3", "timestamp": "2026-04-17T15:11:14Z", "type": "gemini", "content": "It looks solid. I recommend installing it as an extension."},
    {"id": "4", "timestamp": "2026-04-17T15:12:00Z", "type": "user", "content": "No - run it in docker, do not install anything on the host."},
    {"id": "5", "timestamp": "2026-04-17T15:13:00Z", "type": "user", "content": "/stats"}
  ]
}`

const logsFixture = `[
  {"sessionId": "fd685539", "messageId": 0, "type": "user", "message": "/stats", "timestamp": "2026-04-16T12:57:54Z"},
  {"sessionId": "fd685539", "messageId": 1, "type": "user", "message": "Review the OAuth apps and flag anything with broad scopes.", "timestamp": "2026-04-16T12:58:30Z"}
]`

// T5: chats parsing — both content forms, gemini context attached, slash commands dropped.
func TestParseChatFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session-x.json")
	if err := os.WriteFile(path, []byte(chatFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	ms, err := ParseChatFile(path, "topolik", 700)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 2 {
		t.Fatalf("want 2 moments, got %d: %+v", len(ms), ms)
	}
	if !strings.HasPrefix(ms[0].Text, "I found this project") || ms[0].Context != "" {
		t.Fatalf("moment 0 wrong: %+v", ms[0])
	}
	if !strings.HasPrefix(ms[1].Text, "No - run it in docker") {
		t.Fatalf("moment 1 wrong: %+v", ms[1])
	}
	if !strings.Contains(ms[1].Context, "installing it as an extension") {
		t.Fatalf("moment 1 missing gemini context: %q", ms[1].Context)
	}
	if ms[0].Source != "gemini" || ms[0].Session != "a7e8a9db" || ms[0].Project != "topolik" {
		t.Fatalf("bad envelope: %+v", ms[0])
	}
}

// T6: logs.json fallback yields moments without context.
func TestParseLogsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs.json")
	if err := os.WriteFile(path, []byte(logsFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	ms, err := ParseLogsFile(path, "topolik")
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 {
		t.Fatalf("want 1 moment (slash command dropped), got %d", len(ms))
	}
	if !strings.HasPrefix(ms[0].Text, "Review the OAuth apps") || ms[0].Context != "" {
		t.Fatalf("wrong moment: %+v", ms[0])
	}
}
