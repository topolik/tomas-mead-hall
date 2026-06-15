# DSH — Iteration 011: Implementation (Notification Comment Field)

- **Phase:** 2 — Implementation
- **Start:** 2026-05-29
- **MOP Arm:** B (principles-only)

---

## Run Log

1. Created migration `008_notification_comment.sql` — `ALTER TABLE ADD COLUMN comment TEXT NOT NULL DEFAULT ''`
2. Added `Comment string` to `model.Notification`
3. Updated `NotificationsPage` SELECT and Scan to include comment
4. Added `NotificationComment` handler — POST endpoint with 2000 char limit, CSRF, filter preservation via `_return`
5. Registered route `POST /notifications/{id}/comment` with session auth + CSRF
6. Updated `notifications.html` — textarea + [Save] button per notification row, Comment column header
7. Updated `CreateNotification` API — accepts optional `comment` field, validates 2000 char limit
8. Updated `ListNotifications` API — returns `comment` field in JSON response
9. Wrote 5 integration tests: CommentInAPIResponse, CommentEmptyByDefault, CommentMaxLength, CommentSQLInjection, CommentExactlyAtLimit
10. All 27 tests pass (22 existing + 5 new)
11. Docker build + deploy — migration 008 applied, API verified with curl

## Bugs Found

None during implementation.

## Files Changed

- `internal/db/migrations/008_notification_comment.sql` — new migration
- `internal/model/model.go` — Comment field added to Notification struct
- `internal/handler/ui.go` — NotificationComment handler, updated SELECT/Scan
- `internal/handler/api.go` — comment in CreateNotification + ListNotifications
- `cmd/dsh/main.go` — new route registration
- `cmd/dsh/web/templates/notifications.html` — Comment column with textarea
- `cmd/dsh/integration_test.go` — 5 new tests
