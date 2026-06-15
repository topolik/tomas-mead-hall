package propose

import (
	"testing"

	"github.com/topolik/gml-gmail-agent/internal/config"
)

func sender(name, filter string, patterns ...string) config.Rule {
	return config.Rule{
		Name: name,
		Type: "archive_by_sender",
		Params: config.RuleParams{
			Patterns: patterns,
			Filter:   filter,
		},
	}
}

func TestSameSenderConflicts(t *testing.T) {
	tests := []struct {
		name      string
		rules     []config.Rule
		wantBad   []string // senders expected to be flagged
		wantClean []string // senders expected NOT to be flagged
	}{
		{
			name: "distinct filters same sender → conflict",
			rules: []config.Rule{
				sender("socradar", "-Critical", "no-reply@socradar.com"),
				sender("socradar", "-VIP", "no-reply@socradar.com"),
			},
			wantBad: []string{"no-reply@socradar.com"},
		},
		{
			name: "catch-all plus narrow → conflict (snyk)",
			rules: []config.Rule{
				sender("snyk", "", "support-noreply@snyk.io"),
				sender("snyk", `subject:"Vulnerability alert" newer_than:7d`, "support-noreply@snyk.io"),
			},
			wantBad: []string{"support-noreply@snyk.io"},
		},
		{
			name: "canonically equal filters → NOT a conflict",
			rules: []config.Rule{
				sender("socradar", `-"Critical"`, "no-reply@socradar.com"),
				sender("socradar", "-Critical", "no-reply@socradar.com"),
			},
			wantClean: []string{"no-reply@socradar.com"},
		},
		{
			name: "date-unit variants → NOT a conflict",
			rules: []config.Rule{
				sender("snyk", "older_than:1w", "support-noreply@snyk.io"),
				sender("snyk", "older_than:7d", "support-noreply@snyk.io"),
			},
			wantClean: []string{"support-noreply@snyk.io"},
		},
		{
			name: "different senders → independent",
			rules: []config.Rule{
				sender("a", "-Critical", "a@x.com"),
				sender("b", "-VIP", "b@x.com"),
			},
			wantClean: []string{"a@x.com", "b@x.com"},
		},
		{
			name: "case-insensitive sender match → conflict",
			rules: []config.Rule{
				sender("conf", `"meeting notes"`, "Confluence@tracker.example.com"),
				sender("conf", `-"mentioned you"`, "confluence@tracker.example.com"),
			},
			wantBad: []string{"confluence@tracker.example.com"},
		},
		{
			name:    "non-sender rules ignored",
			rules:   []config.Rule{{Name: "age", Type: "archive_by_age"}, sender("ok", "-Critical", "x@y.com")},
			wantClean: []string{"x@y.com"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SameSenderConflicts(tt.rules)
			for _, s := range tt.wantBad {
				if _, ok := got[s]; !ok {
					t.Errorf("expected %q flagged as conflict; got %v", s, got)
				}
			}
			for _, s := range tt.wantClean {
				if _, ok := got[s]; ok {
					t.Errorf("did not expect %q flagged; got %v", s, got)
				}
			}
		})
	}
}

func TestGuardSameSender(t *testing.T) {
	rules := []AnnotatedRule{
		{Rule: sender("socradar", "-Critical", "no-reply@socradar.com")},
		{Rule: sender("socradar", "-VIP", "no-reply@socradar.com")},
		{Rule: sender("okta", "", "hello@okta.com")},
		// multi-sender rule: one sender conflicts, the other is clean
		{Rule: sender("mixed", "-Critical", "no-reply@socradar.com", "clean@x.com")},
	}
	safe, withheld := GuardSameSender(rules)

	if _, ok := withheld["no-reply@socradar.com"]; !ok {
		t.Fatalf("expected socradar withheld; got %v", withheld)
	}

	// No surviving rule may still reference the withheld sender.
	for _, ar := range safe {
		for _, p := range ar.Rule.Params.Patterns {
			if p == "no-reply@socradar.com" {
				t.Errorf("withheld sender still present in safe rule %q", ar.Rule.Name)
			}
		}
	}

	// The clean sender from the multi-sender rule must survive.
	foundClean, foundOkta := false, false
	for _, ar := range safe {
		for _, p := range ar.Rule.Params.Patterns {
			if p == "clean@x.com" {
				foundClean = true
			}
			if p == "hello@okta.com" {
				foundOkta = true
			}
		}
	}
	if !foundClean {
		t.Error("clean sender from multi-sender rule was dropped")
	}
	if !foundOkta {
		t.Error("unrelated okta rule was dropped")
	}
}

func TestGuardSameSender_NoConflicts(t *testing.T) {
	rules := []AnnotatedRule{
		{Rule: sender("a", "-Critical", "a@x.com")},
		{Rule: sender("b", "-VIP", "b@x.com")},
	}
	safe, withheld := GuardSameSender(rules)
	if len(withheld) != 0 {
		t.Errorf("expected no conflicts; got %v", withheld)
	}
	if len(safe) != 2 {
		t.Errorf("expected 2 rules preserved; got %d", len(safe))
	}
}
