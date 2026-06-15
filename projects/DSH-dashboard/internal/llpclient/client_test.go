package llpclient

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// stub starts a fake LLP: a TCP data server (token-authenticated) plus a Unix
// control socket that issues tokens. issuedTokens lets a test rotate tokens to
// exercise re-registration; acceptToken is the token the data server accepts.
type stub struct {
	dataURL    string
	socketPath string
	registers  *int32 // count of /register calls
}

func newStub(t *testing.T, tokens []string, acceptToken string) stub {
	t.Helper()
	var regs int32

	authed := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+acceptToken {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			h(w, r)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(Health{Status: "ok", Impls: []ImplHealth{
			{Name: "gemini", Available: true}, {Name: "claude", Available: true}, {Name: "openllm", Available: false},
		}})
	})
	mux.HandleFunc("GET /admin/usage", authed(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"usage": []UsageRow{{Day: "2026-06-12", Agent: "dsh", Impl: "gemini", Requests: 2}}})
	}))
	mux.HandleFunc("GET /admin/requests", authed(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"requests": []RecentRow{{Ts: "2026-06-12T10:00:00Z", Agent: "dsh", ImplUsed: "gemini", Status: "ok"}}})
	}))
	mux.HandleFunc("POST /v1/chat/completions", authed(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		json.NewEncoder(w).Encode(map[string]any{
			"model":   req["model"],
			"choices": []map[string]any{{"message": map[string]string{"content": "hi from " + req["model"].(string)}}},
		})
	}))
	dataSrv := httptest.NewServer(mux)
	t.Cleanup(dataSrv.Close)

	socketPath := filepath.Join(t.TempDir(), "ctl.sock")
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	ctlMux := http.NewServeMux()
	ctlMux.HandleFunc("POST /register", func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&regs, 1)
		tok := tokens[len(tokens)-1]
		if int(n) <= len(tokens) {
			tok = tokens[n-1]
		}
		json.NewEncoder(w).Encode(map[string]string{"token": tok, "agent": "dsh"})
	})
	ctlSrv := &http.Server{Handler: ctlMux}
	go ctlSrv.Serve(ln)
	t.Cleanup(func() { ctlSrv.Close() })

	return stub{dataURL: dataSrv.URL, socketPath: socketPath, registers: &regs}
}

func TestClient_HandshakeThenCalls(t *testing.T) {
	s := newStub(t, []string{"tok-1"}, "tok-1")
	c := New(s.dataURL, s.socketPath, "dsh")
	ctx := context.Background()

	h, err := c.Health(ctx)
	if err != nil || h.Status != "ok" || h.Impls[2].State() != "disabled" {
		t.Fatalf("health: %v %+v", err, h)
	}
	u, err := c.Usage(ctx)
	if err != nil || len(u) != 1 || u[0].Agent != "dsh" {
		t.Fatalf("usage: %v %+v", err, u)
	}
	rec, err := c.Recent(ctx, 10)
	if err != nil || len(rec) != 1 {
		t.Fatalf("recent: %v %+v", err, rec)
	}
	// token cached: exactly one registration across all three calls
	if got := atomic.LoadInt32(s.registers); got != 1 {
		t.Fatalf("expected 1 registration, got %d", got)
	}
}

func TestClient_Complete(t *testing.T) {
	s := newStub(t, []string{"tok-1"}, "tok-1")
	c := New(s.dataURL, s.socketPath, "dsh")
	text, served, err := c.Complete(context.Background(), "claude", "ping")
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if !strings.Contains(text, "hi from claude") || served != "claude" {
		t.Fatalf("unexpected: text=%q served=%q", text, served)
	}
}

// A stale token (e.g. after an LLP restart) triggers an automatic re-register.
func TestClient_ReRegistersOn401(t *testing.T) {
	// control issues tok-1 first, tok-2 second; data only accepts tok-2.
	s := newStub(t, []string{"tok-1", "tok-2"}, "tok-2")
	c := New(s.dataURL, s.socketPath, "dsh")

	// Usage is an authed endpoint: tok-1 -> 401 -> re-register -> tok-2 -> 200.
	if _, err := c.Usage(context.Background()); err != nil {
		t.Fatalf("usage after re-register: %v", err)
	}
	if got := atomic.LoadInt32(s.registers); got != 2 {
		t.Fatalf("expected 2 registrations (initial + re-register), got %d", got)
	}
}

func TestClient_NotConfigured(t *testing.T) {
	if _, err := New("", "", "dsh").Health(context.Background()); err != ErrNotConfigured {
		t.Fatalf("want ErrNotConfigured, got %v", err)
	}
	if _, err := New("http://x", "", "dsh").Health(context.Background()); err != ErrNotConfigured {
		t.Fatalf("no socket => not configured, got %v", err)
	}
}

// TestLive_RealLLP runs against a real LLP. Skipped unless LLP_LIVE_URL is set.
//
//	LLP_LIVE_URL=http://localhost:4000 LLP_LIVE_SOCKET=$HOME/.llp/control.sock \
//	  go test -run TestLive_RealLLP -v ./internal/llpclient/
func TestLive_RealLLP(t *testing.T) {
	base := os.Getenv("LLP_LIVE_URL")
	sock := os.Getenv("LLP_LIVE_SOCKET")
	if base == "" || sock == "" {
		t.Skip("set LLP_LIVE_URL and LLP_LIVE_SOCKET to run the live LLP check")
	}
	c := New(base, sock, "dsh")
	ctx := context.Background()
	h, err := c.Health(ctx)
	if err != nil {
		t.Fatalf("live health: %v", err)
	}
	t.Logf("live backends: %+v", h.Impls)
	u, err := c.Usage(ctx)
	if err != nil {
		t.Fatalf("live usage: %v", err)
	}
	t.Logf("live usage rows: %d", len(u))
	r, err := c.Recent(ctx, 5)
	if err != nil {
		t.Fatalf("live recent: %v", err)
	}
	t.Logf("live recent rows: %d", len(r))
}
