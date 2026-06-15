# DSH — Iteration 005: Ideation (Filter & Sort Notifications)

- **Phase:** 0 — Ideation
- **Phase lead:** Analyst
- **Start:** 2026-05-29
- **End:** (pending)
- **MOP Arm:** A (persona-driven) — see `project-management/MOP-modus-operandi/iterations/002-experiment.md`

---

## Context

The notifications page (`/notifications`) currently displays a flat, reverse-chronological list of all undismissed notifications. With GML running on a schedule and producing multiple notifications per run, the list grows quickly. Tomas needs to:
- Focus on what matters (Q1/Q2 items) without scanning past info-level noise
- Find notifications from a specific project
- Sort by priority instead of just arrival time

## What Exists Today

- **DB schema:** `notifications` table has `id`, `project_code`, `message`, `type` (enum: `action_needed`, `info`), `link`, `created_at`, `dismissed_at`
- **UI:** Table with columns: Type, Project, Message, When, Dismiss
- **Query:** `SELECT ... WHERE dismissed_at IS NULL ORDER BY created_at DESC LIMIT 500`
- **GML embeds priority in message text:** e.g. `🔴 Q1 [Box 1 — Security] ...` — the Eisenhower priority (Q1-Q4) is part of the message string, not a structured field

## The Schema Question

**This is the central design decision.** Priority lives in the message text, not as a DB column. Two paths:

### Option A: Add a `priority` column to `notifications`

- New migration: `ALTER TABLE notifications ADD COLUMN priority TEXT DEFAULT NULL`
- GML already knows the priority — post it as a field in the API payload
- API change: accept `priority` in `POST /api/notifications`
- Enables clean SQL filtering: `WHERE priority = 'Q1'` and `ORDER BY priority`
- Backwards-compatible: existing notifications get `NULL` priority (treated as unclassified)
- Other agents that don't use Eisenhower can omit priority → still works

### Option B: Parse priority from message text

- Regex on `^(🔴|🟡|🔵|⚪)\s*Q[1-4]` to extract priority at display time
- No migration, no API change
- Fragile: breaks if GML changes message format
- Can't sort in SQL — must fetch all, parse, sort in Go
- Won't work for non-GML notifications that don't embed priority icons

### Option C: Client-side filtering only

- JavaScript hides/shows rows based on text content
- Zero backend changes
- Breaks pagination, doesn't reduce DB load, feels hacky
- Counter to DSH's server-rendered HTMX approach

**Analyst's assessment:** Option A is the right path. It's a small migration, aligns GML's existing priority data with the schema, and makes filtering/sorting trivial in SQL. The API change is additive (new optional field). Option B is a trap — it couples the UI to GML's message format. Option C contradicts the dashboard's architecture.

## Scope Definition

**In scope:**
- Filter notifications by: type (action_needed/info), project_code, priority (Q1-Q4)
- Sort notifications by: date (default), priority, type
- DB migration to add `priority` column
- API change to accept `priority` field
- GML change to send `priority` in the API payload (in addition to embedding it in the message)
- UI filter controls (dropdowns/selects above the notification table)

**Out of scope** (separate todo.txt items):
- Comment/annotation field on notifications (Arm B feature)
- Push notifications to phone
- Timezone display
- Delete todo items, notification badge, project drill-down
- Dismiss-all / bulk operations

## Open Questions

1. **Filter persistence:** Should the selected filters persist across page loads (URL query params) or reset each time? Query params seem natural and allow bookmarking.
2. **Combined sorting:** If sorting by priority, what's the tiebreaker? Priority → date (newest first) seems right.
3. **Null priority display:** Existing notifications have no priority. Show them as "—" or "unclassified"? Filter them into a catch-all bucket?
4. **Filter UI placement:** Inline above the table (HTMX form) or sidebar? Inline matches the dashboard's terminal aesthetic.
5. **GML migration:** Should GML stop embedding icons in the message text once priority is a real field, or keep both? Keeping both means the message is self-describing even outside DSH.

## Alternatives Explored

| Alternative | Why rejected |
|------------|-------------|
| Full-text search | Overkill — Tomas knows the filter dimensions (project, type, priority). Search is for when you don't know what you're looking for. |
| Tabbed view (one tab per project) | Too rigid — what if Tomas wants to see Q1 across all projects? Filters are more flexible. |
| Auto-dismiss old info notifications | Useful but orthogonal — this is about visibility, not lifecycle. Could be a future feature. |
| Priority stored as integer (1-4) | Text "Q1"-"Q4" is more readable in DB and consistent with the rest of the system (todo.txt, PROJECT.md). |

---

## Decisions

### [DSH-005-001] Add priority column to notifications
**Date:** 2026-05-29
**Phase:** Ideation
**Decided by:** Analyst
**Decision:** Add a `priority` column (nullable TEXT) to the `notifications` table rather than parsing priority from message text.
**Alternatives considered:** Parse from message regex, client-side filtering
**Reasoning:** Structured data enables SQL-level filtering and sorting. Nullable column is backwards-compatible with existing notifications. Decouples the UI from GML's message format.
**Revisit if:** We decide notifications should be fully schema-free (unlikely given DSH's structured approach).

### [DSH-005-002] Filter via URL query parameters
**Date:** 2026-05-29
**Phase:** Ideation
**Decided by:** Analyst
**Decision:** Filters applied via URL query params (`?type=action_needed&priority=Q1&project=GML`), which the server uses to build the SQL WHERE clause.
**Alternatives considered:** Session-stored filters, JavaScript-only filtering
**Reasoning:** Query params are bookmarkable, shareable, stateless, and work with the server-rendered HTMX model. No session state to manage.
**Revisit if:** Filter combinations become complex enough to warrant saved views.
