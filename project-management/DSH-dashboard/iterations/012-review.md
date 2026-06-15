# DSH — Iteration 012: Review (Notification Comment Field)

- **Phase:** 3 — Presentation & Review
- **Start:** 2026-05-29
- **End:** 2026-05-29
- **MOP Arm:** B (principles-only)

---

## What was presented

Editable comment field on each notification. Click-to-edit inline textarea, 2000 char limit, CSRF-protected, comment persists after dismiss, exposed via API for future GML re-learning.

## Tomas's feedback

### Round 1: Comment renders as white input box
**Request:** Comment should render as text, not a textarea. Only become editable on click.
**Fix:** Replaced always-visible textarea with click-to-edit pattern: plain text display → click → textarea + Save/Cancel.

### Round 2: Empty comment placeholder too small
**Request:** The `+` sign for empty comments is too small and hard to click.
**Fix:** Changed from dim `+` to styled `[+ comment]` in blue (#58a6ff), matching existing UI button conventions.

## Scorecard: Planning vs. Review

| What | Count |
|------|-------|
| Planning issues flagged | 5 (XSS, SQL injection, CSRF, input size, authorization) |
| False alarms in planning | 0 |
| Review feedback from Tomas | 2 rounds, 2 items |
| ...that planning should have caught | 2 (both are UX — text display vs input, click target size) |
| Bugs found during implementation | 0 |
| Bugs found during review | 0 (both items were UX preferences, not bugs) |
| Security-related bugs | 0 |
| Test requirements → real bugs | 0/5 (tests confirmed correctness) |
| Planning overhead | Low (12 ACs, 5 concerns, 0 formal decisions beyond ideation) |
| Tomas satisfaction | (to be scored by Tomas) |

## Decisions

No new decisions during review. Existing decisions DSH-018 through DSH-020 from ideation confirmed.
