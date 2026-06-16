# 029 — Implementation: embedding-based retrieval + evidence-derived routing

- **Start:** 2026-06-15
- **End:** 2026-06-16
- **Phase:** Implementation

## Summary

Replaced BM25 + LLM classifier routing with embedding-based semantic retrieval + evidence-derived gate. Ollama GPU (nomic-embed-text, 768-dim) on local GTX 1080 as primary embedding provider. LLM classifier eliminated — routing now inspects retrieval evidence directly (zero LLM calls for routing).

## What was built

### 1. Embedding infrastructure (`internal/embed`)

New Go package: `Vector`, `Store` (load/save/delta), `CosineSim`, `TopK`, `Evidence` metadata, `GateDecision`, response parsers for Ollama and Gemini.

- `Store` persists as `data/embeddings.json` (~889 entries, 768-dim each)
- `TopK` filters by active insights, excludes superseded
- `GateDecision(ev, autoCats, minMeanSim, minMinSim, minDominance)` → "auto" | "escalate"
- `InsightText(in)` concatenates statement + context for embedding

### 2. Ollama Docker service

`docker-compose.yml` gains `ollama` service with NVIDIA GPU passthrough, ollama-models volume, embed profile. Commands:

- `embed-start` — start Ollama container, pull nomic-embed-text
- `embed-batch` — batch-embed all active insights (delta: only new/changed)
- `embed-query` — embed single question → 768-dim vector file

### 3. Evidence gate in orchestrate.sh

Replaces the classify → LLM call → gate flow:

1. Embed question via Ollama
2. Cosine top-k against pre-embedded insights
3. Gate: dominant category ∈ auto-set AND mean_sim ≥ 0.60 AND dominance ≥ 50% → auto-answer
4. Otherwise → escalate to DSH

Env vars: `MND_ROUTE_MIN_MEAN` (0.60), `MND_ROUTE_MIN_MIN` (0.40), `MND_ROUTE_MIN_DOM` (0.50). Falls back to legacy classify gate if no embeddings available.

### 4. Embedding retrieval in ask pipeline

`ask-prompt` gained `--embeddings` and `--query-vec` flags. When provided, uses cosine top-k instead of BM25 for evidence selection. Falls back to BM25 when flags absent.

`eval` and `eval-rerun` also gained embedding support — auto-embeds eval case situations when Ollama is up.

### 5. tech_preference added to auto-set

Re-evaluated after embedding routing. Adding it raised fidelity 87% → 91% with +19% coverage. Default auto-cats: `correction_pattern,direction_pattern,tech_preference`.

### 6. Retrain integration

`retrain` now runs `embed-batch` after profile regen, followed by mandatory eval + route-eval + fidelity-check with DSH alert on failure.

## Test results

77 tests pass across 17 packages. New tests:

| ID | Test | Status |
|----|------|--------|
| T71 | CosineSim (identical, orthogonal, opposite, 45deg, mismatch, zero, empty) | ✅ |
| T72 | TopK (ordering, active filter, superseded exclusion) | ✅ |
| T73 | ParseGeminiResponse | ✅ |
| T74 | ParseOllamaResponse | ✅ |
| T75 | ComputeEvidence (mean, min, dominant category/fraction, distribution) | ✅ |
| T76 | GateDecision (safe, judgment, sparse, mixed, lowMin, empty) | ✅ |
| T77 | Store delta (Prune + Set) | ✅ |

T78 (full pipeline integration) not a unit test — covered by live A/B measurement below.

## Live measurements

### Evidence gate routing (10 fresh questions)

Tested live via `orchestrate.sh` with evidence gate. Gate correctly routed all 10 questions — no judgment leaks, no false escalations on safe categories.

### A/B: embedding vs BM25 retrieval (3 cases, LLP quota limited)

| Case | BM25 answer | Embedding answer | Evidence overlap |
|------|------------|-----------------|------------------|
| 1 | ✅ correct | ✅ correct | 0/12 (completely different insights) |
| 2 | ✅ correct | ✅ correct | 6/12 |
| 3 | ✅ correct | ✅ correct | 4/12 |

Embedding selects fundamentally different insights but reaches the same correct answers. The profiles carry the core decision signal; retrieved evidence provides supporting detail. Full 26-case A/B deferred pending LLP quota recovery.

### Fidelity summary

| Policy | Fidelity | Coverage | Judgment leaks |
|--------|----------|----------|----------------|
| correction_pattern,direction_pattern (iter 10) | 87% | 38% | 0 |
| + tech_preference (iter 11) | 91% | 57% | 0 |

## Bugs found & fixed

1. **stdin consumed by subcommands**: `while read` loop in test script only processed 1 line because `embed-query`/docker consumed stdin. Fixed with file descriptor 3.
2. **Container permission denied**: Container couldn't write to freshly created host directories. Workaround: pre-create files with correct UID/GID.
3. **Stale brain/ → data/ references**: Several `--brain-dir brain` defaults pointed to non-existent directory. Fixed across run-task.sh and Go defaults.

## Checklist completion

- [x] `internal/embed` package: Vector, Store, CosineSim, TopK, parsers (T71–T74)
- [x] `data/embeddings.json` format and persistence
- [x] `run-task.sh embed-batch` (Ollama GPU, delta-only)
- [x] `run-task.sh embed-query` (single question → vector)
- [x] `MND_EMBED=auto|ollama|gemini` selection logic
- [x] `ask-prompt` with `--embeddings`/`--query-vec` flags (T75)
- [x] Evidence gate in `orchestrate.sh` (T76)
- [x] Remove classify LLM call from orchestrate flow
- [x] A/B measurement: embedding vs BM25 (3 cases — LLP limited)
- [x] Routing measurement: evidence gate on 10 live questions
- [x] `retrain` integration: embed-batch after profile regen
- [x] `fidelity-check` passes with new pipeline
- [x] Ollama docker-compose entry with GPU passthrough
- [x] Update README, architecture diagram, ASSUMPTIONS
- [ ] Full 26-case A/B eval (deferred: LLP quota exhaustion)
- [ ] Gemini REST fallback tier (deferred: Ollama-first decision)

## Decisions

### Start with Ollama, skip Gemini tier
**Date:** 2026-06-15 22:30
**Phase:** Implementation
**Decided by:** Tomas
**Decision:** Use Ollama GPU as primary and only embedding provider for now. Skip Gemini REST tier.
**Alternatives considered:** Gemini-first with Ollama fallback (original plan).
**Reasoning:** Ollama on local GPU has no API dependency, no quota limits. Gemini can be added later as fallback if needed.
**Revisit if:** Ollama proves unreliable or GTX 1080 VRAM insufficient for future models.

### tech_preference re-included in auto-set
**Date:** 2026-06-16
**Phase:** Implementation
**Decided by:** Tomas
**Decision:** Add `tech_preference` to auto-answer set based on embedding routing evidence.
**Alternatives considered:** Keep tech excluded (original iter 10 decision).
**Reasoning:** With embedding-based evidence gate, adding tech raised fidelity 87%→91% with +19% coverage. The LLM classifier noise that originally tanked tech accuracy is eliminated.
**Revisit if:** tech_preference fidelity drops below 75% threshold on future evals.
