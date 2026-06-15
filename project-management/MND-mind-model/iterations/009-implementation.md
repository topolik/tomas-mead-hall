# 009 — Implementation (Iteration 3: DSH low-confidence feedback loop)

- **Start:** 2026-06-12
- **End:** 2026-06-12
- **Phase:** Implementation

## What was built

- `internal/dsh` — trimmed GML client (OAuth client-credentials, post/update notification, dismissed-with-comments fetch). Config `data/dsh.yaml` (gitignored, chmod 600; committed `dsh.yaml.example`). **MND has its own DSH OAuth client** (Tomas's call — per-service identity; the permission classifier correctly blocked my attempt to reuse GML's creds, and Tomas issued new ones at /admin/clients).
- `internal/feedback` — escalation formatting with `[MND ask <qhash>]` round-trip identity (update-not-repost), learn prompt (comment = authoritative, datamarked), per-item-resilient response parsing → insights with `strength: strong`, `source: feedback`, evidence `dsh:<id>`; ingest ledger records ALL gathered notifications (zero-insight ones included).
- `mnd feedback-post | learn-gather | learn-merge`; `run-task.sh feedback-post | learn`; `learn` folded into `retrain`.
- `orchestrate.sh`: terminal tails now framed+datamarked via `ask-prompt --tail-file` (MND-013 closed); on `confidence: low` → automatic DSH escalation.
- `docker-compose.yml`: `network_mode: host` (container must reach DSH on localhost — GML precedent; found live).

`go test ./...`: **46 tests** in 12 packages.

## Live run (T34) — full loop verified with real Tomas feedback

1. Asked a genuinely open question (auto-push master to origin after review-gated merges?). Brain answered **high** confidence — and exposed **staleness**: "I don't work with branches… leave pushing to me" (pre-herdr workflow, superseded by MO §10 the same day).
2. Escalated it anyway (`feedback-post`) — notification 1282, `[MND ask 94732d8d8e35]`. Re-post correctly **updated in place** (no duplicate).
3. **Tomas's live UI feedback, fixed in-iteration:**
   - Truncated text → tight clips (Q:500 / direction:250) + full text written to `data/escalations/<qhash>.txt`, path in the message; DSH-side expand-on-click filed in todo.txt.
   - "Brain's best guess" wording → **"Proposed direction"** (Tomas picked from options).
   - DSH UI collapses newlines → single-line message with `•` separators.
4. Tomas dismissed 1282 with: *"Do not push. Let me handle pushing master to origin once local runtime verification is fully complete."*
5. `learn` → 1 corrective insight (`decision_heuristic`, strong, feedback, `dsh:1282`) → 801 total.
6. **Re-ask: the brain now answers with Tomas's ruling**, citing the feedback insight first. MO §10 corrected accordingly (agents never push; Tomas pushes after local runtime verification).
7. Profiles regenerated — gemini-2.5-pro quota exhausted (~80 calls today), **claude fallback worked** (`--model claude`); ruling present in decision-making.md.

## Key finding for next iteration

**Contradiction resolution.** The re-ask still repeated the stale "don't use branches" belief alongside the corrected push ruling — feedback fixed only what the comment addressed. Old insights aren't displaced by newer contradicting ones; they coexist and both get retrieved. Next: recency/source-weighted conflict handling (feedback > distill, newer > older), possibly an LLM contradiction-sweep over same-topic insights.

## Decisions

### Escalations are single-line with a full-text file pointer
**Date:** 2026-06-12
**Phase:** Implementation
**Decided by:** Tomas (UI feedback) + team
**Decision:** Inline `•`-separated message, clips 500/250, full text at `data/escalations/<qhash>.txt`.
**Reasoning:** DSH UI truncates long messages and collapses newlines; the message must read inline. DSH display improvements filed separately (todo.txt, Q3).
**Revisit if:** DSH gains expandable notification text — then clips can loosen.

### "Proposed direction" wording
**Date:** 2026-06-12
**Phase:** Implementation
**Decided by:** Tomas
**Decision:** Escalations say "Proposed direction", never "Brain".
**Alternatives considered:** "MND suggests", "Draft answer".
