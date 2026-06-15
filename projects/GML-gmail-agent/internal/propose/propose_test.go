package propose

import (
	"strings"
	"testing"

	"github.com/topolik/gml-gmail-agent/internal/config"
	"github.com/topolik/gml-gmail-agent/internal/knowledge"
)

func TestParseProposals(t *testing.T) {
	good := `[
	  {"proposed_rule":{"name":"socradar notifications","type":"archive_by_sender","params":{"patterns":["no-reply@socradar.com"]}},"pattern":"x","reason":"y"}
	]`
	got, err := ParseProposals([]byte(good))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].ProposedRule.Name != "socradar notifications" {
		t.Fatalf("unexpected parse result: %+v", got)
	}

	// Tolerates a markdown code fence.
	fenced := "```json\n" + good + "\n```"
	if _, err := ParseProposals([]byte(fenced)); err != nil {
		t.Errorf("fenced input should parse: %v", err)
	}

	// Empty array (all dropped) is valid.
	if got, err := ParseProposals([]byte(`[]`)); err != nil || len(got) != 0 {
		t.Errorf("empty array should parse to zero proposals, got %v err %v", got, err)
	}

	// Missing rule name is rejected.
	if _, err := ParseProposals([]byte(`[{"proposed_rule":{"type":"archive_by_sender","params":{"patterns":["a@b.com"]}}}]`)); err == nil {
		t.Error("expected error for missing rule name")
	}

	// archive_by_sender with no patterns is rejected.
	if _, err := ParseProposals([]byte(`[{"proposed_rule":{"name":"x","type":"archive_by_sender","params":{}}}]`)); err == nil {
		t.Error("expected error for archive_by_sender with no patterns")
	}

	// Malformed JSON is rejected.
	if _, err := ParseProposals([]byte(`not json`)); err == nil {
		t.Error("expected error for malformed JSON")
	}
}

func TestGenerate(t *testing.T) {
	kf := &knowledge.KnowledgeFile{
		Patterns: []knowledge.Pattern{
			{
				GmailSearch:    "from:noreply@jira.example.com",
				Pattern:        "Low engagement with Atlassian notifications",
				Status:         "refined",
				Category:       "archive_candidate",
				Senders:        []string{"noreply@jira.example.com"},
				CommentSummary: "Keep those that mention Tomas",
				RefinedAction:  "Do not auto-archive if they mention Tomas",
				Filter:         "-\"Tomas\"",
			},
			{
				GmailSearch:    "from:alerts@crm.example.com",
				Pattern:        "Unread Salesforce alerts ignored",
				Status:         "rejected",
				Category:       "archive_candidate",
				Senders:        []string{"alerts@crm.example.com"},
				CommentSummary: "Need better SOP reactivity",
			},
		},
	}
	cfg := &config.Config{}

	result := Generate(kf, cfg)

	if len(result.Proposals) != 1 {
		t.Fatalf("expected 1 proposal, got %d", len(result.Proposals))
	}
	if len(result.Skipped) != 1 {
		t.Fatalf("expected 1 skipped, got %d", len(result.Skipped))
	}

	p := result.Proposals[0]
	if p.Status != "refined" {
		t.Errorf("expected status refined, got %q", p.Status)
	}
	if p.ProposedRule.Type != "archive_by_sender" {
		t.Errorf("expected archive_by_sender, got %q", p.ProposedRule.Type)
	}
	if len(p.ProposedRule.Params.Patterns) != 1 || p.ProposedRule.Params.Patterns[0] != "noreply@jira.example.com" {
		t.Errorf("unexpected patterns: %v", p.ProposedRule.Params.Patterns)
	}
	if p.Constraint != "Do not auto-archive if they mention Tomas" {
		t.Errorf("unexpected constraint: %q", p.Constraint)
	}
	if p.ProposedRule.Params.Filter != "-Tomas" {
		t.Errorf("expected filter %q, got %q", "-Tomas", p.ProposedRule.Params.Filter)
	}

	s := result.Skipped[0]
	if s.Reason != "rejected by user" {
		t.Errorf("expected 'rejected by user', got %q", s.Reason)
	}
}

func TestGenerateSkipsDuplicate(t *testing.T) {
	kf := &knowledge.KnowledgeFile{
		Patterns: []knowledge.Pattern{
			{
				GmailSearch: "from:noreply@github.com",
				Pattern:     "GitHub notifications",
				Status:      "confirmed",
				Category:    "archive_candidate",
				Senders:     []string{"noreply@github.com"},
			},
		},
	}
	cfg := &config.Config{
		Rules: []config.Rule{
			{
				Name: "github",
				Type: "archive_by_sender",
				Params: config.RuleParams{
					Patterns: []string{"noreply@github.com"},
				},
			},
		},
	}

	result := Generate(kf, cfg)
	if len(result.Proposals) != 0 {
		t.Fatalf("expected 0 proposals (duplicate), got %d", len(result.Proposals))
	}
	if len(result.Skipped) != 1 || result.Skipped[0].Reason != "rule already exists for all senders" {
		t.Errorf("expected skip with duplicate reason, got %v", result.Skipped)
	}
}

func TestGenerateFallsBackToGmailSearch(t *testing.T) {
	kf := &knowledge.KnowledgeFile{
		Patterns: []knowledge.Pattern{
			{
				GmailSearch: "from:bot@service.com",
				Pattern:     "Bot emails",
				Status:      "confirmed",
				Category:    "archive_candidate",
			},
		},
	}
	cfg := &config.Config{}

	result := Generate(kf, cfg)
	if len(result.Proposals) != 1 {
		t.Fatalf("expected 1 proposal, got %d", len(result.Proposals))
	}
	if result.Proposals[0].ProposedRule.Params.Patterns[0] != "bot@service.com" {
		t.Errorf("expected sender from gmail_search, got %v", result.Proposals[0].ProposedRule.Params.Patterns)
	}
}

func TestBuildRuleName(t *testing.T) {
	tests := []struct {
		pattern knowledge.Pattern
		want    string
	}{
		{
			pattern: knowledge.Pattern{Senders: []string{"noreply@jira.example.com"}},
			want:    "example notifications",
		},
		{
			pattern: knowledge.Pattern{Senders: []string{"alerts@crm.example.com"}},
			want:    "example notifications",
		},
		{
			pattern: knowledge.Pattern{
				Senders: []string{"a@x.com", "b@y.com"},
				Pattern: "Multiple sender low engagement pattern",
			},
			want: "multiple sender low",
		},
	}
	for _, tt := range tests {
		got := buildRuleName(tt.pattern)
		if got != tt.want {
			t.Errorf("buildRuleName(%v) = %q, want %q", tt.pattern.Senders, got, tt.want)
		}
	}
}

func TestGenerateBypassesDedupWithFilter(t *testing.T) {
	kf := &knowledge.KnowledgeFile{
		Patterns: []knowledge.Pattern{
			{
				GmailSearch: "from:noreply@sentinelone.net",
				Pattern:     "SentinelOne false positives",
				Status:      "refined",
				Category:    "archive_candidate",
				Senders:     []string{"noreply@sentinelone.net"},
				RefinedAction: "Only archive false positives",
				Filter:      "\"false positive\" -\"true positive\"",
			},
		},
	}
	cfg := &config.Config{
		Rules: []config.Rule{
			{
				Name: "sentinelone notifications",
				Type: "archive_by_sender",
				Params: config.RuleParams{
					Patterns: []string{"noreply@sentinelone.net"},
				},
			},
		},
	}

	result := Generate(kf, cfg)
	if len(result.Proposals) != 1 {
		t.Fatalf("expected 1 proposal (filter adds new constraint), got %d; skipped: %v", len(result.Proposals), result.Skipped)
	}
	if result.Proposals[0].ProposedRule.Params.Filter != "\"false positive\" -\"true positive\"" {
		t.Errorf("expected filter carried through, got %q", result.Proposals[0].ProposedRule.Params.Filter)
	}
}

func TestGenerateBypassesDedupWithRequireReply(t *testing.T) {
	kf := &knowledge.KnowledgeFile{
		Patterns: []knowledge.Pattern{
			{
				GmailSearch:  "from:team-soc@example.com",
				Pattern:      "Team SOC conditional archive",
				Status:       "refined",
				Category:     "archive_candidate",
				Senders:      []string{"team-soc@example.com"},
				RefinedAction: "Only archive if someone responds",
				RequireReply: true,
			},
		},
	}
	cfg := &config.Config{
		Rules: []config.Rule{
			{
				Name: "liferay notifications",
				Type: "archive_by_sender",
				Params: config.RuleParams{
					Patterns: []string{"team-soc@example.com"},
				},
			},
		},
	}

	result := Generate(kf, cfg)
	if len(result.Proposals) != 1 {
		t.Fatalf("expected 1 proposal (require_reply adds constraint), got %d; skipped: %v", len(result.Proposals), result.Skipped)
	}
	if !result.Proposals[0].ProposedRule.Params.RequireReply {
		t.Error("expected require_reply carried through")
	}
}

func TestExtractFilterFromGmailSearch(t *testing.T) {
	tests := []struct {
		pattern knowledge.Pattern
		want    string
	}{
		{
			pattern: knowledge.Pattern{GmailSearch: "from:ac@example.com subject:\"SYSTEM ALERT\""},
			want:    "subject:\"SYSTEM ALERT\"",
		},
		{
			pattern: knowledge.Pattern{GmailSearch: "from:noreply@sentinelone.net"},
			want:    "",
		},
		{
			pattern: knowledge.Pattern{GmailSearch: "from:security@example.com subject:\"Spike in security events\""},
			want:    "subject:\"Spike in security events\"",
		},
		{
			pattern: knowledge.Pattern{GmailSearch: ""},
			want:    "",
		},
		// Multi-sender groupings must yield NO filter (senders live in patterns) —
		// these are the cases that produced the bogus "OR" and "{from:..." filters.
		{
			pattern: knowledge.Pattern{GmailSearch: "{from:info@sentinelone.com from:community@sentinelone.com}"},
			want:    "",
		},
		{
			pattern: knowledge.Pattern{GmailSearch: "from:alerts@crm.example.com OR from:techcomms@crm.example.com"},
			want:    "",
		},
		// A sender group plus a genuine content group: drop the senders, keep content.
		{
			pattern: knowledge.Pattern{GmailSearch: `{from:community@sentinelone.com from:info@sentinelone.com} {subject:"Accelerate SOC Workflows" subject:"The AI Era"}`},
			want:    `{subject:"Accelerate SOC Workflows" subject:"The AI Era"}`,
		},
		// Negations and multi-token content survive.
		{
			pattern: knowledge.Pattern{GmailSearch: `from:secops@example.com subject:"Case Escalation" -subject:Critical -subject:High`},
			want:    `subject:"Case Escalation" -subject:Critical -subject:High`,
		},
	}
	for _, tt := range tests {
		got := extractFilter(tt.pattern)
		if got != tt.want {
			t.Errorf("extractFilter(%q) = %q, want %q", tt.pattern.GmailSearch, got, tt.want)
		}
	}
}

func TestCanonicalFilter(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{`-"Critical"`, `-Critical`},
		{`-"Tomas"`, `-Tomas`},
		{`"false positive" -"true positive"`, `"false positive" -"true positive"`},
		{`subject:"SYSTEM ALERT"`, `subject:"SYSTEM ALERT"`},
		{`  -"Info"  `, `-Info`},
		{``, ``},
		{`older_than:7d`, `older_than:7d`},
	}
	for _, tt := range tests {
		got := CanonicalFilter(tt.input)
		if got != tt.want {
			t.Errorf("CanonicalFilter(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestCanonicalQuery(t *testing.T) {
	tests := []struct {
		name, input, want string
	}{
		{"quote strip", `-"Critical"`, `-Critical`},
		{"multi-word phrase preserved", `subject:"SYSTEM ALERT"`, `subject:"SYSTEM ALERT"`},
		{"operator lowercased", `From:bob@x.com`, `from:bob@x.com`},
		{"operand case preserved", `from:Bob@X.com`, `from:Bob@X.com`},
		{"term order irrelevant", `subject:alert from:x@y.com`, `from:x@y.com subject:alert`},
		{"date unit week to days", `older_than:1w`, `older_than:7d`},
		{"date unit equal forms collapse", `older_than:7d`, `older_than:7d`},
		{"date month to days", `newer_than:2m`, `newer_than:60d`},
		{"whitespace collapsed", `   from:x@y.com    subject:hi `, `from:x@y.com subject:hi`},
		{"duplicate token removed", `from:x@y.com from:x@y.com`, `from:x@y.com`},
		{"negation preserved with quote strip", `-"true positive"`, `-"true positive"`},
		{"empty", ``, ``},
		{"complex equivalent A", `from:x@y.com older_than:1w -"Critical"`, `-Critical from:x@y.com older_than:7d`},
		{"complex equivalent B", `older_than:7d -Critical from:x@y.com`, `-Critical from:x@y.com older_than:7d`},
	}
	for _, tt := range tests {
		got := CanonicalQuery(tt.input)
		if got != tt.want {
			t.Errorf("%s: CanonicalQuery(%q) = %q, want %q", tt.name, tt.input, got, tt.want)
		}
	}

	// The two "complex equivalent" forms must produce the same key.
	if CanonicalQuery(`from:x@y.com older_than:1w -"Critical"`) != CanonicalQuery(`older_than:7d -Critical from:x@y.com`) {
		t.Error("semantically equal queries did not canonicalize to the same key")
	}
}

func TestBuildGeneratedRulesWithFilter(t *testing.T) {
	existing := "rules:\n\nanalysis:\n  days: 3\n"
	rules := []AnnotatedRule{
		{
			Rule: config.Rule{
				Name: "sentinelone notifications",
				Type: "archive_by_sender",
				Params: config.RuleParams{
					Patterns: []string{"noreply@sentinelone.net"},
					Filter:   "\"false positive\"",
				},
			},
			Pattern:    "SentinelOne false positives",
			Constraint: "Only archive false positives",
		},
	}

	result := BuildGeneratedRules(existing, rules)

	if !strings.Contains(result, "filter: \"\\\"false positive\\\"\"") {
		t.Errorf("expected filter line in output, got:\n%s", result)
	}
}

func TestBuildGeneratedRulesWithRequireReply(t *testing.T) {
	existing := "rules:\n\nanalysis:\n  days: 3\n"
	rules := []AnnotatedRule{
		{
			Rule: config.Rule{
				Name: "team soc",
				Type: "archive_by_sender",
				Params: config.RuleParams{
					Patterns:     []string{"team-soc@example.com"},
					Filter:       "older_than:7d",
					RequireReply: true,
				},
			},
			Pattern: "Team SOC conditional archive",
		},
	}

	result := BuildGeneratedRules(existing, rules)

	if !strings.Contains(result, "require_reply: true") {
		t.Errorf("expected require_reply in output, got:\n%s", result)
	}
	if !strings.Contains(result, "filter: \"older_than:7d\"") {
		t.Errorf("expected filter in output, got:\n%s", result)
	}
}

func TestMergeSkipsDifferentFilters(t *testing.T) {
	existing := "rules:\n\nanalysis:\n  days: 3\n"
	rules := []AnnotatedRule{
		{
			Rule: config.Rule{
				Name: "liferay notifications",
				Type: "archive_by_sender",
				Params: config.RuleParams{
					Patterns: []string{"ac@example.com"},
					Filter:   "subject:\"SYSTEM ALERT\"",
				},
			},
			Pattern: "AC system alerts",
		},
		{
			Rule: config.Rule{
				Name: "liferay notifications",
				Type: "archive_by_sender",
				Params: config.RuleParams{
					Patterns: []string{"grafana@monitoring.example.com"},
				},
			},
			Pattern: "Grafana alerts",
		},
	}

	result := BuildGeneratedRules(existing, rules)

	if strings.Count(result, `"liferay notifications"`) != 2 {
		t.Errorf("expected 2 separate liferay rules (different filters), got:\n%s", result)
	}
}

func TestBuildGeneratedRules(t *testing.T) {
	existing := `rules:
  - name: "github"
    type: archive_by_sender
    params:
      patterns:
        - "noreply@github.com"

analysis:
  days: 3
`
	rules := []AnnotatedRule{
		{
			Rule:       config.Rule{Name: "test rule", Type: "archive_by_sender", Params: config.RuleParams{Patterns: []string{"bot@example.com"}}},
			Pattern:    "Auto-archive bot emails",
			Constraint: "Only when subject starts with [BOT]",
		},
	}

	result := BuildGeneratedRules(existing, rules)

	if !strings.Contains(result, `- name: "test rule"`) {
		t.Error("generated rule not found in output")
	}
	if !strings.Contains(result, "# Auto-archive bot emails") {
		t.Error("pattern description comment not found")
	}
	if !strings.Contains(result, "# NOTE: Only when subject starts with [BOT]") {
		t.Error("constraint comment not found")
	}
	if !strings.Contains(result, "# === gml-generated rules ===") {
		t.Error("start marker not found")
	}
	if !strings.Contains(result, "# === end gml-generated rules ===") {
		t.Error("end marker not found")
	}
	if !strings.Contains(result, `- name: "github"`) {
		t.Error("original rule was removed")
	}
	if !strings.Contains(result, "analysis:\n  days: 3") {
		t.Error("analysis section was damaged")
	}

	genIdx := strings.Index(result, "# === gml-generated rules ===")
	analIdx := strings.Index(result, "analysis:")
	if genIdx > analIdx {
		t.Error("generated rules should appear before analysis section")
	}
}

func TestBuildGeneratedRulesIdempotent(t *testing.T) {
	existing := `rules:
  - name: "github"
    type: archive_by_sender
    params:
      patterns:
        - "noreply@github.com"

analysis:
  days: 3
`
	rules := []AnnotatedRule{
		{
			Rule:    config.Rule{Name: "test rule", Type: "archive_by_sender", Params: config.RuleParams{Patterns: []string{"bot@example.com"}}},
			Pattern: "Bot emails",
		},
	}

	first := BuildGeneratedRules(existing, rules)
	second := BuildGeneratedRules(first, rules)

	if strings.Count(second, `- name: "test rule"`) != 1 {
		t.Error("idempotency broken: generated rule duplicated")
	}
	if strings.Count(second, "# === gml-generated rules ===") != 1 {
		t.Error("idempotency broken: marker duplicated")
	}
}

func TestBuildGeneratedRulesMergesSameName(t *testing.T) {
	existing := "rules:\n\nanalysis:\n  days: 3\n"
	rules := []AnnotatedRule{
		{
			Rule:    config.Rule{Name: "liferay notifications", Type: "archive_by_sender", Params: config.RuleParams{Patterns: []string{"grafana@monitoring.example.com"}}},
			Pattern: "Liferay Grafana alerts",
		},
		{
			Rule:    config.Rule{Name: "liferay notifications", Type: "archive_by_sender", Params: config.RuleParams{Patterns: []string{"ac@example.com"}}},
			Pattern: "Liferay AC notifications",
		},
		{
			Rule:       config.Rule{Name: "sentinelone notifications", Type: "archive_by_sender", Params: config.RuleParams{Patterns: []string{"noreply@mailsender.sentinelone.net"}}},
			Pattern:    "Auto-archive SentinelOne alerts",
			Constraint: "Only auto-archive false positives",
		},
	}

	result := BuildGeneratedRules(existing, rules)

	if strings.Count(result, `"liferay notifications"`) != 1 {
		t.Errorf("expected 1 merged liferay rule, got %d", strings.Count(result, `"liferay notifications"`))
	}
	if !strings.Contains(result, `"grafana@monitoring.example.com"`) || !strings.Contains(result, `"ac@example.com"`) {
		t.Error("merged rule should contain both sender patterns")
	}
	if !strings.Contains(result, "# Liferay Grafana alerts") || !strings.Contains(result, "# Liferay AC notifications") {
		t.Error("both pattern descriptions should appear as comments")
	}
	if !strings.Contains(result, "# NOTE: Only auto-archive false positives") {
		t.Error("constraint should appear in sentinelone rule")
	}
}
