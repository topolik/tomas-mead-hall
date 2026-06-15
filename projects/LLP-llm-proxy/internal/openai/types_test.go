package openai

import (
	"encoding/json"
	"testing"
)

// T1: Request/Response round-trip through JSON, minimal + full payloads.
func TestRequestRoundTrip(t *testing.T) {
	temp := 0.7
	max := 256
	in := Request{
		Model:       "gemini",
		Messages:    []Message{{Role: "system", Content: "be terse"}, {Role: "user", Content: "hi"}},
		Temperature: &temp,
		MaxTokens:   &max,
		Stream:      false,
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Request
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Model != in.Model || len(out.Messages) != 2 || out.Messages[1].Content != "hi" {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
	if out.Temperature == nil || *out.Temperature != temp || out.MaxTokens == nil || *out.MaxTokens != max {
		t.Fatalf("optional fields lost: %+v", out)
	}
}

func TestRequestMinimalOmitsOptionals(t *testing.T) {
	in := Request{Model: "auto", Messages: []Message{{Role: "user", Content: "x"}}}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	for _, omitted := range []string{"temperature", "max_tokens", "stream"} {
		if contains(s, omitted) {
			t.Fatalf("expected %q omitted, got %s", omitted, s)
		}
	}
}

func TestResponseRoundTrip(t *testing.T) {
	in := Response{
		ID:      "chatcmpl-1",
		Object:  "chat.completion",
		Created: 1700000000,
		Model:   "gemini",
		Choices: []Choice{{Index: 0, Message: Message{Role: "assistant", Content: "hello"}, FinishReason: "stop"}},
		Usage:   Usage{PromptTokens: 3, CompletionTokens: 2, TotalTokens: 5},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Response
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Choices[0].Message.Content != "hello" || out.Usage.TotalTokens != 5 || out.Object != "chat.completion" {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
