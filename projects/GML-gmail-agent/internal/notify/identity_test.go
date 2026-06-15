package notify

import "testing"

func TestInsightIdentityKey_OrderCaseInvariant(t *testing.T) {
	a := InsightIdentityKey([]string{"B@x.com", "a@x.com"}, "ignore_pattern")
	b := InsightIdentityKey([]string{"a@X.com", "b@x.com"}, "Ignore_Pattern")
	if a != b {
		t.Fatalf("expected order/case invariance: %q != %q", a, b)
	}
	if a != "a@x.com,b@x.com|ignore_pattern" {
		t.Errorf("unexpected key: %q", a)
	}
}

func TestInsightIdentityKey_DedupesSenders(t *testing.T) {
	got := InsightIdentityKey([]string{"a@x.com", "a@x.com", " a@x.com "}, "archive_candidate")
	if got != "a@x.com|archive_candidate" {
		t.Errorf("expected deduped single sender, got %q", got)
	}
}

// The three real "Ignore AC project status" insight links (#1107/#1119/#1137)
// all carry sender ac@example.com and category ignore_pattern — they MUST
// collapse to one identity key even though their query strings differ, while
// the multi-sender {team-soc,ac} insight (#1096) stays separate.
func TestParseInsightIdentity_ACClusterCollapses(t *testing.T) {
	ac := []PreviousNotification{
		{Message: "🔵 Q3 [Insight: ignore_pattern] Ignore AC project status",
			Link: "https://mail.google.com/mail/u/0/#search/from%3Aac%40example.com+%7Bsubject%3A%22project+down%22+subject%3A%22projects+down%22%7D"},
		{Message: "🔵 Q3 [Insight: ignore_pattern] Ignore AC Project Status",
			Link: "https://mail.google.com/mail/u/0/#search/from%3Aac%40example.com+%28%22projects+down%22+OR+%22projects+up%22+OR+%22project+down%22%29"},
		{Message: "🔵 Q3 [Insight: ignore_pattern] Ignore AC project status",
			Link: "https://mail.google.com/mail/u/0/#search/from%3Aac%40example.com+%28%22projects+down%22+OR+%22projects+up%22%29"},
	}
	var keys []string
	for _, n := range ac {
		k, ok := ParseInsightIdentity(n)
		if !ok {
			t.Fatalf("expected parseable identity for %q", n.Message)
		}
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		if keys[i] != keys[0] {
			t.Fatalf("AC insights must share one identity: %q != %q", keys[i], keys[0])
		}
	}
	if keys[0] != "ac@example.com|ignore_pattern" {
		t.Errorf("unexpected AC key: %q", keys[0])
	}

	// #1096 — same category, different (larger) sender set → different key.
	multi := PreviousNotification{
		Message: "🔵 Q3 [Insight: ignore_pattern] High volume entirely unread",
		Link:    "https://mail.google.com/mail/u/0/#search/%7Bfrom%3Ateam-soc%40example.com+from%3Aac%40example.com%7D",
	}
	mk, ok := ParseInsightIdentity(multi)
	if !ok {
		t.Fatal("expected parseable identity for #1096")
	}
	if mk == keys[0] {
		t.Errorf("multi-sender insight must NOT collapse into the {ac} cluster: %q", mk)
	}
	if mk != "ac@example.com,team-soc@example.com|ignore_pattern" {
		t.Errorf("unexpected multi key: %q", mk)
	}
}

func TestParseInsightIdentity_NonInsight(t *testing.T) {
	if _, ok := ParseInsightIdentity(PreviousNotification{Message: "plain note", Link: ""}); ok {
		t.Error("expected ok=false for a row with no category and no senders")
	}
}

// A category alone is too coarse to be an identity — a from:-less insight must
// return ok=false so it falls back to the structural floor (never identity-matched
// against unrelated same-category insights).
func TestParseInsightIdentity_NoSenderFallsBack(t *testing.T) {
	n := PreviousNotification{
		Message: "🔵 Q3 [Insight: ignore_pattern] Ignore project-down chatter",
		Link:    "https://mail.google.com/mail/u/0/#search/subject%3A%22projects+down%22",
	}
	if _, ok := ParseInsightIdentity(n); ok {
		t.Error("expected ok=false for a from:-less insight (identity needs at least one sender)")
	}
}

// The identity key must be derived symmetrically: a candidate keys on its own
// gmail_search from:-tokens (via the rendered notification), exactly as the
// stored side will re-parse it. A candidate that puts senders only in
// affected_senders (not in gmail_search) therefore does NOT identity-match — it
// falls back to the structural floor and posts, rather than silently colliding.
func TestClassifyInsights_SymmetricKeyDerivation(t *testing.T) {
	existing := []PreviousNotification{
		// active, from:-less insight → not in the identity index.
		{ID: 2001, Message: "🔵 Q3 [Insight: ignore_pattern] project-down chatter",
			Link: "https://mail.google.com/mail/u/0/#search/subject%3A%22projects+down%22"},
	}
	candidates := []InsightAnalysis{
		// senders only in affected_senders, gmail_search has no from: → no match.
		{Pattern: "project-down chatter (reworded)", Category: "ignore_pattern",
			AffectedSenders: []string{"ac@example.com"}, SignalStrength: "moderate",
			GmailSearch: "subject:\"project down\""},
	}
	posts, updates, _ := ClassifyInsights(candidates, existing)
	if len(updates) != 0 {
		t.Fatalf("from:-less candidate must not identity-match, got updates %+v", updates)
	}
	if len(posts) != 1 {
		t.Fatalf("expected the candidate to post (floor-only), got %d posts", len(posts))
	}
}

func TestClassifyInsights_UpdatePostSkip(t *testing.T) {
	existing := []PreviousNotification{
		// active AC insight — a reworded candidate should UPDATE this id.
		{ID: 1107, Message: "🔵 Q3 [Insight: ignore_pattern] Ignore AC project status",
			Link: "https://mail.google.com/mail/u/0/#search/from%3Aac%40example.com+%28%22projects+down%22%29"},
		// dismissed jira insight — an exact-query repost should SKIP (floor).
		{ID: 1110, DismissedAt: "2026-06-09 11:00", Message: "⚪ Q4 [Insight: archive_candidate] jira",
			Link: "https://mail.google.com/mail/u/0/#search/from%3Ajira%40tracker.example.com"},
	}
	candidates := []InsightAnalysis{
		// reworded AC, same identity as active #1107 → update.
		{Pattern: "Ignore AC project status", Category: "ignore_pattern",
			AffectedSenders: []string{"ac@example.com"}, SignalStrength: "strong",
			GmailSearch: "from:ac@example.com {subject:\"projects down\" subject:\"projects up\"}"},
		// exact repost of dismissed jira → skip (structural floor).
		{Pattern: "jira", Category: "archive_candidate",
			AffectedSenders: []string{"jira@tracker.example.com"}, SignalStrength: "weak",
			GmailSearch: "from:jira@tracker.example.com"},
		// brand-new sender → post.
		{Pattern: "newsletters", Category: "archive_candidate",
			AffectedSenders: []string{"news@foo.com"}, SignalStrength: "moderate",
			GmailSearch: "from:news@foo.com"},
	}
	posts, updates, skips := ClassifyInsights(candidates, existing)
	if len(updates) != 1 || updates[0].ID != 1107 {
		t.Fatalf("expected one update of #1107, got %+v", updates)
	}
	if len(skips) != 1 || skips[0].Pattern != "jira" {
		t.Fatalf("expected one skip (jira), got %+v", skips)
	}
	if len(posts) != 1 || posts[0].Pattern != "newsletters" {
		t.Fatalf("expected one post (newsletters), got %+v", posts)
	}
}

func TestClassifyInsights_LowestActiveIDWins(t *testing.T) {
	existing := []PreviousNotification{
		{ID: 1137, Message: "[Insight: ignore_pattern] AC", Link: "https://mail.google.com/mail/u/0/#search/from%3Aac%40example.com+%28%22x%22%29"},
		{ID: 1107, Message: "[Insight: ignore_pattern] AC", Link: "https://mail.google.com/mail/u/0/#search/from%3Aac%40example.com+%28%22y%22%29"},
	}
	candidates := []InsightAnalysis{
		// a third rewording (different query → passes the structural floor) whose
		// identity matches both active AC rows.
		{Pattern: "AC", Category: "ignore_pattern", AffectedSenders: []string{"ac@example.com"},
			SignalStrength: "strong", GmailSearch: "from:ac@example.com (\"z\")"},
	}
	_, updates, _ := ClassifyInsights(candidates, existing)
	if len(updates) != 1 || updates[0].ID != 1107 {
		t.Fatalf("expected update of lowest active id 1107, got %+v", updates)
	}
}
