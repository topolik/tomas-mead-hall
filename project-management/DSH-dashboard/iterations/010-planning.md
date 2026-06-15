# DSH — Iteration 010: Planning (Notification Comment Field)

- **Phase:** 1 — Planning
- **Start:** 2026-05-29
- **MOP Arm:** B (principles-only)

---

## Acceptance Criteria

1. Notifications have a `comment` column (TEXT, NOT NULL, default empty string)
2. UI shows a textarea for each notification's comment
3. Saving a comment updates the DB and redirects back with filters preserved
4. Comment endpoint is CSRF-protected and session-authenticated
5. Comment is HTML-escaped in display (Go html/template default — verify no `| safe` or similar bypass)
6. Comment length enforced in Go handler (max 2000 chars, returns HTTP 400 if exceeded)
7. API `GET /api/v1/notifications` includes `comment` field in response
8. API `POST /api/v1/notifications` accepts optional `comment` field
9. Comments persist when notification is dismissed
10. SQL uses parameterized queries for comment writes (no string interpolation)
11. Empty comment is valid (clears previous comment)
12. Comment works on both active and dismissed notifications (but UI only shows active by default)

## Security Considerations

- **XSS**: Comment is user input displayed in HTML. Go `html/template` auto-escapes `{{.Comment}}`. Verify no raw/safe bypass.
- **SQL injection**: Parameterized `UPDATE ... SET comment=? WHERE id=?`. No string interpolation.
- **CSRF**: POST endpoint must go through `CheckCSRF` middleware (same as dismiss).
- **Input size**: 2000 char limit prevents oversized payloads. No binary/file upload.
- **Authorization**: Only session-authenticated users can edit comments. API clients (JWT) can read but not write comments (write is a UI-only action for now).

## Test Requirements

1. `TestNotification_CommentCRUD` — create notification via API, update comment via API or direct DB, verify comment in list response
2. `TestNotification_CommentPersistsAfterDismiss` — add comment, dismiss, verify comment still in DB
3. `TestNotification_CommentMaxLength` — send >2000 chars, expect HTTP 400
4. `TestNotification_CommentEmptyValid` — set comment, then clear it (empty string), verify cleared
5. `TestNotification_CommentSQLInjection` — comment containing `'; DROP TABLE notifications; --`, verify stored literally
6. `TestNotification_CommentInAPIResponse` — verify `comment` field present in GET /api/v1/notifications

## Implementation Checklist

1. [ ] Create migration `008_notification_comment.sql`
2. [ ] Add `Comment string` to `model.Notification`
3. [ ] Update `NotificationsPage` query to SELECT comment
4. [ ] Update `NotificationsPage` Scan to include comment
5. [ ] Add `NotificationComment` handler (POST /notifications/{id}/comment)
6. [ ] Register route with session auth + CSRF
7. [ ] Update notifications.html template — add textarea and save button per row
8. [ ] Update `CreateNotification` API to accept optional comment
9. [ ] Update `ListNotifications` API to return comment field
10. [ ] Write integration tests
11. [ ] Build and run — verify in browser
12. [ ] Test: add comment, reload, verify persists
13. [ ] Test: dismiss notification with comment, verify comment in DB
14. [ ] Test: XSS attempt in comment field — verify escaped in rendered HTML
