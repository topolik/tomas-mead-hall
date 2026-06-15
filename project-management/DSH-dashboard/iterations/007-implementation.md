# DSH — Iteration 007: Implementation (Filter & Sort Notifications)

- **Phase:** 2 — Implementation
- **Phase lead:** Developer
- **Start:** 2026-05-29
- **End:** (pending)
- **MOP Arm:** A (persona-driven)

---

## Changes Made

### Migration
- `007_notification_priority.sql`: adds nullable `priority TEXT` column to `notifications`

### DSH Backend (`internal/handler/`)
- **`api.go`**: `CreateNotification` validates priority (Q1-Q4 or empty, rejects invalid with HTTP 400). `ListNotifications` includes priority in SELECT, supports `priority`, `type` filter params.
- **`ui.go`**: `NotificationsPage` parses query params (`type`, `project`, `priority`, `sort`), builds parameterized WHERE clause. Sort by priority uses `ORDER BY priority IS NULL, priority, created_at DESC` (NULLs last). `NotificationDismiss` preserves filters via `_return` hidden field. `buildFilterQuery` generates query string for template use.
- **`model.go`**: `Notification` struct gains `Priority string` field.

### DSH Frontend
- **`notifications.html`**: Filter form with dropdowns (type, project, priority, sort) above table. Priority options show colored icons. Dismiss form includes hidden `_return` field. Clear button shown when filters active.

### GML
- **`dsh.go`**: `Notification` struct gains `Priority string` field (sent in API payload).
- **`validate.go`**: `ToNotifications` sets `Priority: r.Priority` on each notification. Icons still embedded in message text.

### Tests
- DSH integration tests: 5 new tests (priority POST valid/invalid/missing, filter combinations, SQL injection in filter params)
- GML unit tests: priority field verified in `TestToNotifications` and `TestToNotifications_PriorityMapping`

## Run Log

1. Both projects compile clean
2. DSH integration tests: all pass (including 5 new priority/filter tests)
3. GML tests: all pass (20 tests in notify package)
4. DSH container rebuilt, migration 007 applied on startup
5. API verification: POST with priority stores Q1, GET filters by priority/type/project correctly, invalid priority ignored in filter (returns all)
6. GML container rebuilt — next scheduled run will send priority field

## Bugs Found

None during implementation.
