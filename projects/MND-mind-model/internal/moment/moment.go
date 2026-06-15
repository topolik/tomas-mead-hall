// Package moment defines the decision-moment model — one record per
// user-authored turn extracted from a session, with the assistant context
// it responded to.
package moment

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// Moment is one decision moment: Tomas's message plus the truncated
// assistant context it was responding to.
type Moment struct {
	ID      string `json:"id"`      // sha256(source|session|ts|text)[:12]
	Source  string `json:"source"`  // "claude" | "gemini"
	Project string `json:"project"` // directory-derived project name
	Session string `json:"session"` // session id
	TS      string `json:"ts"`      // RFC3339 timestamp of the user turn
	Context string `json:"context"` // tail of the preceding assistant text (may be empty)
	Text    string `json:"text"`    // Tomas's message
}

// NewID derives a stable moment ID.
func NewID(source, session, ts, text string) string {
	h := sha256.Sum256([]byte(source + "|" + session + "|" + ts + "|" + text))
	return hex.EncodeToString(h[:])[:12]
}

// WriteJSONL writes moments one-per-line.
func WriteJSONL(w io.Writer, moments []Moment) error {
	bw := bufio.NewWriter(w)
	enc := json.NewEncoder(bw)
	for _, m := range moments {
		if err := enc.Encode(m); err != nil {
			return err
		}
	}
	return bw.Flush()
}

// ReadJSONL reads a moments file written by WriteJSONL.
func ReadJSONL(path string) ([]Moment, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []Moment
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	line := 0
	for sc.Scan() {
		line++
		if len(sc.Bytes()) == 0 {
			continue
		}
		var m Moment
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, line, err)
		}
		out = append(out, m)
	}
	return out, sc.Err()
}
