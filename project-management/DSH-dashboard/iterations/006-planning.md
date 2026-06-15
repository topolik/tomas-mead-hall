# DSH — Iteration 006: Planning (Filter & Sort Notifications)

- **Phase:** 1 — Planning
- **Phase lead:** PM
- **Start:** 2026-05-29
- **End:** (pending)
- **MOP Arm:** A (persona-driven)

---

## QA: Acceptance Criteria

_PM Standing Order: "Acceptance criteria (QA's contract) are written before the implementation checklist — always."_

### Filter

- [x] **AC-1:** GET `/notifications?type=action_needed` shows only `action_needed` notifications
- [x] **AC-2:** GET `/notifications?type=info` shows only `info` notifications
- [x] **AC-3:** GET `/notifications?project=GML` shows only GML notifications
- [x] **AC-4:** GET `/notifications?priority=Q1` shows only Q1 notifications
- [x] **AC-5:** Filters combine with AND: `?type=action_needed&priority=Q1` shows only Q1 action_needed
- [x] **AC-6:** Invalid filter values (e.g. `?priority=Q9`) are ignored — page shows all notifications, no error
- [x] **AC-7:** No filter params → shows all undismissed notifications (current behavior preserved)

### Sort

- [x] **AC-8:** GET `/notifications?sort=priority` sorts by priority Q1→Q2→Q3→Q4, then by date DESC within each group
- [x] **AC-9:** GET `/notifications?sort=date` sorts by `created_at DESC` (current default)
- [x] **AC-10:** Notifications with NULL priority sort after Q4 (last)
- [x] **AC-11:** Default sort (no `sort` param) is `date` (backwards-compatible)

### Schema & API

- [x] **AC-12:** Migration adds nullable `priority` column to `notifications` table
- [x] **AC-13:** POST `/api/v1/notifications` accepts optional `priority` field (Q1/Q2/Q3/Q4)
- [x] **AC-14:** POST with invalid priority (e.g. `"priority":"HIGH"`) returns HTTP 400
- [x] **AC-15:** POST without priority stores NULL — no error
- [x] **AC-16:** GET `/api/v1/notifications` response includes `priority` field (empty string for NULL)
- [x] **AC-17:** GET `/api/v1/notifications?priority=Q1` filters by priority (API parity with UI)

### GML

- [x] **AC-18:** GML `Notification` struct includes `Priority` field
- [x] **AC-19:** GML sends priority in POST payload alongside message, type, link
- [x] **AC-20:** GML still embeds priority icon in message text (icons are important — self-describing messages)

### UI

- [x] **AC-21:** Filter controls appear above the notification table
- [x] **AC-22:** Filter controls match the terminal/monospace aesthetic
- [x] **AC-23:** Active filters are reflected in the URL (bookmarkable)
- [x] **AC-24:** Dismiss button works with active filters (redirects back with filters preserved)

---

## Security Review

_Security Standing Order: "Assume the request is malicious. Then prove otherwise."_

### [SEC-1] Filter params must use parameterized queries
The current `NotificationsPage` uses a hardcoded SQL query. Adding filter params (type, project, priority) means building a WHERE clause dynamically. **If these are string-concatenated into SQL, it's injection.** Must use `?` placeholders and `args` slice — same pattern as `ListNotifications` in `api.go:106-113`.

**Verification:** Code review — no `fmt.Sprintf` or `+` operators in SQL strings that include user input.

### [SEC-2] Priority field validation on API input
The `CreateNotification` handler currently doesn't validate `type` beyond defaulting to "info" (the DB CHECK constraint catches bad values). The new `priority` field must be validated server-side against `{Q1, Q2, Q3, Q4, ""}`. Don't rely solely on the DB CHECK — return HTTP 400 with a clear message.

_Security Standing Order: "Any endpoint that touches user data requires authentication."_

### [SEC-3] Filter routes remain under RequireSession
Verified: `/notifications` is already behind `RequireSession` (main.go:130). The filter is just query params on the same route — no new endpoint needed. GET `/api/v1/notifications` is behind `RequireJWT` (main.go:146). **No auth gap.**

### [SEC-4] Filter form is GET, not POST — no CSRF needed
_Security Standing Order: "CSRF protection required on all state-mutating HTML form endpoints."_
The filter form submits via GET (read-only operation). CSRF protection is not required. The dismiss button (POST) already has CSRF. **No gap.**

---

## PM: Implementation Checklist

### DSH Backend

- [x] 1. Migration `007_notification_priority.sql`: `ALTER TABLE notifications ADD COLUMN priority TEXT DEFAULT NULL`
- [x] 2. Add DB CHECK constraint? No — nullable column with app-level validation is simpler. Existing CHECK on `type` column is already in 001_init.sql; adding one for priority would require matching the NULL case. Validate in Go.
- [x] 3. Update `model.Notification`: add `Priority` field (nullable — use `sql.NullString` or scan into `string` with COALESCE)
- [x] 4. Update `CreateNotification` in `api.go`: accept `priority` field, validate Q1/Q2/Q3/Q4/empty, INSERT with priority
- [x] 5. Update `ListNotifications` in `api.go`: include `priority` in SELECT, add `priority` filter param support
- [x] 6. Update `NotificationsPage` in `ui.go`: parse query params (`type`, `project`, `priority`, `sort`), build filtered/sorted SQL query with parameterized WHERE clause
- [x] 7. Update `NotificationDismiss` in `ui.go`: after dismiss, redirect back to `/notifications` with filter params preserved via a hidden `_return` form field (not Referer — can be stripped by browsers/proxies)

### DSH Frontend

- [x] 8. Update `notifications.html`: add filter form (dropdowns for type, project, priority; sort selector) above the table
- [x] 9. Filter form uses GET method, pre-selects current filter values from query params
- [x] 10. Style filter controls to match terminal aesthetic

### GML

- [x] 11. Add `Priority` field to `Notification` struct in `dsh.go`
- [x] 12. Set `Priority` in `ToNotifications()` in `validate.go`
- [x] 13. Update GML tests to verify priority is set in notification output

### Tests (TDD — written before implementation)

_QA Standing Order: "Test requirements are written in Phase 1 before any implementation — no exceptions."_

- [x] 14. Write `handler/notifications_filter_test.go`:
  - Filter by type only (AC-1, AC-2)
  - Filter by project only (AC-3)
  - Filter by priority only (AC-4)
  - Combined filters (AC-5)
  - Invalid filter values ignored (AC-6)
  - No filter params → all notifications (AC-7)
  - Sort by priority with NULL last (AC-8, AC-10)
  - Sort by date (AC-9)
  - Default sort is date (AC-11)
- [x] 15. Write API test cases in `integration_test.go`:
  - POST with valid priority → stored and returned (AC-13, AC-16)
  - POST with invalid priority → HTTP 400 (AC-14)
  - POST without priority → NULL stored, no error (AC-15)
  - GET with priority filter (AC-17)
  - SQL injection attempt in filter params → parameterized, no injection (SEC-1)
- [x] 16. Write GML test: verify `ToNotifications()` includes `Priority` field (AC-18, AC-19)

### Manual Verification (after implementation)

- [x] 17. Run DSH with migration — verify existing notifications still display (NULL priority)
- [x] 18. Post notifications via API with priority — verify filter and sort in live UI
- [x] 19. Run GML → verify new notifications arrive with priority field populated
- [x] 20. Test dismiss with active filters — should return to filtered view
- [x] 21. Test all filter combinations in live UI against real data

---

## QA Concerns

### [QA-1] NULL priority sort ordering
_QA Standing Order: "Never sign off on a feature that hasn't been tested against real data."_
The live DB has existing notifications with no priority. The SQL `ORDER BY priority` puts NULLs first in SQLite by default. We need `ORDER BY priority IS NULL, priority, created_at DESC` or use `COALESCE(priority, 'ZZ')` to push NULLs to the end. **Must test with real DB data, not just fresh inserts.**

### [QA-2] Filter dropdown values must come from actual data
_QA Standing Order: "Acceptance criteria are observable and verifiable."_
The project filter dropdown needs to show only projects that have notifications, not all projects. Otherwise you get empty results for projects that never sent notifications. Query: `SELECT DISTINCT project_code FROM notifications WHERE dismissed_at IS NULL AND project_code IS NOT NULL`.

### [QA-3] Edge case: all notifications dismissed
If Tomas dismisses everything with a filter active, the page should show "No pending notifications" (existing behavior), not a broken empty table. The filter form should still render.

---

## PM Concerns

### [PM-1] Two-project coordination
This plan touches both DSH and GML codebases. The changes must be deployed in order:
1. DSH migration + API change first (new field is optional, backwards-compatible)
2. GML struct change + Docker rebuild second (starts sending priority)

If GML sends priority before DSH has the column, the field is silently ignored (Go's JSON decoder ignores unknown fields, and the INSERT doesn't include the column). **No breakage, but no benefit either.** Deploy order matters for correctness, not safety.

### [PM-2] Scope boundary
_PM Standing Order: "Scope changes after planning starts require Tomas's explicit sign-off."_
The following are NOT in scope and must not creep in during implementation:
- Bulk dismiss (dismiss all Q4, dismiss all from project)
- Notification badge count in nav header
- Comment/annotation field (Arm B feature)
- Push notifications

---

## Architecture Diagram

Skipped — this feature adds filter params and one DB column. No new services, no new data flows, no architectural change. Per MOP 001-ideation P3: "overkill for simple projects."

## Cross-Persona Conflicts

No genuine cross-persona conflicts emerged in this plan. Security, QA, and PM concerns were complementary rather than contradictory. This is an honest datapoint for the MOP experiment — the feature's scope is small enough that the domains don't clash.

---

## Decisions

### [DSH-006-001] NULL priority sorts last
**Date:** 2026-05-29
**Phase:** Planning
**Decided by:** QA + PM
**Decision:** Notifications with NULL priority sort after Q4 when sorting by priority. Use `ORDER BY priority IS NULL, priority` in SQL.
**Alternatives considered:** COALESCE to 'Q5', treating NULL as Q2 default
**Reasoning:** Tomas said "null priority go at the end." Using SQLite's boolean expression (`priority IS NULL`) as first sort key is cleaner than COALESCE hacks.
**Revisit if:** Other agents start sending notifications without priority and Tomas wants them sorted differently.

### [DSH-006-002] Keep icons in message text
**Date:** 2026-05-29
**Phase:** Planning
**Decided by:** Analyst (from Tomas's input)
**Decision:** GML continues embedding priority icons (🔴🟡🔵⚪) in the message text even though priority is now a structured field.
**Alternatives considered:** Strip icons from message, display from priority column
**Reasoning:** Tomas: "icons are important." Self-describing messages are valuable — they make sense in API responses, logs, and anywhere the message is displayed outside DSH's UI.
**Revisit if:** Icons in message + icons from priority column create visual duplication in the UI.

### [DSH-006-003] Validate priority in Go, not DB CHECK
**Date:** 2026-05-29
**Phase:** Planning
**Decided by:** Developer
**Decision:** Validate priority values (Q1/Q2/Q3/Q4 or empty) in the Go handler, not via a DB CHECK constraint.
**Alternatives considered:** DB CHECK like the `type` column
**Reasoning:** The `type` CHECK in 001_init.sql doesn't handle NULL. Adding a CHECK that allows NULL + Q1-Q4 is awkward in SQLite ALTER TABLE. Go validation returns a clear HTTP 400; DB CHECK returns a generic constraint error. Security reviewed and accepted: the single write path (`CreateNotification` handler) means validation can't be bypassed — there's no second code path that inserts notifications without going through the handler.
**Revisit if:** Multiple code paths write notifications and validation might be missed.
