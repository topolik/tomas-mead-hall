package stats

import (
	"fmt"
	"sort"
	"strings"

	"github.com/topolik/gml-gmail-agent/internal/creds"
	"github.com/topolik/gml-gmail-agent/internal/gws"
)

type InboxStats struct {
	TotalMessages int
	TotalThreads  int
	RecentDays    int
	RecentCount   int
	TopSenders    []SenderCount
}

type SenderCount struct {
	Sender string
	Count  int
}

// Collect fetches inbox stats. Profile gives exact totals (1 API call).
// Recent messages (last N days) are listed and fetched individually for
// sender analysis. Default window is 3 days.
func Collect(cr *creds.Creds, days int) (*InboxStats, error) {
	if days <= 0 {
		days = 3
	}

	profile, err := gws.GetProfile(cr)
	if err != nil {
		return nil, fmt.Errorf("getting profile: %w", err)
	}

	query := fmt.Sprintf("in:inbox newer_than:%dd", days)
	refs, err := gws.ListMessages(cr, query, 0)
	if err != nil {
		return nil, fmt.Errorf("listing recent messages: %w", err)
	}

	senderCounts := map[string]int{}
	for _, ref := range refs {
		msg, err := gws.GetMessage(cr, ref.ID)
		if err != nil {
			continue
		}
		from := extractEmail(msg.From())
		senderCounts[from]++
	}

	type kv struct {
		k string
		v int
	}
	var sorted []kv
	for k, v := range senderCounts {
		sorted = append(sorted, kv{k, v})
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].v > sorted[j].v })
	var topSenders []SenderCount
	for i, kv := range sorted {
		if i >= 10 {
			break
		}
		topSenders = append(topSenders, SenderCount{kv.k, kv.v})
	}

	return &InboxStats{
		TotalMessages: profile.MessagesTotal,
		TotalThreads:  profile.ThreadsTotal,
		RecentDays:    days,
		RecentCount:   len(refs),
		TopSenders:    topSenders,
	}, nil
}

func extractEmail(from string) string {
	if start := strings.Index(from, "<"); start != -1 {
		if end := strings.Index(from[start:], ">"); end != -1 {
			return strings.ToLower(from[start+1 : start+end])
		}
	}
	return strings.ToLower(strings.TrimSpace(from))
}
