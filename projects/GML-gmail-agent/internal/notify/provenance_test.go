package notify

import "testing"

// TestProvenanceJoin verifies the deterministic insight→pattern join: an insight
// Link and a knowledge pattern's gmail_search canonicalize to the SAME key even
// when they differ by URL-encoding, a volatile time window, quoting, or term
// order — so cmdDistillApply attributes source IDs without the LLM.
func TestProvenanceJoin(t *testing.T) {
	tests := []struct {
		name        string
		link        string // insight Link (URL-encoded #search/ form)
		gmailSearch string // pattern.gmail_search (decoded)
		wantMatch   bool
	}{
		{
			name:        "url-encoded link vs decoded search",
			link:        "https://mail.google.com/mail/u/0/#search/from%3Ano-reply%40socradar.com%20-Critical",
			gmailSearch: "from:no-reply@socradar.com -Critical",
			wantMatch:   true,
		},
		{
			name:        "time window on the link only",
			link:        "https://mail.google.com/mail/u/0/#search/from%3Asupport-noreply%40snyk.io%20newer_than%3A7d",
			gmailSearch: "from:support-noreply@snyk.io",
			wantMatch:   true,
		},
		{
			name:        "term order + quoting variants",
			link:        "https://mail.google.com/mail/u/0/#search/-Critical%20from%3Ax%40y.com",
			gmailSearch: `from:x@y.com -"Critical"`,
			wantMatch:   true,
		},
		{
			name:        "genuinely different senders do not match",
			link:        "https://mail.google.com/mail/u/0/#search/from%3Aa%40y.com",
			gmailSearch: "from:b@y.com",
			wantMatch:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lk := InsightDedupKey(tt.link)
			gk := NormalizeSearchKey(tt.gmailSearch)
			if (lk == gk) != tt.wantMatch {
				t.Errorf("join mismatch: link key %q vs gmail key %q (wantMatch=%v)", lk, gk, tt.wantMatch)
			}
		})
	}
}
