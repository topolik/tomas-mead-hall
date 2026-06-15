package usage

import (
	"path/filepath"
	"testing"
)

// T7: rows insert, aggregate by agent/impl/day, sums + error count + cost.
func TestRecordAndAggregate(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "u.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	rows := []Record{
		{Agent: "gml", RequestedModel: "auto", ImplUsed: "gemini", PromptTokens: 100, CompletionTokens: 50, CostUSD: 0, Status: "ok"},
		{Agent: "gml", RequestedModel: "auto", ImplUsed: "gemini", PromptTokens: 200, CompletionTokens: 80, CostUSD: 0, Status: "ok"},
		{Agent: "gml", RequestedModel: "auto", ImplUsed: "gemini", PromptTokens: 0, CompletionTokens: 0, CostUSD: 0, Status: "error", Error: "boom"},
		{Agent: "dsh", RequestedModel: "auto", ImplUsed: "claude", PromptTokens: 10, CompletionTokens: 5, CostUSD: 0.000045, Status: "ok"},
	}
	for _, r := range rows {
		if err := s.Record(r); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	agg, err := s.Aggregate()
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	// Expect two groups: gml/gemini and dsh/claude.
	var gml, dsh *AggRow
	for i := range agg {
		switch agg[i].Impl {
		case "gemini":
			gml = &agg[i]
		case "claude":
			dsh = &agg[i]
		}
	}
	if gml == nil || dsh == nil {
		t.Fatalf("missing groups: %+v", agg)
	}
	if gml.Requests != 3 || gml.PromptTokens != 300 || gml.CompletionTokens != 130 || gml.Errors != 1 {
		t.Fatalf("gml/gemini agg wrong: %+v", *gml)
	}
	if dsh.Requests != 1 || dsh.CostUSD != 0.000045 {
		t.Fatalf("dsh/claude agg wrong: %+v", *dsh)
	}
}

// T7: Recent returns rows newest-first, clamped to the limit.
func TestRecent(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "u.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	for i := 0; i < 5; i++ {
		if err := s.Record(Record{Agent: "gml", RequestedModel: "auto", ImplUsed: "gemini", Status: "ok", LatencyMS: int64(i)}); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.Recent(3)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
	// newest first: the last inserted (latency 4) should be first
	if rows[0].LatencyMS != 4 {
		t.Fatalf("expected newest first (latency 4), got %d", rows[0].LatencyMS)
	}
}

// T7: cost is computed from per-1M-token prices; free CLI impls cost 0.
func TestCost(t *testing.T) {
	if got := Cost(1000, 500, 0, 0); got != 0 {
		t.Fatalf("free impl should cost 0, got %v", got)
	}
	// 1,000,000 in @ $3/1M + 1,000,000 out @ $15/1M = $18
	if got := Cost(1_000_000, 1_000_000, 3, 15); got != 18 {
		t.Fatalf("expected 18, got %v", got)
	}
}
