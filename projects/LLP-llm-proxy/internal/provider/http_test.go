package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/topolik/llp-llm-proxy/internal/openai"
)

// T4: a 200 OpenAI-shaped response maps to content + real usage, and the
// request carries the configured model + bearer key.
func TestHttp_SuccessMapping(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer k-123" {
			t.Errorf("auth header = %q", got)
		}
		var req openai.Request
		json.NewDecoder(r.Body).Decode(&req)
		if req.Model != "llama-3" {
			t.Errorf("model = %q", req.Model)
		}
		json.NewEncoder(w).Encode(openai.Response{
			Choices: []openai.Choice{{Message: openai.Message{Role: "assistant", Content: "hi there"}}},
			Usage:   openai.Usage{PromptTokens: 11, CompletionTokens: 7, TotalTokens: 18},
		})
	}))
	defer srv.Close()

	p := NewHttp(HttpConfig{Name: "openllm", BaseURL: srv.URL + "/v1", APIKey: "k-123", ModelID: "llama-3"})
	resp, err := p.Generate(context.Background(), Request{Model: "auto", Messages: userMsg("hello")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "hi there" || resp.PromptTokens != 11 || resp.CompletionTokens != 7 {
		t.Fatalf("bad mapping: %+v", resp)
	}
}

// T4: a per-request model override is sent as the request's model.
func TestHttp_ModelOverride(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req openai.Request
		json.NewDecoder(r.Body).Decode(&req)
		if req.Model != "override-model" {
			t.Errorf("model = %q, want override-model", req.Model)
		}
		json.NewEncoder(w).Encode(openai.Response{Choices: []openai.Choice{{Message: openai.Message{Content: "ok"}}}})
	}))
	defer srv.Close()
	p := NewHttp(HttpConfig{Name: "openllm", BaseURL: srv.URL + "/v1", ModelID: "configured-model"})
	if _, err := p.Generate(context.Background(), Request{ModelID: "override-model", Messages: userMsg("x")}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// T4: 429 => retryable + RateLimit.
func TestHttp_429Retryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "slow down", http.StatusTooManyRequests)
	}))
	defer srv.Close()
	p := NewHttp(HttpConfig{Name: "openllm", BaseURL: srv.URL + "/v1"})
	_, err := p.Generate(context.Background(), Request{Messages: userMsg("x")})
	var pe *Error
	if !errors.As(err, &pe) || !pe.Retryable || !pe.RateLimit {
		t.Fatalf("want retryable+ratelimit, got %v", err)
	}
}

// T4: 400 => terminal (not retryable).
func TestHttp_400Terminal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer srv.Close()
	p := NewHttp(HttpConfig{Name: "openllm", BaseURL: srv.URL + "/v1"})
	_, err := p.Generate(context.Background(), Request{Messages: userMsg("x")})
	var pe *Error
	if !errors.As(err, &pe) {
		t.Fatalf("want *Error, got %T", err)
	}
	if pe.Retryable {
		t.Fatalf("400 should be terminal, got %+v", pe)
	}
}

// T4: empty base URL => disabled (Available() false) and Generate errors retryably.
func TestHttp_DisabledWhenNoBaseURL(t *testing.T) {
	p := NewHttp(HttpConfig{Name: "openllm", BaseURL: ""})
	if p.Available() {
		t.Fatalf("expected Available() == false")
	}
	_, err := p.Generate(context.Background(), Request{Messages: userMsg("x")})
	var pe *Error
	if !errors.As(err, &pe) || !pe.Retryable {
		t.Fatalf("want retryable not-configured error, got %v", err)
	}
}
