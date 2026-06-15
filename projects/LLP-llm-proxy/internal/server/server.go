// Package server wires auth, routing, providers and usage accounting behind the
// OpenAI-compatible HTTP surface.
package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/topolik/llp-llm-proxy/internal/auth"
	"github.com/topolik/llp-llm-proxy/internal/openai"
	"github.com/topolik/llp-llm-proxy/internal/provider"
	"github.com/topolik/llp-llm-proxy/internal/registry"
	"github.com/topolik/llp-llm-proxy/internal/router"
	"github.com/topolik/llp-llm-proxy/internal/usage"
)

type Server struct {
	reg            *registry.Registry
	rtr            *router.Router
	store          *usage.Store
	auth           *auth.Store
	maxPromptBytes int
	previewMax     int // chars of prompt/response stored per request; 0 = don't store content
}

func New(reg *registry.Registry, rtr *router.Router, store *usage.Store, a *auth.Store, maxPromptBytes, previewMax int) *Server {
	if maxPromptBytes <= 0 {
		maxPromptBytes = 1 << 20
	}
	return &Server{reg: reg, rtr: rtr, store: store, auth: a, maxPromptBytes: maxPromptBytes, previewMax: previewMax}
}

// Handler returns the configured router. /healthz is open; everything else is
// behind bearer auth.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.Handle("POST /v1/chat/completions", s.auth.Middleware(http.HandlerFunc(s.chatCompletions)))
	mux.Handle("GET /v1/models", s.auth.Middleware(http.HandlerFunc(s.models)))
	mux.Handle("GET /admin/usage", s.auth.Middleware(http.HandlerFunc(s.adminUsage)))
	mux.Handle("GET /admin/requests", s.auth.Middleware(http.HandlerFunc(s.adminRequests)))
	return mux
}

func (s *Server) chatCompletions(w http.ResponseWriter, r *http.Request) {
	agent := auth.AgentFrom(r.Context())

	body, err := io.ReadAll(io.LimitReader(r.Body, int64(s.maxPromptBytes)+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not read body", "invalid_request")
		return
	}
	if len(body) > s.maxPromptBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "request body exceeds max_prompt_bytes", "payload_too_large")
		return
	}
	var req openai.Request
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body", "invalid_request")
		return
	}
	if len(req.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "messages is required and must be non-empty", "invalid_request")
		return
	}

	chain, modelOverride := s.reg.Resolve(req.Model)
	if len(chain) == 0 {
		writeError(w, http.StatusBadRequest, "no impls available for requested model", "invalid_request")
		return
	}

	promptPreview := ""
	if s.previewMax > 0 {
		promptPreview = previewMessages(req.Messages, s.previewMax)
	}

	start := time.Now()
	used, presp, perr := s.rtr.Do(r.Context(), chain, provider.Request{
		Model:       req.Model,
		ModelID:     modelOverride,
		Messages:    req.Messages,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	})
	latency := time.Since(start).Milliseconds()

	if perr != nil {
		_ = s.store.Record(usage.Record{
			Agent: agent, RequestedModel: req.Model, ImplUsed: used,
			Status: "error", Error: truncate(perr.Error(), 500), LatencyMS: latency,
			PromptPreview: promptPreview,
		})
		status := http.StatusBadGateway
		var pe *provider.Error
		if errors.As(perr, &pe) && pe.Status != 0 {
			status = pe.Status
		}
		writeError(w, status, perr.Error(), "upstream_error")
		return
	}

	var price registry.Price
	if im, ok := s.reg.ImplByName(used); ok {
		price = im.Price
	}
	cost := usage.Cost(presp.PromptTokens, presp.CompletionTokens, price.In, price.Out)
	responsePreview := ""
	if s.previewMax > 0 {
		responsePreview = clip(presp.Content, s.previewMax)
	}
	_ = s.store.Record(usage.Record{
		Agent: agent, RequestedModel: req.Model, ImplUsed: used,
		PromptTokens: presp.PromptTokens, CompletionTokens: presp.CompletionTokens,
		CostUSD: cost, LatencyMS: latency, Status: "ok",
		PromptPreview: promptPreview, ResponsePreview: responsePreview,
	})

	if req.Stream {
		s.writeStream(w, used, presp)
		return
	}

	writeJSON(w, http.StatusOK, openai.Response{
		ID:      "chatcmpl-" + randID(),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   used,
		Choices: []openai.Choice{{
			Index:        0,
			Message:      openai.Message{Role: "assistant", Content: presp.Content},
			FinishReason: "stop",
		}},
		Usage: openai.Usage{
			PromptTokens:     presp.PromptTokens,
			CompletionTokens: presp.CompletionTokens,
			TotalTokens:      presp.PromptTokens + presp.CompletionTokens,
		},
	})
}

// streamChunkRunes bounds how much content a single SSE chunk carries.
const streamChunkRunes = 256

// writeStream replays a completed response as OpenAI-style SSE (stream=true).
// LLP streams at the façade only: the router has already run the full
// completion (failover, queueing, usage accounting unchanged), and the result
// is re-emitted as chat.completion.chunk events ending in `data: [DONE]`.
// Errors are never streamed — a failed request returns the regular JSON error
// before any SSE bytes are written. See ASSUMPTIONS LLP-018.
func (s *Server) writeStream(w http.ResponseWriter, used string, presp provider.Response) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	id := "chatcmpl-" + randID()
	created := time.Now().Unix()
	emit := func(d openai.Delta, finish *string, u *openai.Usage) {
		b, _ := json.Marshal(openai.StreamChunk{
			ID: id, Object: "chat.completion.chunk", Created: created, Model: used,
			Choices: []openai.StreamChoice{{Index: 0, Delta: d, FinishReason: finish}},
			Usage:   u,
		})
		fmt.Fprintf(w, "data: %s\n\n", b)
		if flusher != nil {
			flusher.Flush()
		}
	}

	emit(openai.Delta{Role: "assistant"}, nil, nil)
	for r := []rune(presp.Content); len(r) > 0; {
		n := min(streamChunkRunes, len(r))
		emit(openai.Delta{Content: string(r[:n])}, nil, nil)
		r = r[n:]
	}
	stop := "stop"
	emit(openai.Delta{}, &stop, &openai.Usage{
		PromptTokens:     presp.PromptTokens,
		CompletionTokens: presp.CompletionTokens,
		TotalTokens:      presp.PromptTokens + presp.CompletionTokens,
	})
	fmt.Fprint(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

func (s *Server) models(w http.ResponseWriter, _ *http.Request) {
	var data []openai.Model
	for _, name := range s.reg.ModelNames() {
		data = append(data, openai.Model{ID: name, Object: "model", OwnedBy: "llp"})
	}
	writeJSON(w, http.StatusOK, openai.ModelList{Object: "list", Data: data})
}

// healthzFailureThreshold is how many consecutive failures mark an impl
// unserveable even when it is configured and not cooling down (e.g. repeated
// timeouts, which deliberately set no cooldown).
const healthzFailureThreshold = 3

type implHealth struct {
	Name                string `json:"name"`
	Available           bool   `json:"available"`
	CoolingDown         bool   `json:"cooling_down"`
	Serveable           bool   `json:"serveable"`
	ConsecutiveFailures int    `json:"consecutive_failures"`
	LastError           string `json:"last_error,omitempty"`
	LastErrorAt         string `json:"last_error_at,omitempty"`
	LastOKAt            string `json:"last_ok_at,omitempty"`
}

// healthz reflects serveability, not just config: an impl is serveable when it
// is configured, not cooling down, and not failing consecutively. Top-level
// status is "degraded" when no impl is serveable. Always HTTP 200 — clients
// key off the status field (DSH's getJSON rejects non-200 responses).
func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	var impls []implHealth
	anyServeable := false
	for _, im := range s.reg.AllImpls() {
		available := true
		if a, ok := im.Provider.(provider.Availabler); ok {
			available = a.Available()
		}
		st := s.rtr.StatsFor(im.Name)
		cooling := s.rtr.IsCoolingDown(im.Name)
		serveable := available && !cooling && st.ConsecutiveFailures < healthzFailureThreshold
		anyServeable = anyServeable || serveable
		ih := implHealth{
			Name:                im.Name,
			Available:           available,
			CoolingDown:         cooling,
			Serveable:           serveable,
			ConsecutiveFailures: st.ConsecutiveFailures,
			LastError:           st.LastError,
		}
		if !st.LastErrorAt.IsZero() {
			ih.LastErrorAt = st.LastErrorAt.UTC().Format(time.RFC3339)
		}
		if !st.LastOKAt.IsZero() {
			ih.LastOKAt = st.LastOKAt.UTC().Format(time.RFC3339)
		}
		impls = append(impls, ih)
	}
	status := "ok"
	if !anyServeable {
		status = "degraded"
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": status, "impls": impls})
}

func (s *Server) adminUsage(w http.ResponseWriter, _ *http.Request) {
	agg, err := s.store.Aggregate()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), "internal")
		return
	}
	if agg == nil {
		agg = []usage.AggRow{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"usage": agg})
}

func (s *Server) adminRequests(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	rows, err := s.store.Recent(limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), "internal")
		return
	}
	if rows == nil {
		rows = []usage.RecentRow{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": rows})
}

// helpers

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg, typ string) {
	writeJSON(w, status, openai.ErrorResponse{Error: openai.ErrorBody{Message: msg, Type: typ}})
}

func randID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// clip truncates to at most maxRunes runes (rune-safe), marking truncation.
func clip(s string, maxRunes int) string {
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + "\n…[truncated]"
}

// previewMessages renders chat messages into a single preview string, clipped
// to maxRunes. Non-user roles are labeled so a system prompt is distinguishable.
func previewMessages(msgs []openai.Message, maxRunes int) string {
	var b strings.Builder
	for i, m := range msgs {
		if i > 0 {
			b.WriteString("\n")
		}
		if m.Role != "" && m.Role != "user" {
			b.WriteString(m.Role + ": ")
		}
		b.WriteString(m.Content)
	}
	return clip(b.String(), maxRunes)
}
