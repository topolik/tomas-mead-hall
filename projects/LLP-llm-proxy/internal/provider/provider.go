// Package provider defines the backend abstraction LLP routes to. Two concrete
// impls exist: CliProvider (execs gemini/claude CLIs) and HttpProvider (calls an
// OpenAI-compatible HTTP endpoint). The router selects among them via failover.
package provider

import (
	"context"
	"strings"

	"github.com/topolik/llp-llm-proxy/internal/openai"
)

// Request is the normalized input to a provider.
type Request struct {
	// Model is the client's requested (logical) model. CLI impls use their own
	// configured model id and treat this as informational; HTTP impls fall back
	// to it when they have no configured model id.
	Model string
	// ModelID, when set, overrides the impl's configured upstream model id for
	// this request (the "/<model>" part of an "impl/<model>" request). CLI impls
	// require a configured model_flag to honor it.
	ModelID     string
	Messages    []openai.Message
	Temperature *float64
	MaxTokens   *int
}

// Response is the normalized output. Token counts are estimated for CLI impls.
type Response struct {
	Content          string
	PromptTokens     int
	CompletionTokens int
}

// Provider is a single LLM backend.
type Provider interface {
	Name() string
	Generate(ctx context.Context, req Request) (Response, error)
}

// Availabler is optionally implemented by providers that can be disabled at
// runtime (e.g. an HTTP impl with no base URL configured). The router skips
// providers that report Available() == false.
type Availabler interface {
	Available() bool
}

// Error carries routing hints so the router can decide whether to fail over.
//
//   - Retryable: try the next impl in the chain.
//   - RateLimit: in addition to Retryable, put this impl on cooldown.
//   - QuotaExhausted: the quota is gone for a long window (e.g. gemini's
//     TerminalQuotaError "resets after 3h49m"), not a momentary throttle; the
//     router uses the impl's longer quota_cooldown when one is configured.
//   - Terminal (Retryable == false): stop and return this error to the client.
type Error struct {
	Retryable      bool
	RateLimit      bool
	QuotaExhausted bool
	Status         int // suggested HTTP status for the client (0 = let server decide)
	Err            error
}

func (e *Error) Error() string {
	if e.Err == nil {
		return "provider error"
	}
	return e.Err.Error()
}

func (e *Error) Unwrap() error { return e.Err }

// EstimateTokens is a cheap chars/4 heuristic used for CLI impls, which do not
// report token usage in text mode. See ASSUMPTIONS LLP-P2.
func EstimateTokens(s string) int {
	if s == "" {
		return 0
	}
	return (len(s) + 3) / 4
}

// renderPrompt flattens chat messages into a single prompt string suitable for
// piping to a CLI's stdin. System content leads; turns follow in order. For the
// common single-message case (how GML calls the proxy) this returns the content
// verbatim.
func renderPrompt(messages []openai.Message) string {
	if len(messages) == 1 {
		return messages[0].Content
	}
	var b strings.Builder
	for _, m := range messages {
		c := strings.TrimSpace(m.Content)
		if c == "" {
			continue
		}
		switch m.Role {
		case "system":
			b.WriteString(c)
		case "assistant":
			b.WriteString("Assistant: " + c)
		default: // user and anything else
			b.WriteString("User: " + c)
		}
		b.WriteString("\n\n")
	}
	return strings.TrimSpace(b.String())
}

// stripCodeFence removes a single wrapping ```lang ... ``` fence if the whole
// (trimmed) content is fenced. It is conservative: it does not strip prose and
// is a no-op when there is no enclosing fence. Off by default per impl.
func stripCodeFence(s string) string {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, "```") {
		return s
	}
	// drop the opening fence line (``` or ```json)
	nl := strings.IndexByte(t, '\n')
	if nl < 0 {
		return s
	}
	body := t[nl+1:]
	if i := strings.LastIndex(body, "```"); i >= 0 {
		body = body[:i]
	} else {
		return s // unbalanced; leave as-is
	}
	return strings.TrimSpace(body)
}
