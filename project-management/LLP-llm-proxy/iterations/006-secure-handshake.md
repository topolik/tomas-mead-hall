# LLP — Iteration 006: Auto-provisioned token handshake (secure by default)

- **Phase:** Implementation (review feedback → re-architecture)
- **Phase lead:** Developer
- **Start:** 2026-06-12
- **End:** 2026-06-12

---

## Trigger

Tomas, reviewing the DSH↔LLP integration: the static-key + `DSH_LLP_KEY` env-var approach required manual setup and exposed the key via `/proc/<pid>/environ`. Directive: *"Do not drop keys entirely, they should just be auto-provisioned and mutually exchanged at process start"* — and held in memory, never env/disk/proc. Chosen mechanism: **Unix-socket handshake**.

## What changed

Auth is no longer static keys from config/env. Instead:

- **Loopback by default** — the data API binds `127.0.0.1:<port>` (was `:port`/all interfaces). Off-host callers cannot reach it. (`bind_host`, default `127.0.0.1`.)
- **Control socket handshake** — LLP listens on a Unix socket (`~/.llp/control.sock`; parent dir created `0700`, owner-only — the security gate). An agent connects at its startup, `POST /register {"agent":"dsh"}`, and LLP mints a random 32-byte session token, stores `token→agent` **in memory**, and returns it. The agent holds the token in heap and sends it as `Authorization: Bearer` on the loopback data API.
- **In-memory token store** (`auth.Store`) replaces the static client-keys map. Tokens are session-scoped (regenerated each LLP start); a stale token → 401 → the agent re-registers automatically.
- **Peer credentials** (`SO_PEERCRED`) are captured per handshake and logged (uid/pid); optional strict same-uid enforcement (`control_require_same_uid`, default off — see decision).
- **No keys anywhere**: no `clients` config, no `LLP_KEY_*`/`DSH_LLP_KEY` env, no key files. Nothing in `/proc/<pid>/environ`.

`config.example.yaml` drops `clients`, adds `bind_host` + `control_socket`. `run.sh` no longer generates a `.env`. `smoke.sh` and GML's `run-task.sh` `llp_complete` now do the handshake (both run host-side as the socket owner).

## Live verification (real LLP on the host)

- `~/.llp` is `drwx------` (0700 owner-only); data API listens on `127.0.0.1:4000` only (confirmed via `ss`).
- data API **without token → 401**; handshake over the socket → 64-hex token; **with token → 200**.
- DSH's real client (`TestLive_RealLLP`) registers over the live socket → token → health/usage/recent.
- `smoke.sh` (handshake → model selection → usage) green; usage correctly attributes calls to the registering agent (`agent="smoke"`).
- Unit: `auth` (issue/lookup/reject), `control` (register issues token, empty-agent 400), plus the existing suite — all pass.

## Decisions

### [Decision LLP-012: auth = auto-provisioned session tokens via a Unix-socket handshake]
**Date:** 2026-06-12 · **Phase:** Implementation · **Decided by:** Tomas
**Decision:** Agents obtain a per-session bearer token at startup over LLP's control socket; tokens live only in memory on both sides; no static keys, none in env/disk/proc. Stale token → re-register on 401.
**Alternatives considered:** static keys in env (rejected — `/proc` leak, manual setup); a token rendezvous file (rejected — token transits disk); fully keyless loopback trust (rejected — Tomas wants keys retained).
**Reasoning:** "Secure by default" + "just works" + KISS: zero key management, secret never leaves memory, mutual trust rooted in the 0700 socket dir. Mirrors the GML stdin-to-heap ethos. See memory `feedback-service-secrets`.
**Revisit if:** an agent must reach LLP from another host (then add a networked, credentialed transport — the token model already fits).

### [Decision LLP-013: loopback bind + 0700-dir gate; SO_PEERCRED logged, strict-uid opt-in]
**Date:** 2026-06-12 · **Phase:** Implementation · **Decided by:** Developer (flagged to Tomas)
**Decision:** Data API binds loopback by default. The control socket's security gate is its `0700` owner-only parent dir; `SO_PEERCRED` is captured and logged but uid-match is **not** enforced by default (`control_require_same_uid: false`).
**Reasoning:** The DSH container runs as root and reaches the socket via `CAP_DAC_OVERRIDE` + bind-mount; enforcing peer-uid==llp-uid would break that for no real gain (a same-uid attacker would pass the check anyway, and the 0700 dir already blocks other users). Strict mode remains available for host-only deployments.
**Revisit if:** LLP runs in an environment with untrusted same-host, non-root peers — enable strict mode and align uids.
