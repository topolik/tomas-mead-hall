package gmail

import (
	"strings"
	"testing"
)

func TestValidateQueryValid(t *testing.T) {
	valid := []string{
		"",
		"from:alice@example.com",
		`subject:"SYSTEM ALERT"`,
		`"false positive" -"true positive"`,
		"subject:Confluence older_than:7d",
		"has:attachment filename:pdf",
		"{from:alice@x.com from:bob@y.com}",
		"in:inbox is:unread",
		"-subject:Review -subject:approve has:nouserlabels",
		"newer_than:3d from:bot@x.com",
		"larger:10M",
		"before:2026/01/01 after:2025/01/01",
		"category:promotions",
		"AROUND 5 security incident",
		"bare words are fine",
		`+word`,
		// Quoted phrases are literal text searches — a colon inside the quotes is
		// content, not an operator (regression: the distill "principal_email:" case).
		`"principal_email: lcp-api@cloud-project.iam.gserviceaccount.com"`,
		`"Re: weekly sync"`,
		`from:x@y.com "principal_email: svc@z.com"`,
		`-"Status: closed"`,
	}
	for _, q := range valid {
		if err := ValidateQuery(q); err != nil {
			t.Errorf("ValidateQuery(%q) = %v, want nil", q, err)
		}
	}
}

func TestValidateQueryInvalid(t *testing.T) {
	invalid := []struct {
		query string
		want  string
	}{
		{"foobar:value", `unknown Gmail search operator "foobar"`},
		{"body:text", `unknown Gmail search operator "body"`},
		{"content:word", `unknown Gmail search operator "content"`},
		{"-badop:value from:ok@x.com", `unknown Gmail search operator "badop"`},
	}
	for _, tt := range invalid {
		err := ValidateQuery(tt.query)
		if err == nil {
			t.Errorf("ValidateQuery(%q) = nil, want error containing %q", tt.query, tt.want)
			continue
		}
		if !strings.Contains(err.Error(), tt.want) {
			t.Errorf("ValidateQuery(%q) = %v, want error containing %q", tt.query, err, tt.want)
		}
	}
}

func TestOperatorsReference(t *testing.T) {
	ref := OperatorsReference()
	required := []string{"from:", "subject:", "older_than:", "newer_than:", "has:attachment", "OR", "AROUND"}
	for _, s := range required {
		if !strings.Contains(ref, s) {
			t.Errorf("OperatorsReference() missing %q", s)
		}
	}
}

func TestTokenize(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{`"exact phrase" from:x`, 2},
		{`{from:a from:b} subject:test`, 3},
		{`older_than:7d -"bad words"`, 2},
	}
	for _, tt := range tests {
		got := tokenize(tt.input)
		if len(got) != tt.want {
			t.Errorf("tokenize(%q) = %v (%d tokens), want %d", tt.input, got, len(got), tt.want)
		}
	}
}
