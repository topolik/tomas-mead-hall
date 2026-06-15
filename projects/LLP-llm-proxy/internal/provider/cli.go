package provider

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// CliProvider backs an impl by exec'ing a CLI (e.g. gemini-cli, claude). The
// rendered prompt is piped to the process's stdin; stdout is the completion
// content; stderr is diagnostics only. See LLP-001.
type CliProvider struct {
	name              string
	command           []string          // argv; command[0] is the binary
	env               map[string]string // overrides merged onto os.Environ()
	modelFlag         string            // e.g. "--model" (appended only if modelID != "")
	modelID           string
	timeout           time.Duration
	stripFence        bool
	rateLimitPatterns []string // case-insensitive stderr substrings => rate limited
	quotaPatterns     []string // case-insensitive stderr substrings => quota exhausted for a long window
}

// CliConfig configures a CliProvider.
type CliConfig struct {
	Name              string
	Command           []string
	Env               map[string]string
	ModelFlag         string
	ModelID           string
	Timeout           time.Duration
	StripFence        bool
	RateLimitPatterns []string
	QuotaPatterns     []string
}

// DefaultRateLimitPatterns are substrings emitted by gemini/claude/HTTP backends
// when throttled or out of quota. They are deliberately specific phrases (not
// bare "quota" or "429") so they don't match Node stack-trace filenames like
// `googleQuotaErrors.js` or `:429:` line numbers — that false-match would
// misclassify a model-not-found (404) as a rate limit and wrongly trigger cooldown.
var DefaultRateLimitPatterns = []string{
	"rate limit", "ratelimit", "rate_limit",
	"resource_exhausted", "resource has been exhausted", "resource exhausted",
	"quota exceeded", "exceeded your", "exceeded the quota", "out of quota",
	"too many requests", "overloaded", "usage limit",
	"code: 429", "code 429", "status 429", "http 429", "(429)", "429 too many",
	"retryablequotaerror", "suggested retry after",
}

// DefaultQuotaExhaustedPatterns mark a quota gone for a long window (hours), as
// opposed to a momentary throttle — gemini-cli's TerminalQuotaError ("You have
// exhausted your daily quota on this model.", "quota will reset after 3h49m")
// and claude's "usage limit reached". The same false-match rule as above
// applies: the class name "terminalquotaerror" is matched, but it is NOT a
// substring of the stack-frame filename "googleQuotaErrors.js".
var DefaultQuotaExhaustedPatterns = []string{
	"terminalquotaerror",
	"daily quota", "quota will reset", "resets after",
	"usage limit reached",
}

func NewCli(c CliConfig) *CliProvider {
	if c.Timeout <= 0 {
		c.Timeout = 180 * time.Second
	}
	pats := c.RateLimitPatterns
	if len(pats) == 0 {
		pats = DefaultRateLimitPatterns
	}
	quota := c.QuotaPatterns
	if len(quota) == 0 {
		quota = DefaultQuotaExhaustedPatterns
	}
	return &CliProvider{
		name:              c.Name,
		command:           c.Command,
		env:               c.Env,
		modelFlag:         c.ModelFlag,
		modelID:           c.ModelID,
		timeout:           c.Timeout,
		stripFence:        c.StripFence,
		rateLimitPatterns: pats,
		quotaPatterns:     quota,
	}
}

func (p *CliProvider) Name() string { return p.name }

func (p *CliProvider) Generate(ctx context.Context, req Request) (Response, error) {
	if len(p.command) == 0 {
		return Response{}, &Error{Err: fmt.Errorf("%s: no command configured", p.name)}
	}
	prompt := renderPrompt(req.Messages)

	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	modelID := p.modelID
	if req.ModelID != "" {
		if p.modelFlag == "" {
			return Response{}, &Error{Retryable: false, Err: fmt.Errorf("%s: model override %q requested but impl has no model_flag configured", p.name, req.ModelID)}
		}
		modelID = req.ModelID
	}
	args := append([]string{}, p.command[1:]...)
	if p.modelFlag != "" && modelID != "" {
		args = append(args, p.modelFlag, modelID)
	}
	cmd := exec.CommandContext(ctx, p.command[0], args...)
	cmd.Stdin = strings.NewReader(prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = mergeEnv(p.env)

	// Run the CLI in its own process group and, on context cancel/timeout, kill
	// the whole group. Killing only the direct child leaves grandchildren (e.g.
	// `npx` spawning node) holding the stdout pipe open, which makes Wait() block
	// until they exit on their own. WaitDelay is a backstop.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	cmd.WaitDelay = 2 * time.Second

	err := cmd.Run()
	if err != nil {
		return Response{}, p.classify(ctx, err, stderr.String())
	}

	content := stdout.String()
	if p.stripFence {
		content = stripCodeFence(content)
	}
	content = strings.TrimSpace(content)
	if content == "" {
		// Empty output is treated as a retryable failure so the chain can try
		// another impl rather than handing the client an empty completion.
		return Response{}, &Error{Retryable: true, Err: fmt.Errorf("%s: empty output (stderr: %s)", p.name, truncate(stderr.String(), 200))}
	}
	return Response{
		Content:          content,
		PromptTokens:     EstimateTokens(prompt),
		CompletionTokens: EstimateTokens(content),
	}, nil
}

func (p *CliProvider) classify(ctx context.Context, runErr error, stderr string) error {
	// Timeout / cancellation => retryable.
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return &Error{Retryable: true, Err: fmt.Errorf("%s: timed out after %s", p.name, p.timeout)}
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return &Error{Retryable: false, Err: fmt.Errorf("%s: canceled", p.name)}
	}
	low := strings.ToLower(stderr)
	// Quota-exhausted first: its patterns are the more specific ones, and the
	// router picks the longer quota_cooldown only off this flag.
	for _, pat := range p.quotaPatterns {
		if pat != "" && strings.Contains(low, pat) {
			return &Error{Retryable: true, RateLimit: true, QuotaExhausted: true, Err: fmt.Errorf("%s: quota exhausted: %s", p.name, truncate(stderr, 200))}
		}
	}
	for _, pat := range p.rateLimitPatterns {
		if pat != "" && strings.Contains(low, pat) {
			return &Error{Retryable: true, RateLimit: true, Err: fmt.Errorf("%s: rate limited: %s", p.name, truncate(stderr, 200))}
		}
	}
	// Other non-zero exit: treat as retryable so a sibling impl can serve it,
	// but it is not a rate-limit (no cooldown).
	return &Error{Retryable: true, Err: fmt.Errorf("%s: %v (stderr: %s)", p.name, runErr, truncate(stderr, 200))}
}

// mergeEnv returns os.Environ() with the provided overrides applied.
func mergeEnv(over map[string]string) []string {
	if len(over) == 0 {
		return os.Environ()
	}
	base := os.Environ()
	out := make([]string, 0, len(base)+len(over))
	skip := make(map[string]bool, len(over))
	for k := range over {
		skip[k] = true
	}
	for _, kv := range base {
		if i := strings.IndexByte(kv, '='); i >= 0 && skip[kv[:i]] {
			continue
		}
		out = append(out, kv)
	}
	for k, v := range over {
		out = append(out, k+"="+v)
	}
	return out
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
