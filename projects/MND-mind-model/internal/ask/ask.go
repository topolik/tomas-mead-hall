// Package ask is the orchestrator side of MND: given a question an agent
// would otherwise ask Tomas, retrieve relevant insights (BM25, in-memory —
// MND-005), assemble the prompt (profiles whole — MND-006), and validate the
// LLM's answer.
package ask

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/topolik/mnd-mind-model/internal/distill"
)

// --- BM25 over insights -----------------------------------------------------

const (
	k1 = 1.2
	b  = 0.75
)

type doc struct {
	insight distill.Insight
	tokens  map[string]int
	length  int
}

// Index is an in-memory BM25 index over insight statements + context +
// evidence quotes. Corpus is hundreds of short docs — built per invocation.
type Index struct {
	docs   []doc
	df     map[string]int
	avgLen float64
}

func tokenize(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !('a' <= r && r <= 'z' || '0' <= r && r <= '9')
	})
}

// NewIndex builds the index.
func NewIndex(insights []distill.Insight) *Index {
	idx := &Index{df: map[string]int{}}
	total := 0
	for _, in := range insights {
		text := in.Statement + " " + in.Context + " " + in.Category
		for _, e := range in.Evidence {
			text += " " + e.Quote
		}
		toks := tokenize(text)
		counts := map[string]int{}
		for _, t := range toks {
			counts[t]++
		}
		for t := range counts {
			idx.df[t]++
		}
		idx.docs = append(idx.docs, doc{insight: in, tokens: counts, length: len(toks)})
		total += len(toks)
	}
	if len(idx.docs) > 0 {
		idx.avgLen = float64(total) / float64(len(idx.docs))
	}
	return idx
}

// Top returns the k best-matching insights for the query (T16).
func (idx *Index) Top(query string, k int) []distill.Insight {
	type scored struct {
		s float64
		i int
	}
	n := float64(len(idx.docs))
	var hits []scored
	for i, d := range idx.docs {
		score := 0.0
		for _, qt := range tokenize(query) {
			tf := float64(d.tokens[qt])
			if tf == 0 {
				continue
			}
			idf := math.Log(1 + (n-float64(idx.df[qt])+0.5)/(float64(idx.df[qt])+0.5))
			score += idf * (tf * (k1 + 1)) / (tf + k1*(1-b+b*float64(d.length)/idx.avgLen))
		}
		if score > 0 {
			hits = append(hits, scored{score, i})
		}
	}
	sort.Slice(hits, func(a, c int) bool { return hits[a].s > hits[c].s })
	if len(hits) > k {
		hits = hits[:k]
	}
	out := make([]distill.Insight, len(hits))
	for i, h := range hits {
		out[i] = idx.docs[h.i].insight
	}
	return out
}

// Signals returns retrieval evidence about how well-supported a query is — an
// honest confidence basis (the answering LLM's self-report is uniformly high).
// topScore: best BM25 match; nStrong: how many of the top-k retrieved insights
// are strength=strong. Lever A (iter 9): does this separate right from wrong?
func (idx *Index) Signals(query string, k int) (topScore float64, nStrong int) {
	type scored struct {
		s float64
		i int
	}
	n := float64(len(idx.docs))
	var hits []scored
	for i, d := range idx.docs {
		score := 0.0
		for _, qt := range tokenize(query) {
			tf := float64(d.tokens[qt])
			if tf == 0 {
				continue
			}
			idf := math.Log(1 + (n-float64(idx.df[qt])+0.5)/(float64(idx.df[qt])+0.5))
			score += idf * (tf * (k1 + 1)) / (tf + k1*(1-b+b*float64(d.length)/idx.avgLen))
		}
		if score > 0 {
			hits = append(hits, scored{score, i})
		}
	}
	sort.Slice(hits, func(a, c int) bool { return hits[a].s > hits[c].s })
	if len(hits) > k {
		hits = hits[:k]
	}
	for i, h := range hits {
		if i == 0 {
			topScore = h.s
		}
		if idx.docs[h.i].insight.Strength == "strong" {
			nStrong++
		}
	}
	return topScore, nStrong
}

// --- prompt + answer ---------------------------------------------------------

const askSystemPrompt = `You are Tomas's stand-in orchestrator. An agent is asking for direction. Answer EXACTLY as Tomas would, based on his mind-model profiles and the evidence below — his priorities, his heuristics, his tech defaults, his way of scoping and correcting.

CRITICAL RULES:
1. The profiles and evidence are your ONLY source for how Tomas decides. Do not substitute generic best practices where Tomas has an explicit position. Where the mind model is silent, say so and give your best Tomas-consistent guess.
1a. Insights are context-scoped. If two seem to conflict (e.g. "run services in containers" vs "build locally, not in Docker"), they are almost always scoped to different situations — apply the one whose context ("when ...") matches the agent's actual situation; do not average them or treat them as a contradiction.
2. Output ONLY valid JSON: {"answer": "...", "confidence": "high|medium|low", "citations": ["insight-id", ...], "pending": "question|none"}
3. answer: the direction Tomas would give — concrete, decisive, scoped. Directives, not essays. If Tomas would push back on the question itself (wrong scope, gold-plating, missing safety step), do that.
4. confidence: high = directly supported by strong insights; medium = extrapolated from related insights; low = mind model is silent here.
5. citations: ids of the insights (from profiles or evidence section) the answer rests on. Empty only when confidence is low.
6. pending: "none" ONLY when the question is a terminal tail and it shows no pending question, decision, or request for direction (the agent finished cleanly or is mid-work) — then keep answer to a one-line reason. Otherwise "question".`

// BuildPrompt assembles the ask prompt: profiles whole + retrieved evidence +
// question (T17).
func BuildPrompt(profiles string, evidence []distill.Insight, question string) string {
	var sb strings.Builder
	sb.WriteString(askSystemPrompt)
	sb.WriteString("\n\n<profiles>\n")
	sb.WriteString(profiles)
	sb.WriteString("</profiles>\n\n<evidence>\n")
	for _, in := range evidence {
		fmt.Fprintf(&sb, "- id=%s [%s/%s, seen %dx] %s\n", in.ID, in.Category, in.Strength, in.Occurrences, in.Statement)
		for i, e := range in.Evidence {
			if i >= 2 {
				break
			}
			if e.Quote != "" {
				fmt.Fprintf(&sb, "  Tomas said: %q\n", e.Quote)
			}
		}
	}
	sb.WriteString("</evidence>\n\n<question>\n")
	sb.WriteString(question)
	sb.WriteString("\n</question>\n")
	return sb.String()
}

// Answer is the validated orchestrator response. Pending is "none" when the
// input was a terminal tail with no question waiting (T35/MND-023) — the
// watch loop must not send fabricated direction to healthy agents.
type Answer struct {
	Answer     string   `json:"answer"`
	Confidence string   `json:"confidence"`
	Citations  []string `json:"citations"`
	Pending    string   `json:"pending"`
}

// ParseAnswer validates the LLM response (prose-tolerant, like distill).
func ParseAnswer(resp string) (Answer, error) {
	var a Answer
	start, end := strings.Index(resp, "{"), strings.LastIndex(resp, "}")
	if start < 0 || end <= start {
		return a, fmt.Errorf("no JSON object in answer")
	}
	if err := json.Unmarshal([]byte(resp[start:end+1]), &a); err != nil {
		return a, fmt.Errorf("answer JSON invalid: %w", err)
	}
	if strings.TrimSpace(a.Answer) == "" {
		return a, fmt.Errorf("empty answer")
	}
	switch a.Confidence {
	case "high", "medium", "low":
	default:
		a.Confidence = "low"
	}
	if a.Pending != "none" {
		a.Pending = "question"
	}
	return a, nil
}
