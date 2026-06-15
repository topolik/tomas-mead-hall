package provider

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/topolik/llp-llm-proxy/internal/openai"
)

// writeScript drops an executable shell script and returns its path.
func writeScript(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "fake-cli.sh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return p
}

func userMsg(s string) []openai.Message { return []openai.Message{{Role: "user", Content: s}} }

// T3: stdin is piped to the CLI and stdout becomes the content.
func TestCli_PipesStdinReturnsStdout(t *testing.T) {
	// echoes "ANSWER:" + whatever it read on stdin
	script := writeScript(t, `printf 'ANSWER: '; cat`)
	p := NewCli(CliConfig{Name: "fake", Command: []string{script}})
	resp, err := p.Generate(context.Background(), Request{Messages: userMsg("ping")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "ANSWER: ping" {
		t.Fatalf("got %q", resp.Content)
	}
	if resp.PromptTokens == 0 || resp.CompletionTokens == 0 {
		t.Fatalf("expected estimated tokens, got %+v", resp)
	}
}

// T3: stripFence removes a wrapping code fence when enabled.
func TestCli_StripFence(t *testing.T) {
	script := writeScript(t, "printf '```json\\n{\\\"a\\\":1}\\n```\\n'")
	p := NewCli(CliConfig{Name: "fake", Command: []string{script}, StripFence: true})
	resp, err := p.Generate(context.Background(), Request{Messages: userMsg("x")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != `{"a":1}` {
		t.Fatalf("fence not stripped: %q", resp.Content)
	}
}

// T3: rate-limit text on stderr => retryable + RateLimit.
func TestCli_RateLimitStderr(t *testing.T) {
	script := writeScript(t, `echo "Error: RESOURCE_EXHAUSTED quota" 1>&2; exit 1`)
	p := NewCli(CliConfig{Name: "fake", Command: []string{script}})
	_, err := p.Generate(context.Background(), Request{Messages: userMsg("x")})
	var pe *Error
	if !errors.As(err, &pe) {
		t.Fatalf("want *Error, got %T %v", err, err)
	}
	if !pe.Retryable || !pe.RateLimit {
		t.Fatalf("want retryable+ratelimit, got %+v", pe)
	}
}

// T3 (regression): a model-not-found error whose stderr stack trace mentions
// the file `googleQuotaErrors.js` must NOT be misread as a rate limit (the bare
// "quota" substring used to match the filename and wrongly trigger cooldown).
func TestCli_ModelNotFoundIsNotRateLimit(t *testing.T) {
	stderr := `Loaded cached credentials.
Error when talking to Gemini API ModelNotFoundError: Requested entity was not found.
    at classifyGoogleError (file:///home/x/node_modules/@google/gemini-cli-core/dist/src/utils/googleQuotaErrors.js:145:16)
  code: 404`
	script := writeScript(t, `echo `+shellQuote(stderr)+` 1>&2; exit 1`)
	p := NewCli(CliConfig{Name: "fake", Command: []string{script}})
	_, err := p.Generate(context.Background(), Request{Messages: userMsg("x")})
	var pe *Error
	if !errors.As(err, &pe) {
		t.Fatalf("want *Error, got %T", err)
	}
	if pe.RateLimit {
		t.Fatalf("model-not-found must NOT be classified as rate limit (would wrongly cooldown): %+v", pe)
	}
}

// Gemini's TerminalQuotaError (daily/long-window quota, observed live 2026-06-12:
// "resets after 3h49m") must be classified QuotaExhausted so the router can apply
// the longer quota_cooldown instead of the 60s throttle cooldown.
func TestCli_TerminalQuotaErrorIsQuotaExhausted(t *testing.T) {
	cases := []string{
		// gemini text mode: "[API Error: <msg>]" then the re-thrown stack trace
		`[API Error: You have exhausted your daily quota on this model.]
TerminalQuotaError: You have exhausted your daily quota on this model.
    at classifyGoogleError (file:///home/x/node_modules/@google/gemini-cli-core/dist/src/utils/googleQuotaErrors.js:225:16)`,
		`[API Error: Quota exceeded for model gemini-3-pro-preview. Your quota will reset after 3h49m.]`,
		// claude CLI out of the usage window
		`Claude AI usage limit reached|1765576800`,
	}
	for _, stderr := range cases {
		script := writeScript(t, `echo `+shellQuote(stderr)+` 1>&2; exit 1`)
		p := NewCli(CliConfig{Name: "fake", Command: []string{script}})
		_, err := p.Generate(context.Background(), Request{Messages: userMsg("x")})
		var pe *Error
		if !errors.As(err, &pe) {
			t.Fatalf("want *Error, got %T (stderr %q)", err, stderr)
		}
		if !pe.QuotaExhausted || !pe.RateLimit || !pe.Retryable {
			t.Fatalf("want quota-exhausted+ratelimit+retryable for %q, got %+v", stderr, pe)
		}
	}
}

// A RetryableQuotaError that exhausted gemini-cli's internal attempts is a plain
// rate limit (regular cooldown), not a long-window quota exhaustion.
func TestCli_RetryableQuotaErrorIsRateLimitNotQuota(t *testing.T) {
	stderr := `[API Error: RetryableQuotaError: Resource exhausted, please try again later.
Suggested retry after 34.07s.]`
	script := writeScript(t, `echo `+shellQuote(stderr)+` 1>&2; exit 1`)
	p := NewCli(CliConfig{Name: "fake", Command: []string{script}})
	_, err := p.Generate(context.Background(), Request{Messages: userMsg("x")})
	var pe *Error
	if !errors.As(err, &pe) {
		t.Fatalf("want *Error, got %T", err)
	}
	if !pe.RateLimit || pe.QuotaExhausted {
		t.Fatalf("want ratelimit && !quota-exhausted, got %+v", pe)
	}
}

// LLP-011 extended: stack frames mentioning googleQuotaErrors.js must not match
// the quota-exhausted patterns either (the 404 case below carries such a frame).
func TestCli_ModelNotFoundIsNotQuotaExhausted(t *testing.T) {
	stderr := `Error when talking to Gemini API ModelNotFoundError: Requested entity was not found.
    at classifyGoogleError (file:///home/x/node_modules/@google/gemini-cli-core/dist/src/utils/googleQuotaErrors.js:145:16)
  code: 404`
	script := writeScript(t, `echo `+shellQuote(stderr)+` 1>&2; exit 1`)
	p := NewCli(CliConfig{Name: "fake", Command: []string{script}})
	_, err := p.Generate(context.Background(), Request{Messages: userMsg("x")})
	var pe *Error
	if !errors.As(err, &pe) {
		t.Fatalf("want *Error, got %T", err)
	}
	if pe.QuotaExhausted || pe.RateLimit {
		t.Fatalf("model-not-found must NOT look like quota exhaustion: %+v", pe)
	}
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// T3: a plain non-zero exit is retryable but not a rate limit.
func TestCli_NonZeroExitRetryableNotRateLimit(t *testing.T) {
	script := writeScript(t, `echo "boom" 1>&2; exit 3`)
	p := NewCli(CliConfig{Name: "fake", Command: []string{script}})
	_, err := p.Generate(context.Background(), Request{Messages: userMsg("x")})
	var pe *Error
	if !errors.As(err, &pe) {
		t.Fatalf("want *Error, got %T", err)
	}
	if !pe.Retryable || pe.RateLimit {
		t.Fatalf("want retryable && !ratelimit, got %+v", pe)
	}
}

// T3: a hung CLI hits the timeout and yields a retryable error.
func TestCli_Timeout(t *testing.T) {
	script := writeScript(t, `sleep 5`)
	p := NewCli(CliConfig{Name: "fake", Command: []string{script}, Timeout: 150 * time.Millisecond})
	start := time.Now()
	_, err := p.Generate(context.Background(), Request{Messages: userMsg("x")})
	if time.Since(start) > 2*time.Second {
		t.Fatalf("timeout not enforced")
	}
	var pe *Error
	if !errors.As(err, &pe) || !pe.Retryable {
		t.Fatalf("want retryable timeout error, got %v", err)
	}
}

// T3: empty stdout is treated as a retryable failure.
func TestCli_EmptyOutputRetryable(t *testing.T) {
	script := writeScript(t, `exit 0`)
	p := NewCli(CliConfig{Name: "fake", Command: []string{script}})
	_, err := p.Generate(context.Background(), Request{Messages: userMsg("x")})
	var pe *Error
	if !errors.As(err, &pe) || !pe.Retryable {
		t.Fatalf("want retryable empty-output error, got %v", err)
	}
}

// T3: a per-request model override appends the configured model flag + value.
func TestCli_ModelOverrideAppliesFlag(t *testing.T) {
	script := writeScript(t, `printf 'ARGS:%s' "$*"`) // echo argv (after the script path)
	p := NewCli(CliConfig{Name: "fake", Command: []string{script}, ModelFlag: "--model"})
	resp, err := p.Generate(context.Background(), Request{Messages: userMsg("x"), ModelID: "m-123"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "ARGS:--model m-123" {
		t.Fatalf("model flag not applied: %q", resp.Content)
	}
}

// T3: requesting an override with no model_flag configured is a terminal error.
func TestCli_ModelOverrideNoFlagErrors(t *testing.T) {
	script := writeScript(t, `cat`)
	p := NewCli(CliConfig{Name: "fake", Command: []string{script}}) // no ModelFlag
	_, err := p.Generate(context.Background(), Request{Messages: userMsg("x"), ModelID: "m-123"})
	var pe *Error
	if !errors.As(err, &pe) || pe.Retryable {
		t.Fatalf("want terminal error, got %v", err)
	}
}

// T3: env overrides reach the process.
func TestCli_EnvOverride(t *testing.T) {
	script := writeScript(t, `printf '%s' "$LLP_TEST_VAR"`)
	p := NewCli(CliConfig{Name: "fake", Command: []string{script}, Env: map[string]string{"LLP_TEST_VAR": "hello"}})
	resp, err := p.Generate(context.Background(), Request{Messages: userMsg("x")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "hello" {
		t.Fatalf("env not applied: %q", resp.Content)
	}
}
