package redact

import (
	"strings"
	"testing"
)

// T9: secret shapes are redacted, prose survives.
func TestRedactsSecretShapes(t *testing.T) {
	cases := []struct {
		name string
		in   string
		gone string // substring that must not survive
		kind string // expected redaction marker kind
	}{
		{"github", "push with ghp_AbCdEfGhIjKlMnOpQrStUvWxYz123456 please", "ghp_AbCdEf", "github-token"},
		{"github-pat", "use github_pat_11ABCDEFG0_abcdefghijklmnopqrstuv", "github_pat_11", "github-token"},
		{"openai", "key is sk-proj-abc123DEF456ghi789JKL012 ok", "sk-proj-abc", "api-key"},
		{"aws", "creds AKIAIOSFODNN7EXAMPLE here", "AKIAIOSFODNN7", "aws-key"},
		{"google", "AIzaSyA-1234567890abcdefghijklmnopqrstu", "AIzaSyA-12345", "google-key"},
		{"jwt", "token eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJVadQssw5c sent", "eyJhbGciOiJIUzI1NiJ9.", "jwt"},
		{"op-ref", "read op://Private/GML Creds/credential now", "op://Private", "op-ref"},
		{"password-assign", `set password = "hunter2-extra-long" in env`, "hunter2", "assignment"},
		{"token-colon", "token: ABCDEF123456789012345", "ABCDEF12345678", "assignment"},
		{"bearer", "Authorization: Bearer abcdefghij1234567890XYZ", "abcdefghij123456", "bearer"},
		{"pem", "-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBg\n-----END PRIVATE KEY-----", "MIIEvQIBADANBg", "pem"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := String(c.in)
			if strings.Contains(out, c.gone) {
				t.Fatalf("secret survived: %q -> %q", c.in, out)
			}
			if !strings.Contains(out, "[REDACTED:"+c.kind+"]") {
				t.Fatalf("missing marker [REDACTED:%s] in %q", c.kind, out)
			}
		})
	}
}

// T9: ordinary content is untouched.
func TestLeavesProseAlone(t *testing.T) {
	cases := []string{
		"Use Go and Docker, keep it KISS, no host installs.",
		"email me at user@example.com about https://github.com/example-user/x",
		"the token bucket algorithm is fine here", // word "token" without value
		"set the priority to Q2 and run it",
	}
	for _, c := range cases {
		if out := String(c); out != c {
			t.Fatalf("prose mangled: %q -> %q", c, out)
		}
	}
}
