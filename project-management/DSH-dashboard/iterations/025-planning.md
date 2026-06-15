# DSH — Iteration 025: Planning — Threads

- **Phase:** Planning
- **Phase lead:** Developer
- **Start:** 2026-06-12
- **End:** 2026-06-12
- **Input:** `024-ideation.md` (scope: durable M:N threads; first consumer: GML processed-tracking)

---

## Test requirements (written FIRST — these define "done")

### API (`internal/handler`, direct-call pattern)
1. **CreateThread** — `POST /api/v1/threads {subject, body, ref_type?, ref_id?}`:
   creates thread + first message, author taken from the authenticated agent
   (context), 200 + id. 400 on: missing subject/body, invalid ref_type,
   subject > 200 chars, body > 10000 chars, dangling ref (ref'd notification/plan
   doesn't exist).
2. **ListThreads** — `GET /api/v1/threads?ref_type=&ref_id=&status=`:
   returns only matching threads with message_count + last activity.
   **The GML contract:** `?ref_type=notification&ref_id=N&status=resolved`
   non-empty ⇔ insight N is processed.
3. **GetThread** — `GET /api/v1/threads/{id}`: thread + messages in
   chronological order; 404 unknown.
4. **PostMessage** — `POST /api/v1/threads/{id}/messages {body}`: appends with
   author from context, bumps thread updated_at; 404 unknown thread; 400 empty
   or oversized body.
5. **UpdateThreadStatus** — `PATCH /api/v1/threads/{id} {status}`:
   open ⇄ resolved; 400 invalid status; 404 unknown.
6. **Author is not spoofable** — author comes from the OAuth client name
   (request context), never from the JSON payload.

### UI (`internal/handler`, real templates)
7. ThreadsPage renders open threads with status tabs + counts.
8. ThreadDetailPage renders messages and reply form; reply POST appends
   (author = session username) and redirects back.
9. New-thread form (optionally prefilled with a notification ref) creates
   thread + first message.
10. Notifications page shows a 💬 badge linking to the thread for notifications
    that have one.

### Integration / smoke
11. Binary boots with the new routes (no mux conflict); API routes JWT-protected,
    UI routes session-protected.
12. **Real-data acceptance (GML-shaped):** against a copy of the live dsh.db,
    pick a real dismissed GML insight notification, create a thread ref'ing it,
    resolve it, and verify the GML query (`ref_type=notification&ref_id=N&status=resolved`)
    returns it — i.e. the distill skip-check works on production data.

## Plan checklist

- [ ] Migration `011_threads.sql`: `threads` + `thread_messages`, CHECKs, indexes
- [ ] `RequireJWT` resolves the OAuth client **name** into request context (`ctxAgent`)
- [ ] Model structs: `Thread`, `ThreadMessage`
- [ ] API handlers: CreateThread, ListThreads, GetThread, PostMessage, UpdateThreadStatus
- [ ] UI handlers: ThreadsPage, ThreadDetailPage, ThreadNewPage/Create, ThreadReply, ThreadStatus (+notification badge map)
- [ ] Templates: `threads.html`, `thread_detail.html`, `thread_new.html`; nav `[Threads]` + badge in all templates
- [ ] `navBadges` → include open-threads count
- [ ] Routes in `main.go` (API: JWT; UI: session + CSRF)
- [ ] Tests 1–10 above; full suite green
- [ ] Smoke boot + real-data acceptance run (11–12)
- [ ] Docs: README (API reference + features), ASSUMPTIONS (DSH-028..), SKILLS, PROJECT.md
- [ ] GML follow-up todo: distill-side skip-check is a separate GML iteration

## Design

### Schema (migration 011)
```sql
CREATE TABLE threads (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  subject TEXT NOT NULL,
  ref_type TEXT CHECK(ref_type IN ('notification','plan','project')),
  ref_id TEXT,
  status TEXT NOT NULL DEFAULT 'open' CHECK(status IN ('open','resolved')),
  created_by TEXT NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL DEFAULT (datetime('now')),
  updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_threads_ref ON threads(ref_type, ref_id);

CREATE TABLE thread_messages (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  thread_id INTEGER NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
  author TEXT NOT NULL,
  body TEXT NOT NULL,
  created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_thread_messages_thread ON thread_messages(thread_id);
```
- **Polymorphic ref** (`ref_type` + `ref_id` TEXT), not a real FK — a thread can
  attach to a notification (id), plan (id), or project (code). Validated at the
  API layer on create.
- **No `todo` ref_type** — todo IDs are `todo.txt` line indexes and shift on
  every edit; a ref would silently re-point. Revisit if todos get stable IDs.

### Authorship
- Agents: `RequireJWT` already resolves `clientID`; extend it to look up the
  client's display name and stash it in the request context. API handlers read
  it from there — **the payload never carries `author`**, so it can't be spoofed.
- Tomas (UI): author = session username.

### Push / badges
- Web-push on **thread creation only** (mirrors plans). Message replies don't
  push — the nav badge (count of open threads) covers awareness; per-agent
  unread semantics are explicitly deferred (ideation 024).

### UI
- `/threads`: status tabs (open / resolved / all) with counts, table of threads
  (subject → detail, ref, author, messages, updated). New-thread form.
- `/threads/{id}`: message list (pre-line bodies, same display rules as
  notifications), reply box, resolve/reopen button.
- Notifications rows get `[discuss]` (no thread yet → prefilled new-thread form)
  or `[💬 n]` (existing thread → detail).

## Safety checklist review (§7)

- Parameterized SQL everywhere (incl. the polymorphic-ref existence checks) ✓
- `html/template` auto-escaping for all thread/message content ✓
- CSRF on all UI POSTs; JWT on all API routes ✓
- Input limits: subject ≤ 200, message body ≤ 10000, enum validation on
  ref_type/status ✓
- Author from authenticated identity, not payload ✓
- No secrets involved ✓

## Out of scope (re-affirmed from ideation)

LLM cross-linking, per-agent unread/inbox, shared context store, GML's own
distill change (separate GML iteration — different project's code).

---

## Decisions

### [Decision: polymorphic ref column instead of per-type FKs or join tables]
**Date:** 2026-06-12 · **Phase:** Planning · **Decided by:** Developer
**Decision:** One nullable `ref_type`/`ref_id` pair on `threads`, API-validated.
**Alternatives considered:** Three nullable FK columns (notification_id, plan_id,
project_code); a thread_refs join table (M:N refs).
**Reasoning:** KISS — iteration 1 needs exactly one ref per thread; a join table
is speculative (the LLM cross-linker may want it later, revisit then); per-type
FK columns triple the schema for no current gain.
**Revisit if:** Cross-linking iteration needs one thread attached to many
notifications — then add `thread_refs` and migrate this column into it.

### [Decision: author resolved server-side from OAuth client name / session user]
**Date:** 2026-06-12 · **Phase:** Planning · **Decided by:** Developer
**Decision:** Extend `RequireJWT` to put the authenticated client's display name
in the request context; thread/message authorship always comes from there.
**Alternatives considered:** `author` field in the JSON payload.
**Reasoning:** Payload authorship is spoofable by any credentialed agent; the
client name is already authenticated and human-meaningful ("gml", "mnd").
**Revisit if:** One client legitimately posts on behalf of multiple identities.
