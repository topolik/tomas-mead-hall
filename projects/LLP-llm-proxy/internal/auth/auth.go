// Package auth holds the in-memory session-token store and the bearer-auth
// middleware. Tokens are minted at runtime by the control socket (see
// internal/control) when an agent registers — there are no static keys, nothing
// in env, on disk, or in /proc.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"sync"

	"github.com/topolik/llp-llm-proxy/internal/openai"
)

type ctxKey int

const agentKey ctxKey = 0

// Store maps live bearer tokens to agent names. Safe for concurrent use.
type Store struct {
	mu     sync.RWMutex
	tokens map[string]string // token -> agent
}

func NewStore() *Store {
	return &Store{tokens: make(map[string]string)}
}

// Issue mints a fresh random token for agent and records it.
func (s *Store) Issue(agent string) (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(b[:])
	s.mu.Lock()
	s.tokens[tok] = agent
	s.mu.Unlock()
	return tok, nil
}

// Set records a specific token->agent mapping (used by tests).
func (s *Store) Set(token, agent string) {
	s.mu.Lock()
	s.tokens[token] = agent
	s.mu.Unlock()
}

// Agent resolves a token to its agent name.
func (s *Store) Agent(token string) (string, bool) {
	s.mu.RLock()
	a, ok := s.tokens[token]
	s.mu.RUnlock()
	return a, ok
}

// Count returns the number of live tokens (for /healthz / logging).
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.tokens)
}

func (s *Store) bearer(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return "", false
	}
	token := strings.TrimSpace(h[len(prefix):])
	if token == "" {
		return "", false
	}
	return s.Agent(token)
}

// Middleware rejects requests without a valid session token (401) and stashes
// the agent name in the request context.
func (s *Store) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agent, ok := s.bearer(r)
		if !ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(openai.ErrorResponse{Error: openai.ErrorBody{
				Message: "missing or invalid session token — register via the control socket",
				Type:    "unauthorized",
			}})
			return
		}
		ctx := context.WithValue(r.Context(), agentKey, agent)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// AgentFrom returns the authenticated agent name from the request context.
func AgentFrom(ctx context.Context) string {
	a, _ := ctx.Value(agentKey).(string)
	return a
}
