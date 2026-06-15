package rules

import "testing"

func TestMatchedPattern(t *testing.T) {
	tests := []struct {
		name     string
		from     string
		patterns []string
		want     string
	}{
		{
			name:     "simple email match",
			from:     "GitHub <notifications@github.com>",
			patterns: []string{"notifications@github.com"},
			want:     "notifications@github.com",
		},
		{
			name:     "no match",
			from:     "someone@example.com",
			patterns: []string{"notifications@github.com"},
			want:     "",
		},
		{
			name:     "regex match",
			from:     "bot-12345@service.example.com",
			patterns: []string{`bot-\d+@service\.example\.com`},
			want:     `bot-\d+@service\.example\.com`,
		},
		{
			name:     "multiple patterns first matches",
			from:     "alerts@crm.example.com",
			patterns: []string{"noreply@jira.com", "alerts@crm.example.com"},
			want:     "alerts@crm.example.com",
		},
		{
			name:     "case insensitive",
			from:     "NOREPLY@GitHub.Com",
			patterns: []string{"noreply@github.com"},
			want:     "noreply@github.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchedPattern(tt.from, tt.patterns)
			if got != tt.want {
				t.Errorf("matchedPattern(%q, %v) = %q, want %q", tt.from, tt.patterns, got, tt.want)
			}
		})
	}
}

func TestMatchesSenderPatterns(t *testing.T) {
	if matchesSenderPatterns("test@example.com", []string{"example.com"}) != true {
		t.Error("expected match for example.com")
	}
	if matchesSenderPatterns("test@other.com", []string{"example.com"}) != false {
		t.Error("expected no match for other.com")
	}
}
