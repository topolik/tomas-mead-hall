package prompt

import (
	"strings"
	"testing"

	"github.com/topolik/gml-gmail-agent/internal/fetch"
	"github.com/topolik/gml-gmail-agent/internal/knowledge"
)

func TestBuildWithKnowledgeHints(t *testing.T) {
	boxes := []fetch.BoxResult{
		{
			Box: fetch.Box{Number: 1, Name: "TODO"},
		},
	}
	patterns := []knowledge.Pattern{
		{
			GmailSearch: "from:david@gen0sec.com",
			Pattern:     "High priority for Gen0sec POC notifications",
			Category:    "priority_pattern",
		},
		{
			GmailSearch:   "from:team-soc@example.com",
			Pattern:       "Team SOC conditional archive",
			Category:      "archive_candidate",
			RefinedAction: "Only ignore if someone responds on the thread",
		},
		{
			GmailSearch: "from:noreply@github.com",
			Pattern:     "GitHub notifications",
			Category:    "archive_candidate",
		},
	}

	result := Build(boxes, "", "", patterns)

	if !strings.Contains(result, "PRIORITY KNOWLEDGE") {
		t.Error("expected PRIORITY KNOWLEDGE section")
	}
	if !strings.Contains(result, "from:david@gen0sec.com") {
		t.Error("expected Gen0sec hint")
	}
	if !strings.Contains(result, "from:team-soc@example.com") {
		t.Error("expected Team SOC hint (has refined_action)")
	}
	if strings.Contains(result, "from:noreply@github.com") {
		t.Error("GitHub should NOT appear in hints (not priority_pattern, no refined_action)")
	}
}

func TestBuildWithoutKnowledgeHints(t *testing.T) {
	boxes := []fetch.BoxResult{
		{
			Box: fetch.Box{Number: 1, Name: "TODO"},
		},
	}

	result := Build(boxes, "", "", nil)

	if strings.Contains(result, "PRIORITY KNOWLEDGE") {
		t.Error("PRIORITY KNOWLEDGE section should be absent when no patterns match")
	}
}

func TestBuildIncludesGmailOperators(t *testing.T) {
	boxes := []fetch.BoxResult{
		{
			Box: fetch.Box{Number: 1, Name: "TODO"},
		},
	}

	result := Build(boxes, "", "", nil)

	if !strings.Contains(result, "GMAIL SEARCH OPERATORS") {
		t.Error("expected Gmail operators reference in prompt")
	}
	if !strings.Contains(result, "from:") {
		t.Error("expected operator examples in reference")
	}
}
