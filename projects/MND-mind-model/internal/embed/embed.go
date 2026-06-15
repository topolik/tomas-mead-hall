// Package embed provides embedding-based semantic retrieval over MND insights.
// Vectors are pre-computed (batch) and stored in a flat JSON file; queries are
// embedded at ask-time. Cosine similarity replaces BM25 (MND-005 successor).
package embed

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"

	"github.com/topolik/mnd-mind-model/internal/distill"
)

type Vector []float64

type Entry struct {
	ID     string `json:"id"`
	Vector Vector `json:"vector"`
}

type Store struct {
	Model   string  `json:"model"`
	Dim     int     `json:"dim"`
	Entries []Entry `json:"entries"`
}

func (s *Store) Lookup(id string) (Vector, bool) {
	for _, e := range s.Entries {
		if e.ID == id {
			return e.Vector, true
		}
	}
	return nil, false
}

func (s *Store) IDs() map[string]bool {
	m := make(map[string]bool, len(s.Entries))
	for _, e := range s.Entries {
		m[e.ID] = true
	}
	return m
}

func (s *Store) Set(id string, v Vector) {
	for i, e := range s.Entries {
		if e.ID == id {
			s.Entries[i].Vector = v
			return
		}
	}
	s.Entries = append(s.Entries, Entry{ID: id, Vector: v})
}

// Prune removes entries not in the keep set.
func (s *Store) Prune(keep map[string]bool) int {
	n := 0
	j := 0
	for _, e := range s.Entries {
		if keep[e.ID] {
			s.Entries[j] = e
			j++
		} else {
			n++
		}
	}
	s.Entries = s.Entries[:j]
	return n
}

type Match struct {
	Insight distill.Insight
	Score   float64
}

func CosineSim(a, b Vector) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}

// TopK returns the k insights most similar to the query vector, filtered to
// active-only insights. Brute-force cosine over the store — fine for <10k vectors.
func (s *Store) TopK(query Vector, k int, insights []distill.Insight) []Match {
	active := make(map[string]distill.Insight, len(insights))
	for _, in := range insights {
		active[in.ID] = in
	}
	type scored struct {
		id    string
		score float64
	}
	var hits []scored
	for _, e := range s.Entries {
		if _, ok := active[e.ID]; !ok {
			continue
		}
		sim := CosineSim(query, e.Vector)
		hits = append(hits, scored{e.ID, sim})
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	if len(hits) > k {
		hits = hits[:k]
	}
	matches := make([]Match, len(hits))
	for i, h := range hits {
		matches[i] = Match{Insight: active[h.id], Score: h.score}
	}
	return matches
}

// Evidence holds the retrieval metadata the orchestrator's gate inspects.
type Evidence struct {
	TopKScores           []float64      `json:"top_k_scores"`
	CategoryDistribution map[string]int `json:"category_distribution"`
	MeanSimilarity       float64        `json:"mean_similarity"`
	MinSimilarity        float64        `json:"min_similarity"`
	DominantCategory     string         `json:"dominant_category"`
	DominantFraction     float64        `json:"dominant_fraction"`
}

func ComputeEvidence(matches []Match) Evidence {
	ev := Evidence{
		CategoryDistribution: map[string]int{},
	}
	if len(matches) == 0 {
		return ev
	}
	sum := 0.0
	ev.MinSimilarity = 1.0
	for _, m := range matches {
		ev.TopKScores = append(ev.TopKScores, m.Score)
		ev.CategoryDistribution[m.Insight.Category]++
		sum += m.Score
		if m.Score < ev.MinSimilarity {
			ev.MinSimilarity = m.Score
		}
	}
	ev.MeanSimilarity = sum / float64(len(matches))
	best := ""
	bestN := 0
	for cat, n := range ev.CategoryDistribution {
		if n > bestN {
			best = cat
			bestN = n
		}
	}
	ev.DominantCategory = best
	ev.DominantFraction = float64(bestN) / float64(len(matches))
	return ev
}

// GateDecision returns "auto" or "escalate" based on evidence metadata.
func GateDecision(ev Evidence, autoCats map[string]bool, minMeanSim, minMinSim, minDominance float64) string {
	if len(ev.TopKScores) == 0 {
		return "escalate"
	}
	if ev.MeanSimilarity < minMeanSim {
		return "escalate"
	}
	if ev.MinSimilarity < minMinSim {
		return "escalate"
	}
	if ev.DominantFraction < minDominance {
		return "escalate"
	}
	if !autoCats[ev.DominantCategory] {
		return "escalate"
	}
	return "auto"
}

// --- Ollama response parsing ---

type OllamaResponse struct {
	Embeddings [][]float64 `json:"embeddings"`
}

func ParseOllamaResponse(data []byte) ([]Vector, error) {
	var resp OllamaResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("ollama response: %w", err)
	}
	if len(resp.Embeddings) == 0 {
		return nil, fmt.Errorf("ollama: no embeddings in response")
	}
	vecs := make([]Vector, len(resp.Embeddings))
	for i, e := range resp.Embeddings {
		vecs[i] = Vector(e)
	}
	return vecs, nil
}

// --- Gemini response parsing ---

type GeminiEmbedding struct {
	Values []float64 `json:"values"`
}

type GeminiContentEmbedding struct {
	Embedding GeminiEmbedding `json:"embedding"`
}

type GeminiBatchResponse struct {
	Embeddings []GeminiContentEmbedding `json:"embeddings"`
}

func ParseGeminiResponse(data []byte) ([]Vector, error) {
	var resp GeminiBatchResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("gemini response: %w", err)
	}
	if len(resp.Embeddings) == 0 {
		return nil, fmt.Errorf("gemini: no embeddings in response")
	}
	vecs := make([]Vector, len(resp.Embeddings))
	for i, e := range resp.Embeddings {
		vecs[i] = Vector(e.Embedding.Values)
	}
	return vecs, nil
}

// --- Store persistence ---

func LoadStore(path string) (*Store, error) {
	f, err := os.Open(path)
	if err != nil {
		return &Store{Entries: []Entry{}}, nil
	}
	defer f.Close()
	var s Store
	if strings.HasSuffix(path, ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return nil, fmt.Errorf("gzip open %s: %w", path, err)
		}
		defer gz.Close()
		if err := json.NewDecoder(gz).Decode(&s); err != nil {
			return nil, fmt.Errorf("decode %s: %w", path, err)
		}
	} else {
		if err := json.NewDecoder(f).Decode(&s); err != nil {
			return nil, fmt.Errorf("decode %s: %w", path, err)
		}
	}
	if s.Entries == nil {
		s.Entries = []Entry{}
	}
	return &s, nil
}

func SaveStore(path string, s *Store) error {
	if strings.HasSuffix(path, ".gz") {
		f, err := os.Create(path)
		if err != nil {
			return err
		}
		defer f.Close()
		gz := gzip.NewWriter(f)
		if err := json.NewEncoder(gz).Encode(s); err != nil {
			gz.Close()
			return err
		}
		return gz.Close()
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// InsightText returns the text to embed for an insight — statement + context.
func InsightText(in distill.Insight) string {
	t := in.Statement
	if in.Context != "" {
		t += " " + in.Context
	}
	return t
}
