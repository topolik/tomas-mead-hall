package rules

import (
	"testing"

	"github.com/topolik/gml-gmail-agent/internal/config"
)

func TestBuildExclusionsSimpleSender(t *testing.T) {
	rules := []config.Rule{
		{Name: "test", Type: "archive_by_sender", Params: config.RuleParams{Patterns: []string{"foo@bar.com"}}},
	}
	got := BuildExclusions(rules)
	want := "-from:foo@bar.com"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildExclusionsSenderWithFilter(t *testing.T) {
	rules := []config.Rule{
		{Name: "test", Type: "archive_by_sender", Params: config.RuleParams{
			Patterns: []string{"noreply@example.com"},
			Filter:   `"false positive"`,
		}},
	}
	got := BuildExclusions(rules)
	want := `-(from:noreply@example.com "false positive")`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildExclusionsSenderWithNegativeFilter(t *testing.T) {
	rules := []config.Rule{
		{Name: "test", Type: "archive_by_sender", Params: config.RuleParams{
			Patterns: []string{"no-reply@socradar.com"},
			Filter:   "-Critical",
		}},
	}
	got := BuildExclusions(rules)
	want := `-(from:no-reply@socradar.com -Critical)`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildExclusionsSenderWithNegativeQuotedFilter(t *testing.T) {
	rules := []config.Rule{
		{Name: "test", Type: "archive_by_sender", Params: config.RuleParams{
			Patterns: []string{"no-reply@socradar.com"},
			Filter:   `-"Critical"`,
		}},
	}
	got := BuildExclusions(rules)
	want := `-(from:no-reply@socradar.com -"Critical")`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildExclusionsSkipsRequireReply(t *testing.T) {
	rules := []config.Rule{
		{Name: "team soc", Type: "archive_by_sender", Params: config.RuleParams{
			Patterns:     []string{"team-soc@example.com"},
			Filter:       "older_than:7d",
			RequireReply: true,
		}},
	}
	got := BuildExclusions(rules)
	if got != "" {
		t.Errorf("require_reply rule should be excluded, got %q", got)
	}
}

func TestBuildExclusionsSkipsRegexSender(t *testing.T) {
	rules := []config.Rule{
		{Name: "regex", Type: "archive_by_sender", Params: config.RuleParams{Patterns: []string{`.*@example\.com`}}},
	}
	got := BuildExclusions(rules)
	if got != "" {
		t.Errorf("regex sender should be skipped, got %q", got)
	}
}

func TestBuildExclusionsLabel(t *testing.T) {
	rules := []config.Rule{
		{Name: "test", Type: "archive_by_label", Params: config.RuleParams{Label: "1-JIRA"}},
	}
	got := BuildExclusions(rules)
	want := "-label:1-JIRA"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildExclusionsSkipsAge(t *testing.T) {
	rules := []config.Rule{
		{Name: "test", Type: "archive_by_age", Params: config.RuleParams{Days: 30, State: "read"}},
	}
	got := BuildExclusions(rules)
	if got != "" {
		t.Errorf("age rules should not generate exclusions, got %q", got)
	}
}

func TestBuildExclusionsMixed(t *testing.T) {
	rules := []config.Rule{
		{Name: "s1", Type: "archive_by_sender", Params: config.RuleParams{Patterns: []string{"a@b.com"}}},
		{Name: "s2", Type: "archive_by_sender", Params: config.RuleParams{Patterns: []string{"c@d.com"}, RequireReply: true}},
		{Name: "l1", Type: "archive_by_label", Params: config.RuleParams{Label: "spam"}},
		{Name: "age", Type: "archive_by_age", Params: config.RuleParams{Days: 7, State: "read"}},
	}
	got := BuildExclusions(rules)
	want := "-from:a@b.com -label:spam"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

