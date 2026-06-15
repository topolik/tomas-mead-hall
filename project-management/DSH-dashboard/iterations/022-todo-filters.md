# DSH — Iteration 022: todo.txt screen — filters, sort, status badges, bulk actions

- **Phase:** Implementation (single-doc iteration, mirrors an existing screen)
- **Phase lead:** Developer
- **Start:** 2026-06-12
- **End:** 2026-06-12

---

## Trigger / scope

Tomas: "I need filters for todo.txt in the UI similar to other screens, let's say
notifications. Also include filters/badges for status and checkboxes for bulk
actions." So: bring the notifications-screen UX to the todo list.

Requirements:
- Filter by **priority** (Q1–Q4), **status** (open/in_progress/done/parked), and
  **free-text** — include/exclude (`!`) semantics, same as the notifications message filter.
- **Status badges**: each status filter shows its live count across all items.
- **Sort**: priority (default), status, added date.
- **Bulk actions**: per-row checkboxes + select-all → Mark Done / Park / Delete Selected.

## Design

- **Flat, filterable table** replaces the previous group-by-quadrant layout — the
  notifications screen is flat, and bulk-select needs one selectable list. Priority
  is now a colored column (`pri-Q1`…`pri-Q4`) so the quadrant is still obvious; the
  default sort is by priority, so it reads grouped without separate tables.
- **Filtering happens in memory**, not SQL: todos live in `todo.txt`, not the DB.
  Added `filterSpec.Accept` (exact include/exclude) and `filterSpec.AcceptLike`
  (case-insensitive substring, OR-includes) as the in-memory mirror of the existing
  `applySQL` / `applyLikeSQL`. Same `parseFilter` and same `Raw()`/`Has()`/`IsNegated()`
  helpers drive both screens, so the filter-panel template + JS are a direct port.
- **Bulk ops are one atomic read-modify-write** (`todoreader.BulkDelete`,
  `BulkSetStatus`). Delete builds a deletion set (item line + its continuation
  lines) and rewrites the file once — so the line-index-as-ID scheme doesn't suffer
  the index-shifting bug a delete-one-at-a-time loop would hit.
- All row actions and bulk actions carry a validated `_return` so an action keeps
  you on your filtered view instead of bouncing to bare `/todo`.

## Changes

- `internal/todoreader`: `BulkDelete([]int)`, `BulkSetStatus([]int, status)`.
- `internal/handler/ui.go`: `filterSpec.Accept` / `AcceptLike`; `TodoPage` rewritten
  (parse filters → count statuses → filter → sort → render); `sortTodos`,
  `buildTodoFilterQuery`, `TodoBulk`, `redirectTodo`; row handlers honor `_return`.
- `cmd/dsh/main.go`: `POST /todo/bulk` (session + CSRF). More specific than
  `POST /todo/{id}`, so Go's mux routes it first — verified no panic on boot.
- `web/templates/todo.html`: rewritten — filter `<details>` panel (priority/status/
  text/sort with NOT toggles and live status counts), bulk form + Mark Done / Park /
  Delete Selected with select-all, flat colored table.
- `web/static/style.css`: `.pri-Q*` and `.st-*` colors.

## Testing

- `internal/todoreader`: `BulkDelete` (multi-select + continuation lines survive on
  non-deleted items), `BulkSetStatus` (only selected change).
- `internal/handler`: `TodoPage` text filter (matches kept, others dropped, counts
  show full picture, "Showing 2 of 4"), status filter, `TodoBulk` delete + mark-done
  — all render/operate against the real template and a temp `todo.txt`.
- Full suite: **68 tests pass** across 9 packages; `go build ./...` clean.
- Smoke: binary boots (no route panic), `/api/v1/health` 200, `/todo` + `/todo/bulk`
  both session-protected (302 → /setup unauthenticated).

## Decisions

### [Decision: flat filterable table replaces group-by-quadrant on the todo screen]
**Date:** 2026-06-12 · **Phase:** Implementation · **Decided by:** Developer
**Decision:** Render one flat, sortable, filterable table (priority as a colored
column) instead of four per-quadrant tables.
**Alternatives considered:** Keep the quadrant grouping and bolt a filter bar on top.
**Reasoning:** Parity with the notifications screen (the stated model), and bulk
select-all/checkboxes need a single selectable list. Default priority sort preserves
the quadrant reading order without the rigid separate-table structure.
**Revisit if:** Tomas prefers the quadrant headers back (could group within the
filtered+sorted result).

### [Decision: filter todos in memory, reusing the notifications filterSpec]
**Date:** 2026-06-12 · **Phase:** Implementation · **Decided by:** Developer
**Decision:** Add `Accept`/`AcceptLike` to `filterSpec` and filter the loaded slice,
rather than moving todos into SQLite to reuse `applySQL`.
**Alternatives considered:** Ingest `todo.txt` into a DB table and filter via SQL.
**Reasoning:** `todo.txt` is the git-tracked source of truth; a DB mirror adds a sync
problem for no gain at this scale (tens of items). One `filterSpec` type now serves
both screens with identical include/exclude semantics.
**Revisit if:** The backlog grows large enough that in-memory filtering is slow
(not foreseeable for a personal todo file).
