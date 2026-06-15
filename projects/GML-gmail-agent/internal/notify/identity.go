package notify

import (
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// Insight identity, distinct from InsightDedupKey (the structural query-string
// key). The learn LLM re-derives the SAME insight every cycle with a DIFFERENT
// gmail_search string (`{subject:…}` vs `(… OR …)` vs a subset), so the
// structural key can't collapse them. The identity key is coarse on purpose —
// the SET of affected senders plus the category — because those are what stays
// stable across rewordings, while the query is what varies.
//
// Example (verified against live data): the three "Ignore AC project status"
// insights for ac@example.com all key to "ac@example.com|ignore_pattern" and
// collapse, while {team-soc,ac}|ignore_pattern and {jira,confluence}|… stay
// separate (different sender set → different topic).

var (
	insightCategoryRe = regexp.MustCompile(`(?i)\[Insight:\s*([a-z_]+)\s*\]`)
	fromTokenRe       = regexp.MustCompile(`(?i)from:([^\s})]+)`)
)

// InsightIdentityKey builds the coarse identity key from a candidate insight's
// affected senders and category: sorted, lowercased senders joined by "," then
// "|category". Order- and case-invariant so two learn runs that list the same
// senders in any order/casing produce the same key.
func InsightIdentityKey(senders []string, category string) string {
	norm := make([]string, 0, len(senders))
	seen := make(map[string]bool)
	for _, s := range senders {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		norm = append(norm, s)
	}
	sort.Strings(norm)
	return strings.Join(norm, ",") + "|" + strings.ToLower(strings.TrimSpace(category))
}

// ParseInsightIdentity recovers the identity key from a posted insight
// notification: the category from the "[Insight: <cat>]" tag in Message and the
// senders from "from:<addr>" tokens in the (URL-decoded) Link query.
//
// CRITICAL: identity keys on the query's from:-tokens (NOT a separate
// affected_senders field), because that is all a *stored* notification carries.
// A candidate must derive its key the SAME way — see identityFromMessageLink and
// ClassifyInsights, which both route through the notification's Message+Link so
// the key a candidate computes is provably the key it will have once stored.
//
// ok=false when no sender can be parsed: a category alone is too coarse to be an
// identity (it would merge unrelated same-category topics), so a from:-less
// insight falls back to the structural floor only — never identity-matched.
func ParseInsightIdentity(n PreviousNotification) (key string, ok bool) {
	return identityFromMessageLink(n.Message, n.Link)
}

func identityFromMessageLink(message, link string) (key string, ok bool) {
	var category string
	if m := insightCategoryRe.FindStringSubmatch(message); m != nil {
		category = m[1]
	}

	q := link
	if i := strings.Index(q, "#search/"); i >= 0 {
		if dec, err := url.QueryUnescape(q[i+len("#search/"):]); err == nil {
			q = dec
		}
	}
	var senders []string
	for _, m := range fromTokenRe.FindAllStringSubmatch(q, -1) {
		senders = append(senders, m[1])
	}

	if len(senders) == 0 {
		return "", false
	}
	return InsightIdentityKey(senders, category), true
}

// InsightUpdate pairs an active notification ID with the candidate insight that
// should refresh it in place.
type InsightUpdate struct {
	ID        int64
	Candidate InsightAnalysis
}

// ClassifyInsights routes each candidate insight against the existing
// notifications (active + dismissed) into three deterministic buckets — no LLM:
//
//   - skip:   the candidate's canonical query already exists (the structural
//     floor; an exact repost of something seen or dismissed).
//   - update: the candidate's identity (sender-set + category) matches an ACTIVE
//     insight — refresh that one in place instead of posting a duplicate (2a).
//     On collision the lowest active ID wins (the canonical row); extras are
//     left for the caller to log and drain as they're dismissed.
//   - post:   everything else — genuinely new (including a re-surfaced
//     clarification of a dismissed insight, which has no active identity match).
func ClassifyInsights(candidates []InsightAnalysis, existing []PreviousNotification) (posts []InsightAnalysis, updates []InsightUpdate, skips []InsightAnalysis) {
	structural := make(map[string]bool)
	activeByIdentity := make(map[string]int64)
	for _, n := range existing {
		if n.Link != "" {
			structural[InsightDedupKey(n.Link)] = true
		}
		if n.DismissedAt != "" {
			continue
		}
		if key, ok := ParseInsightIdentity(n); ok {
			if prev, exists := activeByIdentity[key]; !exists || n.ID < prev {
				activeByIdentity[key] = n.ID
			}
		}
	}

	for _, c := range candidates {
		if c.GmailSearch != "" && structural[NormalizeSearchKey(c.GmailSearch)] {
			skips = append(skips, c)
			continue
		}
		// Derive the candidate's identity through the SAME parse the stored side
		// uses (its rendered Message+Link), so it can only match on what a stored
		// row would actually expose. A from:-less candidate yields ok=false → no
		// identity match → post (structural floor only).
		nn := InsightToNotification(c)
		if key, ok := identityFromMessageLink(nn.Message, nn.Link); ok {
			if id, matched := activeByIdentity[key]; matched {
				updates = append(updates, InsightUpdate{ID: id, Candidate: c})
				continue
			}
		}
		posts = append(posts, c)
	}
	return posts, updates, skips
}
