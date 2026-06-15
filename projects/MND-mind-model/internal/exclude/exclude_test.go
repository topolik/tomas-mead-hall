package exclude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// T22: datamarked content (our own injection defense) marks pipeline prompts.
func TestDatamarkFingerprint(t *testing.T) {
	if !IsPipelineContent("analyzethesemoments") {
		t.Fatal("datamarked text must be pipeline content")
	}
	if IsPipelineContent("normal direction from Tomas, use flat files") {
		t.Fatal("plain text wrongly flagged")
	}
}

// T23: prompt template phrases mark pipeline prompts.
func TestPhraseMarkers(t *testing.T) {
	flagged := []string{
		"You are a decision-pattern distiller for Tomas's \"mind model\". ...",
		"blah blah\nOUTPUT SCHEMA:\n{...}",
		"An agent in a herdr terminal pane... <terminal-tail>\n❯ stuff\n</terminal-tail>",
		"[MND orchestrator] Use flat files. KISS — no DB until the data outgrows it.", // T40: delivered directions carry the attribution prefix
	}
	for _, s := range flagged {
		if !IsPipelineContent(s) {
			t.Fatalf("should be pipeline content: %.50q", s)
		}
	}
}

// T24: ledger matches normalized text; missing file is an empty ledger.
func TestLedger(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sent-ledger.jsonl")

	empty, err := LoadLedger(path)
	if err != nil || empty.Size() != 0 {
		t.Fatalf("missing ledger must be empty: %v %d", err, empty.Size())
	}

	sent := "Store the execution logs in flat files. KISS."
	e := LedgerEntry{TS: "2026-06-12T13:00:00Z", Target: "w123-1", Hash: NormHash(sent)}
	data, _ := json.Marshal(e)
	os.WriteFile(path, append(data, '\n'), 0o644)

	l, err := LoadLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	// whitespace/case mangling from terminal round-trip must still match
	if !l.Contains("store the execution  logs in\nflat files. kiss.") {
		t.Fatal("normalized match failed")
	}
	if l.Contains("a different direction entirely") {
		t.Fatal("false positive")
	}
}
