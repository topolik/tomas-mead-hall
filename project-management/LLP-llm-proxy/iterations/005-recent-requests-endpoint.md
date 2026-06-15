# LLP — Iteration 005: Recent-requests endpoint (for the DSH tab)

- **Phase:** Implementation
- **Phase lead:** Developer
- **Start:** 2026-06-12
- **End:** 2026-06-12

---

## Trigger

The DSH "LLP" tab (DSH iteration 018) needs a recent-calls log, which `/admin/usage` (aggregates only) doesn't provide.

## What shipped

- `usage.Store.Recent(limit)` — returns the most recent request rows (newest first; limit clamped to [1,1000], default 50): ts, agent, requested model, impl used, prompt/completion tokens, latency, status, error.
- `GET /admin/requests?limit=N` — bearer-auth handler returning `{"requests":[...]}`.

Tests: `TestRecent` (newest-first + clamp); server test asserts `GET /admin/requests` returns 200 + `requests`. Verified live on `:4000` (returns real rows from prior runs). No other LLP changes; the DSH playground uses the existing `/v1/chat/completions`.

## Decision

### [Decision: /admin endpoints share the client-key auth (no admin role)]
**Date:** 2026-06-12 · **Phase:** Implementation · **Decided by:** Developer
**Decision:** `/admin/usage` and `/admin/requests` accept any valid client bearer key (DSH uses its `dsh` key). There is no separate admin role in v1.
**Reasoning:** Single-user agent family; all clients are trusted. Keeps auth simple.
**Revisit if:** untrusted clients are ever added — then gate `/admin/*` behind a distinct key/role.
