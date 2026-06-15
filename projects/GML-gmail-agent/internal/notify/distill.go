package notify

import (
	"encoding/json"
	"fmt"

	"github.com/topolik/gml-gmail-agent/internal/gmail"
	"github.com/topolik/gml-gmail-agent/internal/knowledge"
)

type DistilledPattern struct {
	GmailSearch    string   `json:"gmail_search"`
	Pattern        string   `json:"pattern"`
	Status         string   `json:"status"`
	Category       string   `json:"category"`
	Senders        []string `json:"senders"`
	CommentSummary string   `json:"comment_summary"`
	RefinedAction  string   `json:"refined_action,omitempty"`
	Filter         string   `json:"filter,omitempty"`
	RequireReply   bool     `json:"require_reply,omitempty"`
}

type DistilledTodo struct {
	Text        string  `json:"text"`
	Priority    string  `json:"priority"`
	ProjectCode string  `json:"project_code"`
	// SourceInsights are the insight #IDs this todo derives from (LLM-attributed,
	// best-effort — used only for the back-link suffix; todo dedup stays on the
	// deterministic text-floor in todoreader.Add).
	SourceInsights []int64 `json:"source_insights,omitempty"`
}

type DistillResult struct {
	Patterns []DistilledPattern `json:"patterns"`
	Todos    []DistilledTodo    `json:"todos"`
}

// ParseAndValidateDistilled parses the distill LLM output and returns the VALID
// subset. It is lenient by design: a single bad item (a hallucinated Gmail
// operator, a missing field) must NOT sink the whole batch — otherwise one bad
// pattern blocks all distillation every cycle and the same insights re-distill
// forever. Per-item problems drop or sanitize the item and append a warning;
// only an unparseable top-level JSON is a hard error. Salvageable cases keep the
// pattern: an invalid optional `filter` is stripped (the gmail_search key is what
// matters), an invalid todo priority is reset to default.
func ParseAndValidateDistilled(data []byte) (*DistillResult, []string, error) {
	cleaned := stripCodeFence(string(data))
	if len(cleaned) == 0 {
		return nil, nil, fmt.Errorf("empty input")
	}

	var raw DistillResult
	if err := json.Unmarshal([]byte(cleaned), &raw); err != nil {
		preview := cleaned
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		return nil, nil, fmt.Errorf("invalid JSON: %w\nraw output: %s", err, preview)
	}

	var warnings []string
	out := &DistillResult{}

	for i, r := range raw.Patterns {
		switch {
		case r.GmailSearch == "":
			warnings = append(warnings, fmt.Sprintf("patterns[%d]: dropped (gmail_search required)", i))
			continue
		case r.Pattern == "":
			warnings = append(warnings, fmt.Sprintf("patterns[%d]: dropped (pattern required)", i))
			continue
		case !validDistillStatuses[r.Status]:
			warnings = append(warnings, fmt.Sprintf("patterns[%d]: dropped (invalid status %q)", i, r.Status))
			continue
		}
		if err := gmail.ValidateQuery(r.GmailSearch); err != nil {
			warnings = append(warnings, fmt.Sprintf("patterns[%d] %q: dropped (invalid gmail_search: %v)", i, r.Pattern, err))
			continue
		}
		// Salvage: an invalid optional filter is stripped, not fatal — keep the
		// pattern (its sender/intent) so the insight is still learned and recorded.
		if r.Filter != "" {
			if err := gmail.ValidateQuery(r.Filter); err != nil {
				warnings = append(warnings, fmt.Sprintf("patterns[%d] %q: stripped invalid filter %q (kept pattern): %v", i, r.Pattern, r.Filter, err))
				r.Filter = ""
			}
		}
		// A refined pattern needs at least one expressible action; if stripping the
		// filter left none, drop it.
		if r.Status == "refined" && r.RefinedAction == "" && r.Filter == "" && !r.RequireReply {
			warnings = append(warnings, fmt.Sprintf("patterns[%d] %q: dropped (refined with no expressible action/filter/require_reply)", i, r.Pattern))
			continue
		}
		out.Patterns = append(out.Patterns, r)
	}

	for i, td := range raw.Todos {
		if td.Text == "" {
			warnings = append(warnings, fmt.Sprintf("todos[%d]: dropped (text required)", i))
			continue
		}
		if td.Priority != "" && !validTodoPriorities[td.Priority] {
			warnings = append(warnings, fmt.Sprintf("todos[%d]: reset invalid priority %q to default", i, td.Priority))
			td.Priority = ""
		}
		out.Todos = append(out.Todos, td)
	}

	return out, warnings, nil
}

var validDistillStatuses = map[string]bool{
	"confirmed": true,
	"rejected":  true,
	"refined":   true,
}

var validTodoPriorities = map[string]bool{
	"Q1": true, "Q2": true, "Q3": true, "Q4": true,
}

func DistilledToKnowledge(distilled []DistilledPattern) []knowledge.Pattern {
	today := knowledge.Today()
	var patterns []knowledge.Pattern
	for _, d := range distilled {
		patterns = append(patterns, knowledge.Pattern{
			GmailSearch:    d.GmailSearch,
			Pattern:        d.Pattern,
			Status:         d.Status,
			Category:       d.Category,
			Senders:        d.Senders,
			FirstSeen:      today,
			LastUpdated:    today,
			CommentSummary: d.CommentSummary,
			RefinedAction:  d.RefinedAction,
			Filter:         d.Filter,
			RequireReply:   d.RequireReply,
		})
	}
	return patterns
}
