# 014 — Review (Iteration 4)

- **Start:** 2026-06-12
- **End:** 2026-06-12
- **Phase:** Review
- **Reviewer:** Tomas

## What was presented

Smart LLM routing + hands-off orchestration, all live-verified in-session with Tomas driving the design:

- **LLP capability ladder** (Tomas's tiered-impl idea — pure config, no LLP code change): `auto` = gemini-3-pro-preview → claude-fable-5 → 2.5-pro → opus-4-8 → flash → sonnet, per-tier quota cooldowns, `--approval-mode default` on all gemini tiers, `gml-analyze` kept cheap. Tomas created the runtime config via MND (agent-authored, his restart) and restarted LLP from the master checkout. Verified: gemini tier quota-cooled, claude-fable-5 served, `agent: mnd` in the usage log.
- **MND routing**: single `model: auto` request when LLP is up; `MND_LLP_CHAIN` escape hatch; direct-CLI fallback (proven under real LLP breakage); profiles stay direct-CLI until LLP's example config ships the gemini auto-deny flag.
- **Watch mode** (`orchestrate-watch.sh`): blocked/idle agents answered once per question, repeat-after-answer ⇒ DSH escalation, `pending: none` gate, self-exclusion, per-pane cooldown. Live drill: all four paths (dry-run, forced escalation → notification 1291, real send → agent unblocked in Tomas style, natural `none`).
- 50 commits' worth of side discoveries filed: LLP running from deleted worktree (fixed via restart), healthz liveness gap (open), stdin-slurp fix, model-steering env.

## Tomas's feedback (2026-06-12)

Iterative design steering throughout ("use the highest models… check the quotas… choose the best"; "multiple configs, like gemini, claude, gemini2, claude2"; "create that config"), then **"finish"** — iteration accepted; close and merge per MO §10.

## Next iteration plan (iteration 5 — proposed)

1. **Contradiction resolution** (carried from 010-review, still the top quality gap): feedback > distill, newer > older; LLM contradiction sweep retiring superseded insights with provenance. Test case: the stale "I don't work with branches" belief vs. MO §10.
2. Semantic insight dedup (paraphrase occurrences all =1).
3. Watch-mode soak: run `orchestrate-watch.sh` during a real multi-agent work session; validate send-ledger T28 exclusion against the next retrain.

## Decisions

### Iteration 4 accepted; merge to master
**Date:** 2026-06-12
**Phase:** Review
**Decided by:** Tomas ("finish")
**Decision:** Merge per MO §10 (`--no-ff` from the parent workspace). No push — Tomas pushes after local runtime verification (dsh:1282 ruling).
