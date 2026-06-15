package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/topolik/llp-llm-proxy/internal/auth"
	"github.com/topolik/llp-llm-proxy/internal/openai"
	"github.com/topolik/llp-llm-proxy/internal/provider"
	"github.com/topolik/llp-llm-proxy/internal/registry"
	"github.com/topolik/llp-llm-proxy/internal/router"
	"github.com/topolik/llp-llm-proxy/internal/usage"
)

type fakeProvider struct {
	name string
	fn   func(provider.Request) (provider.Response, error)
}

func (f *fakeProvider) Name() string { return f.name }
func (f *fakeProvider) Generate(_ context.Context, req provider.Request) (provider.Response, error) {
	return f.fn(req)
}

// newTestServer builds a Server backed by one fake "good" impl plus an
// always-failing "bad" impl, and a temp SQLite store.
func newTestServer(t *testing.T, maxBytes int) (*Server, *usage.Store) {
	t.Helper()
	good := &fakeProvider{name: "good", fn: func(r provider.Request) (provider.Response, error) {
		return provider.Response{Content: "echo:" + r.Messages[len(r.Messages)-1].Content, PromptTokens: 12, CompletionTokens: 4}, nil
	}}
	impls := map[string]*registry.Impl{
		"good": {Name: "good", Provider: good, Concurrency: 1, Price: registry.Price{In: 3, Out: 15}},
	}
	reg := registry.NewRegistry(impls, map[string][]string{"auto": {"good"}}, "auto")
	rtr := router.New(reg.AllImpls())
	store, err := usage.Open(filepath.Join(t.TempDir(), "u.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	a := auth.NewStore()
	a.Set("secret", "tester")
	return New(reg, rtr, store, a, maxBytes, 4096), store
}

func post(t *testing.T, h http.Handler, body string, withAuth bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader([]byte(body)))
	if withAuth {
		req.Header.Set("Authorization", "Bearer secret")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// T9: happy path returns an OpenAI-shaped response and records usage.
func TestChatCompletionsHappyPath(t *testing.T) {
	s, store := newTestServer(t, 1<<20)
	h := s.Handler()

	rec := post(t, h, `{"model":"auto","messages":[{"role":"user","content":"ping"}]}`, true)
	if rec.Code != 200 {
		t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
	}
	var resp openai.Response
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Object != "chat.completion" || resp.Model != "good" {
		t.Fatalf("bad envelope: %+v", resp)
	}
	if resp.Choices[0].Message.Content != "echo:ping" || resp.Choices[0].FinishReason != "stop" {
		t.Fatalf("bad choice: %+v", resp.Choices)
	}
	if resp.Usage.TotalTokens != 16 {
		t.Fatalf("usage total = %d", resp.Usage.TotalTokens)
	}
	if !strings.HasPrefix(resp.ID, "chatcmpl-") {
		t.Fatalf("bad id %q", resp.ID)
	}
	// usage recorded
	agg, _ := store.Aggregate()
	if len(agg) != 1 || agg[0].Agent != "tester" || agg[0].Impl != "good" || agg[0].Requests != 1 {
		t.Fatalf("usage not recorded: %+v", agg)
	}
	// cost = (12 in @ $3/1M) + (4 out @ $15/1M)
	wantCost := usage.Cost(12, 4, 3, 15)
	if agg[0].CostUSD != wantCost {
		t.Fatalf("cost = %v want %v", agg[0].CostUSD, wantCost)
	}
	// prompt/response previews are stored (preview enabled in newTestServer)
	recent, _ := store.Recent(1)
	if len(recent) != 1 || !strings.Contains(recent[0].PromptPreview, "ping") || recent[0].ResponsePreview != "echo:ping" {
		t.Fatalf("previews not stored: %+v", recent)
	}
}

// T9: no auth => 401.
func TestChatCompletionsRequiresAuth(t *testing.T) {
	s, _ := newTestServer(t, 1<<20)
	rec := post(t, s.Handler(), `{"model":"auto","messages":[{"role":"user","content":"x"}]}`, false)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

// T9: bad JSON => 400.
func TestChatCompletionsBadJSON(t *testing.T) {
	s, _ := newTestServer(t, 1<<20)
	rec := post(t, s.Handler(), `{not json`, true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// T9: empty messages => 400.
func TestChatCompletionsEmptyMessages(t *testing.T) {
	s, _ := newTestServer(t, 1<<20)
	rec := post(t, s.Handler(), `{"model":"auto","messages":[]}`, true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// T9: oversized body => 413.
func TestChatCompletionsTooLarge(t *testing.T) {
	s, _ := newTestServer(t, 32) // tiny limit
	big := `{"model":"auto","messages":[{"role":"user","content":"` + strings.Repeat("x", 200) + `"}]}`
	rec := post(t, s.Handler(), big, true)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d", rec.Code)
	}
}

// T9: terminal upstream error propagates the provider's status.
func TestChatCompletionsUpstreamTerminal(t *testing.T) {
	bad := &fakeProvider{name: "bad", fn: func(provider.Request) (provider.Response, error) {
		return provider.Response{}, &provider.Error{Retryable: false, Status: 400, Err: fmt.Errorf("nope")}
	}}
	reg := registry.NewRegistry(
		map[string]*registry.Impl{"bad": {Name: "bad", Provider: bad, Concurrency: 1}},
		map[string][]string{"auto": {"bad"}}, "auto")
	store, _ := usage.Open(filepath.Join(t.TempDir(), "u.db"))
	a := auth.NewStore()
	a.Set("secret", "t")
	s := New(reg, router.New(reg.AllImpls()), store, a, 1<<20, 4096)

	rec := post(t, s.Handler(), `{"model":"auto","messages":[{"role":"user","content":"x"}]}`, true)
	if rec.Code != 400 {
		t.Fatalf("expected propagated 400, got %d", rec.Code)
	}
	agg, _ := store.Aggregate()
	if len(agg) != 1 || agg[0].Errors != 1 {
		t.Fatalf("expected 1 error row, got %+v", agg)
	}
}

// T9: requesting "impl/<model>" pins the impl and plumbs the model override
// through to the provider.
func TestChatCompletionsModelSelection(t *testing.T) {
	var gotModelID string
	rec := &fakeProvider{name: "claudeish", fn: func(r provider.Request) (provider.Response, error) {
		gotModelID = r.ModelID
		return provider.Response{Content: "ok", PromptTokens: 1, CompletionTokens: 1}, nil
	}}
	reg := registry.NewRegistry(
		map[string]*registry.Impl{"claudeish": {Name: "claudeish", Provider: rec, Concurrency: 1}},
		map[string][]string{"auto": {"claudeish"}}, "auto")
	store, _ := usage.Open(filepath.Join(t.TempDir(), "u.db"))
	a := auth.NewStore()
	a.Set("secret", "t")
	s := New(reg, router.New(reg.AllImpls()), store, a, 1<<20, 4096)
	h := s.Handler()

	// pin by impl name + override the underlying model id
	rsp := post(t, h, `{"model":"claudeish/some-model-9","messages":[{"role":"user","content":"x"}]}`, true)
	if rsp.Code != 200 {
		t.Fatalf("status %d body=%s", rsp.Code, rsp.Body.String())
	}
	if gotModelID != "some-model-9" {
		t.Fatalf("provider saw ModelID=%q, want some-model-9", gotModelID)
	}
	var resp openai.Response
	json.Unmarshal(rsp.Body.Bytes(), &resp)
	if resp.Model != "claudeish" {
		t.Fatalf("served_by = %q, want claudeish", resp.Model)
	}
}

// parseSSE splits an SSE body into its `data:` payloads.
func parseSSE(t *testing.T, body string) []string {
	t.Helper()
	var out []string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "data: ") {
			out = append(out, strings.TrimPrefix(line, "data: "))
		}
	}
	if len(out) == 0 {
		t.Fatalf("no SSE data lines in %q", body)
	}
	return out
}

// stream=true replays the completed response as OpenAI chunk events:
// role first, content deltas that concatenate to the full text, a final
// finish_reason=stop chunk carrying usage, then [DONE]. Usage is recorded
// exactly as in the non-streaming path.
func TestChatCompletionsStreaming(t *testing.T) {
	s, store := newTestServer(t, 1<<20)
	h := s.Handler()

	rec := post(t, h, `{"model":"auto","stream":true,"messages":[{"role":"user","content":"ping"}]}`, true)
	if rec.Code != 200 {
		t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type %q", ct)
	}

	events := parseSSE(t, rec.Body.String())
	if events[len(events)-1] != "[DONE]" {
		t.Fatalf("stream must end with [DONE], got %q", events[len(events)-1])
	}

	var content strings.Builder
	var finish string
	var usageChunk *openai.Usage
	for i, ev := range events[:len(events)-1] {
		var c openai.StreamChunk
		if err := json.Unmarshal([]byte(ev), &c); err != nil {
			t.Fatalf("chunk %d: %v (%q)", i, err, ev)
		}
		if c.Object != "chat.completion.chunk" || c.Model != "good" || !strings.HasPrefix(c.ID, "chatcmpl-") {
			t.Fatalf("bad chunk envelope: %+v", c)
		}
		if i == 0 && c.Choices[0].Delta.Role != "assistant" {
			t.Fatalf("first chunk must carry the role: %+v", c)
		}
		content.WriteString(c.Choices[0].Delta.Content)
		if fr := c.Choices[0].FinishReason; fr != nil {
			finish = *fr
			usageChunk = c.Usage
		}
	}
	if content.String() != "echo:ping" {
		t.Fatalf("concatenated deltas = %q", content.String())
	}
	if finish != "stop" || usageChunk == nil || usageChunk.TotalTokens != 16 {
		t.Fatalf("final chunk: finish=%q usage=%+v", finish, usageChunk)
	}

	agg, _ := store.Aggregate()
	if len(agg) != 1 || agg[0].Requests != 1 {
		t.Fatalf("usage not recorded for streamed request: %+v", agg)
	}
}

// Long content is split across multiple delta chunks (rune-safe).
func TestStreamingSplitsLongContent(t *testing.T) {
	long := strings.Repeat("é", streamChunkRunes+10) // multi-byte runes
	big := &fakeProvider{name: "big", fn: func(provider.Request) (provider.Response, error) {
		return provider.Response{Content: long, PromptTokens: 1, CompletionTokens: 1}, nil
	}}
	impls := map[string]*registry.Impl{"big": {Name: "big", Provider: big, Concurrency: 1}}
	reg := registry.NewRegistry(impls, map[string][]string{"auto": {"big"}}, "auto")
	store, err := usage.Open(filepath.Join(t.TempDir(), "u.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	a := auth.NewStore()
	a.Set("secret", "tester")
	s := New(reg, router.New(reg.AllImpls()), store, a, 1<<20, 4096)

	rec := post(t, s.Handler(), `{"model":"auto","stream":true,"messages":[{"role":"user","content":"x"}]}`, true)
	events := parseSSE(t, rec.Body.String())
	var content strings.Builder
	contentChunks := 0
	for _, ev := range events {
		if ev == "[DONE]" {
			continue
		}
		var c openai.StreamChunk
		if err := json.Unmarshal([]byte(ev), &c); err != nil {
			t.Fatalf("chunk: %v", err)
		}
		if d := c.Choices[0].Delta.Content; d != "" {
			contentChunks++
			content.WriteString(d)
		}
	}
	if contentChunks < 2 {
		t.Fatalf("long content should span multiple chunks, got %d", contentChunks)
	}
	if content.String() != long {
		t.Fatalf("reassembled content differs (len %d vs %d)", len(content.String()), len(long))
	}
}

// A failure with stream=true returns the regular JSON error (no SSE bytes).
func TestStreamingErrorStaysJSON(t *testing.T) {
	bad := &fakeProvider{name: "bad", fn: func(provider.Request) (provider.Response, error) {
		return provider.Response{}, &provider.Error{Retryable: true, Err: fmt.Errorf("boom")}
	}}
	impls := map[string]*registry.Impl{"bad": {Name: "bad", Provider: bad, Concurrency: 1}}
	reg := registry.NewRegistry(impls, map[string][]string{"auto": {"bad"}}, "auto")
	store, err := usage.Open(filepath.Join(t.TempDir(), "u.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	a := auth.NewStore()
	a.Set("secret", "tester")
	s := New(reg, router.New(reg.AllImpls()), store, a, 1<<20, 4096)

	rec := post(t, s.Handler(), `{"model":"auto","stream":true,"messages":[{"role":"user","content":"x"}]}`, true)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("error must be JSON, got content-type %q (body %q)", ct, rec.Body.String())
	}
}

// healthz reflects serveability: consecutive failures flip the impl (and, with
// no other impl serveable, the whole proxy) to degraded; a success recovers it.
func TestHealthzReflectsServeability(t *testing.T) {
	calls := 0
	flaky := &fakeProvider{name: "flaky", fn: func(provider.Request) (provider.Response, error) {
		calls++
		if calls <= healthzFailureThreshold {
			return provider.Response{}, &provider.Error{Retryable: true, Err: fmt.Errorf("boom")}
		}
		return provider.Response{Content: "ok"}, nil
	}}
	impls := map[string]*registry.Impl{
		"flaky": {Name: "flaky", Provider: flaky, Concurrency: 1}, // no cooldown: failures must show anyway
	}
	reg := registry.NewRegistry(impls, map[string][]string{"auto": {"flaky"}}, "auto")
	rtr := router.New(reg.AllImpls())
	store, err := usage.Open(filepath.Join(t.TempDir(), "u.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	a := auth.NewStore()
	a.Set("secret", "tester")
	s := New(reg, rtr, store, a, 1<<20, 4096)
	h := s.Handler()

	getHealth := func() (string, implHealth) {
		t.Helper()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
		if rec.Code != 200 {
			t.Fatalf("healthz must stay HTTP 200 (DSH rejects non-200), got %d", rec.Code)
		}
		var body struct {
			Status string       `json:"status"`
			Impls  []implHealth `json:"impls"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("healthz json: %v", err)
		}
		return body.Status, body.Impls[0]
	}

	// Fresh server, no traffic: configured impl counts as serveable.
	if status, ih := getHealth(); status != "ok" || !ih.Serveable {
		t.Fatalf("fresh: status=%s impl=%+v", status, ih)
	}

	// Fail threshold times -> impl unserveable, status degraded, error visible.
	for i := 0; i < healthzFailureThreshold; i++ {
		post(t, h, `{"model":"auto","messages":[{"role":"user","content":"hi"}]}`, true)
	}
	status, ih := getHealth()
	if status != "degraded" || ih.Serveable || ih.ConsecutiveFailures != healthzFailureThreshold {
		t.Fatalf("after %d failures: status=%s impl=%+v", healthzFailureThreshold, status, ih)
	}
	if !strings.Contains(ih.LastError, "boom") || ih.LastErrorAt == "" {
		t.Fatalf("last error should be visible: %+v", ih)
	}

	// One success -> recovered.
	post(t, h, `{"model":"auto","messages":[{"role":"user","content":"hi"}]}`, true)
	if status, ih := getHealth(); status != "ok" || !ih.Serveable || ih.LastOKAt == "" {
		t.Fatalf("after success: status=%s impl=%+v", status, ih)
	}
}

// T9: /v1/models lists logical models; /healthz is open; /admin/usage needs auth.
func TestModelsHealthAndUsageEndpoints(t *testing.T) {
	s, _ := newTestServer(t, 1<<20)
	h := s.Handler()

	// models (auth)
	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"auto"`) {
		t.Fatalf("models: %d %s", rec.Code, rec.Body.String())
	}

	// healthz (open)
	req = httptest.NewRequest("GET", "/healthz", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Fatalf("healthz: %d %s", rec.Code, rec.Body.String())
	}

	// admin/usage without auth => 401
	req = httptest.NewRequest("GET", "/admin/usage", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("admin/usage should require auth, got %d", rec.Code)
	}

	// admin/requests with auth => 200 + {"requests":[...]}
	req = httptest.NewRequest("GET", "/admin/requests?limit=10", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"requests"`) {
		t.Fatalf("admin/requests: %d %s", rec.Code, rec.Body.String())
	}
}
