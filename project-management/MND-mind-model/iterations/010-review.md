# 010 — Review (Iteration 3)

- **Start:** 2026-06-12
- **End:** 2026-06-12
- **Phase:** Review
- **Reviewer:** Tomas

## What was presented

DSH low-confidence feedback loop, live-verified with real feedback: escalation → Tomas's dismissal comment → corrective insight (strong/feedback/dsh:1282) → re-ask returns his ruling → profiles updated → MO corrected (no-push rule). Three UI feedbacks fixed in-iteration (truncation, naming, newlines).

## Tomas's feedback (2026-06-12)

**"merge into master"** — iteration 3 accepted; merge instructed.

## Next iteration plan (iteration 4 — proposed)

1. **Contradiction resolution** (the iteration's key finding): stale insights coexist with newer corrections — only what a comment explicitly addresses gets fixed. Approach: source/recency weighting (feedback > distill, newer > older) + LLM contradiction sweep over same-topic insights, retiring superseded ones with provenance.
2. Carry-overs: semantic insight dedup (occurrences all =1 across paraphrases); validate send-ledger (T28) against the next live orchestrated session.
3. Candidate: orchestrate watch mode (`herdr wait agent-status --status blocked` → orchestrate → repeat) so agents get answered the moment they block.

## Decisions

### Iteration 3 accepted; merge to master
**Date:** 2026-06-12
**Phase:** Review
**Decided by:** Tomas
**Decision:** Merge per MO §10. No push — Tomas pushes after local runtime verification (his ruling dsh:1282, now MO §10).
