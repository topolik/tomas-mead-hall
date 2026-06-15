package knowledge

import (
	"reflect"
	"testing"
)

// TestUpsertUnionsSourceInsights verifies that re-distilling the same pattern
// (same gmail_search) with a new source insight accumulates provenance rather
// than overwriting it — so a refined pattern keeps the full insight history.
func TestUpsertUnionsSourceInsights(t *testing.T) {
	kf := &KnowledgeFile{}
	kf.Upsert(Pattern{GmailSearch: "from:x@y.com -Critical", Pattern: "p", Status: "confirmed", SourceInsights: []int64{12}})
	kf.Upsert(Pattern{GmailSearch: "from:x@y.com -Critical", Pattern: "p refined", Status: "confirmed", SourceInsights: []int64{15, 12}})

	if len(kf.Patterns) != 1 {
		t.Fatalf("expected 1 pattern after upsert, got %d", len(kf.Patterns))
	}
	got := kf.Patterns[0].SourceInsights
	if !reflect.DeepEqual(got, []int64{12, 15}) {
		t.Errorf("source insights = %v, want [12 15] (deduped, unioned, sorted)", got)
	}
	if kf.Patterns[0].Pattern != "p refined" {
		t.Errorf("pattern body should be updated to the new value, got %q", kf.Patterns[0].Pattern)
	}
}
