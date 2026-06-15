package handler

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"os"
	"sync"
	"time"
)

type SetupToken struct {
	mu       sync.Mutex
	token    string
	created  time.Time
	filePath string
}

const setupTokenTTL = 10 * time.Minute

func NewSetupToken(token, filePath string) *SetupToken {
	st := &SetupToken{
		token:    token,
		created:  time.Now(),
		filePath: filePath,
	}
	if filePath != "" && token != "" {
		os.WriteFile(filePath, []byte(token+"\n"), 0600)
	}
	return st
}

// Regenerate mints a fresh random token and resets the TTL clock to now,
// then persists it. The boot-time token's TTL is anchored to process start,
// which makes it useless on a long-running container — by the time you want to
// enroll a new device it has long expired. Regenerate is what an authenticated
// user calls (via the Passkeys page) to get a working enrollment link on demand.
func (s *SetupToken) Regenerate() (string, error) {
	if s == nil {
		return "", errors.New("setup token not configured")
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(b)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.token = tok
	s.created = time.Now()
	if s.filePath != "" {
		os.WriteFile(s.filePath, []byte(tok+"\n"), 0600)
	}
	return tok, nil
}

// TTL returns the configured lifetime of a setup token.
func (s *SetupToken) TTL() time.Duration { return setupTokenTTL }

func (s *SetupToken) Valid(t string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token == "" || t == "" {
		return false
	}
	if time.Since(s.created) > setupTokenTTL {
		s.clear()
		return false
	}
	return subtle.ConstantTimeCompare([]byte(s.token), []byte(t)) == 1
}

func (s *SetupToken) Consume() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clear()
}

func (s *SetupToken) clear() {
	s.token = ""
	if s.filePath != "" {
		os.Remove(s.filePath)
	}
}
