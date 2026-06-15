package auth

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func echoAgent() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, AgentFrom(r.Context()))
	})
}

// T8: an issued token resolves to its agent through the middleware.
func TestIssuedTokenResolvesAgent(t *testing.T) {
	s := NewStore()
	tok, err := s.Issue("gml")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	h := s.Middleware(echoAgent())

	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != 200 || rec.Body.String() != "gml" {
		t.Fatalf("got code=%d agent=%q", rec.Code, rec.Body.String())
	}
}

// T8: missing / malformed / unknown tokens are rejected with 401.
func TestRejectsBadTokens(t *testing.T) {
	s := NewStore()
	s.Set("known", "gml")
	h := s.Middleware(echoAgent())

	cases := []struct{ name, header string }{
		{"missing", ""},
		{"no-bearer-prefix", "known"},
		{"empty-bearer", "Bearer "},
		{"unknown-token", "Bearer nope"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/v1/models", nil)
			if c.header != "" {
				req.Header.Set("Authorization", c.header)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("%s: expected 401, got %d", c.name, rec.Code)
			}
		})
	}
}

// Two issues for the same agent produce distinct tokens, both valid.
func TestIssueUniqueTokens(t *testing.T) {
	s := NewStore()
	a, _ := s.Issue("dsh")
	b, _ := s.Issue("dsh")
	if a == b {
		t.Fatal("expected distinct tokens")
	}
	if ag, ok := s.Agent(a); !ok || ag != "dsh" {
		t.Fatalf("token a: %q %v", ag, ok)
	}
	if ag, ok := s.Agent(b); !ok || ag != "dsh" {
		t.Fatalf("token b: %q %v", ag, ok)
	}
}
