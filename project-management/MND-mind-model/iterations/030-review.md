# 030 — Review: iteration 11 — embedding retrieval + evidence gate

- **Start:** 2026-06-16
- **Phase:** Review (pending Tomas)

## What changed

**Before (iter 10):** question → LLM classify → category gate → BM25 top-k → LLM answer → deliver/escalate. Two LLM calls. Classifier went from 49% to 11% accuracy on fresh questions. BM25 saturated on ~800 homogeneous insights.

**After (iter 11):** question → embed (Ollama GPU, local) → cosine top-k → evidence gate (inspect retrieval metadata) → LLM answer → deliver/escalate. One LLM call + one local embed call. Classifier eliminated. Routing based on what the brain actually found, not a broken standalone classifier.

## Metrics

| Metric | Iter 10 | Iter 11 | Change |
|--------|---------|---------|--------|
| Auto-set | correction, direction | + tech_preference | +1 category |
| Fidelity | 87% | 91% | +4pp |
| Coverage | 38% | 57% | +19pp |
| Judgment leaks | 0 | 0 | — |
| LLM calls per question | 2 (classify + ask) | 1 (ask only) | -50% |
| Routing reliability | LLM classifier (11% on fresh) | Evidence gate (deterministic) | fixed |

## How to run it

### Prerequisites
```bash
# Start Ollama (one-time, stays running)
cd projects/MND-mind-model
./run-task.sh embed-start

# Embed all insights (runs delta — skip already-embedded)
./run-task.sh embed-batch
```

### Ask a question (embedding retrieval)
```bash
./run-task.sh ask "Should we use Redis or PostgreSQL for session storage?"
```
This auto-detects Ollama + embeddings and uses semantic retrieval. Falls back to BM25 if Ollama is down.

### Orchestration (evidence gate)
```bash
# orchestrate.sh already uses evidence gate when embeddings available
# Test with:
echo "Should we use Redis or PostgreSQL for session storage?" | ./orchestrate.sh
```

### Run eval
```bash
./run-task.sh eval           # uses embedding retrieval when available
./run-task.sh route-eval     # evidence gate routing simulation
./run-task.sh fidelity-check # 75% threshold + judgment leak check
```

## What to look at

1. **Routing decisions**: `orchestrate.sh` now prints evidence metadata (dominant category, similarity scores, gate decision) before routing. Watch for correct auto vs escalate calls.
2. **Evidence selection**: compare the cited insights in answers — embedding retrieval pulls semantically relevant insights, not just keyword matches.
3. **Retrain pipeline**: `./run-task.sh retrain` now includes embed-batch + mandatory fidelity check with DSH alert.

## Open items

1. **Full A/B eval deferred**: only 3 cases completed (LLP quota exhaustion). All 3 matched — same correct answers with different evidence. 26-case run recommended once LLP recovers.
2. **Gemini REST fallback**: parser exists in Go (`ParseGeminiResponse`), shell integration deferred. Ollama-only for now per Tomas's decision.

## Commits (5, on mnd-mind-model branch)

1. `[MND] ideation: add local GPU embedding fallback tier (Ollama + GTX 1080)`
2. `[MND] iteration 11 planning: embedding retrieval + evidence-derived routing`
3. `[MND] embedding infrastructure: internal/embed, Ollama GPU, batch/query/evidence pipeline`
4. `[MND] evidence gate replaces LLM classifier, tech_preference added to auto-set`
5. `[MND] embedding retrieval wired into ask + eval pipeline`
