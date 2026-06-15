# 018 — Review (Iteration 5)

- **Start:** 2026-06-13
- **End:** 2026-06-13
- **Phase:** Review
- **Reviewer:** Tomas

## What was presented

Iteration 5 — "trust & transparency":

- **A. Attribution prefix** (`700a042`): every direction `orchestrate.sh` delivers leads with `[MND orchestrator]` (env-overridable); doubles as a self-exclusion phrase-marker and is folded into the sent-ledger hash so retraining can never relearn MND's own output. Live dry-run verified.
- **B. Contradiction resolution** (`0eaa994`, `693e9b3`): **three-way verdict** per flagged set — `contradiction` (same context, opposite → Go retires the loser by provenance: feedback > distill, newer > older, strong > weak) or `context_split` (both valid, different situations → keep both, sharpen each `context`). Unknown verdict defaults to the non-destructive side. `ask` honors context scoping. Superseded insights kept for audit, excluded from profiles/answers via `distill.Active()`. Live over the real 801-insight brain: 2 genuine contradictions retired, 4 context-split pairs scoped (incl. Tomas's #4 containers/local-builds, now scoped not retired).
- **Live-LLM acceptance test** (`1757b16`): planted-brain "does it change its mind?" experiment — 8/8, incl. proof the retired belief does not leak into answers.
- **Bonus fix**: the `.gitignore` pattern `mnd` matched the `cmd/mnd/` source dir, so `cmd/mnd/main.go` (the entrypoint) had never been tracked — every prior merge silently omitted it. Anchored to `/mnd`; main.go now committed.

54 Go tests + the live acceptance test.

## Tomas's feedback (2026-06-13)

Steered #4 mid-iteration ("run vs deploy — ensure context is clear") → produced the three-way verdict. Requested + reviewed the live test. **"merge iteration 5 into master."** Accepted.

## Known limitations (recorded in MND-025) — carried, not blockers

1. **Non-deterministic sweep coverage** — the LLM flags different conflict sets each pass (the env-var-credentials contradiction was caught one run, missed the next). Repeated `retrain` converges; a single pass doesn't guarantee. Refinement: loop-until-dry sweep.
2. **Contradiction resolution only retires when a contradicting insight exists** — the "don't use branches" relic persists because the branch-per-project workflow was never distilled into an insight to defeat it.

## Next iteration (6 — proposed)

1. Loop-until-dry contradiction sweep (close the coverage gap from limitation 1).
2. Capture the branch-per-project workflow as an insight (so the relic can be retired) — via the feedback loop or a targeted distill.
3. Semantic insight dedup (carried from iter 5 ideation §C).
4. Authority-boundary insight (carried §D): codify "may an agent restart shared infra?".

## Decisions

### Iteration 5 accepted; merge to master
**Date:** 2026-06-13 · **Decided by:** Tomas ("merge iteration 5 into master")
**Decision:** Merge per MO §10 (`--no-ff` from the parent workspace). No push — Tomas pushes after local runtime verification (dsh:1282 ruling).
