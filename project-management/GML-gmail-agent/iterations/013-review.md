# GML — Iteration 013: Review

- **Phase:** 3 — Review
- **Reviewer:** Tomas
- **Date:** 2026-05-31

---

## What Was Reviewed

Knowledge distillation pipeline + DSH todo API + action-item routing.

### Live Test Results

**Distill pipeline:**
1. Tomas dismissed 2 insight notifications with comments via DSH web UI
2. `./run.sh distill` ran successfully:
   - Gathered 2 dismissed insights with comments
   - Gemini produced 2 patterns + 1 todo
   - `knowledge.yaml` created with: Atlassian (refined — keep ones that mention Tomas), Salesforce (rejected — needs better SOP)
   - Todo posted to DSH: "Connect Salesforce alerts to Google SecOps and improve SOP reactivity" (Q3)

**Learn pipeline (with knowledge context):**
1. `./run.sh learn --days 7` ran successfully with knowledge context:
   - 17 senders, 16 dismissed notifications with comments, 2 knowledge patterns included in prompt
   - 8 new insights posted — no duplicates of Atlassian/Salesforce (knowledge base respected)

### Bugs Found During Live Test

1. **DSH container running stale code** — `has_comment` API filter didn't exist in deployed image. `distill-gather` got all notifications (most with empty comments), LLM saw no actionable data, returned empty arrays. Fixed by rebuilding DSH Docker image.
2. **knowledge.yaml permission denied** — container runs as uid 999, file owned by uid 1002 with mode 644. Fixed: `run.sh distill` now `chmod 666` before mounting.

### Feedback & Future Direction

- **Learn duplicating insights** — root cause was same stale DSH container (previous insights not returned properly). Fixed by rebuild. LLM-prompt-based dedup is "good enough" per MO principles.
- **Distill reprocesses same dismissed insights** — known limitation; same 2 dismissed insights produce same output on re-run. Acceptable until discussions project provides "processed" tracking.
- **DSH discussions vision** — Tomas wants M:N threaded discussions linked to notifications. Human + agents post messages. LLM agent cross-links related notifications into shared threads. Captured in todo.txt as separate future project.
- **DSH portal vision** — DSH as connecting portal for all agents: shared notifications, todos, inter-agent messaging. Also captured in todo.txt.

### Verdict

Ship it. Both pipelines working end-to-end with real data. Known limitations documented and accepted.

---

## Decisions

### Distill processes dismissed-only comments (for now)
**Date:** 2026-05-31
**Phase:** Review
**Decided by:** Tomas
**Decision:** `distill-gather` only processes dismissed insight notifications with comments. Active notifications are not processed.
**Alternatives considered:** Process all commented notifications (requires "processed" marker to avoid re-processing — leads to discussions system which is a separate project).
**Reasoning:** Good enough for now. Discussions project will supersede this.
**Revisit if:** Discussions project starts, or the dismissed-only workflow becomes a bottleneck.

### LLM-based dedup is sufficient for learn
**Date:** 2026-05-31
**Phase:** Review
**Decided by:** Tomas
**Decision:** No code-side dedup for learn insights. Previous insights + knowledge base in prompt is sufficient.
**Alternatives considered:** Code-side dedup by gmail_search/Link URL (catches exact matches but not semantic near-duplicates).
**Reasoning:** LLM output varies each run — exact string matching can't catch semantic duplicates. Two-layer approach (prompt context + knowledge base) is good enough per MO principles.
**Revisit if:** Duplicate noise becomes a real problem in practice.
