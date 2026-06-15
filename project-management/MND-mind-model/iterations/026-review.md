# 026 — Review (Iteration 10)

- **Start:** 2026-06-15
- **End:** 2026-06-15
- **Phase:** Review
- **Reviewer:** Tomas

## What was presented

Iteration 10 — competence-boundary routing (measurement-first):

- **Competence gate** in `orchestrate.sh`: classify incoming question by category (cheap LLM), auto-answer categories in `MND_ROUTE_AUTO` (default `correction_pattern,direction_pattern`), escalate the rest to Tomas. Replaces the broken confidence gate (uniformly `high` while 41% wrong).
- **Measured against production baseline** (43 cases, in-sample): auto-set delivers 78% fidelity at 42% coverage with 0 judgment leaks (vs 59% blanket).
- **Routing simulation tooling**: `classify`, `route-eval`, `route-sim` with sweep frontier.

Additionally during review session:
- **Mandatory post-retrain fidelity eval**: `eval` + `route-eval` + `fidelity-check` wired into every `retrain` run. DSH `action_needed/Q1` alert fires when auto-set fidelity drops below `MND_FIDELITY_MIN_AUTO` (default 75%, approved by Tomas) or judgment questions leak into auto-answer.
- **Sweep fix**: `--auto-cats` ensures the configured policy always appears in the sweep (discovered during live testing — the natural ordering skipped it when classifier accuracy was low).
- **Stale docs fixed**: PROJECT.md, architecture diagram, README, ASSUMPTIONS updated through iter 10. All `brain/` → `data/` references corrected.
- **Live eval run** (19 cases, fresh sample): brain improved to 84% overall (correction 93%, judgment 72%, direction 100%). But classifier accuracy collapsed to 11% on fresh questions → 7 judgment leaks. The fidelity-check pipeline correctly caught and alerted this.

## Tomas's feedback (2026-06-15)

- Approved 75% as the fidelity threshold for auto-answered categories.
- Policy decision: accepted default `correction_pattern,direction_pattern`.
- Identified classifier generalization failure as the next problem — the category-based routing premise holds (per-category fidelity is real and separated) but the classifier can't reliably predict categories on novel questions.
- Directed next iteration: replace BM25 retrieval with **embeddings** for semantic precision on the small corpus (~800 insights). Embeddings also subsume the classifier — category distribution + cluster density of the retrieved set is a direct routing signal, replacing the broken one-shot LLM classification.
- **"merge"** — accepted.

## Decisions

### Iteration 10 accepted; merge to master
**Date:** 2026-06-15 21:05
**Decided by:** Tomas
**Decision:** Merge per MO §10 (`--no-ff` from the parent workspace).

### Fidelity threshold: 75%
**Date:** 2026-06-15
**Decided by:** Tomas
**Decision:** `MND_FIDELITY_MIN_AUTO=75`. Auto-answered categories must deliver ≥75% fidelity; below triggers DSH alert.
**Reasoning:** Just below current measured 78%. Catches real degradation, tolerates measurement noise from small n.

### Next iteration: embedding-based retrieval + routing
**Date:** 2026-06-15
**Decided by:** Tomas
**Decision:** Replace BM25 with embeddings for retrieval and derive the routing signal from retrieval evidence (category distribution + semantic density) instead of a standalone classifier LLM call.
**Reasoning:** BM25 saturates on ~800 lexically homogeneous insights (iter 9: topScore/nStrong identical for right vs wrong). Classifier doesn't generalize (49% → 11%). Embeddings encode meaning, not terms — semantic proximity discriminates on small corpora where BM25 can't. The routing decision moves after retrieval (direct measurement) instead of before it (proxy classification).
**Revisit if:** Embedding API costs or latency are prohibitive (unlikely at 800 vectors + 1 query).
