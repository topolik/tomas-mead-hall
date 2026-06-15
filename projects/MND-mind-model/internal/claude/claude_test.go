package claude

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Fixture lines modeled on real ~/.claude/projects records (surveyed 2026-06-12).
const fixture = `{"type":"mode","mode":"normal","sessionId":"s1"}
{"type":"user","isSidechain":false,"userType":"external","timestamp":"2026-05-27T10:05:31.161Z","sessionId":"s1","message":{"role":"user","content":"My name is Tomas and I want to create family of agents that will do my work."}}
{"type":"assistant","timestamp":"2026-05-27T10:05:40.000Z","sessionId":"s1","message":{"role":"assistant","content":[{"type":"text","text":"I suggest we use Postgres for storage because of transactions."},{"type":"tool_use","id":"t1","name":"Bash","input":{}}]}}
{"type":"user","isSidechain":false,"userType":"external","timestamp":"2026-05-27T10:06:18.955Z","sessionId":"s1","message":{"role":"user","content":[{"type":"tool_result","content":"File does not exist.","is_error":true,"tool_use_id":"t1"}]}}
{"type":"user","isSidechain":false,"userType":"external","timestamp":"2026-05-27T10:07:00.000Z","sessionId":"s1","message":{"role":"user","content":"No, keep it simple - use a flat yaml file instead of a database."}}
{"type":"user","isSidechain":true,"userType":"external","timestamp":"2026-05-27T10:08:00.000Z","sessionId":"s1","message":{"role":"user","content":"sidechain noise"}}
{"type":"user","isSidechain":false,"userType":"external","timestamp":"2026-05-27T10:09:00.000Z","sessionId":"s1","message":{"role":"user","content":"<command-name>/model</command-name> output"}}
{"type":"user","isSidechain":false,"userType":"external","timestamp":"2026-05-27T10:10:00.000Z","sessionId":"s1","message":{"role":"user","content":[{"type":"text","text":"Also add tests."},{"type":"text","text":"<system-reminder>ignore me</system-reminder>Run them in docker."}]}}
`

func parse(t *testing.T) []struct {
	Text, Context, TS string
} {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "s1.jsonl")
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	ms, err := ParseFile(path, "test-project", 700)
	if err != nil {
		t.Fatal(err)
	}
	var out []struct{ Text, Context, TS string }
	for _, m := range ms {
		if m.Source != "claude" || m.Project != "test-project" || m.Session != "s1" || m.ID == "" {
			t.Fatalf("bad envelope: %+v", m)
		}
		out = append(out, struct{ Text, Context, TS string }{m.Text, m.Context, m.TS})
	}
	return out
}

// T1+T2: human turns kept; tool results, sidechains, command echoes skipped.
func TestKeepsHumanTurnsOnly(t *testing.T) {
	ms := parse(t)
	if len(ms) != 3 {
		t.Fatalf("want 3 moments, got %d: %+v", len(ms), ms)
	}
	if !strings.HasPrefix(ms[0].Text, "My name is Tomas") {
		t.Fatalf("moment 0: %q", ms[0].Text)
	}
	if !strings.HasPrefix(ms[1].Text, "No, keep it simple") {
		t.Fatalf("moment 1: %q", ms[1].Text)
	}
}

// T3: context is the last assistant text before the turn.
func TestAttachesAssistantContext(t *testing.T) {
	ms := parse(t)
	if ms[0].Context != "" {
		t.Fatalf("first turn should have no context, got %q", ms[0].Context)
	}
	if !strings.Contains(ms[1].Context, "Postgres for storage") {
		t.Fatalf("missing context: %q", ms[1].Context)
	}
}

// T4: array-of-text-blocks content is joined; system reminders stripped.
func TestJoinsTextBlocksAndStripsReminders(t *testing.T) {
	ms := parse(t)
	last := ms[2]
	if !strings.Contains(last.Text, "Also add tests.") || !strings.Contains(last.Text, "Run them in docker.") {
		t.Fatalf("blocks not joined: %q", last.Text)
	}
	if strings.Contains(last.Text, "ignore me") {
		t.Fatalf("system-reminder leaked: %q", last.Text)
	}
}
