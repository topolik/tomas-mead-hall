# DSH — Iteration 027: Review (Threads — L32+L33 merged)

- **Phase:** Review
- **Start:** 2026-06-12
- **End:** 2026-06-13

## What was presented

Iteration 026 Threads feature, runnable via `make watch` / `./run.sh`:
- `011_threads.sql`: `threads` + `thread_messages` tables with polymorphic ref (notification/plan/project), status (open/resolved), authenticated authorship, timestamps
- API (JWT): `POST/GET /api/v1/threads`, `GET /api/v1/threads/{id}`, `POST /api/v1/threads/{id}/messages`, `PATCH /api/v1/threads/{id}`
- UI (session+CSRF): `/threads` (tabs + new-thread form), `/threads/{id}` (transcript + reply + resolve/reopen)
- Notifications page: `[discuss]` link (prefills new-thread) or 💬/💬✓ badge to existing thread
- Nav: `[Threads]` + open-count badge on every page
- GML contract: `GET /api/v1/threads?ref_type=notification&ref_id=N&status=resolved` non-empty ⇔ insight N processed

78 tests pass (9 packages); `go vet ./internal/...` clean.
Real-data acceptance test (`DSH_ACCEPT_DB`): insight #1294 round-tripped against a live-DB copy — thread created, resolved, found by GML skip-query, `created_by` verified.

## Tomas's feedback

- Accepted — "Merge it to master if it's not."

## Outcome

Ship it. Merge iteration 026 to master (review gate = merge gate, MO §10).

## Next iteration

None scheduled. Thread adoption by GML is a `[GML]` todo.txt entry with the exact recipe.
L32/L33 are now fully resolved — closed by this merge.
