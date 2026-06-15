package propose

import (
	"strings"
	"testing"

	"github.com/topolik/gml-gmail-agent/internal/config"
)

// TestBuildGeneratedRulesInsightComment verifies the rule provenance comment:
// an AnnotatedRule carrying InsightIDs emits a "# insights #..." line, and
// merging rules for the same (name,type,filter) unions their insight IDs.
func TestBuildGeneratedRulesInsightComment(t *testing.T) {
	rules := []AnnotatedRule{
		{
			Rule:       config.Rule{Name: "socradar notifications", Type: "archive_by_sender", Params: config.RuleParams{Patterns: []string{"a@socradar.com"}, Filter: "-Critical"}},
			PlanIDs:    []int64{50},
			InsightIDs: []int64{12, 7},
		},
		{
			Rule:       config.Rule{Name: "socradar notifications", Type: "archive_by_sender", Params: config.RuleParams{Patterns: []string{"b@socradar.com"}, Filter: "-Critical"}},
			PlanIDs:    []int64{51},
			InsightIDs: []int64{7, 20},
		},
	}
	out := BuildGeneratedRules("", rules)
	if !strings.Contains(out, "# insights #7, #12, #20") {
		t.Errorf("expected unioned+sorted insight comment '# insights #7, #12, #20' in output:\n%s", out)
	}
	if !strings.Contains(out, "# plan #50, #51") {
		t.Errorf("expected plan comment in output:\n%s", out)
	}
}
