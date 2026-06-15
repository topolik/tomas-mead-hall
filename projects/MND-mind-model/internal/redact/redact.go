// Package redact removes secret-shaped substrings before moments leave the
// extract stage — nothing unredacted reaches disk or the LLM (MND-002).
package redact

import "regexp"

type rule struct {
	kind string
	re   *regexp.Regexp
}

var rules = []rule{
	// PEM blocks first — they would otherwise be shredded by narrower rules.
	{"pem", regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`)},
	{"pem", regexp.MustCompile(`(?s)-----BEGIN (?:CERTIFICATE|OPENSSH PRIVATE KEY)-----.*?-----END (?:CERTIFICATE|OPENSSH PRIVATE KEY)-----`)},
	// GitHub tokens: ghp_, gho_, ghu_, ghs_, ghr_, github_pat_
	{"github-token", regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}\b`)},
	{"github-token", regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}\b`)},
	// OpenAI/Anthropic-style keys
	{"api-key", regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{16,}\b`)},
	// AWS access key id + secret-looking neighbor
	{"aws-key", regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)},
	// Google API keys
	{"google-key", regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{30,}\b`)},
	// Slack tokens
	{"slack-token", regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}\b`)},
	// JWT: three base64url segments, first one decoding to {"alg"... is too
	// fancy — shape match is enough here.
	{"jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`)},
	// 1Password secret references
	{"op-ref", regexp.MustCompile(`\bop://[^\s"']+`)},
	// key=value / key: value assignments for sensitive names
	{"assignment", regexp.MustCompile(`(?i)\b(password|passwd|secret|api[_-]?key|access[_-]?token|auth[_-]?token|client[_-]?secret|token)\b(\s*[:=]\s*)("[^"\n]{4,}"|'[^'\n]{4,}'|[^\s"',;]{8,})`)},
	// Bearer headers
	{"bearer", regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{16,}`)},
}

// String replaces secret-shaped substrings with [REDACTED:<kind>].
func String(s string) string {
	for _, r := range rules {
		if r.kind == "assignment" {
			// Keep the key name and separator, drop only the value.
			s = r.re.ReplaceAllString(s, `$1$2[REDACTED:assignment]`)
			continue
		}
		s = r.re.ReplaceAllString(s, "[REDACTED:"+r.kind+"]")
	}
	return s
}
