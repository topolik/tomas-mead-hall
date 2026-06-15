package propose

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/topolik/gml-gmail-agent/internal/config"
)

type MergeResult struct {
	MergedRules []MergedRuleEntry `json:"merged_rules"`
	Conflicts   []ConflictEntry   `json:"conflicts"`
}

type MergedRuleEntry struct {
	Name         string           `json:"name"`
	Type         string           `json:"type"`
	Params       config.RuleParams `json:"params"`
	FromPlanIDs  []int64          `json:"from_plan_ids"`
	Descriptions []string         `json:"descriptions"`
	Constraints  []string         `json:"constraints"`
	Rationale    string           `json:"rationale"`
}

type ConflictEntry struct {
	AffectedPlanIDs    []int64              `json:"affected_plan_ids"`
	Description        string               `json:"description"`
	SuggestedResolution *MergedRuleEntry    `json:"suggested_resolution,omitempty"`
}

func ParseAndValidateMerge(raw []byte) (*MergeResult, error) {
	cleaned := stripCodeFence(raw)

	var result MergeResult
	if err := json.Unmarshal(cleaned, &result); err != nil {
		return nil, fmt.Errorf("invalid merge JSON: %w", err)
	}

	for i, r := range result.MergedRules {
		if r.Name == "" {
			return nil, fmt.Errorf("merged_rules[%d]: name is required", i)
		}
		if r.Type == "" {
			return nil, fmt.Errorf("merged_rules[%d]: type is required", i)
		}
		if r.Type == "archive_by_sender" && len(r.Params.Patterns) == 0 {
			return nil, fmt.Errorf("merged_rules[%d] %q: archive_by_sender requires at least one pattern", i, r.Name)
		}
		if len(r.FromPlanIDs) == 0 {
			return nil, fmt.Errorf("merged_rules[%d] %q: from_plan_ids is required", i, r.Name)
		}
	}

	for i, c := range result.Conflicts {
		if len(c.AffectedPlanIDs) == 0 {
			return nil, fmt.Errorf("conflicts[%d]: affected_plan_ids is required", i)
		}
		if c.Description == "" {
			return nil, fmt.Errorf("conflicts[%d]: description is required", i)
		}
	}

	return &result, nil
}

func ValidateMergeCompleteness(result *MergeResult, inputPlanIDs []int64) error {
	covered := make(map[int64]bool)
	for _, r := range result.MergedRules {
		for _, id := range r.FromPlanIDs {
			covered[id] = true
		}
	}
	for _, c := range result.Conflicts {
		for _, id := range c.AffectedPlanIDs {
			covered[id] = true
		}
	}

	var missing []int64
	for _, id := range inputPlanIDs {
		if !covered[id] {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("plans not accounted for in merge result: %v", missing)
	}
	return nil
}

func MergeResultToAnnotatedRules(result *MergeResult) []AnnotatedRule {
	var rules []AnnotatedRule
	for _, m := range result.MergedRules {
		pattern := strings.Join(m.Descriptions, "; ")
		constraint := strings.Join(m.Constraints, "; ")
		rules = append(rules, AnnotatedRule{
			Rule: config.Rule{
				Name:   m.Name,
				Type:   m.Type,
				Params: m.Params,
			},
			Pattern:    pattern,
			Constraint: constraint,
			PlanIDs:    m.FromPlanIDs,
		})
	}
	return rules
}

func sortedPlanIDs(ids []int64) []int64 {
	sorted := make([]int64, len(ids))
	copy(sorted, ids)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return sorted
}

func ConflictToProposal(conflict ConflictEntry) *Proposal {
	if conflict.SuggestedResolution == nil {
		return nil
	}
	res := conflict.SuggestedResolution
	sorted := sortedPlanIDs(conflict.AffectedPlanIDs)
	return &Proposal{
		KnowledgeRef: fmt.Sprintf("merge_conflict:%v", sorted),
		Pattern:      strings.Join(res.Descriptions, "; "),
		Status:       "confirmed",
		Constraint:   strings.Join(res.Constraints, "; "),
		ProposedRule: config.Rule{
			Name:   res.Name,
			Type:   res.Type,
			Params: res.Params,
		},
		Reason: fmt.Sprintf("LLM merge conflict resolution (plans %v): %s", sorted, conflict.Description),
	}
}

func FormatConflictPlanDetail(conflict ConflictEntry) string {
	p := ConflictToProposal(conflict)
	if p == nil {
		detail := struct {
			Type        string  `json:"type"`
			Conflict    string  `json:"conflict_description"`
			SourcePlans []int64 `json:"source_plan_ids"`
		}{
			Type:        "merge_conflict_unresolved",
			Conflict:    conflict.Description,
			SourcePlans: conflict.AffectedPlanIDs,
		}
		b, _ := json.MarshalIndent(detail, "", "  ")
		return string(b)
	}
	b, _ := json.MarshalIndent(p, "", "  ")
	return string(b)
}

func stripCodeFence(raw []byte) []byte {
	s := strings.TrimSpace(string(raw))

	if strings.HasPrefix(s, "```") {
		if idx := strings.Index(s, "\n"); idx >= 0 {
			s = s[idx+1:]
		}
	}
	if strings.HasSuffix(s, "```") {
		s = s[:len(s)-3]
	}

	s = strings.TrimSpace(s)

	if idx := strings.Index(s, "{"); idx > 0 {
		s = s[idx:]
	}

	return []byte(s)
}
