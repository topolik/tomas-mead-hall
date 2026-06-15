// Package openai defines the OpenAI-compatible request/response shapes that LLP
// exposes to clients on /v1/chat/completions. It is a leaf package: no internal
// imports, so every other package can depend on it.
package openai

// Message is a single chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Request is the body of POST /v1/chat/completions.
type Request struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature *float64  `json:"temperature,omitempty"`
	MaxTokens   *int      `json:"max_tokens,omitempty"`
	Stream      bool      `json:"stream,omitempty"`
}

// Usage reports token counts. For CLI-backed impls these are estimated
// (see provider.EstimateTokens); for HTTP impls they are the upstream's real counts.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Choice is one completion option. LLP always returns a single choice.
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// Response is the body returned from POST /v1/chat/completions.
type Response struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// Delta is the incremental payload of a streaming choice. Role is sent on the
// first chunk only; Content on the chunks that carry text.
type Delta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

// StreamChoice is one choice inside a chat.completion.chunk event.
// FinishReason is null until the final content-bearing chunk.
type StreamChoice struct {
	Index        int     `json:"index"`
	Delta        Delta   `json:"delta"`
	FinishReason *string `json:"finish_reason"`
}

// StreamChunk is the body of a single SSE `data:` event when stream=true.
// The final chunk carries Usage (estimated for CLI impls, as in Response).
type StreamChunk struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"` // "chat.completion.chunk"
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []StreamChoice `json:"choices"`
	Usage   *Usage         `json:"usage,omitempty"`
}

// ErrorBody / ErrorResponse mirror OpenAI's error envelope.
type ErrorBody struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
}

type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

// Model and ModelList back GET /v1/models.
type Model struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	OwnedBy string `json:"owned_by"`
}

type ModelList struct {
	Object string  `json:"object"`
	Data   []Model `json:"data"`
}
