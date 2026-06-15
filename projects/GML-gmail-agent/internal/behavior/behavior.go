package behavior

import (
	"fmt"
	"sort"
	"strings"

	"github.com/topolik/gml-gmail-agent/internal/config"
	"github.com/topolik/gml-gmail-agent/internal/gws"
	"github.com/topolik/gml-gmail-agent/internal/notify"
)

type GmailClient interface {
	ListMessages(query string, maxPages int) ([]gws.MessageRef, error)
	GetMessage(id string) (*gws.Message, error)
	ListThreads(query string, maxPages int) ([]gws.ThreadRef, error)
	GetThread(id string) (*gws.Thread, error)
}

type SenderBehavior struct {
	Email          string  `json:"email"`
	TotalEmails    int     `json:"total_emails"`
	ReadCount      int     `json:"read_count"`
	ReadRate       float64 `json:"read_rate"`
	ThreadCount    int     `json:"thread_count"`
	RepliedThreads int     `json:"replied_threads"`
	ReplyRate      float64 `json:"reply_rate"`
}

type BehaviorSummary struct {
	WindowDays             int                          `json:"window_days"`
	Senders                []SenderBehavior             `json:"senders"`
	DismissedNotifications []notify.PreviousNotification `json:"dismissed_notifications"`
	ActiveRules            []config.Rule                `json:"active_rules"`
}

type senderAgg struct {
	email     string
	total     int
	readCount int
	threadIDs map[string]bool
}

func CollectSenderBehavior(client GmailClient, days int, topN int, minEmails int) ([]SenderBehavior, error) {
	query := fmt.Sprintf("in:inbox newer_than:%dd", days)
	refs, err := client.ListMessages(query, 10)
	if err != nil {
		return nil, fmt.Errorf("listing messages: %w", err)
	}

	agg := make(map[string]*senderAgg)
	for _, ref := range refs {
		msg, err := client.GetMessage(ref.ID)
		if err != nil {
			continue
		}
		from := extractEmail(msg.From())
		if from == "" {
			continue
		}

		s, ok := agg[from]
		if !ok {
			s = &senderAgg{email: from, threadIDs: make(map[string]bool)}
			agg[from] = s
		}
		s.total++
		if !msg.HasLabel("UNREAD") {
			s.readCount++
		}
		if ref.ThreadID != "" {
			s.threadIDs[ref.ThreadID] = true
		}
	}

	var filtered []*senderAgg
	for _, s := range agg {
		if s.total >= minEmails {
			filtered = append(filtered, s)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].total > filtered[j].total
	})
	if len(filtered) > topN {
		filtered = filtered[:topN]
	}

	var results []SenderBehavior
	for _, s := range filtered {
		sb := SenderBehavior{
			Email:       s.email,
			TotalEmails: s.total,
			ReadCount:   s.readCount,
			ReadRate:    float64(s.readCount) / float64(s.total),
			ThreadCount: len(s.threadIDs),
		}

		replied := 0
		threadList := make([]string, 0, len(s.threadIDs))
		for tid := range s.threadIDs {
			threadList = append(threadList, tid)
		}
		maxThreads := 50
		if len(threadList) > maxThreads {
			threadList = threadList[:maxThreads]
		}

		for _, tid := range threadList {
			thread, err := client.GetThread(tid)
			if err != nil {
				continue
			}
			for _, m := range thread.Messages {
				if m.HasLabel("SENT") {
					replied++
					break
				}
			}
		}

		sb.RepliedThreads = replied
		if sb.ThreadCount > 0 {
			sb.ReplyRate = float64(replied) / float64(sb.ThreadCount)
		}
		results = append(results, sb)
	}

	return results, nil
}

func extractEmail(from string) string {
	if i := strings.Index(from, "<"); i >= 0 {
		if j := strings.Index(from[i:], ">"); j >= 0 {
			return strings.ToLower(from[i+1 : i+j])
		}
	}
	return strings.ToLower(strings.TrimSpace(from))
}
