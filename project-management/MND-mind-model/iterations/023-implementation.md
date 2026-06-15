# 023 — Implementation (Iteration 8: fidelity eval)

- **Start / End:** 2026-06-14
- **Phase:** Implementation (built per the 022 plan; small→large→retest loop per Tomas)

## What was built
- `internal/eval` — 4-stage harness: `Sample` (deterministic), `BuildCasesPrompt`/`ParseCases`, `AskQuestion` (blind — gold never in prompt), `BuildJudgePrompt`/`ParseJudge` (junk⇒disagree), `Aggregate`/`Report` (fidelity %, per-category, confidence calibration, disagreement work-list).
- 7 subcommands `mnd eval-build-prompt|build-merge|ask-prompts|ask-merge|judge-prompt|judge-merge|report`; `run-task.sh eval` orchestrates (in-sample; `MND_EVAL_N` candidates).
- Tests T50–T55, T57 (degenerate-confidence flag). **63 Go tests.**

## Test loop (per Tomas)
- **Small (N=5):** ran clean end-to-end; 2 cases. One bug-class fix: added a guard that aborts clearly if zero decision cases are built (was failing confusingly three stages later).
- **Large (N=80):** 80 candidates → **43 cases** (37 non-decisions skipped), 43/43 answered, 43/43 judged — **no drops, no missing, no parse failures.** Harness is bug-free at scale.
- **Improve + retest:** the large run exposed degenerate confidence (below). Added a report warning for it; regenerated the report from the existing scored data (no new LLM calls) — warning renders.

## Findings — the eval immediately earned its keep

**Headline: 59% in-sample fidelity** over 43 cases (agree=19, partial=13, disagree=11). In-sample is an *optimistic upper bound* (the brain was distilled from these moments), so true fidelity is ≤59%. The clone matches Tomas roughly 6/10 even generously — **not redundant-grade yet**, now measured rather than hoped.

**Critical: confidence is non-discriminating.** The clone returned `high` on **100%** of cases (verified in the raw responses — not a capture bug) while being wrong/partial 41% of the time. The orchestrate-watch safety gate only withholds on `confidence: low`, so **it would deliver every answer, including the wrong ones** — the gate can't protect anything. This is the #1 trust blocker and directly answers "is it safe unsupervised?": not with today's confidence model.

**Weakest area: `decision_heuristic` (38%)** vs `tech_preference` 80%, `correction_pattern` 67%, `direction_pattern` 57%. The clone is best at Tomas's tech defaults and worst at *how he weighs options* — the judgment calls.

**Disagreements are genuine and actionable** (not judge noise), e.g.: approved a complex dynamic-loading scheme Tomas rejected on KISS; fixed password-reversion when Tomas cared about session privilege; delegated to the SAST tool but dropped his "provide distilled files" step. These are the retraining work-list.

## Decisions
### In-sample first, held-out deferred
**Decided:** v1 ships in-sample (cheap, immediate, honestly labeled); the disagreement list is valid regardless of leakage. Held-out (train/test eval-brain) is a documented flag for an unbiased number once the harness proved sound — it has.

## Review
**Tomas: "merge"** — accepted; merged per MO §10. Next directive: attack the disproportionate tech (80%) vs judgment (38%) split → iteration 9.

## Carried to iteration 9 (informed by these findings)
1. **Fix confidence calibration** — the top trust blocker; the clone must say `medium`/`low` when it's actually unsure so the gate works. (Could calibrate against this very eval.)
2. **Lift `decision_heuristic` fidelity** — target retraining at the disagreement work-list.
3. Held-out eval run for an unbiased baseline.
4. Then the bigger "drive the loop" work (review-gate/prioritization), now that there's a metric to steer by.
