package notify

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/topolik/gml-gmail-agent/internal/propose"
)

var timeWindowOp = regexp.MustCompile(`(?i)\b(?:newer|older)_than:\S+\s*`)

// InsightDedupKey turns an insight notification Link into a stable dedup key.
// Insight links are URL-encoded Gmail searches (see GmailSearchURL) whose query
// the LLM regenerates each run — for the SAME recurring insight it inconsistently
// adds/drops a newer_than/older_than time window and reorders terms, which
// defeated naive link-equality dedup (the grafana "ETD Bucket" insight reappears
// with and without "+newer_than:7d"). We decode the query, drop the volatile
// time-window operators, and canonicalize the rest so equivalent searches
// collapse to one key. Subject-phrase differences are deliberately preserved —
// those are semantic and are the learn LLM's job, not this structural guard's.
func InsightDedupKey(link string) string {
	q := link
	if i := strings.Index(link, "#search/"); i >= 0 {
		if dec, err := url.QueryUnescape(link[i+len("#search/"):]); err == nil {
			q = dec
		}
	}
	return NormalizeSearchKey(q)
}

// NormalizeSearchKey canonicalizes an already-decoded Gmail search/query into the
// same stable key InsightDedupKey produces: it drops volatile newer_than/older_than
// time windows then applies CanonicalQuery (lowercased operators, single-token
// quote strip, date-unit canon, dedupe+sort). Used to JOIN an insight's Link to a
// knowledge pattern's gmail_search so provenance is attributed deterministically
// (no LLM) even when the two differ only by a time window, quoting, or term order.
func NormalizeSearchKey(query string) string {
	q := timeWindowOp.ReplaceAllString(query, "")
	return propose.CanonicalQuery(strings.TrimSpace(q))
}

type InsightAnalysis struct {
	Pattern         string   `json:"pattern"`
	Evidence        string   `json:"evidence"`
	SignalStrength  string   `json:"signal_strength"`
	Category        string   `json:"category"`
	AffectedSenders []string `json:"affected_senders"`
	SuggestedAction string   `json:"suggested_action"`
	GmailSearch     string   `json:"gmail_search"`
}

var validSignalStrengths = map[string]bool{
	"strong":   true,
	"moderate": true,
	"weak":     true,
}

var validCategories = map[string]bool{
	"reply_pattern":     true,
	"ignore_pattern":    true,
	"priority_pattern":  true,
	"archive_candidate": true,
}

var signalToPriority = map[string]string{
	"strong":   "Q3",
	"moderate": "Q4",
	"weak":     "Q4",
}

func ParseAndValidateInsights(data []byte) ([]InsightAnalysis, error) {
	data = []byte(stripCodeFence(string(data)))
	if len(data) == 0 {
		return nil, fmt.Errorf("empty input")
	}

	var results []InsightAnalysis
	if err := json.Unmarshal(data, &results); err != nil {
		preview := string(data)
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		return nil, fmt.Errorf("invalid JSON: %w\nraw output: %s", err, preview)
	}

	for i, r := range results {
		if r.Pattern == "" {
			return nil, fmt.Errorf("result[%d]: pattern is required", i)
		}
		if r.Evidence == "" {
			return nil, fmt.Errorf("result[%d]: evidence is required", i)
		}
		if !validSignalStrengths[r.SignalStrength] {
			return nil, fmt.Errorf("result[%d]: signal_strength must be strong/moderate/weak, got %q", i, r.SignalStrength)
		}
		if !validCategories[r.Category] {
			return nil, fmt.Errorf("result[%d]: category must be reply_pattern/ignore_pattern/priority_pattern/archive_candidate, got %q", i, r.Category)
		}
		if len(r.AffectedSenders) == 0 {
			return nil, fmt.Errorf("result[%d]: affected_senders is required (at least one)", i)
		}
		if r.SuggestedAction == "" {
			return nil, fmt.Errorf("result[%d]: suggested_action is required", i)
		}
		if r.GmailSearch == "" {
			return nil, fmt.Errorf("result[%d]: gmail_search is required", i)
		}
	}
	return results, nil
}

// InsightToNotification formats a single analyzed insight into a DSH
// notification. Shared by the post path (InsightsToNotifications) and the
// in-place update path (cmdInsights → UpdateNotification) so a refreshed insight
// renders identically to a freshly posted one.
func InsightToNotification(r InsightAnalysis) Notification {
	priority := signalToPriority[r.SignalStrength]
	icon := priorityIcon[priority]
	dshType := priorityToDSHType[priority]
	return Notification{
		ProjectCode: "GML",
		Message:     fmt.Sprintf("%s %s [Insight: %s] %s — %s", icon, priority, r.Category, r.Pattern, r.SuggestedAction),
		Type:        dshType,
		Priority:    priority,
		Link:        GmailSearchURL(r.GmailSearch),
	}
}

func InsightsToNotifications(insights []InsightAnalysis) []Notification {
	var notifs []Notification
	for _, r := range insights {
		notifs = append(notifs, InsightToNotification(r))
	}
	return notifs
}
