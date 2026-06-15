// Package llpclient is a thin HTTP client for the LLP LLM-proxy, used by the
// DSH "LLP" tab. It obtains a session token at startup by registering over LLP's
// Unix control socket (no keys in env/disk/proc), holds the token in memory, and
// re-registers automatically if the token is rejected (e.g. after an LLP restart).
package llpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrNotConfigured is returned when the base URL or control socket is unset.
var ErrNotConfigured = errors.New("LLM proxy not configured (DSH_LLP_URL / DSH_LLP_SOCKET)")

type Client struct {
	baseURL string
	socket  string
	agent   string

	dataHTTP     *http.Client // loopback TCP data API (fast timeout)
	completeHTTP *http.Client // completions (long timeout — CLI backends are slow)
	ctlHTTP      *http.Client // control socket (HTTP over UDS)

	mu    sync.Mutex
	token string
}

// New builds a client. socket is the path to LLP's control socket (as visible to
// this process, e.g. the bind-mounted /llp/control.sock); agent is the name this
// client registers as (appears in usage).
func New(baseURL, socket, agent string) *Client {
	if agent == "" {
		agent = "dsh"
	}
	return &Client{
		baseURL:      strings.TrimRight(baseURL, "/"),
		socket:       socket,
		agent:        agent,
		dataHTTP:     &http.Client{Timeout: 12 * time.Second},
		completeHTTP: &http.Client{Timeout: 210 * time.Second},
		ctlHTTP: &http.Client{
			Timeout: 5 * time.Second,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, "unix", socket)
				},
			},
		},
	}
}

func (c *Client) Configured() bool { return c.baseURL != "" && c.socket != "" }

// ensureToken returns the cached session token, registering over the control
// socket on first use.
func (c *Client) ensureToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" {
		return c.token, nil
	}
	body, _ := json.Marshal(map[string]string{"agent": c.agent})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/register", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.ctlHTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("register via control socket %s: %w", c.socket, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("register: status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.Token == "" {
		return "", fmt.Errorf("register: bad response")
	}
	c.token = out.Token
	return c.token, nil
}

func (c *Client) clearToken() {
	c.mu.Lock()
	c.token = ""
	c.mu.Unlock()
}

// do issues an authenticated request, re-registering once on a 401.
func (c *Client) do(ctx context.Context, hc *http.Client, method, path string, reqBody []byte) (int, []byte, error) {
	token, err := c.ensureToken(ctx)
	if err != nil {
		return 0, nil, err
	}
	status, body, err := c.raw(ctx, hc, method, path, reqBody, token)
	if err != nil {
		return 0, nil, err
	}
	if status == http.StatusUnauthorized {
		c.clearToken()
		if token, err = c.ensureToken(ctx); err != nil {
			return 0, nil, err
		}
		status, body, err = c.raw(ctx, hc, method, path, reqBody, token)
		if err != nil {
			return 0, nil, err
		}
	}
	return status, body, nil
}

func (c *Client) raw(ctx context.Context, hc *http.Client, method, path string, reqBody []byte, token string) (int, []byte, error) {
	var r io.Reader
	if reqBody != nil {
		r = bytes.NewReader(reqBody)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, r)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := hc.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return resp.StatusCode, body, nil
}

func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	if !c.Configured() {
		return ErrNotConfigured
	}
	status, body, err := c.do(ctx, c.dataHTTP, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("llp %s: status %d", path, status)
	}
	return json.Unmarshal(body, out)
}

// ---- typed responses ----

type Health struct {
	Status string       `json:"status"`
	Impls  []ImplHealth `json:"impls"`
}

type ImplHealth struct {
	Name        string `json:"name"`
	Available   bool   `json:"available"`
	CoolingDown bool   `json:"cooling_down"`
}

func (i ImplHealth) State() string {
	switch {
	case i.CoolingDown:
		return "cooldown"
	case i.Available:
		return "up"
	default:
		return "disabled"
	}
}

type UsageRow struct {
	Day              string  `json:"day"`
	Agent            string  `json:"agent"`
	Impl             string  `json:"impl"`
	Requests         int     `json:"requests"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	CostUSD          float64 `json:"cost_usd"`
	Errors           int     `json:"errors"`
}

type RecentRow struct {
	Ts               string  `json:"ts"`
	Agent            string  `json:"agent"`
	RequestedModel   string  `json:"requested_model"`
	ImplUsed         string  `json:"impl_used"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	CostUSD          float64 `json:"cost_usd"`
	LatencyMS        int64   `json:"latency_ms"`
	Status           string  `json:"status"`
	Error            string  `json:"error"`
	PromptPreview    string  `json:"prompt_preview"`
	ResponsePreview  string  `json:"response_preview"`
}

func (c *Client) Health(ctx context.Context) (*Health, error) {
	var h Health
	if err := c.getJSON(ctx, "/healthz", &h); err != nil {
		return nil, err
	}
	return &h, nil
}

func (c *Client) Usage(ctx context.Context) ([]UsageRow, error) {
	var wrap struct {
		Usage []UsageRow `json:"usage"`
	}
	if err := c.getJSON(ctx, "/admin/usage", &wrap); err != nil {
		return nil, err
	}
	return wrap.Usage, nil
}

func (c *Client) Recent(ctx context.Context, limit int) ([]RecentRow, error) {
	var wrap struct {
		Requests []RecentRow `json:"requests"`
	}
	if err := c.getJSON(ctx, "/admin/requests?limit="+strconv.Itoa(limit), &wrap); err != nil {
		return nil, err
	}
	return wrap.Requests, nil
}

// Message is one chat turn (matches the proxy's OpenAI shape).
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Complete runs a single-message completion. Convenience over Chat.
func (c *Client) Complete(ctx context.Context, model, prompt string) (string, string, error) {
	return c.Chat(ctx, model, []Message{{Role: "user", Content: prompt}})
}

// Chat runs a multi-turn conversation through the proxy: the caller passes the
// full message history; the proxy is stateless. Returns the assistant text and
// the impl that served it.
func (c *Client) Chat(ctx context.Context, model string, messages []Message) (text, servedBy string, err error) {
	if !c.Configured() {
		return "", "", ErrNotConfigured
	}
	if model == "" {
		model = "auto"
	}
	reqBody, _ := json.Marshal(map[string]any{"model": model, "messages": messages})
	ctx, cancel := context.WithTimeout(ctx, 205*time.Second)
	defer cancel()

	status, body, err := c.do(ctx, c.completeHTTP, http.MethodPost, "/v1/chat/completions", reqBody)
	if err != nil {
		return "", "", err
	}
	var out struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", "", fmt.Errorf("decode response (status %d)", status)
	}
	if out.Error != nil {
		return "", "", errors.New(out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return "", "", fmt.Errorf("empty response (status %d)", status)
	}
	return out.Choices[0].Message.Content, out.Model, nil
}
