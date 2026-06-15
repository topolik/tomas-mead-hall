package embed

import (
	"math"
	"testing"

	"github.com/topolik/mnd-mind-model/internal/distill"
)

func approx(a, b, eps float64) bool { return math.Abs(a-b) < eps }

// T71: CosineSim — known vectors.
func TestCosineSim(t *testing.T) {
	// identical vectors → 1.0
	if s := CosineSim(Vector{1, 0, 0}, Vector{1, 0, 0}); !approx(s, 1.0, 1e-9) {
		t.Fatalf("identical: got %f", s)
	}
	// orthogonal → 0.0
	if s := CosineSim(Vector{1, 0, 0}, Vector{0, 1, 0}); !approx(s, 0.0, 1e-9) {
		t.Fatalf("orthogonal: got %f", s)
	}
	// opposite → -1.0
	if s := CosineSim(Vector{1, 0, 0}, Vector{-1, 0, 0}); !approx(s, -1.0, 1e-9) {
		t.Fatalf("opposite: got %f", s)
	}
	// 45 degrees → cos(45°) ≈ 0.7071
	if s := CosineSim(Vector{1, 0}, Vector{1, 1}); !approx(s, math.Sqrt(2)/2, 1e-4) {
		t.Fatalf("45deg: got %f", s)
	}
	// mismatched length → 0.0
	if s := CosineSim(Vector{1}, Vector{1, 2}); s != 0 {
		t.Fatalf("mismatch: got %f", s)
	}
	// zero vector → 0.0
	if s := CosineSim(Vector{0, 0}, Vector{1, 1}); s != 0 {
		t.Fatalf("zero: got %f", s)
	}
	// empty → 0.0
	if s := CosineSim(Vector{}, Vector{}); s != 0 {
		t.Fatalf("empty: got %f", s)
	}
}

func testInsight(id, cat string) distill.Insight {
	return distill.Insight{ID: id, Category: cat, Statement: "s" + id, Strength: "strong"}
}

// T72: Store.TopK — returns k nearest, respects active filter.
func TestTopK(t *testing.T) {
	s := &Store{
		Model: "test",
		Dim:   3,
		Entries: []Entry{
			{ID: "a", Vector: Vector{1, 0, 0}},        // closest to query
			{ID: "b", Vector: Vector{0.7, 0.7, 0}},    // second
			{ID: "c", Vector: Vector{0, 0, 1}},         // orthogonal
			{ID: "superseded", Vector: Vector{1, 0, 0}}, // same as a but not active
		},
	}
	active := []distill.Insight{
		testInsight("a", "correction_pattern"),
		testInsight("b", "direction_pattern"),
		testInsight("c", "decision_heuristic"),
		// "superseded" is NOT in active list
	}
	query := Vector{1, 0, 0}

	matches := s.TopK(query, 2, active)
	if len(matches) != 2 {
		t.Fatalf("expected 2, got %d", len(matches))
	}
	if matches[0].Insight.ID != "a" {
		t.Fatalf("first should be a, got %s", matches[0].Insight.ID)
	}
	if !approx(matches[0].Score, 1.0, 1e-9) {
		t.Fatalf("a score should be 1.0, got %f", matches[0].Score)
	}
	if matches[1].Insight.ID != "b" {
		t.Fatalf("second should be b, got %s", matches[1].Insight.ID)
	}
	// superseded must not appear even at k=10
	all := s.TopK(query, 10, active)
	for _, m := range all {
		if m.Insight.ID == "superseded" {
			t.Fatal("superseded insight should not appear")
		}
	}
}

// T73: ParseGeminiResponse.
func TestParseGeminiResponse(t *testing.T) {
	data := []byte(`{"embeddings":[{"embedding":{"values":[0.1,0.2,0.3]}},{"embedding":{"values":[0.4,0.5,0.6]}}]}`)
	vecs, err := ParseGeminiResponse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(vecs))
	}
	if len(vecs[0]) != 3 || vecs[0][0] != 0.1 {
		t.Fatalf("vec[0] wrong: %v", vecs[0])
	}
	if len(vecs[1]) != 3 || vecs[1][2] != 0.6 {
		t.Fatalf("vec[1] wrong: %v", vecs[1])
	}
	// empty response
	if _, err := ParseGeminiResponse([]byte(`{"embeddings":[]}`)); err == nil {
		t.Fatal("expected error on empty")
	}
}

// T74: ParseOllamaResponse.
func TestParseOllamaResponse(t *testing.T) {
	data := []byte(`{"model":"nomic-embed-text","embeddings":[[0.1,0.2],[0.3,0.4]]}`)
	vecs, err := ParseOllamaResponse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(vecs))
	}
	if vecs[0][1] != 0.2 || vecs[1][0] != 0.3 {
		t.Fatalf("wrong values: %v %v", vecs[0], vecs[1])
	}
	if _, err := ParseOllamaResponse([]byte(`{"embeddings":[]}`)); err == nil {
		t.Fatal("expected error on empty")
	}
}

// T75: Evidence metadata computation.
func TestComputeEvidence(t *testing.T) {
	matches := []Match{
		{Insight: testInsight("a", "correction_pattern"), Score: 0.9},
		{Insight: testInsight("b", "correction_pattern"), Score: 0.8},
		{Insight: testInsight("c", "direction_pattern"), Score: 0.7},
		{Insight: testInsight("d", "correction_pattern"), Score: 0.6},
	}
	ev := ComputeEvidence(matches)

	if !approx(ev.MeanSimilarity, 0.75, 1e-9) {
		t.Fatalf("mean: got %f", ev.MeanSimilarity)
	}
	if !approx(ev.MinSimilarity, 0.6, 1e-9) {
		t.Fatalf("min: got %f", ev.MinSimilarity)
	}
	if ev.DominantCategory != "correction_pattern" {
		t.Fatalf("dominant: got %s", ev.DominantCategory)
	}
	if !approx(ev.DominantFraction, 0.75, 1e-9) {
		t.Fatalf("fraction: got %f", ev.DominantFraction)
	}
	if ev.CategoryDistribution["correction_pattern"] != 3 {
		t.Fatalf("corr count: got %d", ev.CategoryDistribution["correction_pattern"])
	}
	if ev.CategoryDistribution["direction_pattern"] != 1 {
		t.Fatalf("dir count: got %d", ev.CategoryDistribution["direction_pattern"])
	}
	if len(ev.TopKScores) != 4 {
		t.Fatalf("scores count: got %d", len(ev.TopKScores))
	}

	// empty
	empty := ComputeEvidence(nil)
	if len(empty.TopKScores) != 0 {
		t.Fatal("empty should have no scores")
	}
}

// T76: GateDecision logic.
func TestGateDecision(t *testing.T) {
	autoCats := map[string]bool{"correction_pattern": true, "direction_pattern": true}

	// safe: high similarity, dominant safe category
	safe := Evidence{
		TopKScores: []float64{0.9, 0.85, 0.8}, MeanSimilarity: 0.85,
		MinSimilarity: 0.7, DominantCategory: "correction_pattern", DominantFraction: 0.8,
	}
	if d := GateDecision(safe, autoCats, 0.6, 0.4, 0.5); d != "auto" {
		t.Fatalf("safe case: got %s", d)
	}

	// escalate: judgment dominant
	judgment := Evidence{
		TopKScores: []float64{0.9, 0.85}, MeanSimilarity: 0.85,
		MinSimilarity: 0.7, DominantCategory: "decision_heuristic", DominantFraction: 0.8,
	}
	if d := GateDecision(judgment, autoCats, 0.6, 0.4, 0.5); d != "escalate" {
		t.Fatalf("judgment: got %s", d)
	}

	// escalate: sparse (low mean similarity)
	sparse := Evidence{
		TopKScores: []float64{0.5, 0.4}, MeanSimilarity: 0.45,
		MinSimilarity: 0.3, DominantCategory: "correction_pattern", DominantFraction: 0.8,
	}
	if d := GateDecision(sparse, autoCats, 0.6, 0.4, 0.5); d != "escalate" {
		t.Fatalf("sparse: got %s", d)
	}

	// escalate: mixed categories (no majority)
	mixed := Evidence{
		TopKScores: []float64{0.8, 0.8, 0.8}, MeanSimilarity: 0.8,
		MinSimilarity: 0.7, DominantCategory: "correction_pattern", DominantFraction: 0.4,
	}
	if d := GateDecision(mixed, autoCats, 0.6, 0.4, 0.5); d != "escalate" {
		t.Fatalf("mixed: got %s", d)
	}

	// escalate: low min similarity
	lowMin := Evidence{
		TopKScores: []float64{0.9, 0.1}, MeanSimilarity: 0.7,
		MinSimilarity: 0.1, DominantCategory: "correction_pattern", DominantFraction: 0.8,
	}
	if d := GateDecision(lowMin, autoCats, 0.6, 0.4, 0.5); d != "escalate" {
		t.Fatalf("lowMin: got %s", d)
	}

	// escalate: empty
	if d := GateDecision(Evidence{}, autoCats, 0.6, 0.4, 0.5); d != "escalate" {
		t.Fatalf("empty: got %s", d)
	}
}

// T77: Store delta — Prune removes stale, Set adds new.
func TestStoreDelta(t *testing.T) {
	s := &Store{
		Entries: []Entry{
			{ID: "keep", Vector: Vector{1, 0}},
			{ID: "stale", Vector: Vector{0, 1}},
		},
	}
	pruned := s.Prune(map[string]bool{"keep": true, "new": true})
	if pruned != 1 {
		t.Fatalf("expected 1 pruned, got %d", pruned)
	}
	if len(s.Entries) != 1 || s.Entries[0].ID != "keep" {
		t.Fatalf("after prune: %v", s.Entries)
	}
	s.Set("new", Vector{0.5, 0.5})
	if len(s.Entries) != 2 {
		t.Fatalf("after set: %d entries", len(s.Entries))
	}
	// overwrite existing
	s.Set("keep", Vector{0, 0})
	if s.Entries[0].Vector[0] != 0 {
		t.Fatal("overwrite failed")
	}
}
