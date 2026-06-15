package handler

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"dsh/internal/db"
)

// stubLLP fakes LLP: a TCP data server plus a Unix control socket that issues a
// token at /register. Returns (dataURL, socketPath).
func stubLLP(t *testing.T) (string, string) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"status": "ok", "impls": []map[string]any{
			{"name": "gemini", "available": true, "cooling_down": false},
			{"name": "claude", "available": true, "cooling_down": false},
			{"name": "openllm", "available": false, "cooling_down": false},
		}})
	})
	mux.HandleFunc("GET /admin/usage", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"usage": []map[string]any{
			{"day": "2026-06-12", "agent": "gml", "impl": "gemini", "requests": 7, "prompt_tokens": 100, "completion_tokens": 40, "cost_usd": 0, "errors": 0},
		}})
	})
	mux.HandleFunc("GET /admin/requests", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"requests": []map[string]any{
			{"ts": "2026-06-12T10:00:00Z", "agent": "gml", "requested_model": "auto", "impl_used": "gemini", "prompt_tokens": 9, "completion_tokens": 1, "latency_ms": 9800, "status": "ok", "prompt_preview": "PROMPT-PREVIEW-TEXT", "response_preview": "RESPONSE-PREVIEW-TEXT"},
		}})
	})
	mux.HandleFunc("POST /v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model    string           `json:"model"`
			Messages []map[string]any `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		json.NewEncoder(w).Encode(map[string]any{
			"model":   req.Model,
			"choices": []map[string]any{{"message": map[string]string{"content": fmt.Sprintf("hi from %s (msgs=%d)", req.Model, len(req.Messages))}}},
		})
	})
	dataSrv := httptest.NewServer(mux)
	t.Cleanup(dataSrv.Close)

	sockPath := filepath.Join(t.TempDir(), "ctl.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	ctlMux := http.NewServeMux()
	ctlMux.HandleFunc("POST /register", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"token": "tok-test", "agent": "dsh"})
	})
	ctlSrv := &http.Server{Handler: ctlMux}
	go ctlSrv.Serve(ln)
	t.Cleanup(func() { ctlSrv.Close() })

	return dataSrv.URL, sockPath
}

func newUIHandler(t *testing.T, llpURL, llpSocket string) *UIHandler {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	tmpls, err := template.ParseGlob("../../cmd/dsh/web/templates/*.html")
	if err != nil {
		t.Fatalf("parse templates: %v", err)
	}
	return &UIHandler{DB: database, Tmpls: tmpls, LLPURL: llpURL, LLPSocket: llpSocket}
}

// The tab renders backend health, usage and recent panels (after the handshake).
func TestLLMProxyPage_RendersPanels(t *testing.T) {
	dataURL, sock := stubLLP(t)
	h := newUIHandler(t, dataURL, sock)

	req := httptest.NewRequest("GET", "/llm-proxy", nil)
	w := httptest.NewRecorder()
	h.LLMProxyPage(w, req)

	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		"Backends", "gemini", "claude", "openllm", "disabled",
		"Usage", "gml",
		"Recent requests", "9800ms",
		"PROMPT-PREVIEW-TEXT", "RESPONSE-PREVIEW-TEXT", "request:", "response:", // expandable detail
		"Playground", `hx-post="/llm-proxy/run"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("rendered tab missing %q", want)
		}
	}
}

// The playground posts a prompt and shows the proxy's response.
func TestLLMProxyRun_ShowsResult(t *testing.T) {
	dataURL, sock := stubLLP(t)
	h := newUIHandler(t, dataURL, sock)

	form := url.Values{"model": {"claude"}, "prompt": {"ping"}}
	req := httptest.NewRequest("POST", "/llm-proxy/run", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.LLMProxyRun(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "hi from claude") || !strings.Contains(body, "served by") {
		t.Fatalf("playground result not rendered:\n%s", body)
	}
}

// An HTMX request returns only the result fragment (not the whole page).
func TestLLMProxyRun_HTMXFragment(t *testing.T) {
	dataURL, sock := stubLLP(t)
	h := newUIHandler(t, dataURL, sock)

	form := url.Values{"model": {"gemini"}, "prompt": {"ping"}}
	req := httptest.NewRequest("POST", "/llm-proxy/run", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	w := httptest.NewRecorder()
	h.LLMProxyRun(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "hi from gemini") {
		t.Fatalf("fragment missing result: %s", body)
	}
	if strings.Contains(body, "[todo.txt]") || strings.Contains(body, "<html") {
		t.Fatalf("HTMX response should be a fragment, not the full page:\n%s", body)
	}
}

// An empty prompt is rejected without calling the proxy.
func TestLLMProxyRun_EmptyPrompt(t *testing.T) {
	dataURL, sock := stubLLP(t)
	h := newUIHandler(t, dataURL, sock)
	form := url.Values{"model": {"auto"}, "prompt": {"  "}}
	req := httptest.NewRequest("POST", "/llm-proxy/run", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.LLMProxyRun(w, req)
	if !strings.Contains(w.Body.String(), "prompt is required") {
		t.Fatalf("expected validation error")
	}
}

// A second turn carries the conversation history, so the proxy receives the
// full transcript (user + assistant + user = 3 messages).
func TestLLMProxyRun_MultiTurn(t *testing.T) {
	dataURL, sock := stubLLP(t)
	h := newUIHandler(t, dataURL, sock)

	body1 := runPlayground(t, h, url.Values{"model": {"gemini"}, "prompt": {"first"}})
	hist := extractHistoryField(body1)
	if hist == "" {
		t.Fatalf("no history hidden field after turn 1:\n%s", body1)
	}

	body2 := runPlayground(t, h, url.Values{"model": {"gemini"}, "prompt": {"second"}, "history": {hist}})
	if !strings.Contains(body2, "msgs=3") {
		t.Fatalf("turn 2 should send 3 messages (user/assistant/user):\n%s", body2)
	}
	if !strings.Contains(body2, "first") || !strings.Contains(body2, "second") {
		t.Fatalf("transcript should show both user turns:\n%s", body2)
	}
}

// Clear resets the conversation (no transcript, no history).
func TestLLMProxyRun_Clear(t *testing.T) {
	dataURL, sock := stubLLP(t)
	h := newUIHandler(t, dataURL, sock)
	body1 := runPlayground(t, h, url.Values{"model": {"gemini"}, "prompt": {"hello"}})
	hist := extractHistoryField(body1)
	body := runPlayground(t, h, url.Values{"clear": {"1"}, "history": {hist}})
	if extractHistoryField(body) != "" {
		t.Fatalf("clear should empty the history:\n%s", body)
	}
}

func runPlayground(t *testing.T, h *UIHandler, form url.Values) string {
	t.Helper()
	req := httptest.NewRequest("POST", "/llm-proxy/run", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	w := httptest.NewRecorder()
	h.LLMProxyRun(w, req)
	return w.Body.String()
}

func extractHistoryField(body string) string {
	const marker = `name="history" value="`
	i := strings.Index(body, marker)
	if i < 0 {
		return ""
	}
	rest := body[i+len(marker):]
	j := strings.IndexByte(rest, '"')
	if j < 0 {
		return ""
	}
	return rest[:j]
}

// With no LLP configured the tab shows a clear hint instead of erroring.
func TestLLMProxyPage_NotConfigured(t *testing.T) {
	h := newUIHandler(t, "", "")
	req := httptest.NewRequest("GET", "/llm-proxy", nil)
	w := httptest.NewRecorder()
	h.LLMProxyPage(w, req)
	if !strings.Contains(w.Body.String(), "not configured") {
		t.Fatalf("expected not-configured hint")
	}
}
