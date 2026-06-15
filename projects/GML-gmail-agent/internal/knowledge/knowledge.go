package knowledge

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type KnowledgeFile struct {
	LastDistilledAt string    `yaml:"last_distilled_at"`
	Patterns        []Pattern `yaml:"patterns"`
	// DistilledInsights is the local "already distilled" ledger (iter 023): DSH
	// insight (notification) IDs that distill has processed but that produced no
	// matching knowledge pattern — the residual gap provenance can't derive
	// (todo-only / distilled-to-nothing / non-matching query). Together with
	// pattern SourceInsights it forms the full skip-set, so distill never re-feeds
	// a handled insight to the LLM. Append-only.
	DistilledInsights []int64 `yaml:"distilled_insights,omitempty"`
}

type Pattern struct {
	GmailSearch    string   `yaml:"gmail_search"`
	Pattern        string   `yaml:"pattern"`
	Status         string   `yaml:"status"`
	Category       string   `yaml:"category"`
	Senders        []string `yaml:"senders"`
	FirstSeen      string   `yaml:"first_seen"`
	LastUpdated    string   `yaml:"last_updated"`
	CommentSummary string   `yaml:"comment_summary"`
	RefinedAction  string   `yaml:"refined_action,omitempty"`
	Filter         string   `yaml:"filter,omitempty"`
	RequireReply   bool     `yaml:"require_reply,omitempty"`
	// SourceInsights are the DSH insight (notification) IDs this pattern was
	// distilled from — back-tracking provenance, attributed by the deterministic
	// Link↔gmail_search join in cmdDistillApply. Unioned across refinements.
	SourceInsights []int64 `yaml:"source_insights,omitempty"`
}

var validStatuses = map[string]bool{
	"confirmed": true,
	"rejected":  true,
	"refined":   true,
}

func Load(path string) (*KnowledgeFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &KnowledgeFile{}, nil
		}
		return nil, fmt.Errorf("read knowledge file: %w", err)
	}
	var kf KnowledgeFile
	if err := yaml.Unmarshal(data, &kf); err != nil {
		return nil, fmt.Errorf("parse knowledge file: %w", err)
	}
	return &kf, nil
}

func Save(path string, kf *KnowledgeFile) error {
	data, err := yaml.Marshal(kf)
	if err != nil {
		return fmt.Errorf("marshal knowledge file: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

func (kf *KnowledgeFile) Upsert(p Pattern) {
	for i, existing := range kf.Patterns {
		if existing.GmailSearch == p.GmailSearch {
			p.FirstSeen = existing.FirstSeen
			p.SourceInsights = unionInsights(existing.SourceInsights, p.SourceInsights)
			kf.Patterns[i] = p
			return
		}
	}
	kf.Patterns = append(kf.Patterns, p)
}

// unionInsights merges two insight-ID lists, deduped and ascending, so a
// refinement accumulates provenance rather than dropping prior source IDs.
func unionInsights(a, b []int64) []int64 {
	seen := map[int64]bool{}
	var out []int64
	for _, id := range a {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	for _, id := range b {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func ValidatePattern(p Pattern) error {
	if p.GmailSearch == "" {
		return fmt.Errorf("gmail_search is required")
	}
	if p.Pattern == "" {
		return fmt.Errorf("pattern is required")
	}
	if !validStatuses[p.Status] {
		return fmt.Errorf("status must be confirmed/rejected/refined, got %q", p.Status)
	}
	if p.Status == "refined" && p.RefinedAction == "" && p.Filter == "" && !p.RequireReply {
		return fmt.Errorf("refined_action, filter, or require_reply is required when status is refined")
	}
	return nil
}

func Format(kf *KnowledgeFile) string {
	if len(kf.Patterns) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, p := range kf.Patterns {
		fmt.Fprintf(&sb, "- [%s] %s (gmail: %s)\n", p.Status, p.Pattern, p.GmailSearch)
		if p.CommentSummary != "" {
			fmt.Fprintf(&sb, "  Comment: %s\n", p.CommentSummary)
		}
		if p.RefinedAction != "" {
			fmt.Fprintf(&sb, "  Refined action: %s\n", p.RefinedAction)
		}
		if p.Filter != "" {
			fmt.Fprintf(&sb, "  Filter: %s\n", p.Filter)
		}
		if p.RequireReply {
			sb.WriteString("  Require reply: true\n")
		}
	}
	return sb.String()
}

func Now() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func Today() string {
	return time.Now().UTC().Format("2006-01-02")
}
