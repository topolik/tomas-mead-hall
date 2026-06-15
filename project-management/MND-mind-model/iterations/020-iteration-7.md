# 020 — Iteration 7: trustworthy continuous learning

- **Start:** 2026-06-14
- **Trigger:** Tomas, "continue" — make the on-master continuous-learning loop reliable (the two top carry-overs from iteration 6).

## Ideation / Planning

The model correction (iteration 6, "code vs production data") made on-master `retrain` the production learning path. Two known defects would break a hands-off loop:
1. **Profile regen breaks on gemini** — gemini emits raw newlines inside JSON string values; the iter-6 retrain's profile step failed and needed a manual claude fallback. A recurring retrain (gemini by default) would fail every time.
2. **Single-pass contradiction coverage is non-deterministic** — the LLM flags different conflict sets each pass (the env-var-credentials contradiction was caught one run, missed another).

TDD: T48 (profile JSON repair), T49 (idempotent context_split). Loop behavior verified live.

## Implementation

- **#1 Profile JSON repair (MND-030):** `brain.WriteProfiles` now escapes raw control chars inside JSON string literals (`repairJSONStringNewlines`, byte-level in-string scanner) and retries before failing. T48 covers a response with real newlines/tabs inside values.
- **#2 Loop-until-dry sweep (MND-029):** `run-task.sh contradictions` repeats the sweep until `MND_SWEEP_DRY` (default 2) consecutive passes change nothing, capped at `MND_SWEEP_MAX` (default 4). Each pass hashes `insights.yaml` to detect change.
- **#2a Idempotent context_split (MND-029):** `contradiction.Resolve` scopes an insight only if it has no context yet. Without this the LLM re-flags the same pairs and rewords contexts cosmetically every pass, so the brain never stabilizes — loop-until-dry would always hit the cap.

55 → 57 Go tests (T48, T49 added).

## Live verification (real LLM, real brain — then reverted, dev/test only)

- **First loop run (before #2a) exposed the bug:** 4 passes, never converged — context_split re-scoped the same insights with reworded contexts every pass (98303b…, 924d22…, 73978b… re-scoped 3–4× with cosmetic phrasing changes), so the hash never settled. The live test caught the convergence flaw the unit tests couldn't.
- **After #2a:** clean convergence — **pass 1 retired 1** (a genuine contradiction the iter-6 single sweep had missed — loop-until-dry earning its keep), **passes 2 & 3 changed nothing** (still flagged 8 then 7 conflict sets, but all already-contexted → no-ops) → 2 consecutive dry → done in 3 passes, well under the cap.
- The brain mutations from these dev/test sweeps were reverted; this commit is **code only** (MO §10 "code vs production data"). The pass-1 retirement will land for real on the next production retrain on master.

## Review

**Tomas: "merge to master and finish MO correctly"** — accepted. Post-implementation docs done (SKILLS.md iter 5–7 entry, ASSUMPTIONS MND-029/030); merged per MO §10 (--no-ff, branch synced, no push).

## Carried to iteration 8
- Semantic insight dedup.
- Authority-boundary insight ("may an agent restart shared infra?").
- A safe **recurring** retrain on master (the daemon, corrected) — now unblocked: profile regen is reliable (#1) and the sweep converges (#2). Re-enabling it is Tomas's call.
