// Package exclude is the self/other discriminator (MND-015): it identifies
// agent-team-generated content masquerading as "user" turns so retraining
// never feeds the brain its own output. Turn-level, layered:
//
//  1. datamark fingerprint — pipeline prompts carry U+E000 between words
//  2. pipeline phrase markers — our prompt template openers
//  3. send-ledger — directions orchestrate.sh delivered into agent panes
package exclude

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
)

const datamarker = ""

// pipeline prompt fingerprints — phrases that only occur in machine-built
// prompts from this team's pipelines (MND, GML share the style).
var phraseMarkers = []string{
	"OUTPUT SCHEMA:",
	"You are a decision-pattern distiller",
	"You are writing Tomas's \"mind model\" profiles",
	"You are Tomas's stand-in orchestrator",
	"<dismissed_insights>", // GML distill prompts
	"<moments>",            // MND distill prompts
	"<terminal-tail>",      // MND orchestrate questions
	"[MND orchestrator]",   // attribution prefix on delivered directions (iter 5) — keep in sync with orchestrate.sh MND_SEND_PREFIX
}

// IsPipelineContent reports whether a user turn is machine-generated
// pipeline content (T22, T23).
func IsPipelineContent(text string) bool {
	if strings.Contains(text, datamarker) {
		return true
	}
	for _, m := range phraseMarkers {
		if strings.Contains(text, m) {
			return true
		}
	}
	return false
}

// --- send-ledger -------------------------------------------------------------

// LedgerEntry is one direction the orchestrator delivered to an agent pane.
type LedgerEntry struct {
	TS     string `json:"ts"`
	Target string `json:"target"`
	Hash   string `json:"hash"` // sha256 of normalized text
}

// Ledger answers "did we send this text?" (T24).
type Ledger struct {
	hashes map[string]bool
}

// NormHash normalizes (lowercase, collapsed whitespace) and hashes a text —
// terminal round-trips mangle whitespace, not words.
func NormHash(text string) string {
	norm := strings.Join(strings.Fields(strings.ToLower(text)), " ")
	h := sha256.Sum256([]byte(norm))
	return hex.EncodeToString(h[:])
}

// LoadLedger reads a sent-ledger JSONL file; missing file = empty ledger.
func LoadLedger(path string) (*Ledger, error) {
	l := &Ledger{hashes: map[string]bool{}}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return l, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var e LedgerEntry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			continue // one bad line doesn't poison the ledger
		}
		if e.Hash != "" {
			l.hashes[e.Hash] = true
		}
	}
	return l, sc.Err()
}

// Contains reports whether the ledger holds this text.
func (l *Ledger) Contains(text string) bool {
	return l.hashes[NormHash(text)]
}

// Size returns the number of ledger entries (for run stats).
func (l *Ledger) Size() int { return len(l.hashes) }
