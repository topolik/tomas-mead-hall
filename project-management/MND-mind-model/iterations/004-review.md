# 004 — Review (Iteration 1)

- **Start:** 2026-06-12
- **End:** 2026-06-12
- **Phase:** Review
- **Reviewer:** Tomas

## What was presented

Full pipeline live-tested on real data: 1853 moments → 787 insights → profiles v2 → `ask` with citations → live herdr orchestration loop (scratch agent asked, brain answered as Tomas, agent proceeded).

## Tomas's feedback (2026-06-12)

In-flight rulings during the iteration:
1. "Iteration 1 should deliver both [brain + orchestrator]. I want to test it on real live data/sessions." — shaped scope.
2. "Get the shit done. Voice & communication is not important, but technical/domain and how I direct & correct." — shaped extraction priorities.
3. "I won't unlock 1pass, go with unsigned commits." / "Let's focus on the task at hand — we can fix GML later but it's working as of now." — also the validity-probe counterexample: the brain said "fix GML now", Tomas ruled "stay on task".
4. "The 6% coverage is too small, it's too early to judge. Continue until all is done." — drove the full-corpus run.

Final verdict: **"Looks good."** Iteration 1 accepted. Plus four directives:

1. **Close the iteration per Modus Operandi.** (this document)
2. **MO gap:** no instructions for working with hwt/worktrees and merging back to master — research and add the best possible instructions. → [MOP] work, immediately after this close.
3. **Retraining job:** a job that learns from NEW conversations, and it must distinguish the agent team's own conversations (ignore — recursion risk: the pipeline's gemini sessions contain MND's own prompts; orchestrator-sent directions appear as "user" turns in agent panes) from Tomas's real claude/gemini sessions (learn). → MND iteration 2.
4. **Feedback loop on low confidence:** when the orchestrator refuses (`confidence: low`), get the answer from Tomas through DSH notifications — same concept as GML insights (his comments become corrective knowledge). → MND iteration 3 (after 2; "then we can continue").

## Next iteration plan (iteration 2 — retraining)

1. **Self/other discrimination at extract time:**
   - Fingerprint pipeline-generated content: datamarker (U+E000) in user turns, distill/profile prompt schema markers — hard skip.
   - Project-dir exclusion list for pipeline working dirs (MND/GML run dirs in `~/.gemini/tmp`).
   - `orchestrate.sh` send-ledger: log every direction sent; extraction drops exact matches (orchestrator output must never become training input).
2. **`retrain` command + daemon:** incremental extract→distill→profile following the GML `watch.sh` pattern; the existing `--skip-insights` incrementality already limits LLM cost to new moments.
3. Carry-over fixes if budget allows: semantic insight dedup (occurrences all =1), processed-moments ledger (re-sending silent moments every run).

Deferred to iteration 3: DSH low-confidence feedback loop; datamarking of terminal tails in `orchestrate.sh` (MND-013).

## Decisions

### Iteration 1 accepted; iteration 2 = retraining with self-exclusion
**Date:** 2026-06-12
**Phase:** Review
**Decided by:** Tomas
**Decision:** Ship iteration 1 as reviewed. Next: retraining job on new conversations with strict exclusion of agent-team-generated content; DSH feedback loop afterward.
**Reasoning:** The brain works (live orchestration proven) but goes stale without retraining and risks feeding on its own output once orchestration is active; the validity probe showed real decisions must flow back into the brain.
**Revisit if:** Retraining quality shows the exclusion rules are too blunt (dropping Tomas's real directions typed into agent panes) or too porous (self-content leaking in).
