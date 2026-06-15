# GML — Iteration 009: Mode 2 Review

- **Phase:** 3 — Presentation & Review
- **Phase lead:** PM
- **Start:** 2026-05-29
- **End:** 2026-05-29
- **Reviewer:** Tomas
- **Verdict:** Success — Mode 2 shipped

---

## Review Summary

Tomas reviewed Mode 2 live against his real Gmail inbox. The pipeline ran end-to-end: fetch → Gemini analysis → DSH notification posting. Multiple rounds of feedback were given and addressed in the same session.

---

## Feedback Given & Addressed

### 1. Gmail search links too broad
**Issue:** Notification links used `from:sender1 OR from:sender2` — matched far too many emails.
**Fix:** Prompt now requires `subject:` keywords, prohibits broad OR across unrelated senders. Suggests Gmail `{subject:"a" subject:"b"}` syntax for multi-email concerns.

### 2. Missing priority — wants Eisenhower quadrants
**Issue:** Notifications lacked prioritization, making it hard to scan what matters.
**Fix:** Replaced `type` field with `priority` (Q1-Q4). Each notification prefixed with colored icon:
- 🔴 Q1 (urgent + important) → DSH `action_needed`
- 🟡 Q2 (important, not urgent) → DSH `action_needed`
- 🔵 Q3 (urgent, not important) → DSH `info`
- ⚪ Q4 (neither) → DSH `info`

Prompt includes detailed guidance on assigning quadrants per box context.

### 3. Duplicate "Unchanged" notifications
**Issue:** Subsequent runs re-posted identical concerns with "Unchanged:" prefix.
**Fix:** Two layers — prompt now says "SKIP unchanged items entirely" + code-level filter drops summaries starting with "Unchanged:".

### 4. Wants `--hours` / `--minutes` for shorter windows
**Issue:** `--days` is too coarse for scheduled runs. Reanalyzing a full day wastes LLM tokens.
**Fix:** Added `--hours N` and `--minutes N` flags. Time window is now mandatory (no silent defaults). Watch daemon auto-derives window from `schedule_minutes` config + state file catch-up.

### 5. Watch daemon needs tmux management script
**Issue:** Starting/monitoring the daemon required manual tmux commands.
**Fix:** `watch.sh` — starts detached, with `stop`, `status`, `logs`, `attach` subcommands.

### 6. Container runs as root
**Issue:** Security concern — container had no user isolation.
**Fix:** Added `gml` system user with home directory (required by gws discovery cache). Container now runs as UID 999.

### 7. `run.sh` trap bug — unbound variable on exit
**Issue:** `prompt_file` local variable referenced in trap after function scope exited.
**Fix:** Moved temp file paths to script-level `GML_PROMPT_FILE`/`GML_ANALYSIS_FILE` variables.

### 8. Docker image not rebuilt after code changes
**Issue:** Container ran old binary without priority support.
**Fix:** Rebuilt. (Note for future: code changes require `docker compose build` before testing.)

---

## What Shipped (Mode 2 complete feature set)

- Three-step pipeline: container fetch/sanitize → host LLM → container validate/notify
- Gemini default, Claude via `--model claude`
- Per-concern notifications with Eisenhower priority icons and Gmail search links
- 5-layer prompt injection defense (datamarking, XML structure, sanitization, output validation, architectural)
- `--days`, `--hours`, `--minutes` time windows (mandatory)
- `./run.sh watch` scheduled daemon with credential caching and state-file catch-up
- `watch.sh` tmux management script
- Non-root container
- Dedup filter (prompt + code)
- 49 tests (30 sanitize, 19 notify)
- DSH integration: `link` column, `GET /api/v1/notifications`, 14 integration tests

---

## Decisions

### [GML-040] Eisenhower priority replaces info/action_needed type
**Date:** 2026-05-29
**Phase:** Review
**Decided by:** Tomas
**Decision:** LLM assigns Q1-Q4 priority per concern. Icons displayed in notification message. DSH type derived from priority (Q1/Q2 → action_needed, Q3/Q4 → info).
**Alternatives considered:** Keep info/action_needed and add separate priority field
**Reasoning:** Tomas uses Eisenhower quadrants across all projects (todo.txt, Modus Operandi). Consistent mental model. Two-field approach would be redundant.
**Revisit if:** Need finer-grained DSH type distinctions

### [GML-041] Time window is mandatory for fetch/analyze
**Date:** 2026-05-29
**Phase:** Review
**Decided by:** Tomas
**Decision:** `gml fetch` requires exactly one of `--days`, `--hours`, or `--minutes`. No silent default. Watch daemon computes window from its interval.
**Alternatives considered:** Keep `--days 3` default (from config)
**Reasoning:** Silent defaults led to expensive re-analysis. Explicit window forces intentionality. Daemon handles its own window automatically.
**Revisit if:** Never — explicit is better than implicit

### [GML-042] Skip unchanged concerns (dedup)
**Date:** 2026-05-29
**Phase:** Review
**Decided by:** Tomas
**Decision:** Prompt instructs LLM to omit unchanged items entirely. Code filters summaries starting with "Unchanged:" as safety net. Supersedes GML-031 (accept duplicates).
**Alternatives considered:** Content-hash dedup in DSH (too complex), client-side dedup cache
**Reasoning:** Prompt-level skip is simplest. Code filter catches LLM non-compliance. No state management needed.
**Revisit if:** LLM consistently ignores the skip instruction (then add server-side dedup)

### [GML-043] Non-root container
**Date:** 2026-05-29
**Phase:** Review
**Decided by:** Tomas
**Decision:** Container runs as `gml` system user (UID 999) with home directory. gws requires writable `$HOME` for discovery cache.
**Alternatives considered:** Root (status quo)
**Reasoning:** Principle of least privilege. Container only needs to run two binaries and write to stdout/stderr.
**Revisit if:** Never
