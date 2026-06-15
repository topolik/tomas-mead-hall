# DSH — Iteration 024: Ideation — Threads (L32 portal re-scope + L33 discussions, merged)

- **Phase:** Ideation
- **Phase lead:** Developer
- **Start:** 2026-06-12
- **End:** 2026-06-12

---

## Trigger

Tomas on backlog L32 (DSH portal: inter-agent messaging, shared todo API, shared
context): *"I think we are half way there but then I switched to herdr. Now I
don't know what / how to continue."*

## Current-state inventory (the "half way")

Already built — agents already use DSH as a platform for:

| Capability | Status |
|---|---|
| Shared notifications + comment feedback loop | ✅ `POST/GET/PATCH /api/v1/notifications`; GML + MND post, Tomas's dismissal comments feed MND retraining |
| Shared todo API | ✅ `GET/POST /api/v1/todos` with dedup (GML distill posts) |
| Plans propose→approve→execute | ✅ incl. LLM conflict merge |
| Projects registry, OAuth2 agent auth, audit log, push | ✅ |

What herdr + MND took over since L32 was written: **live inter-agent direction**.
Blocked agent → MND watch mode reads the pane → answers in Tomas's style →
escalates to DSH only on failure. Agents are CLI processes in panes; they don't
poll DSH mid-task. The synchronous half of "inter-agent messaging" is done —
just in herdr, not DSH.

## What's genuinely missing (the other half)

1. **No durable agent↔agent channel.** Pane text is ephemeral and 1:1; the MO
   worktree rule (never touch a sibling worktree) means GML cannot tell MND
   anything today except via a notification aimed at Tomas.
2. **No threads.** Notification comments are a single field, not a discussion —
   that's exactly backlog L33.
3. **A consumer is already waiting:** GML iteration 013-review documented
   *"distill reprocesses same dismissed insights — acceptable until discussions
   project provides 'processed' tracking."*

## Re-scope decision

**herdr = live channel, DSH = durable channel.** The remaining build is L32+L33
merged into one feature: **DSH Threads** — M:N threaded discussions, attachable
to notifications/plans, posted by human + agents via API, browsable in the UI.

First consumer (drives the real-data acceptance test): **GML processed-tracking**
— distill checks for a resolved thread on a dismissed insight notification and
skips re-processing it.

## Iteration-1 shape (planning will firm this up)

- Schema: `threads` (subject, optional ref to notification/plan, status
  open/resolved, created_by) + `thread_messages` (thread, author, body).
- API: create thread (with first message), list/filter threads by ref + status,
  get thread with messages, post message, resolve/reopen.
- UI: `/threads` list + thread detail with reply form; `[discuss]` / thread badge
  on notification rows.
- GML's own distill-side change = follow-up GML iteration (different project's
  code; DSH ships the API + UI and a GML-shaped acceptance test).

## Explicitly out of iteration-1 scope

- **LLM cross-linking agent** (auto-grouping related notifications into threads,
  from L33) — later iteration, needs the manual thread mechanics proven first.
- **Per-agent unread/inbox semantics** — defer until an agent↔agent message
  actually exists; GML's consumer needs ref+status lookup, not an inbox.
- **Shared context store** (K/V document API) — rejected for now; `knowledge/`
  + master merges cover it and there is no concrete consumer.

## Priority

Q2 (Important, Not Urgent) — as on the backlog.

---

## Decisions

### [Decision: re-scope L32 — herdr owns live direction; DSH builds the durable half as Threads (L32+L33 merged)]
**Date:** 2026-06-12 · **Phase:** Ideation · **Decided by:** Tomas
**Decision:** Merge L32's remaining gap and L33 into one DSH Threads feature:
durable M:N threaded messages attachable to notifications/plans, human + agents
post via API, per the gap map above.
**Alternatives considered:** (a) Shared context store first — rejected: no
concrete consumer, `knowledge/` covers it; (b) Park L32 entirely — rejected: a
real consumer (GML processed-tracking) is documented and waiting; (c) Rebuild
live messaging in DSH — rejected: herdr+MND already do it better for pane-bound
CLI agents.
**Reasoning:** Everything still missing from L32 (durable messaging, discussions,
processed-state) is one threads schema away; the live half is already shipped in
herdr/MND.
**Revisit if:** Agents move off herdr panes, or a use case needs push-style
delivery to agents rather than poll/lookup.

### [Decision: GML processed-tracking is the first consumer]
**Date:** 2026-06-12 · **Phase:** Ideation · **Decided by:** Tomas
**Decision:** The acceptance test for threads iteration 1 is the GML distill
flow: a dismissed insight notification with a resolved thread is skipped on
re-distill.
**Alternatives considered:** MND escalation threads (works already via
notification comments); generic agent↔agent inbox (no real message to send yet).
**Reasoning:** It's a documented, real limitation with real data (dismissed
insights in the live DSH DB) — satisfies "run it or it doesn't count" without
inventing synthetic flows.
**Revisit if:** GML's distill is retired or moves off DSH notifications.
