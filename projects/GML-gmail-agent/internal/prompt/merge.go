package prompt

import (
	"encoding/json"
	"fmt"
	"strings"
)

type MergePlan struct {
	PlanID      int64  `json:"plan_id"`
	Title       string `json:"title"`
	Pattern     string `json:"pattern"`
	Constraint  string `json:"constraint,omitempty"`
	RuleName    string `json:"rule_name"`
	RuleType    string `json:"rule_type"`
	Senders     []string `json:"senders"`
	Filter      string `json:"filter,omitempty"`
	RequireReply bool  `json:"require_reply,omitempty"`
	Reason      string `json:"reason"`
}

type ExistingRule struct {
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	Senders      []string `json:"senders,omitempty"`
	Filter       string   `json:"filter,omitempty"`
	RequireReply bool     `json:"require_reply,omitempty"`
}

const mergeSystemPrompt = `You are a rule conflict detector and merger for Tomas's email management system. You receive approved plans (each proposing an archive rule) and must merge them into a non-conflicting rule set.

CRITICAL RULES:
1. Output ONLY valid JSON matching the schema below. No markdown, no explanations, no preamble.
2. Every sender from every approved plan must appear either in merged_rules or in a conflict's affected_plan_ids. Nothing disappears silently.
3. Conflict detection is about SEMANTIC overlap, not just structural matching.

WHAT COUNTS AS A CONFLICT:
- Two rules targeting the same sender with different filters where the broader filter would archive emails the narrower filter was designed to protect
  Example: Rule A archives "from:x@y.com -Critical" (all non-critical). Rule B archives "from:x@y.com subject:Credential -VIP" (credentials except VIP). Rule A would archive VIP credential emails that Rule B explicitly protects.
- Two rules with contradictory constraints (e.g., "only archive non-critical" vs. "archive all")
- A rule that subsumes another rule entirely, making the narrower one meaningless

WHAT IS NOT A CONFLICT:
- Two rules for different senders (even same domain, different addresses) — these are independent
- Two rules for the same sender with identical filters — merge senders into one rule
- A rule with a filter and a rule without a filter for completely different senders

MERGE STRATEGY:
For non-conflicting plans:
- Group by (rule_name, rule_type, filter, require_reply) — merge sender patterns within the same group
- Preserve all descriptions and constraints as comments

For conflicting plans:
- Do NOT merge conflicting plans into rules
- Instead, produce a conflict entry with: affected plan IDs, description of the conflict, and a suggested_resolution that proposes the most restrictive (safest) rule that respects all constraints

OUTPUT SCHEMA:
{
  "merged_rules": [
    {
      "name": "rule name",
      "type": "archive_by_sender",
      "params": {
        "patterns": ["sender@domain.com"],
        "filter": "optional Gmail filter",
        "require_reply": false
      },
      "from_plan_ids": [12, 15],
      "descriptions": ["human-readable pattern description"],
      "constraints": ["advisory constraint"],
      "rationale": "why these plans merge cleanly"
    }
  ],
  "conflicts": [
    {
      "affected_plan_ids": [13, 14],
      "description": "what conflicts and why",
      "suggested_resolution": {
        "name": "suggested rule name",
        "type": "archive_by_sender",
        "params": {
          "patterns": ["sender@domain.com"],
          "filter": "most restrictive safe filter",
          "require_reply": false
        },
        "descriptions": ["merged description"],
        "constraints": ["merged constraints"],
        "rationale": "why this resolution is safe"
      }
    }
  ]
}

If there are no conflicts, the "conflicts" array must be empty [].
If ALL plans conflict, the "merged_rules" array must be empty [].`

func BuildMergePlans(plans []MergePlan, existingRules []ExistingRule, knowledgeContext string) string {
	var sb strings.Builder

	sb.WriteString(mergeSystemPrompt)
	sb.WriteString("\n\n")

	sb.WriteString("APPROVED PLANS TO MERGE:\n\n")
	sb.WriteString("<approved_plans>\n")
	plansJSON, _ := json.MarshalIndent(plans, "", "  ")
	sb.WriteString(string(plansJSON))
	sb.WriteString("\n</approved_plans>\n\n")

	sb.WriteString("EXISTING RULES (already active — new rules must not conflict with these either):\n\n")
	sb.WriteString("<existing_rules>\n")
	if len(existingRules) > 0 {
		rulesJSON, _ := json.MarshalIndent(existingRules, "", "  ")
		sb.WriteString(string(rulesJSON))
	} else {
		sb.WriteString("No existing rules.\n")
	}
	sb.WriteString("\n</existing_rules>\n\n")

	sb.WriteString("KNOWLEDGE CONTEXT (Tomas's preferences — use to understand intent behind each plan):\n\n")
	sb.WriteString("<knowledge>\n")
	if knowledgeContext != "" {
		sb.WriteString(knowledgeContext)
	} else {
		sb.WriteString("No knowledge context available.\n")
	}
	sb.WriteString("\n</knowledge>\n\n")

	fmt.Fprintf(&sb, "Merge the %d approved plans above. Detect conflicts and produce the JSON output.\n", len(plans))

	return sb.String()
}
