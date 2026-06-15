# DSH — Iteration 020: backlog todo items batch (delete, drill-down, favicon, notif display, watch)

- **Phase:** Implementation
- **Phase lead:** Developer
- **Start:** 2026-06-12
- **End:** 2026-06-12

---

## Trigger

Tomas: "implement all todo items related to dsh." Swept `todo.txt` for open DSH
entries and grouped them by size. Five were small, well-scoped UI/ops items that
fit one batch iteration; two are large platform features deferred (see below).

## Items shipped

1. **Delete todo items from UI** (todo.txt L21, the "DSH 005" leftover — the
   notification badge half was already done in iter for L47).
   - `todoreader.DeleteItem(path, lineIdx)` removes the item line *and* its
     indented continuation lines (multi-line legacy format) so nothing is orphaned.
   - `UIHandler.TodoDelete` + route `POST /todo/{id}/delete` (session + CSRF).
   - `[Delete]` button in `todo.html` with a `confirm()` guard.

2. **Project detail drill-down** (todo.txt L21).
   - `pmreader.ProjectDetail(pmPath, code)` — finds the project dir by *matching
     the parsed `Code`* against the URL value (case-insensitive); the URL string
     is never joined into a filesystem path → no traversal surface. Returns raw
     `PROJECT.md`, `ASSUMPTIONS.md`, and every `iterations/*.md` (sorted, with the
     first `# H1` as title).
   - `UIHandler.ProjectDetailPage` + route `GET /projects/{code}`.
   - `project_detail.html`: metadata table, overview, collapsible ASSUMPTIONS,
     and one `<details>` per iteration showing the raw markdown in `pre.doc`
     (wrapped). No markdown library added — raw text in `<pre>` matches the
     ASCII-terminal aesthetic and keeps the dependency set minimal (KISS).
   - Project code/name in `projects.html` now link to the detail page.

3. **Browser favicon** (todo.txt L50).
   - `web/static/favicon.svg` — terminal-style `>` prompt + cursor underscore,
     GitHub-dark palette matching the app.
   - Served at `GET /favicon.ico` (browsers auto-request this on every page, so
     one route covers the whole app — zero per-template `<link>` edits).

4. **Notification message display** (todo.txt L51).
   - (b) newlines now render: `.notif-msg { white-space: pre-line }`.
   - (a) long messages clamp to ~3 lines; a `[more]`/`[less]` toggle appears only
     when the body actually overflows (JS measures `scrollHeight` vs `clientHeight`).

5. **Auto-rebuild container on code change** (todo.txt L36).
   - `develop.watch` (`action: rebuild`, ignores markdown) in `docker-compose.yml`.
   - `make watch` target. Solves the "stale image missing has_comment filter" bite.

## Deferred (need their own ideation — flagged to Tomas, not built blind)

- **L32** DSH portal: inter-agent messaging + shared todo API + shared context.
- **L33** DSH discussions: M:N threaded discussions linked to notifications with
  LLM cross-linking.

Both are multi-iteration platform features (new schema, new APIs, LLM integration).
Per MO §4, architecture-level scope needs ideation + Tomas's sign-off before code.

## Testing

- `internal/todoreader`: `DeleteItem` removes the item + continuation lines and
  leaves siblings intact; out-of-range index errors.
- `internal/pmreader`: `ProjectDetail` returns the right project (case-insensitive,
  not a sibling), loads ASSUMPTIONS, lists iterations sorted with parsed H1 titles;
  unknown code errors.
- `internal/handler`: `TodoDelete` (302 + line gone), `ProjectDetailPage` renders
  the real template (200, content present), unknown code → 404.
- Full suite: **62 tests pass** across 9 packages; `go build ./...` clean.
- Smoke test (binary booted on :19099): `/api/v1/health` 200, `/favicon.ico` 200
  `image/svg+xml`, `/projects/DSH` wired (302 → /setup when unauthenticated).
- `docker compose config` validates (the new `develop.watch` block parses).

## Decisions

### [Decision: project drill-down renders raw markdown in `<pre>`, no markdown lib]
**Date:** 2026-06-12 · **Phase:** Implementation · **Decided by:** Developer
**Decision:** Show `PROJECT.md` / `ASSUMPTIONS.md` / iteration files as raw text in
`pre.doc` blocks rather than rendering markdown to HTML.
**Alternatives considered:** Add goldmark (or similar) to render markdown.
**Reasoning:** KISS — the app deliberately has a tiny dependency set and an
ASCII-terminal look; raw markdown is perfectly readable in a monospace `<pre>` and
avoids both a new dependency and an HTML-injection surface from rendered content.
**Revisit if:** Tomas wants rendered headings/links in the drill-down.

### [Decision: favicon served at /favicon.ico as SVG, not via per-template link tags]
**Date:** 2026-06-12 · **Phase:** Implementation · **Decided by:** Developer
**Decision:** One `GET /favicon.ico` handler returns the SVG with
`Content-Type: image/svg+xml`.
**Alternatives considered:** Add `<link rel="icon">` to every template `<head>`
(~15 duplicated heads); ship a binary `.ico`.
**Reasoning:** Browsers auto-request `/favicon.ico` on every page and honor the
response `Content-Type` over the extension, so a single route covers the whole app
with no template churn and no binary asset. SVG is crisp at any size and matches
the app's vector aesthetic.
**Revisit if:** A target browser stops honoring SVG for the bare `/favicon.ico`
request (then add an explicit `<link rel="icon" type="image/svg+xml">`).

### [Decision: delete is permanent (no soft-delete) for todo items]
**Date:** 2026-06-12 · **Phase:** Implementation · **Decided by:** Developer
**Decision:** `[Delete]` removes the line from `todo.txt` outright (with a
`confirm()` guard), unlike notifications which soft-dismiss.
**Alternatives considered:** Mark items as a "deleted" status / archive section.
**Reasoning:** `todo.txt` is the source of truth and is git-tracked — history is
recoverable from git, so an in-app trash adds complexity for no real safety gain.
Notifications differ because they live only in SQLite.
**Revisit if:** Accidental deletions become a problem in practice.
