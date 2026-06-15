package extract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/topolik/mnd-mind-model/internal/exclude"
)

// T7: noise filter — bare acks and slash commands drop, terse directions survive.
func TestIsNoise(t *testing.T) {
	noise := []string{"continue", "ok", "yes", "/stats", "thanks", "go ahead", ""}
	for _, s := range noise {
		if !IsNoise(s) {
			t.Errorf("should be noise: %q", s)
		}
	}
	signal := []string{
		"no, use yaml",
		"keep it simple",
		"that's wrong",
		"Q2 priority",
		"don't install on host",
		"I want you to implement a new project that will learn from my sessions and distill my brain.",
	}
	for _, s := range signal {
		if IsNoise(s) {
			t.Errorf("should be signal: %q", s)
		}
	}
}

// T7b (found in real run 2026-06-12): machine-fed payloads arriving as user
// turns are noise — piped file dumps, harness limit messages.
func TestMachinePayloadIsNoise(t *testing.T) {
	payloads := []string{
		"--- FILE: /opt/liferay.git/portal-master/modules/apps/expando/Expando.java\npackage com;",
		"You have exceeded the maximum number of turns. You have one more.",
	}
	for _, s := range payloads {
		if !IsNoise(s) {
			t.Errorf("machine payload should be noise: %.60q", s)
		}
	}
}

// T8b (found in real run 2026-06-12): templated loops repeat the same text
// across sessions with distinct timestamps — collapse to one exemplar.
func TestNearDupCollapse(t *testing.T) {
	mk := func(session, ts string) string {
		return `{"sessionId":"` + session + `","messages":[{"timestamp":"` + ts +
			`","type":"user","content":"use sast-manual-auditor to audit the modules at /opt/x and report only true positives please"}]}`
	}
	gemDir := t.TempDir()
	chats := filepath.Join(gemDir, "proj", "chats")
	os.MkdirAll(chats, 0o755)
	os.WriteFile(filepath.Join(chats, "session-a.json"), []byte(mk("s1", "t1")), 0o600)
	os.WriteFile(filepath.Join(chats, "session-b.json"), []byte(mk("s2", "t2")), 0o600)

	ms, st, err := Run("", gemDir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 || st.DroppedDup != 1 {
		t.Fatalf("near-dup not collapsed: %d moments, %+v", len(ms), st)
	}
}

// T24 + T25: orchestrator-sent directions and excluded gemini projects are
// dropped; Tomas's turns in the same session survive.
func TestSelfExclusion(t *testing.T) {
	gemDir := t.TempDir()
	sent := "Store the execution logs in flat files. This keeps the project strictly self-contained."

	// project "agent-pane": one orchestrator-sent turn + one real Tomas turn
	chats := filepath.Join(gemDir, "agent-pane", "chats")
	os.MkdirAll(chats, 0o755)
	os.WriteFile(filepath.Join(chats, "session-a.json"), []byte(`{"sessionId":"s1","messages":[
	  {"timestamp":"t1","type":"user","content":"`+sent+`"},
	  {"timestamp":"t2","type":"user","content":"No, wrong direction - use a database here because we need concurrent writers."}
	]}`), 0o600)

	// project "MND-mind-model": pipeline working dir, excluded wholesale
	chats2 := filepath.Join(gemDir, "MND-mind-model", "chats")
	os.MkdirAll(chats2, 0o755)
	os.WriteFile(filepath.Join(chats2, "session-b.json"), []byte(`{"sessionId":"s2","messages":[
	  {"timestamp":"t3","type":"user","content":"You must not learn from this pipeline session content."}
	]}`), 0o600)

	// ledger holding the sent direction
	ledgerPath := filepath.Join(gemDir, "sent-ledger.jsonl")
	os.WriteFile(ledgerPath, []byte(`{"ts":"t1","target":"w1-1","hash":"`+exclude.NormHash(sent)+`"}`+"\n"), 0o600)

	ms, st, err := Run("", gemDir, Options{
		LedgerPath:            ledgerPath,
		ExcludeGeminiProjects: []string{"MND-mind-model"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 || !strings.HasPrefix(ms[0].Text, "No, wrong direction") {
		t.Fatalf("want only Tomas's turn, got %+v", ms)
	}
	if st.DroppedSelf != 1 {
		t.Fatalf("want 1 self-drop (ledger), got %d", st.DroppedSelf)
	}
}

// T22 via extract: datamarked pipeline prompts in any session are dropped.
func TestPipelinePromptExcluded(t *testing.T) {
	gemDir := t.TempDir()
	chats := filepath.Join(gemDir, "proj", "chats")
	os.MkdirAll(chats, 0o755)
	os.WriteFile(filepath.Join(chats, "session-a.json"), []byte(`{"sessionId":"s1","messages":[
	  {"timestamp":"t1","type":"user","content":"You are a decision-pattern distiller for the mind model. OUTPUT SCHEMA: ..."}
	]}`), 0o600)
	ms, st, err := Run("", gemDir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 || st.DroppedSelf != 1 {
		t.Fatalf("pipeline prompt not excluded: %+v %+v", ms, st)
	}
}

// T8 + T10: dedup across overlapping checkpoint files; redaction applied to
// text and context before write.
func TestRunDedupsAndRedacts(t *testing.T) {
	gemDir := t.TempDir()
	chats := filepath.Join(gemDir, "proj", "chats")
	if err := os.MkdirAll(chats, 0o755); err != nil {
		t.Fatal(err)
	}
	// Two checkpoint files of the SAME session — second is a superset (real
	// Gemini CLI behavior: session-...T17-08 / T17-09 share one sessionId).
	snap1 := `{"sessionId":"s1","messages":[
	  {"timestamp":"t1","type":"user","content":"Use the token ghp_AbCdEfGhIjKlMnOpQrStUvWxYz123456 to push, and never commit secrets again."}
	]}`
	snap2 := `{"sessionId":"s1","messages":[
	  {"timestamp":"t1","type":"user","content":"Use the token ghp_AbCdEfGhIjKlMnOpQrStUvWxYz123456 to push, and never commit secrets again."},
	  {"timestamp":"t2","type":"gemini","content":"Done. Key sk-proj-abc123DEF456ghi789JKL000 worked."},
	  {"timestamp":"t3","type":"user","content":"Good, now don't ever print keys in output."}
	]}`
	os.WriteFile(filepath.Join(chats, "session-a.json"), []byte(snap1), 0o600)
	os.WriteFile(filepath.Join(chats, "session-b.json"), []byte(snap2), 0o600)

	ms, st, err := Run("", gemDir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 2 {
		t.Fatalf("want 2 moments after dedup, got %d (%+v)", len(ms), st)
	}
	if st.DroppedDup != 1 {
		t.Fatalf("want 1 dup dropped, got %d", st.DroppedDup)
	}
	for _, m := range ms {
		if strings.Contains(m.Text, "ghp_") || strings.Contains(m.Context, "sk-proj-") {
			t.Fatalf("unredacted secret: %+v", m)
		}
	}
	// context of second moment must carry the redaction marker, not the key
	if !strings.Contains(ms[1].Context, "[REDACTED:api-key]") {
		t.Fatalf("context not redacted: %q", ms[1].Context)
	}
}
