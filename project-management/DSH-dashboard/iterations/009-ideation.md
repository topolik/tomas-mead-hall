# DSH — Iteration 009: Ideation (Notification Comment Field)

- **Phase:** 0 — Ideation
- **Start:** 2026-05-29
- **MOP Arm:** B (principles-only)

---

## Origin

From todo.txt:
> DSH: Notifications should have a comment field so that I can update it. Comment can be later used to re-learn or improve the respective tool (GML) to better understand my intents and what is more important and what is less or other course of actions I took. For example — replied on JIRA ticket, ignoring this email — not important, this email is more important → should be recategorized, replied on an email, ...

## Scope

Add an editable plain-text comment to each notification. The comment is:
- User input — typed by Tomas in the UI
- Persisted in DB alongside the notification
- Exposed via API so agents (GML) can read it later for learning
- Editable on both active and dismissed notifications (the learning value persists after dismiss)

## Key Defaults (recorded as decisions below)

1. **1:1 model** — one comment per notification, not a thread. KISS.
2. **Inline edit** — textarea directly in the notification row, auto-saves. No separate edit page.
3. **Plain text** — no markdown, no rich text. The content is primarily for machine consumption (GML re-learning).
4. **No length limit in DB** — TEXT column. Go handler enforces a reasonable max (e.g., 2000 chars) to prevent abuse.
5. **Comments survive dismiss** — dismissed notifications keep their comments. Accessible via API.

## Out of Scope

- Comment history / versioning
- Multiple comments per notification
- Comment-based notifications or alerts
- GML consuming comments (that's a GML feature, not DSH)

## Changes Required

1. **Migration 008**: `ALTER TABLE notifications ADD COLUMN comment TEXT NOT NULL DEFAULT '';`
2. **Model**: add `Comment string` to `Notification` struct
3. **UI handler**: new `POST /notifications/{id}/comment` endpoint — accepts form `comment` field, updates DB, redirects back
4. **UI template**: add textarea per notification row with save button
5. **API handler**: include `comment` in `ListNotifications` response; accept optional `comment` in `CreateNotification`
6. **Tests**: comment CRUD, XSS in comment display, SQL injection in comment input, comment persists after dismiss

## Decisions

### [DSH-018] One comment per notification (1:1)
**Date:** 2026-05-29
**Phase:** Ideation (009)
**Decided by:** Developer (default)
**Decision:** Single editable comment field per notification, not a thread.
**Alternatives considered:** 1:N comment thread with timestamps
**Reasoning:** KISS. The use case is annotation, not conversation. A thread adds a join table and UI complexity with no stated need.
**Revisit if:** Tomas wants to track changes over time or multiple actors comment.

### [DSH-019] Inline auto-save comment
**Date:** 2026-05-29
**Phase:** Ideation (009)
**Decided by:** Developer (default)
**Decision:** Comment is edited inline via a textarea in the notification row. Saves on blur or button click.
**Alternatives considered:** Separate edit page (like todo edit), modal dialog
**Reasoning:** Minimizes clicks. Comments are short annotations — a full page is overkill. Auto-save on blur matches the "quick annotation" workflow.
**Revisit if:** Comment editing needs more structure (formatting, preview).

### [DSH-020] Comments persist after dismiss
**Date:** 2026-05-29
**Phase:** Ideation (009)
**Decided by:** Developer (default)
**Decision:** Dismissed notifications retain their comments. API returns comments on dismissed notifications too.
**Alternatives considered:** Clear comment on dismiss; hide dismissed from API
**Reasoning:** The stated purpose is "re-learn / improve GML" — that learning happens after the notification is handled. Clearing on dismiss destroys the value.
**Revisit if:** Storage growth becomes a concern (unlikely at current volume).
