package propose

import (
	"strings"
	"testing"

	"github.com/topolik/gml-gmail-agent/internal/config"
)

func TestParseAndValidateMerge_Valid(t *testing.T) {
	input := `{
		"merged_rules": [{
			"name": "socradar notifications",
			"type": "archive_by_sender",
			"params": {"patterns": ["no-reply@socradar.com"], "filter": "-Critical"},
			"from_plan_ids": [10, 11],
			"descriptions": ["Archive non-critical SOCRadar"],
			"constraints": [],
			"rationale": "both plans agree on -Critical filter"
		}],
		"conflicts": []
	}`
	result, err := ParseAndValidateMerge([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.MergedRules) != 1 {
		t.Fatalf("got %d merged rules, want 1", len(result.MergedRules))
	}
	if result.MergedRules[0].Name != "socradar notifications" {
		t.Errorf("name: got %q", result.MergedRules[0].Name)
	}
	if len(result.Conflicts) != 0 {
		t.Errorf("got %d conflicts, want 0", len(result.Conflicts))
	}
}

func TestParseAndValidateMerge_WithConflict(t *testing.T) {
	input := `{
		"merged_rules": [{
			"name": "liferay notifications",
			"type": "archive_by_sender",
			"params": {"patterns": ["ac@example.com"], "filter": "subject:\"projects down\""},
			"from_plan_ids": [10],
			"descriptions": ["Liferay Cloud projects down"],
			"constraints": [],
			"rationale": "standalone rule"
		}],
		"conflicts": [{
			"affected_plan_ids": [11, 12],
			"description": "Broad -Critical filter subsumes VIP constraint",
			"suggested_resolution": {
				"name": "socradar notifications",
				"type": "archive_by_sender",
				"params": {"patterns": ["no-reply@socradar.com"], "filter": "-Critical -VIP"},
				"from_plan_ids": [11, 12],
				"descriptions": ["Merged socradar rules"],
				"constraints": ["Preserves VIP visibility"],
				"rationale": "most restrictive filter"
			}
		}]
	}`
	result, err := ParseAndValidateMerge([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.MergedRules) != 1 {
		t.Fatalf("got %d merged rules, want 1", len(result.MergedRules))
	}
	if len(result.Conflicts) != 1 {
		t.Fatalf("got %d conflicts, want 1", len(result.Conflicts))
	}
	if result.Conflicts[0].SuggestedResolution == nil {
		t.Error("expected suggested resolution")
	}
}

func TestParseAndValidateMerge_CodeFence(t *testing.T) {
	input := "```json\n" + `{"merged_rules":[],"conflicts":[]}` + "\n```"
	result, err := ParseAndValidateMerge([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.MergedRules) != 0 {
		t.Errorf("got %d merged rules", len(result.MergedRules))
	}
}

func TestParseAndValidateMerge_MissingName(t *testing.T) {
	input := `{"merged_rules":[{"name":"","type":"archive_by_sender","params":{"patterns":["a@b.com"]},"from_plan_ids":[1],"descriptions":[],"constraints":[],"rationale":"x"}],"conflicts":[]}`
	_, err := ParseAndValidateMerge([]byte(input))
	if err == nil || !strings.Contains(err.Error(), "name") {
		t.Errorf("expected name error, got %v", err)
	}
}

func TestParseAndValidateMerge_MissingPatterns(t *testing.T) {
	input := `{"merged_rules":[{"name":"test","type":"archive_by_sender","params":{"patterns":[]},"from_plan_ids":[1],"descriptions":[],"constraints":[],"rationale":"x"}],"conflicts":[]}`
	_, err := ParseAndValidateMerge([]byte(input))
	if err == nil || !strings.Contains(err.Error(), "pattern") {
		t.Errorf("expected pattern error, got %v", err)
	}
}

func TestParseAndValidateMerge_MissingPlanIDs(t *testing.T) {
	input := `{"merged_rules":[{"name":"test","type":"archive_by_sender","params":{"patterns":["a@b.com"]},"from_plan_ids":[],"descriptions":[],"constraints":[],"rationale":"x"}],"conflicts":[]}`
	_, err := ParseAndValidateMerge([]byte(input))
	if err == nil || !strings.Contains(err.Error(), "from_plan_ids") {
		t.Errorf("expected from_plan_ids error, got %v", err)
	}
}

func TestParseAndValidateMerge_ConflictMissingDescription(t *testing.T) {
	input := `{"merged_rules":[],"conflicts":[{"affected_plan_ids":[1,2],"description":""}]}`
	_, err := ParseAndValidateMerge([]byte(input))
	if err == nil || !strings.Contains(err.Error(), "description") {
		t.Errorf("expected description error, got %v", err)
	}
}

func TestValidateMergeCompleteness(t *testing.T) {
	result := &MergeResult{
		MergedRules: []MergedRuleEntry{
			{FromPlanIDs: []int64{10, 11}},
		},
		Conflicts: []ConflictEntry{
			{AffectedPlanIDs: []int64{12}},
		},
	}
	if err := ValidateMergeCompleteness(result, []int64{10, 11, 12}); err != nil {
		t.Errorf("should be complete: %v", err)
	}
}

func TestValidateMergeCompleteness_Missing(t *testing.T) {
	result := &MergeResult{
		MergedRules: []MergedRuleEntry{
			{FromPlanIDs: []int64{10}},
		},
		Conflicts: []ConflictEntry{},
	}
	err := ValidateMergeCompleteness(result, []int64{10, 11})
	if err == nil || !strings.Contains(err.Error(), "11") {
		t.Errorf("expected missing plan 11, got %v", err)
	}
}

func TestMergeResultToAnnotatedRules(t *testing.T) {
	result := &MergeResult{
		MergedRules: []MergedRuleEntry{
			{
				Name:         "test rule",
				Type:         "archive_by_sender",
				Params:       config.RuleParams{Patterns: []string{"a@b.com"}},
				FromPlanIDs:  []int64{1},
				Descriptions: []string{"desc1", "desc2"},
				Constraints:  []string{"constraint1"},
				Rationale:    "test",
			},
		},
	}
	rules := MergeResultToAnnotatedRules(result)
	if len(rules) != 1 {
		t.Fatalf("got %d rules, want 1", len(rules))
	}
	if rules[0].Rule.Name != "test rule" {
		t.Errorf("name: got %q", rules[0].Rule.Name)
	}
	if rules[0].Pattern != "desc1; desc2" {
		t.Errorf("pattern: got %q", rules[0].Pattern)
	}
	if rules[0].Constraint != "constraint1" {
		t.Errorf("constraint: got %q", rules[0].Constraint)
	}
}

func TestFormatConflictPlanDetail(t *testing.T) {
	conflict := ConflictEntry{
		AffectedPlanIDs: []int64{11, 12},
		Description:     "broad filter subsumes narrow",
	}
	detail := FormatConflictPlanDetail(conflict)
	if !strings.Contains(detail, "merge_conflict_unresolved") {
		t.Error("expected unresolved type field in detail")
	}
	if !strings.Contains(detail, "broad filter subsumes narrow") {
		t.Error("expected conflict description in detail")
	}
}

func TestStripCodeFence(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain json", `{"a":1}`, `{"a":1}`},
		{"code fence", "```json\n{\"a\":1}\n```", `{"a":1}`},
		{"preamble", "Here is the result:\n{\"a\":1}", `{"a":1}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(stripCodeFence([]byte(tt.input)))
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
