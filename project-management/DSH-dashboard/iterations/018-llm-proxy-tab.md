# DSH — Iteration 018: LLM Proxy (LLP) tab

- **Phase:** Implementation
- **Phase lead:** Developer
- **Start:** 2026-06-12
- **End:** 2026-06-12

---

## Trigger

Tomas, after reviewing the LLP LLM-proxy: *"I need an UI. A new tab in DSH would do it."* Chosen contents (all four): backend health, usage & cost, recent requests log, interactive playground.

## What shipped

A new `/llm-proxy` tab in DSH that visualizes the LLP proxy by fetching its HTTP endpoints server-side:

- **Backends** — each impl's state (up / cooldown / disabled) from LLP `GET /healthz`.
- **Usage & cost** — by day / agent / impl from LLP `GET /admin/usage`.
- **Recent requests** — last 50 calls (time, agent, requested→served, tokens, latency, status) from LLP `GET /admin/requests` (new endpoint, added to LLP this iteration).
- **Playground** — a form to send a prompt with a chosen model (`auto`, `claude`, `gemini/gemini-2.5-flash`, …) through `POST /v1/chat/completions`; the response and which impl served it are shown inline. CSRF-protected.

### Design

- New `internal/llpclient` package: a thin bearer-auth HTTP client (Health / Usage / Recent / Complete) with typed responses; returns `ErrNotConfigured` when no base URL is set so the tab degrades gracefully.
- `UIHandler` gains `LLPURL`/`LLPKey` (from `DSH_LLP_URL`/`DSH_LLP_KEY`). `LLMProxyPage` (GET) renders the panels; `LLMProxyRun` (POST, CSRF) runs a playground completion and re-renders with the result. Each panel fails independently (a down proxy shows a per-panel error, not a broken page).
- DSH reaches LLP over the host network (`network_mode: host` → `http://localhost:4000`). The LLP key is a client key (agent `dsh`); passed via env (`${DSH_LLP_KEY}`), never committed.
- Template `llm_proxy.html` follows DSH's monospace/ASCII-box style; `[LLP]` nav entry added to all nav-bearing templates.

### Testing

- `internal/llpclient`: unit tests vs an httptest stub (health/usage/recent/complete, not-configured); a `TestLive_RealLLP` (skipped unless `LLP_LIVE_URL` set) — run live against `:4000`: backends [claude up, gemini up, openllm disabled], 3 usage rows, 5 recent rows.
- `internal/handler`: renders the real `llm_proxy.html` against a stub LLP and asserts all four panels + playground appear; playground POST shows the response; empty prompt rejected; not-configured shows a hint.
- Route wired + auth-protected verified on the running binary: `GET /llm-proxy` → 302 → `/setup` when unauthenticated.
- DSH internal suite: 16 tests pass; full build OK. (Pre-existing `go vet` warnings in `cmd/dsh/integration_test.go` are unrelated and untouched.)

## Decisions

### [Decision: DSH consumes LLP over HTTP server-side (not a shared DB)]
**Date:** 2026-06-12 · **Phase:** Implementation · **Decided by:** Developer
**Decision:** The tab fetches LLP's `/healthz`, `/admin/usage`, `/admin/requests` server-side per request and renders; the playground POSTs to LLP `/v1/chat/completions`. DSH holds an LLP client key.
**Alternatives considered:** Mounting/reading LLP's SQLite directly (tight coupling, schema lock-in); LLP pushing usage into DSH's DB (inverts ownership).
**Reasoning:** Keeps LLP the source of truth and DSH a pure viewer; no shared storage; works the moment LLP is reachable. Per-request fetch is fine at this scale (one user, a few panels).
**Revisit if:** the panels need history beyond what LLP retains, or fetch latency becomes noticeable (then cache or push).

### [Decision: playground re-renders the page (no SPA/streaming)]
**Date:** 2026-06-12 · **Phase:** Implementation · **Decided by:** Developer
**Decision:** Submitting the playground POSTs and re-renders the whole tab with the result, matching DSH's existing form style.
**Reasoning:** KISS and consistent with the rest of DSH; no streaming needed for ad-hoc tests. CLI-backed completions are slow, so the client allows ~200s.
**Revisit if:** streaming responses or partial-swap UX is wanted (HTMX is already loaded).
