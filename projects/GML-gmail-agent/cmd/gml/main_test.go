package main

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/topolik/gml-gmail-agent/internal/config"
	"github.com/topolik/gml-gmail-agent/internal/knowledge"
	"github.com/topolik/gml-gmail-agent/internal/notify"
	"github.com/topolik/gml-gmail-agent/internal/propose"
)

func planWithProposal(status string, p propose.Proposal) notify.Plan {
	detail, _ := json.Marshal(p)
	return notify.Plan{Status: status, Detail: string(detail)}
}

// TestStructuralDedup_InsightProvenance verifies the provenance-keyed dedup:
// a candidate whose source insight #IDs are all covered by an existing live plan
// is skipped even when its filter differs (e.g. a re-proposal of a folded plan).
func TestStructuralDedup_InsightProvenance(t *testing.T) {
	existing := []notify.Plan{
		planWithProposal("approved", propose.Proposal{
			ProposedRule:   config.Rule{Name: "socradar", Type: "archive_by_sender", Params: config.RuleParams{Patterns: []string{"x@socradar.com"}, Filter: "-Critical -VIP"}},
			SourceInsights: []int64{12, 15},
		}),
	}
	// Candidate from the same insights but a DIFFERENT (pre-fold) filter → still a dup.
	covered := propose.Proposal{
		ProposedRule:   config.Rule{Name: "socradar", Type: "archive_by_sender", Params: config.RuleParams{Patterns: []string{"x@socradar.com"}, Filter: "-VIP"}},
		SourceInsights: []int64{12},
	}
	// Candidate from a NEW insight → must survive.
	fresh := propose.Proposal{
		ProposedRule:   config.Rule{Name: "okta", Type: "archive_by_sender", Params: config.RuleParams{Patterns: []string{"z@okta.com"}, Filter: ""}},
		SourceInsights: []int64{99},
	}
	survivors, skipped := structuralDedup([]propose.Proposal{covered, fresh}, existing)
	if skipped != 1 {
		t.Errorf("expected 1 skipped (provenance-covered), got %d", skipped)
	}
	if len(survivors) != 1 || survivors[0].ProposedRule.Name != "okta" {
		t.Errorf("expected only the fresh okta candidate to survive, got %+v", survivors)
	}
}

// TestSelectUndistilled is the repeat-killer: an insight whose ID is already in a
// knowledge pattern's SourceInsights must be skipped on the next distill, while a
// fresh dismissed notification is still gathered.
func TestSelectUndistilled(t *testing.T) {
	kf := &knowledge.KnowledgeFile{Patterns: []knowledge.Pattern{
		{GmailSearch: "from:x@y.com -Critical", SourceInsights: []int64{101}},
	}}
	dismissed := []notify.PreviousNotification{
		{ID: 101, Message: "[Insight: archive_candidate] socradar", Comment: "yes"},       // already distilled
		{ID: 102, Message: "[Insight: priority_pattern] google noisy", Comment: "filter"}, // new insight
		{ID: 103, Message: "plain notification", Comment: "do this"},                      // new commented regular
		{ID: 104, Message: "plain notification", Comment: ""},                             // no comment → ignored
	}
	insights, regulars, skipped := selectUndistilled(dismissed, kf)
	if skipped != 1 {
		t.Errorf("expected 1 skipped (already distilled), got %d", skipped)
	}
	if len(insights) != 1 || insights[0].ID != 102 {
		t.Errorf("expected only insight #102 gathered, got %+v", insights)
	}
	if len(regulars) != 1 || regulars[0].ID != 103 {
		t.Errorf("expected only regular #103 gathered, got %+v", regulars)
	}
	// Second pass after #102/#103 get distilled → nothing left.
	kf.Patterns = append(kf.Patterns, knowledge.Pattern{GmailSearch: "from:g", SourceInsights: []int64{102, 103}})
	ins2, reg2, skip2 := selectUndistilled(dismissed, kf)
	if len(ins2) != 0 || len(reg2) != 0 || skip2 != 3 {
		t.Errorf("second pass should skip all 3 prior, got insights=%v regulars=%v skipped=%d", ins2, reg2, skip2)
	}
}

// TestSelectUndistilled_LedgerUnion verifies the iter-023 union skip: an insight
// recorded in the local distilled-ledger is skipped even when provenance does NOT
// cover it (the residual gap), alongside provenance-covered ones, while a
// genuinely-fresh insight is still gathered.
func TestSelectUndistilled_LedgerUnion(t *testing.T) {
	kf := &knowledge.KnowledgeFile{
		Patterns:          []knowledge.Pattern{{GmailSearch: "from:x@y.com", SourceInsights: []int64{101}}}, // provenance covers #101
		DistilledInsights: []int64{102},                                                                     // ledger covers the gap #102
	}
	dismissed := []notify.PreviousNotification{
		{ID: 101, Message: "[Insight: archive_candidate] a", Comment: "x"}, // provenance → skip
		{ID: 102, Message: "[Insight: ignore_pattern] b", Comment: "y"},    // ledger → skip (the gap)
		{ID: 103, Message: "[Insight: priority_pattern] c", Comment: "z"},  // neither → gather
	}
	insights, _, skipped := selectUndistilled(dismissed, kf)
	if skipped != 2 {
		t.Fatalf("expected 2 skipped (provenance #101 + ledger #102), got %d", skipped)
	}
	if len(insights) != 1 || insights[0].ID != 103 {
		t.Fatalf("expected only #103 gathered, got %+v", insights)
	}
}

// TestAppendDistilledLedger verifies ledger recording: exactly distillable ∧
// ¬provenance ∧ ¬already-ledgered IDs are appended (and returned). Provenance-
// covered, already-ledgered, and non-distillable rows are left out.
func TestAppendDistilledLedger(t *testing.T) {
	kf := &knowledge.KnowledgeFile{
		Patterns:          []knowledge.Pattern{{GmailSearch: "g", SourceInsights: []int64{3}}}, // provenance covers #3
		DistilledInsights: []int64{4},                                                          // already ledgered
	}
	dismissed := []notify.PreviousNotification{
		{ID: 1, Message: "[Insight: x] todo-only", Comment: "do"},        // gap → add
		{ID: 2, Message: "plain", Comment: "distilled to nothing"},       // gap (commented) → add
		{ID: 3, Message: "[Insight: y] produced a pattern", Comment: ""}, // provenance → skip
		{ID: 4, Message: "[Insight: z] already ledgered", Comment: ""},   // ledger → skip
		{ID: 5, Message: "plain", Comment: ""},                           // not distillable → skip
	}
	added := appendDistilledLedger(kf, dismissed)
	if !reflect.DeepEqual(added, []int64{1, 2}) {
		t.Fatalf("added = %v, want [1 2]", added)
	}
	got := map[int64]bool{}
	for _, id := range kf.DistilledInsights {
		got[id] = true
	}
	for _, id := range []int64{1, 2, 4} {
		if !got[id] {
			t.Errorf("ledger missing #%d: %v", id, kf.DistilledInsights)
		}
	}
	if got[3] || got[5] {
		t.Errorf("ledger should not contain #3 or #5: %v", kf.DistilledInsights)
	}
}

func TestFormatInsightSuffix(t *testing.T) {
	if got := formatInsightSuffix(nil); got != "" {
		t.Errorf("empty ids → %q, want empty", got)
	}
	if got := formatInsightSuffix([]int64{12}); got != "(insight #12)" {
		t.Errorf("single → %q", got)
	}
	if got := formatInsightSuffix([]int64{12, 15}); got != "(insights #12, #15)" {
		t.Errorf("multi → %q", got)
	}
}

func TestParseConflictPlanIDs(t *testing.T) {
	tests := []struct {
		in   string
		want []int64
	}{
		{"merge_conflict:[57 68]", []int64{57, 68}},
		{"merge_conflict:[57]", []int64{57}},
		{"merge_conflict:[]", nil},
		{"from:no-reply@socradar.com", nil}, // a normal propose knowledge_ref → not superseding
		{"", nil},
	}
	for _, tt := range tests {
		got := parseConflictPlanIDs(tt.in)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("parseConflictPlanIDs(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

// TestFoldedProposalRoundTrip proves the end-to-end contract a folded plan relies
// on: a reconcile-gate output carrying a merge_conflict marker survives
// ParseProposals (the propose-apply parse) and, once posted+re-read as a plan,
// yields the superseded ids that cmdApplyRules uses to retire the old per-sender
// plans. If this breaks, folding silently degrades to a withheld sender.
func TestFoldedProposalRoundTrip(t *testing.T) {
	folded := propose.Proposal{
		KnowledgeRef: "merge_conflict:[57 68]",
		Pattern:      "socradar — fold non-critical + non-VIP",
		Status:       "confirmed",
		ProposedRule: config.Rule{
			Name: "socradar notifications",
			Type: "archive_by_sender",
			Params: config.RuleParams{
				Patterns: []string{"no-reply@socradar.com"},
				Filter:   "-Critical -VIP",
			},
		},
		Reason: "folded from existing socradar plans",
	}

	// Reconcile gate emits a JSON array; propose-apply parses it.
	arr, _ := json.Marshal([]propose.Proposal{folded})
	kept, err := propose.ParseProposals(arr)
	if err != nil {
		t.Fatalf("ParseProposals rejected folded proposal: %v", err)
	}
	if len(kept) != 1 {
		t.Fatalf("expected 1 kept proposal, got %d", len(kept))
	}
	if kept[0].KnowledgeRef != "merge_conflict:[57 68]" {
		t.Fatalf("marker not preserved through parse: %q", kept[0].KnowledgeRef)
	}

	// propose-apply posts the proposal; cmdApplyRules later unmarshals the stored
	// detail and extracts the superseded ids. Simulate that round trip.
	detail, _ := json.Marshal(kept[0])
	var reread propose.Proposal
	if err := json.Unmarshal(detail, &reread); err != nil {
		t.Fatalf("re-read failed: %v", err)
	}
	ids := parseConflictPlanIDs(reread.KnowledgeRef)
	if !reflect.DeepEqual(ids, []int64{57, 68}) {
		t.Fatalf("supersede ids lost: got %v, want [57 68]", ids)
	}
}
