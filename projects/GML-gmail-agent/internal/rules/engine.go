package rules

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/topolik/gml-gmail-agent/internal/config"
	"github.com/topolik/gml-gmail-agent/internal/creds"
	"github.com/topolik/gml-gmail-agent/internal/gws"
)

type Action struct {
	MessageID string    `json:"message_id"`
	Subject   string    `json:"subject"`
	From      string    `json:"from"`
	Date      string    `json:"date,omitempty"`
	RuleName  string    `json:"rule_name"`
	RuleType  string    `json:"rule_type"`
	Reason    string    `json:"reason"`
	Archived  bool      `json:"archived"`
	Timestamp time.Time `json:"timestamp"`
}

type Result struct {
	Actions []Action `json:"actions"`
	Errors  []string `json:"errors,omitempty"`
}

// TracingLabelName is the Gmail label applied to every message GML archives.
const TracingLabelName = "GML/archived"

// RunWithSenderFilter evaluates all rules against the inbox.
// For archive_by_sender rules with regex patterns, it fetches inbox broadly and filters client-side.
// If tracingLabelID is non-empty, it is atomically applied alongside the archive.
func RunWithSenderFilter(cfg *config.Config, cr *creds.Creds, dryRun bool, pageLimit int, sinceHours int, tracingLabelID string) (*Result, error) {
	if pageLimit <= 0 {
		pageLimit = 5
	}
	result := &Result{Actions: []Action{}}
	threadCache := make(map[string]*gws.Thread)

	for _, rule := range cfg.Rules {
		var refs []gws.MessageRef
		var err error

		var sinceFilter string
		if sinceHours > 0 {
			sinceFilter = fmt.Sprintf("newer_than:%dh", sinceHours)
		}

		if rule.Type == "archive_by_sender" && hasSenderRegex(rule.Params.Patterns) {
			q := "in:inbox"
			if sinceFilter != "" {
				q += " " + sinceFilter
			}
			if rule.Params.Filter != "" {
				q += " " + rule.Params.Filter
			}
			refs, err = gws.ListMessages(cr, q, pageLimit)
		} else {
			query, qErr := buildQuery(rule, sinceFilter)
			if qErr != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("rule %q: %v", rule.Name, qErr))
				continue
			}
			refs, err = gws.ListMessages(cr, query, pageLimit)
		}
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("rule %q list: %v", rule.Name, err))
			continue
		}

		for _, ref := range refs {
			msg, err := gws.GetMessage(cr, ref.ID)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("get message %s: %v", ref.ID, err))
				continue
			}

			if rule.Type == "archive_by_sender" {
				if !matchesSenderPatterns(msg.From(), rule.Params.Patterns) {
					continue
				}
			}

			if rule.Params.RequireReply && ref.ThreadID != "" {
				thread, ok := threadCache[ref.ThreadID]
				if !ok {
					t, tErr := gws.GetThread(cr, ref.ThreadID)
					if tErr != nil {
						result.Errors = append(result.Errors, fmt.Sprintf("get thread %s: %v", ref.ThreadID, tErr))
						continue
					}
					thread = t
					threadCache[ref.ThreadID] = thread
				}
				if len(thread.Messages) <= 1 {
					continue
				}
			}

			var reason string
			var msgDate string

			if rule.Type == "archive_by_age" {
				ts, parseErr := strconv.ParseInt(msg.InternalDate, 10, 64)
				if parseErr == nil {
					msgTime := time.UnixMilli(ts)
					age := time.Since(msgTime)
					ageDays := int(age.Hours() / 24)
					msgDate = msgTime.Format("2006-01-02")
					if age < time.Duration(rule.Params.Days)*24*time.Hour {
						continue
					}
					reason = fmt.Sprintf("message is %d days old (threshold: %d days, state: %s)", ageDays, rule.Params.Days, rule.Params.State)
				}
				switch rule.Params.State {
				case "read":
					if msg.HasLabel("UNREAD") {
						continue
					}
				case "unread":
					if !msg.HasLabel("UNREAD") {
						continue
					}
				}
			}

			if rule.Type == "archive_by_sender" {
				matched := matchedPattern(msg.From(), rule.Params.Patterns)
				reason = fmt.Sprintf("sender matches pattern: %s", matched)
			}

			if rule.Type == "archive_by_label" {
				reason = fmt.Sprintf("has label: %s", rule.Params.Label)
			}

			if rule.Params.RequireReply {
				reason += " (thread has replies)"
			}

			if msgDate == "" {
				ts, parseErr := strconv.ParseInt(msg.InternalDate, 10, 64)
				if parseErr == nil {
					msgDate = time.UnixMilli(ts).Format("2006-01-02")
				}
			}

			action := Action{
				MessageID: ref.ID,
				Subject:   msg.Subject(),
				From:      msg.From(),
				Date:      msgDate,
				RuleName:  rule.Name,
				RuleType:  rule.Type,
				Reason:    reason,
				Timestamp: time.Now(),
			}
			if !dryRun {
				var archiveErr error
				if tracingLabelID != "" {
					archiveErr = gws.ArchiveWithLabel(cr, ref.ID, tracingLabelID)
				} else {
					archiveErr = gws.Archive(cr, ref.ID)
				}
				if archiveErr != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("archive %s: %v", ref.ID, archiveErr))
					continue
				}
				action.Archived = true
			}
			result.Actions = append(result.Actions, action)
		}
	}
	return result, nil
}

func buildQuery(rule config.Rule, sinceFilter string) (string, error) {
	p := rule.Params
	var parts []string
	parts = append(parts, "in:inbox")
	if sinceFilter != "" {
		parts = append(parts, sinceFilter)
	}

	switch rule.Type {
	case "archive_by_age":
		cutoff := time.Now().AddDate(0, 0, -p.Days)
		parts = append(parts, fmt.Sprintf("before:%s", cutoff.Format("2006/01/02")))
		switch p.State {
		case "read":
			parts = append(parts, "is:read")
		case "unread":
			parts = append(parts, "is:unread")
		}

	case "archive_by_sender":
		var senderParts []string
		for _, pattern := range p.Patterns {
			if isSimpleEmailPattern(pattern) {
				senderParts = append(senderParts, fmt.Sprintf("from:(%s)", pattern))
			} else {
				return "in:inbox", nil
			}
		}
		if len(senderParts) == 1 {
			parts = append(parts, senderParts[0])
		} else {
			parts = append(parts, "{"+strings.Join(senderParts, " ")+"}")
		}

	case "archive_by_label":
		parts = append(parts, "label:"+p.Label)

	default:
		return "", fmt.Errorf("unknown rule type %q", rule.Type)
	}

	if rule.Params.Filter != "" {
		parts = append(parts, rule.Params.Filter)
	}

	return strings.Join(parts, " "), nil
}

func isSimpleEmailPattern(s string) bool {
	for _, c := range s {
		if strings.ContainsRune(`^$.()+?[]{}\\`, c) {
			return false
		}
	}
	return true
}

func hasSenderRegex(patterns []string) bool {
	for _, p := range patterns {
		if !isSimpleEmailPattern(p) {
			return true
		}
	}
	return false
}

func matchesSenderPatterns(from string, patterns []string) bool {
	return matchedPattern(from, patterns) != ""
}

func matchedPattern(from string, patterns []string) string {
	from = strings.ToLower(from)
	for _, p := range patterns {
		if isSimpleEmailPattern(p) {
			if strings.Contains(from, strings.ToLower(p)) {
				return p
			}
		} else {
			re, err := regexp.Compile("(?i)" + p)
			if err == nil && re.MatchString(from) {
				return p
			}
		}
	}
	return ""
}
