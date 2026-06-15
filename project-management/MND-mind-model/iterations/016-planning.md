# 016 — Planning (Iteration 5B: contradiction resolution)

- **Start:** 2026-06-13
- **Phase:** Planning
- **Scope:** retire stale insights that newer corrections supersede, so the brain never carries a belief it has already corrected (015-ideation §B). The "I don't work with branches" relic vs. the MO §10 push ruling is the acceptance case.

## Design — LLM finds conflicts, Go decides winners

Split mirrors `distill` (LLM for semantics, Go for rules, MND-003):

1. **`mnd contradiction-prompt`** — builds a sweep prompt over the **active** insights (grouped by category to keep comparisons local). The LLM's only job is to identify *sets of insights that directly contradict* (one says do X in context C, another says the opposite in the same context) and give a one-line reason. It does **not** pick a winner.
2. **LLM call** (run-task.sh) → response.
3. **`mnd contradiction-merge`** — for each conflict set, Go applies a **deterministic provenance rule** to pick the winner and marks the rest `superseded` (kept in `insights.yaml` with `superseded_by` + reason — never deleted, the evidence trail survives).

**Provenance rule** (`MoreAuthoritative`, higher wins, in order):
1. `source: feedback` > `distill` — a direct Tomas correction beats a distilled inference (MND-017's principle, now enforced at conflict time).
2. newer > older — by the insight's latest evidence timestamp (RFC3339 lexical compare).
3. `strong` > `moderate` > `weak`.
4. stable id tiebreak (deterministic reruns).

**Active-only everywhere downstream**: `distill.Active()` filters `status == superseded`; profile generation, ask retrieval (BM25 index), and stats all build over active insights only. Superseded insights stay in the file for audit but never reach an answer or a profile.

**Idempotent**: the sweep only ever sees active insights, so a resolved loser is never re-presented; a no-conflict response is a no-op. Folds into `retrain` after `learn` (a retirement changes `insights.yaml` → the existing before/after hash gate regenerates profiles).

## Data model (`distill.Insight`, all `omitempty` — backward compatible)
- `Status string` — "" (active) | "superseded"
- `SupersededBy string` — winner insight id
- `SupersededReason string` — short why (from the LLM's conflict reason)

## Tests (written first)

| ID | What | How |
|----|------|-----|
| T41 | `MoreAuthoritative`: feedback beats distill; among same source, newer evidence ts wins; then strength; deterministic id tiebreak | Go unit |
| T42 | `Resolve`: losers in a conflict set get `status=superseded`, `superseded_by=winner`, reason; winner untouched; superseded insights remain in the slice (evidence trail) | Go unit |
| T43 | `ParseResponse`: prose-tolerant; drops groups citing unknown or already-superseded ids; group with <2 active members is a no-op | Go unit |
| T44 | `distill.Active` excludes superseded; profile prompt + ask index contain only active insights | Go unit |
| T45 | idempotent: empty conflicts → unchanged; re-Resolve over the post-resolution set supersedes nothing new | Go unit |
| T46 | live: run the sweep over the real brain; confirm the stale "branches" belief is retired by the push-ruling feedback insight (or report honestly if the brain no longer holds the conflict) | live |

## Risks
- **False contradictions** (LLM flags a non-conflict): bounded — worst case retires a still-useful insight, recoverable by flipping `status` back; the reason is recorded. Prompt stresses "same applicability context, genuine opposite," not mere topical difference.
- **Insights derive from sessions** (possible injected text): the sweep prompt marks `<insights>` as data-not-instructions; not datamarked (these are already-curated short directives and the contradiction judgment needs clean text) — noted as an accepted trade-off.
