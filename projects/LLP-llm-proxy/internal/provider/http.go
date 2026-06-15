package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/topolik/llp-llm-proxy/internal/openai"
)

// HttpProvider backs an impl by calling an OpenAI-compatible HTTP endpoint
// (OpenLLM, Ollama's /v1, OpenRouter, or any remote that speaks the protocol).
// It is disabled (and skipped by the router) when baseURL is empty — that is the
// v1 "stubbed" state for impl #3. See LLP-007.
type HttpProvider struct {
	name    string
	baseURL string // e.g. https://host/v1 ; "/chat/completions" is appended
	apiKey  string
	modelID string
	client  *http.Client
}

type HttpConfig struct {
	Name    string
	BaseURL string
	APIKey  string
	ModelID string
	Timeout time.Duration
}

func NewHttp(c HttpConfig) *HttpProvider {
	if c.Timeout <= 0 {
		c.Timeout = 180 * time.Second
	}
	return &HttpProvider{
		name:    c.Name,
		baseURL: strings.TrimRight(c.BaseURL, "/"),
		apiKey:  c.APIKey,
		modelID: c.ModelID,
		client:  &http.Client{Timeout: c.Timeout},
	}
}

func (p *HttpProvider) Name() string { return p.name }

// Available reports whether a base URL is configured. The router skips
// unavailable providers.
func (p *HttpProvider) Available() bool { return p.baseURL != "" }

func (p *HttpProvider) Generate(ctx context.Context, req Request) (Response, error) {
	if !p.Available() {
		return Response{}, &Error{Retryable: true, Err: fmt.Errorf("%s: not configured", p.name)}
	}
	model := p.modelID
	if req.ModelID != "" {
		model = req.ModelID
	} else if model == "" {
		model = req.Model
	}
	body, err := json.Marshal(openai.Request{
		Model:       model,
		Messages:    req.Messages,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	})
	if err != nil {
		return Response{}, &Error{Err: fmt.Errorf("%s: marshal: %w", p.name, err)}
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Response{}, &Error{Err: fmt.Errorf("%s: build request: %w", p.name, err)}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		// transport errors (incl. timeout) => retryable
		return Response{}, &Error{Retryable: true, Err: fmt.Errorf("%s: %w", p.name, err)}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))

	if resp.StatusCode != http.StatusOK {
		return Response{}, p.classifyStatus(resp.StatusCode, raw)
	}

	var out openai.Response
	if err := json.Unmarshal(raw, &out); err != nil {
		return Response{}, &Error{Retryable: true, Err: fmt.Errorf("%s: decode: %w", p.name, err)}
	}
	if len(out.Choices) == 0 {
		return Response{}, &Error{Retryable: true, Err: fmt.Errorf("%s: no choices in response", p.name)}
	}
	return Response{
		Content:          out.Choices[0].Message.Content,
		PromptTokens:     out.Usage.PromptTokens,
		CompletionTokens: out.Usage.CompletionTokens,
	}, nil
}

func (p *HttpProvider) classifyStatus(status int, raw []byte) error {
	msg := truncate(string(raw), 300)
	switch {
	case status == http.StatusTooManyRequests:
		return &Error{Retryable: true, RateLimit: true, Status: status, Err: fmt.Errorf("%s: 429: %s", p.name, msg)}
	case status >= 500:
		return &Error{Retryable: true, Status: status, Err: fmt.Errorf("%s: %d: %s", p.name, status, msg)}
	default: // 400, 401, 403, ... => terminal
		return &Error{Retryable: false, Status: status, Err: fmt.Errorf("%s: %d: %s", p.name, status, msg)}
	}
}
