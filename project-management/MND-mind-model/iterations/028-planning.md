# 028 — Planning: embedding-based retrieval + evidence-derived routing

- **Start:** 2026-06-15
- **Phase:** Planning

## Findings from research

1. **Anthropic has no embedding API.** Claude is out for embeddings entirely.
2. **Gemini `text-embedding-004`** — 768-dim, `batchEmbedContents` (up to 100 texts/call), free tier generous. Direct REST call required — `gemini-cli` is chat-only.
3. **LLP doesn't support embeddings** — chat completions only. Adding it: new route + types + HttpProvider method. Not blocking — MND can call Gemini REST directly (same pattern as gemini-cli but to a different endpoint).
4. **Local GPU (GTX 1080, 8GB)** — Ollama with `nomic-embed-text` (768-dim, 300MB) as tier 2 fallback.

## Architecture change

```
BEFORE:
  question → [classify: LLM call] → gate → [ask-prompt: BM25 top-k] → [LLM call] → answer
  (2 LLM calls, BM25 saturates, classifier doesn't generalize)

AFTER:
  question → [embed question: API/Ollama] → [cosine top-k from pre-embedded insights]
           → [ask-prompt: embedding top-k + evidence metadata] → [LLM call] → answer
           → [evidence gate: inspect metadata] → deliver / escalate
  (1 LLM call + 1 embed call, classifier eliminated)
```

## Implementation plan

### Step 1: Embedding infrastructure (`internal/embed`)

New Go package: `internal/embed`

```go
// Vector is a float64 slice — the embedding of one insight or query.
type Vector []float64

// Store holds pre-computed insight embeddings, keyed by insight ID.
type Store struct {
    Vectors map[string]Vector  // insight ID → embedding
    Model   string             // model used (for invalidation)
    Dim     int                // vector dimensionality
}

func CosineSim(a, b Vector) float64
func (s *Store) TopK(query Vector, k int, active map[string]bool) []Match
```

- `Store` persisted as `data/embeddings.json` (gzipped if >1MB)
- `Match` carries insight ID + cosine score
- `TopK` filters by active insights (MND-025 exclusion)

### Step 2: Embedding providers (host-side, in `run-task.sh`)

Three-tier, shell-level:

**Tier 1 — Gemini REST** (`embed-gemini`):
```bash
curl "https://generativelanguage.googleapis.com/v1beta/models/text-embedding-004:batchEmbedContents" \
  -H "x-goog-api-key: $GEMINI_API_KEY" \
  -d '{"requests": [{"content": {"parts": [{"text": "..."}]}, "model": "models/text-embedding-004"}, ...]}'
```
- Batch up to 100 per call, ~800 insights = 8 calls
- API key from existing `GOOGLE_API_KEY` or `GEMINI_API_KEY` env var
- Output: JSON → Go parses into `Store`

**Tier 2 — Ollama** (`embed-ollama`):
```bash
curl http://localhost:11434/api/embed -d '{"model": "nomic-embed-text", "input": ["text1", "text2", ...]}'
```
- Ollama Docker service with GPU passthrough
- `nomic-embed-text` (768-dim, 300MB)
- Batch all 800 in one call (Ollama handles batching internally)

**Tier 3 — Ollama CPU** (same endpoint, no GPU passthrough):
- Fallback if GPU unavailable; ~200ms/embed, ~3min for full batch

New `run-task.sh` commands:
- `embed-batch` — embed all active insights → `data/embeddings.json` (delta: skip insights already embedded with same model)
- `embed-query` — embed a single question → stdout JSON

Selection: `MND_EMBED=auto|ollama|gemini` (auto: try ollama → gemini → fail). Tomas: start with Ollama, no API dependency.

### Step 3: Replace BM25 in ask pipeline

New Go command `ask-prompt-embed`:
- Takes `--embeddings data/embeddings.json --query-embedding <file>` instead of building BM25
- Loads `Store`, does `TopK(queryVec, k, activeSet)` → `[]distill.Insight`
- Same `ask.BuildPrompt()` call — profiles whole + embedding-retrieved evidence
- Also emits **evidence metadata** to a sidecar file (`data/ask.evidence.json`):

```json
{
  "top_k_scores": [0.89, 0.85, 0.82, ...],
  "category_distribution": {"correction_pattern": 5, "direction_pattern": 3, "decision_heuristic": 1},
  "mean_similarity": 0.78,
  "min_similarity": 0.65,
  "agreement": "high"
}
```

### Step 4: Evidence-derived routing gate

Replace the classify → gate flow in `orchestrate.sh`:

1. `ask` always runs (embed query + retrieve + LLM answer)
2. Read `data/ask.evidence.json`
3. Gate logic (thresholds tuned from eval, starting points):
   - **Auto-answer** if: dominant category ∈ `MND_ROUTE_AUTO` AND mean_similarity ≥ 0.75 AND min_similarity ≥ 0.5
   - **Escalate** if: dominant category is `decision_heuristic` OR mean_similarity < 0.6 OR category distribution is mixed (no >50% majority)
4. Escalated notifications include the proposed answer (Tomas can see what the brain would have said)

The `classify` LLM call is eliminated. The embed call replaces it (cheaper, faster, more reliable).

### Step 5: Measurement

- **A/B eval**: run `eval-rerun` with embedding retrieval vs BM25 retrieval on the same 19 cases. Compare fidelity — embedding retrieval should improve or match because better evidence selection.
- **Routing accuracy**: on the scored eval cases, simulate evidence-derived routing vs category-based routing. Does the evidence gate produce fewer judgment leaks?
- **Retrieval quality**: for each eval case, compare BM25 top-12 vs embedding top-12 — are the embedding-retrieved insights more relevant (judged by gold category match)?
- `fidelity-check` still runs with the same 75% threshold and judgment-leak detection.

### Step 6: Retrain integration

`retrain` gains:
- After `profile`: `embed-batch` to re-embed any new/changed insights (delta)
- `eval` + `route-eval` still mandatory, now using embedding retrieval

### Step 7: Ollama setup (if tier 2 needed)

- `docker-compose.yml`: add `ollama` service with GPU passthrough
- `setup.sh`: pull `nomic-embed-text` model
- Only deployed if tier 1 (Gemini) proves inadequate or unavailable

## Test requirements (TDD)

| ID | Test | Pass condition |
|----|------|----------------|
| T71 | `CosineSim` unit test — known vectors | Correct cosine distance within ε |
| T72 | `Store.TopK` — returns k nearest, respects active filter | Gold ordering matches hand-computed |
| T73 | `embed.ParseGeminiResponse` — batch JSON → vectors | Correct dimensions, correct count |
| T74 | `embed.ParseOllamaResponse` — batch JSON → vectors | Correct dimensions, correct count |
| T75 | Evidence metadata calculation — category distribution, mean/min similarity | Matches hand-computed from test vectors |
| T76 | Evidence gate logic — auto/escalate decision from metadata | Known inputs → expected gate decisions |
| T77 | `Store` delta embedding — only new/changed insights re-embedded | Count of embed calls = count of new insights |
| T78 | Full ask pipeline with embeddings — prompt contains correct evidence | Retrieved insights match expected nearest |

## Checklist

- [ ] `internal/embed` package: `Vector`, `Store`, `CosineSim`, `TopK`, `ParseGeminiResponse`, `ParseOllamaResponse` (T71–T74)
- [ ] `data/embeddings.json` format and persistence
- [ ] `run-task.sh embed-batch` (Gemini REST, delta-only)
- [ ] `run-task.sh embed-query` (single question → vector)
- [ ] `MND_EMBED=auto|gemini|ollama` selection logic
- [ ] `ask-prompt-embed` Go command with evidence metadata output (T75, T78)
- [ ] Evidence gate in `orchestrate.sh` (T76)
- [ ] Remove classify LLM call from orchestrate flow
- [ ] A/B measurement: embedding vs BM25 on eval cases (T77)
- [ ] Routing measurement: evidence-derived vs category-based
- [ ] `retrain` integration: `embed-batch` after profile regen
- [ ] `fidelity-check` still passes with new pipeline
- [ ] Ollama docker-compose entry (tier 2, only if needed)
- [ ] Update README, architecture diagram, ASSUMPTIONS

## Safety checklist (MO §7)

- **Secrets**: Gemini API key via env var (not in code). Ollama is localhost-only.
- **No new dependencies**: Go `math` for cosine, `net/http` for REST calls (already imported). Ollama is a Docker service, not a host package.
- **Containers**: Ollama runs in Docker. Gemini REST runs host-side like all LLM calls (MND-003).
- **Reversibility**: BM25 code stays — `MND_EMBED=off` falls back. No destructive removal until embedding retrieval is proven better.

## Order of implementation

1. `internal/embed` + tests (T71–T74) — pure Go, no external calls
2. Ollama Docker setup + `embed-batch` + `embed-query` in run-task.sh (Ollama first, Gemini fallback)
3. `ask-prompt-embed` + evidence metadata (T75, T78)
4. A/B measurement — does embedding retrieval improve fidelity?
5. Evidence gate in orchestrate.sh (T76)
6. Routing measurement — does evidence-derived gate reduce judgment leaks?
7. Retrain integration
8. Docs update
9. Gemini REST tier (fallback if Ollama unavailable)

## Decisions

### Gemini-only for tier 1 (Claude has no embedding API)
**Date:** 2026-06-15 21:18
**Phase:** Planning
**Decided by:** Research finding
**Decision:** Tier 1 embeddings are Gemini `text-embedding-004` via direct REST. Claude/Anthropic has no embedding endpoint.
**Alternatives considered:** LLP routing (would need new LLP feature), Claude embeddings (don't exist).
**Reasoning:** Gemini REST is the simplest path — one `curl`-equivalent per batch, no LLP extension needed. LLP embedding support can come later as a convenience, not a blocker.
**Revisit if:** Anthropic ships an embedding API, or LLP gains embedding support (then route through LLP for unified quota management).
