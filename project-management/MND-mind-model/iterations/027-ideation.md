# 027 — Ideation: embedding-based retrieval + evidence-derived routing

- **Start:** 2026-06-15
- **Trigger:** Tomas (review of iter 10) — BM25 saturates on the small corpus, classifier doesn't generalize. Embeddings encode meaning; the routing signal should come from what the brain actually found, not a one-shot classification.

## Problem statement

The brain answers well (84% fidelity) but the gate deciding whether to let it answer is unreliable:
- **BM25 retrieval** can't discriminate: ~800 insights with overlapping vocabulary → every question pulls ~10 strong matches regardless of relevance. topScore ~24-25 across right AND wrong answers (iter 9 lever A).
- **LLM classifier** doesn't generalize: 49% accuracy on the training set, 11% on fresh questions. Abstract category definitions are ambiguous without examples; different eval-framed questions break the pattern matching.
- **The routing decision happens before retrieval** (classify → gate → ask), so it can't use the brain's actual evidence.

## Proposed solution

Replace BM25 with embedding-based semantic retrieval and derive the routing signal from the retrieval evidence itself.

### A. Embedding-based retrieval

1. **Embed all active insights** (batch, ~800 vectors) via LLP gateway → store in `data/embeddings.yaml` alongside `data/insights.yaml`. Regenerate during `retrain` when insights change (delta: only embed new/changed insights).
2. **Embed the incoming question** at `ask` time (1 API call via LLP).
3. **Cosine similarity sort** → top-k. In-memory brute-force over 800 vectors — no vector DB needed.
4. Replace BM25 in `internal/ask` with embedding retrieval. Profiles still included whole (MND-006 unchanged).

### B. Evidence-derived routing (replaces the classifier)

The `ask` response already returns to the orchestrator. After retrieval, the system knows:
- **Semantic proximity** of the top-k insights (cosine scores)
- **Category distribution** of the retrieved set (e.g., 5 correction + 2 direction vs 3 judgment + 2 tech)
- **Agreement** — do the retrieved insights point the same direction or conflict?
- **Density** — sparse neighborhood (nearest insight at 0.6) vs dense (5 insights above 0.85)

These signals replace both the classifier AND the broken confidence field:
- Dense neighborhood, high agreement, dominant safe category → auto-answer
- Sparse, conflicting, or judgment-heavy → escalate

The routing decision moves **after** retrieval:
```
current:  question → classify(LLM) → gate → ask(LLM) → deliver
new:      question → ask(embed + LLM) → evidence inspection → gate → deliver/escalate
```

Cost: one LLM call (ask) + one embedding call (question). The classify LLM call is eliminated. Net: same or fewer LLM calls.

### C. Orchestrator flow change

`orchestrate.sh` currently has two LLM calls: classify (routing) then ask (answering). New flow:
1. `ask` always runs (retrieves + answers + emits evidence metadata in `--json`)
2. Evidence metadata includes: top-k cosine scores, category distribution, agreement signal
3. Orchestrator inspects metadata → deliver or escalate (with the proposed answer attached to the DSH notification, so Tomas can see what the brain would have said)

### D. What to measure

- **Retrieval precision**: do embedding top-k insights match the gold category better than BM25?
- **Discrimination**: does evidence density/agreement separate right from wrong answers? (The iter 9 lever A question, now with a signal that can actually discriminate.)
- **Routing accuracy**: does evidence-derived routing beat category-based routing on the eval set?
- **Fidelity**: overall and per-category, compared to BM25 baseline (84% current).

## Implementation notes

- Embedding model: use whatever LLP routes to (`model: auto` with an embedding-capable chain). Gemini and Claude both have embedding endpoints. If LLP doesn't support embeddings yet, direct API call with fallback.
- Vector storage: flat YAML/JSON, keyed by insight ID. ~800 × 768-dim (or whatever the model emits) ≈ small file.
- Go: `math` package for cosine distance. No external vector libs.
- MO compliance: everything in Docker, no host packages. The embedding API call is host-side like all LLM calls (MND-003 pattern).

## Embedding provider strategy (tiered fallback)

Three tiers, tried in order. The first that works becomes the default; the others are fallbacks.

### Tier 1: Cloud API via LLP (preferred)

Gemini `text-embedding-004` (768-dim, free tier generous) or Claude's embedding endpoint via LLP gateway. Zero local setup, highest-quality models, quota-managed. LLP would need an embedding-capable chain — may need a small LLP extension if it only handles chat completions today.

**Pro:** best quality, no local resources, fits existing MND-003 pattern (LLM calls are host-side API calls).
**Con:** API dependency, quota limits, latency (~100ms per call but only 1 per question + batch on retrain).

### Tier 2: Local GPU via Ollama (fallback if cloud APIs fail or cost too much)

The host has a **GTX 1080 (8GB VRAM, ~7.5GB free, CUDA 13.0)**. Ollama runs embedding models in Docker with GPU passthrough — fits MO §8 ("everything in containers").

Suitable models that fit in 8GB VRAM:
- **`nomic-embed-text`** (137M params, 768-dim) — ~300MB, excellent quality-to-size ratio, Apache 2.0. Benchmarks near OpenAI `text-embedding-3-small`. The sweet spot for this GPU.
- **`mxbai-embed-large`** (335M, 1024-dim) — ~700MB, slightly better quality, still fits easily.
- **`all-minilm`** (33M, 384-dim) — ~70MB, fastest, lower quality but possibly sufficient for 800 well-structured insights.
- **`snowflake-arctic-embed`** (335M, 1024-dim) — strong retrieval benchmark scores.

Implementation: add Ollama as a `docker-compose.yml` service with `deploy.resources.reservations.devices` for GPU passthrough. MND calls `ollama api/embeddings` endpoint (HTTP, localhost). Batch-embed all insights on retrain; single-embed the question at ask time. Latency: <50ms for a single embedding on GPU — faster than cloud.

```yaml
# docker-compose.yml addition
ollama:
  image: ollama/ollama
  deploy:
    resources:
      reservations:
        devices:
          - driver: nvidia
            count: 1
            capabilities: [gpu]
  volumes:
    - ollama-models:/root/.ollama
  ports:
    - "11434:11434"
```

**Pro:** zero API cost, zero latency, works offline, no quota. GPU is idle anyway.
**Con:** local setup (Ollama install, model pull), smaller model = possibly lower quality than cloud, GPU memory shared with other potential uses.

### Tier 3: Local CPU-only (last resort)

If GPU passthrough fails (driver issues, container runtime doesn't support it): Ollama also runs CPU-only. `nomic-embed-text` at ~300MB embeds in ~200ms per call on CPU — acceptable for 1 question at ask time. Batch embedding 800 insights takes ~2-3 minutes, runs only on retrain.

Alternatively: Go-native embedding via ONNX runtime (`onnxruntime-go`) with a quantized model. No Python, no Ollama, pure Go binary in the existing Docker container. Higher implementation cost but zero external dependencies.

### Selection logic in run-task.sh

```
MND_EMBED=auto (default) | cloud | ollama | cpu
  auto: try LLP embedding endpoint → Ollama localhost:11434 → CPU fallback
  cloud: LLP only, fail if unavailable
  ollama: local GPU only
  cpu: Ollama CPU-only or Go-native ONNX
```

## Risks

- **Embedding model quality**: cheaper/smaller embedding models may not separate the categories well on domain-specific content. Mitigated: test with the best available model first (cloud), downgrade to local only with measured comparison.
- **Embedding API availability**: if neither Gemini nor Claude embedding endpoints are available via LLP, need a direct path. Mitigated: Ollama local GPU as tier 2; BM25 stays as a last-resort retrieval fallback (never removed, just deprioritized).
- **Answer quality change**: switching retrieval from BM25 to embeddings changes what context the answering LLM sees. Could help (better context) or hurt (different context). Mitigated: A/B via `eval-rerun` before replacing BM25.
- **GPU memory contention**: if other services start using the GTX 1080, embedding model may not fit. Mitigated: `nomic-embed-text` is only 300MB; even `mxbai-embed-large` at 700MB leaves 7GB free.

## Not in scope

- Vector database (overkill at 800 vectors)
- Hybrid retrieval (BM25 + embeddings) — try pure embeddings first, add BM25 fusion only if needed
- Embedding fine-tuning
- Ollama for LLM inference (only for embeddings — LLM calls stay with Gemini/Claude via LLP)
