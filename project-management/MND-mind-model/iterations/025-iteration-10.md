# 025 — Iteration 10: competence-boundary routing (measurement-first)

- **Start:** 2026-06-15
- **Trigger:** Tomas — "Do competence-routing but do some testing about the 'measurable' part. In the past the self-measure or self-confidence was totally off."
- **Premise (from iter 8/9):** the clone is reliable on concrete *preferences* (tech 80%) and not on contextual *judgment* (decision_heuristic 38%); three attempts to close that gap failed (iter 9). The product-correct response isn't a smarter clone — it's a clone that knows its boundary: auto-answer where it's measured-safe, escalate the rest to Tomas.

## The constraint that shaped the design
Tomas's caveat is the whole point. Two prior "measures" were **off**:
- The answering LLM's self-reported confidence is uniformly `high` (eval: 100% high while 41% wrong).
- Lever A (retrieval-confidence) couldn't separate right from wrong either (saturated brain).

So the router **must not** key on any self-measure. It keys on the **question's category**, and — critically — the routing signal is validated **externally**: every number below comes from the independent judge's verdicts on the held eval cases, never from anything the clone says about itself. The one place a "measure" enters (the classifier predicting a category) is itself **measured** against gold.

## Built (TDD, `internal/route`, 5 new tests T61–T65)
- **Classifier** (`route-classify-prompt`/`-merge`, `run-task.sh classify`): a cheap LLM labels a question's category from the *situation text alone* — no gold, no answer. Single-question mode prints one word (fail-safe `other` ⇒ escalate). Live-verified: "map or slice?"→tech, "accept non-determinism or dig in?"→decision_heuristic, "wrote a fix but never ran it"→correction_pattern.
- **Routing simulation** (`route-sim`, `route-eval`): routes the 43 judged cases by *predicted* category under a policy and measures **delivered fidelity** vs blanket, **oracle** (gold-category) ceiling, **classifier accuracy** + confusion, per-predicted-bucket fidelity, and the **judgment-leak** safety metric. Sweep = coverage-vs-fidelity frontier, ordered by *predicted* fidelity (the only signal available at routing time).
- **Competence gate** in `orchestrate.sh` (+ `orchestrate-watch.sh` via env): classify the pending question; auto-answer if its category ∈ `MND_ROUTE_AUTO` (default `correction_pattern,direction_pattern`), else escalate to Tomas (DSH) — *even when confidence is high*. `MND_ROUTE=off` restores legacy answer-everything. This **replaces the broken confidence gate as the primary safety valve.** Live-verified end-to-end in dry-run against a real agent (classified `direction_pattern` → auto; showed exactly what it would deliver, sent nothing).

## RESULT — measured against the production baseline (43 cases, in-sample)

Blanket fidelity (answer everything): **59%**. Classifier accuracy (pred vs gold category): **49%**.

**Premise holds (gold fidelity):** tech 80% > correction 67% > direction 57% > judgment 38%.

**The predicted label DISCRIMINATES** (unlike the broken self-confidence, which was flat):

| predicted category | n | fidelity |
|---|---|---|
| correction_pattern | 11 | **86%** |
| direction_pattern | 7 | 64% |
| other / tech_preference | 4 / 8 | 50% / 50% |
| decision_heuristic | 13 | 42% |

**Operational frontier (route by predicted category):**

| auto-answer policy | coverage | delivered(pred) | escalated | judg→auto |
|---|---|---|---|---|
| correction | 26% | **86%** | 32 | 0 |
| correction+direction | 42% | **78%** | 25 | 0 |
| +tech | 60% | 69% | 17 | ⚠ 1 @ 100% |
| everything | 91% | 60% | 4 | ⚠ 7 @ 36% |

_baseline: answering everything = 59%._

## What the numbers say
1. **Competence routing works, and it's measurable without any self-measure.** Auto-answering `correction(+direction)` and escalating the rest delivers **78–86%** fidelity (vs 59% blanket) with **zero judgment questions leaking into auto-answer** — the safety property holds.
2. **Don't route on the naive premise.** Gold says tech is best (80%), but routing on *predicted* tech gives only 50% — the classifier is 49% accurate and **over-assigns "tech"**, and adding tech to the auto set starts leaking judgment. The reliable *predicted* buckets are correction and direction, not tech. (Counterintuitive, and only visible because we measured the predicted label, not the gold one.)
3. **Low classifier accuracy is OK** — what matters is per-predicted-bucket fidelity, and the buckets we auto-answer (correction 86%, direction 64%) are reliable; the unreliable labels (tech, judgment, other) all escalate.
4. **The honest cost:** at the safe operating points the orchestrator escalates **58–74%** of decisions to Tomas. That's correct — the clone is only trustworthy on ~30–40% of decisions, and now it *knows which* and acts on it.

## Caveats (stated, not hidden)
- **In-sample** (brain distilled from these moments) → optimistic upper bound; held-out would be lower. The *discrimination* and *direction* are the robust findings; the exact percentages are noisy.
- **Small n** (tech gold n=5, pred-corr n=11) → wide error bars on individual cells.
- Held-out validation (eval-brain) remains the deferred next rigor step (MND-031).

## Policy decision for Tomas (the one knob)
Default shipped: **`correction_pattern,direction_pattern`** (78% delivered, 42% coverage, 0 leaks). Conservative alternative: **`correction_pattern`** only (86% delivered, 26% coverage). Tune via `MND_ROUTE_AUTO`; both are measured-safe.

## Shipped
`internal/route` (+T61–T65), `mnd route-classify-prompt/-merge/route-sim`, `run-task.sh classify` + `route-eval`, competence gate in `orchestrate.sh`/`orchestrate-watch.sh`. 70 Go tests pass; scripts lint; classifier + full gate live-verified. Code/docs only — no production brain/data on this branch.
