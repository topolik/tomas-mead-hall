# DSH — Iteration 026: Implementation — Threads

- **Phase:** Implementation
- **Phase lead:** Developer
- **Start:** 2026-06-12
- **End:** 2026-06-12
- **Input:** `024-ideation.md` (scope), `025-planning.md` (contract + tests)

---

## What was built

### Schema
- `011_threads.sql`: `threads` (subject, polymorphic ref to
  notification/plan/project, status open/resolved, created_by, timestamps) +
  `thread_messages` (author, body), indexes on ref and thread_id.

### API (JWT, `internal/handler/api_threads.go`)
- `POST /api/v1/threads` — create thread + first message (validates subject ≤200,
  body ≤10000, ref existence); web-push on creation.
- `GET /api/v1/threads?ref_type=&ref_id=&status=` — list with message_count.
  **GML contract:** `ref_type=notification&ref_id=N&status=resolved` non-empty ⇔
  insight N processed.
- `GET /api/v1/threads/{id}` — thread + messages in order.
- `POST /api/v1/threads/{id}/messages` — reply; bumps `updated_at`.
- `PATCH /api/v1/threads/{id}` — status open|resolved.
- `RequireJWT` now resolves the OAuth client's display name into the request
  context — all authorship comes from there (DSH-029).

### UI (session + CSRF)
- `/threads` — open/resolved/all tabs with counts, new-thread form (prefillable
  via query params), thread table.
- `/threads/{id}` — message transcript (chat-style, reuses `.llp-chat` styling),
  reply box, resolve/reopen.
- Notifications rows: `[discuss]` (prefilled new-thread form) or `[💬]`/`[💬✓]`
  linking to the existing thread.
- Nav: `[Threads]` + open-count badge on every page (`navBadges` extended).

## Testing

- **API tests** (`api_threads_test.go`): create with ref + authenticated author;
  7 validation rejections; the GML processed-contract (resolved thread found,
  unprocessed insight empty); message ordering; reply bumps updated_at; status
  transitions; 404s.
- **UI tests** (`ui_threads_test.go`): tabs + counts, detail + reply round-trip,
  create-with-ref from form, notification 💬 badge/discuss links.
- Full suite: **78 tests pass** in 9 packages; `go vet ./internal/...` clean
  (pre-existing vet noise in `integration_test.go` is untouched scope).
- **Smoke:** binary boots; `/threads*` UI routes 302→auth, API routes 401
  without token; health 200.
- **Real-data acceptance** (`acceptance_threads_test.go`, skip-unless
  `DSH_ACCEPT_DB`): full GML flow over HTTP with a real OAuth2 token against a
  **copy** of the live dsh.db — pick a real dismissed GML insight, probe,
  create thread, resolve, probe (found), verify created_by = client name.
  **RAN AND PASSED** (2026-06-12, Tomas produced the copy via `sqlite3 .backup`):
  real dismissed insight **#1294** ("Confluence Mention in 2026-6-15 Meeting
  Notes…") round-tripped — thread created, resolved, found by the GML
  skip-query, `created_by` = the OAuth client name. Live DB untouched.

## Bugs found & fixed during implementation

- `itoa` helper lived in a test file — production `ui_threads.go` initially
  referenced it; switched to `strconv.FormatInt`.
- `go vet ./...` fails on pre-existing `resp, _ :=` patterns in
  `integration_test.go` — scoped vet to `./internal/...`; cleanup left as-is
  (out of scope, pre-existing).

## Deliberately not in this iteration (per ideation 024)

LLM cross-linking, per-agent unread/inbox, shared context store, and the GML
distill-side change — the last is now a `[GML]` todo.txt entry with the exact
adopt recipe.

## Decisions

(Recorded in ASSUMPTIONS.md as DSH-028, DSH-029, DSH-030: polymorphic ref w/o
`todo`, authenticated authorship, push-on-creation-only.)
