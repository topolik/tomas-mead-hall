# 015 — Ideation (Iteration 5: trust & transparency)

- **Start:** 2026-06-13
- **Phase:** Ideation
- **Trigger:** Tomas, 2026-06-13: *"I want to see in the conversation when the MND answers. There should be some prefix in every response that this comes from MND orchestrator. With this, start iteration 5"* — plus the carried backlog from 014-review.

## Theme

The first live run (iteration 4) worked: MND answered real agents in Tomas's style, escalated when silent, left finished agents alone. What it exposed is now the iteration: **can you trust and see what the brain is doing?** Three gaps — attribution, stale beliefs, and an undefined authority boundary.

## Scope

### A. Attribution prefix (DONE this turn — the explicit ask)
Every direction `orchestrate.sh` delivers into a pane is prefixed `[MND orchestrator]` (env `MND_SEND_PREFIX`), so the receiving agent and anyone reading the pane can tell it's the brain, not Tomas typing. The prefix doubles as a self-exclusion phrase-marker (`internal/exclude`) and is ledgered as part of the sent text — retraining can never relearn MND's own output. Test T40; live dry-run verified the preview renders.

### B. Contradiction resolution (the main event — carried from 010/014-review)
The standing quality gap: stale insights coexist with newer corrections and both get retrieved (the "I don't work with branches" relic vs. the MO §10 push ruling is the test case). Proposed approach:
- **Provenance weighting at retrieval/merge:** `feedback` > `distill`, newer > older, higher-strength > lower.
- **LLM contradiction sweep:** over same-topic insight clusters, identify pairs that conflict; retire the superseded one with a provenance note (don't delete — mark `superseded_by`, keep the evidence trail).
- Surface in `ask` so the prompt never carries a belief the brain has already corrected.

### C. Semantic insight dedup (carried)
Paraphrases of the same insight have `Occurrences: 1` each (the impersonating-domains todo showed the same content six times). Identity-key dedup is lexical; add a semantic merge pass so occurrence counts reflect reality and the evidence base isn't padded.

### D. Authority-boundary insight (surfaced live, iteration 4)
The brain granted the todo-70 agent authority to restart the shared LLP service on its own judgment — defensible (Tomas had tasked that agent with the fix) and it held up (the fix was runtime-verified), but no insight actually covered "may an agent restart shared infra?". Codify the boundary explicitly (likely a `decision_heuristic`: shared-service restarts are Tomas's call unless he's pre-authorized the specific agent+fix), so the brain isn't improvising it.

## Out of scope / notes
- LLP `claude` tier model_id bump (fable-5 → opus-4-8) is Tomas's edit+restart (filed in todo.txt); MND's own pins are already fixed.
- Watch-mode soak validated the send-ledger (T28) live across the iteration-4 run — no further work needed there.

## Open questions → ASSUMPTIONS (next: planning)
- MND-024 (attribution prefix + self-exclusion), MND-025 (provenance-weighted contradiction handling), MND-026 (semantic dedup), MND-027 (shared-infra authority boundary).
