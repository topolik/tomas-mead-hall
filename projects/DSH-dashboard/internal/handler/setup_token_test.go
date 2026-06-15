package handler

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSetupTokenRegenerate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup-token")
	st := NewSetupToken("boot-token", path)

	// Boot token is valid and persisted.
	if !st.Valid("boot-token") {
		t.Fatal("boot token should be valid")
	}

	// Regenerate mints a fresh token, invalidates the old one, and rewrites the file.
	tok, err := st.Regenerate()
	if err != nil {
		t.Fatalf("Regenerate: %v", err)
	}
	if tok == "" || tok == "boot-token" {
		t.Fatalf("Regenerate returned %q, want a new non-empty token", tok)
	}
	if st.Valid("boot-token") {
		t.Error("old token should no longer be valid after Regenerate")
	}
	if !st.Valid(tok) {
		t.Error("new token should be valid")
	}
	onDisk, _ := os.ReadFile(path)
	if string(onDisk) != tok+"\n" {
		t.Errorf("file = %q, want %q", string(onDisk), tok+"\n")
	}

	// Consume is single-use.
	st.Consume()
	if st.Valid(tok) {
		t.Error("token should be invalid after Consume")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("token file should be removed after Consume")
	}
}

func TestSetupTokenTTLAnchoredToGeneration(t *testing.T) {
	st := NewSetupToken("x", "")
	// Simulate a long-running process: token was minted at boot, long ago.
	st.created = time.Now().Add(-2 * time.Hour)
	if st.Valid("x") {
		t.Fatal("a 2-hour-old token must be expired")
	}
	// Regenerate must reset the clock so the new token is usable now.
	tok, err := st.Regenerate()
	if err != nil {
		t.Fatalf("Regenerate: %v", err)
	}
	if !st.Valid(tok) {
		t.Error("freshly regenerated token must be valid regardless of process uptime")
	}
}

func TestSetupTokenNilSafe(t *testing.T) {
	var st *SetupToken
	if st.Valid("anything") {
		t.Error("nil SetupToken.Valid should be false")
	}
	if _, err := st.Regenerate(); err == nil {
		t.Error("nil SetupToken.Regenerate should error")
	}
}
