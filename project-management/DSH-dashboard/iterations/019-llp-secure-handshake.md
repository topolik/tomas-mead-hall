# DSH — Iteration 019: LLP tab — secure-by-default handshake (no keys)

- **Phase:** Implementation (review feedback)
- **Phase lead:** Developer
- **Start:** 2026-06-12
- **End:** 2026-06-12

---

## Trigger

The LLP tab (iter 018) authenticated with a `DSH_LLP_KEY` env var — manual setup and visible in `/proc/<pid>/environ`. Tomas: keys must be **auto-provisioned and mutually exchanged at process start**, in memory only. Mechanism chosen: Unix-socket handshake (LLP iter 006).

## What changed (DSH side)

- `internal/llpclient` no longer takes a static key. It now registers over LLP's **control socket** (`New(baseURL, socketPath, agent)`): on first use it `POST /register {"agent":"dsh"}` over the bind-mounted Unix socket (HTTP-over-UDS), caches the returned token **in memory**, sends it as `Authorization: Bearer` on the loopback data API, and **re-registers automatically on a 401** (handles LLP restarts).
- Config: `DSH_LLP_KEY` removed; added `DSH_LLP_SOCKET` (default `/llp/control.sock`); `DSH_LLP_URL` defaults to `http://localhost:4000` (so the tab works with zero config).
- `docker-compose.yml`: bind-mounts `${HOME}/.llp:/llp` so the container can reach the host control socket. DSH runs as root (its Dockerfile sets no user), so `CAP_DAC_OVERRIDE` lets it open the `0700`-dir/`0600`-socket — no uid juggling, no volume-perm changes.
- No `DSH_LLP_KEY` anywhere; nothing in `/proc`.

## Testing

- `internal/llpclient`: stub with a TCP data server + a Unix `/register` socket — handshake-then-calls (one registration cached across calls), completion, **re-register on 401** (stale token → 2nd registration → success), not-configured. Plus `TestLive_RealLLP` (skipped unless `LLP_LIVE_URL`+`LLP_LIVE_SOCKET` set) — run live: registered over the real socket, fetched health/usage/recent.
- `internal/handler`: renders the real `llm_proxy.html` against a stub LLP (control socket + data); all four panels + playground appear; playground POST shows the response.
- DSH internal suite: 17 tests pass; full build OK.

## Decision

### [Decision: DSH registers over LLP's control socket; container runs as root to reach it]
**Date:** 2026-06-12 · **Phase:** Implementation · **Decided by:** Developer
**Decision:** The DSH container bind-mounts `~/.llp` and registers over `/llp/control.sock`; it relies on running as root (its existing Dockerfile default) + `CAP_DAC_OVERRIDE` to open the owner-only socket.
**Alternatives considered:** Run the container as the host uid (rejected — risks the `/data` volume perms); relax the socket dir to non-0700 (rejected — weakens the host-side gate).
**Reasoning:** Zero key management, token never leaves memory, no changes to DSH's working volume/uid setup. Start LLP before DSH so `~/.llp` exists (the tab degrades gracefully — per-panel "unavailable" — until the proxy is reachable).
**Revisit if:** DSH is made non-root, or LLP and DSH move to different hosts.
