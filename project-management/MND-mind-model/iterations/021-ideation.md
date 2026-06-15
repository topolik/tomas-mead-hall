# 021 — Ideation (Iteration 8: fidelity eval)

- **Start:** 2026-06-14
- **Phase:** Ideation
- **Trigger:** Tomas asked how the backlog aligns with the goal (orchestrator = Tomas brain clone, "make me redundant"). Conclusion: the machinery works, but there is **no measure of how often the clone is actually right** — so "redundant" is faith, not evidence. This iteration builds that measure.

## Problem

The orchestrator answers agents in Tomas's style and learns continuously. Every validation to date is anecdotal (database-vs-files, deploy-on-k8s, branches relic). To trust it unsupervised we need a **fidelity number**: on real situations where Tomas gave direction, how often does the clone's blind answer match what Tomas actually decided — and does its stated confidence predict its correctness?

The most valuable output isn't the headline %, it's the **disagreement list**: the concrete cases where the clone is wrong. That's what targets the next retrain and tells us where it can't yet be trusted.

## Goal (this iteration)

A repeatable `mnd eval` that:
1. Selects real decision situations from session history (with Tomas's actual decision as ground truth).
2. Asks the clone each situation **blind** (situation only, never Tomas's answer).
3. Has an independent LLM judge score agreement (agree / partial / disagree + reason).
4. Reports: overall agreement %, per-category breakdown, **confidence calibration** (does high-confidence ⇒ high-agreement?), and the full disagreement list.

## Explicitly out of scope (this iteration)
- Semantic insight dedup, authority-boundary insight — folded into a later iteration; they're calibration/safety polish, not the trust measure.
- Extending the orchestrator from reactive answering to the **review-gate / prioritization** role — the other big "redundant" gap. Tracked for a future iteration; this one establishes the measuring stick first, because that work needs a fidelity metric to know if it's improving.

## Key decision to resolve in planning (for Tomas)
**Leakage**: the production brain was distilled from all moments, so testing on them is partially in-sample. True rigor needs a held-out test split + an eval-brain built from train-only (expensive: a full distill of the train split). Cheap v1 tests the current brain and labels the number an optimistic upper bound. The disagreement list is valid either way. → decide in planning.
