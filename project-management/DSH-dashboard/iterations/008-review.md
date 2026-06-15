# DSH — Iteration 008: Review (Filter & Sort Notifications)

- **Phase:** 3 — Presentation & Review
- **Phase lead:** PM
- **Start:** 2026-05-29
- **End:** 2026-05-29
- **MOP Arm:** A (persona-driven)

---

## What was presented

Filter and sort controls for the notifications page. Priority column added to DB, GML sends priority in API payload, UI supports filtering by type/project/priority/message with multi-select, negation, and sort by date or priority.

## Tomas's feedback

### Round 1: Three missing capabilities
**Request:** (A) message text filter, (B) negative filters (exclude values), (C) multi-select for all filters.

**Impact:** These were not in the original scope. Planning assumed single-value dropdowns. Required:
- Rewrite filter backend from single-value `=` to multi-value `IN`/`NOT IN` with `filterSpec` abstraction
- Replace `<select>` dropdowns with checkboxes + NOT toggles
- Add message text search with `LIKE` clause
- 3 new integration tests

### Round 2: NOT toggle bug + filter panel too large
**Bug:** Clicking action_needed then NOT lost the action_needed selection. Cause: `Has()` method only checked include list; after negation the value moved to exclude list and the checkbox unchecked.
**Fix:** `Has()` now checks both Include and Exclude lists. Added `IsNegated()` method.

**Request:** Filter panel takes too much space.
**Fix:** Wrapped in `<details>`/`<summary>`, collapsed by default, auto-opens when filters active.

### Round 3: Message input loses focus
**Bug:** After typing in message box and debounce-submit, page reload lost input focus.
**Fix:** Added `autofocus` attribute when message field has a value.

### Round 4: Message multi-value/negative + tooltip + auto-collapse
**Request:** Message field should support same comma-separated and `!` exclude syntax as other filters.
**Fix:** Replaced single string with `filterSpec`, added `applyLikeSQL()` for LIKE-based include/exclude. Include terms use OR, exclude terms use AND NOT LIKE.

**Bug:** Clearing message box collapsed the filter panel (empty filters → no `FilterQuery` → `<details>` without `open`).
**Fix:** Added hidden `fo` param to track details-open state across submits.

**Request:** Add tooltip showing message syntax.
**Fix:** `title` attribute on Message label and input: "Comma-separated terms. Prefix with ! to exclude."

## Scorecard: Planning vs. Review

| What | Count |
|------|-------|
| Planning issues flagged (SEC+QA+PM) | 9 |
| False alarms in planning | 0 |
| Review feedback from Tomas | 4 rounds, 6 distinct items |
| ...that planning should have caught | 3 (multi-select, negative filters, message search — all are filter UX that a real PM would have asked about) |
| Bugs found during implementation | 0 |
| Bugs found during review | 3 (NOT toggle, focus loss, auto-collapse) |
| Security-related bugs | 0 |
| Test requirements → real bugs | N/A (no bugs caught by tests, but tests confirmed correctness) |
| Planning overhead | Medium — 24 ACs, 9 concerns, 3 decisions |
| Tomas satisfaction | (to be scored by Tomas) |

## Decisions

### [DSH-014] Multi-value comma-separated filter syntax
**Date:** 2026-05-29
**Phase:** Review (008)
**Decided by:** Tomas
**Decision:** All filters accept comma-separated values for multi-select. Prefix with `!` to negate. URL: `?priority=Q1,Q2` or `?priority=!Q3,!Q4`.
**Alternatives considered:** Multiple `<select>` elements, JavaScript-only filtering
**Reasoning:** Tomas requested multi-select and negation. Comma-separated URL params are bookmarkable and consistent across all filter types.
**Revisit if:** Filter expressions become complex enough to need a query language.

### [DSH-015] Message text search with LIKE
**Date:** 2026-05-29
**Phase:** Review (008)
**Decided by:** Tomas
**Decision:** Message filter uses SQL `LIKE '%term%'` (case-insensitive in SQLite for ASCII). Multiple terms are OR'd, excluded terms use NOT LIKE.
**Alternatives considered:** Full-text search (FTS5), regex matching
**Reasoning:** LIKE is sufficient for the current notification volume. FTS5 adds complexity for no current benefit.
**Revisit if:** Notification volume grows large enough that LIKE scans become slow.
