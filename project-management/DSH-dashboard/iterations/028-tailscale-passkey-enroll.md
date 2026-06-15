# DSH — Iteration 028: Fix Tailscale phone passkey enrollment

- **Start:** 2026-06-15
- **End:** 2026-06-15
- **Phase:** Ideation → Planning → Implementation (focused bug fix — combined)
- **Branch:** `dsh-taila-fix`

## Trigger

Tomas, from his phone over Tailscale: *"DSH tailscale doesn't work from phone. I cannot use my existing passkey and the setup token URL printed does not work."*

## Diagnosis (run against the live instance)

Two distinct problems behind one symptom — "can't get in from the phone":

1. **"Can't use existing passkey" — inherent, not a bug.** Passkeys are bound to the
   RPID (the origin's host). The phone reaches DSH at `https://dsh-1.your-tailnet.ts.net`
   (RPID `dsh-1.your-tailnet.ts.net`). The usable phone creds are the platform
   RPID (the origin's host). The phone reaches DSH at `https://dsh-1.your-tailnet.ts.net`
   (RPID `dsh-1.your-tailnet.ts.net`). The usable phone creds are the platform
   "Android"/"Android 2" passkeys (DB ids 7, 8). If those are gone (new device / OS
   cleared them) the only synced cred is "1pass" (id 6, created 2026-05-27 via the
   laptop SSH tunnel) which is bound to **`localhost`** — 1Password syncs it to the
   phone, but the browser won't offer a `localhost` passkey on a `ts.net` origin. So
   no usable passkey appears. The fix is to **register a fresh passkey on the phone** —
   which is exactly what (2) was supposed to enable.

   Verified the server side is *correct*: through the real Tailscale serve path,
   `GET /auth/passkey/login/begin` returns `rpId=dsh-1.your-tailnet.ts.net` (Tailscale
   `GET /auth/passkey/login/begin` returns `rpId=dsh-1.your-tailnet.ts.net` (Tailscale
   preserves the Host header), so a phone passkey bound to ts.net *does* work — login
   was never the broken part.

2. **"Setup token URL doesn't work" — the real bug.** The boot-time setup token has a
   **10-minute TTL anchored to process start**, is minted only at boot, and is only
   printed by `run.sh`. The live container had been up **38 hours** → the token expired
   ~37 h ago and its file was gone. `run.sh` re-run doesn't help: `docker compose up -d`
   doesn't recreate an already-running container, so no new token is minted, and the
   stale file read prints no URL. Confirmed on the live box:
   - `GET /setup?token=<stale>` → `302 → /login`
   - `GET /setup/passkey/begin?token=<stale>` → `403 "setup not allowed"`
   - `/data/setup-token` absent; container `StartedAt 2026-06-13 18:47`.

   Net: there was **no way to obtain a working enrollment URL on demand** — so the
   phone could never register the passkey it needs.

3. **Latent:** `waForRequest`'s unknown-Host fallback used `for range map { return }` —
   randomized order, could flip the rpId between requests.

## Decision

Add an **on-demand device-enrollment flow** for an already-authenticated user, and
anchor the token TTL to *generation*. See **[DSH-031]** and **[DSH-032]**.

- `SetupToken.Regenerate()` — mints a fresh token, resets the TTL clock, rewrites the file.
- `POST /settings/passkeys/enroll` (session + CSRF) — regenerates the token and returns
  `{url, qr (PNG data-URL), expires_in}`, the URL pointing at `Config.ExternalOrigin()`
  (the first non-loopback `DSH_ORIGIN`, i.e. the ts.net host).
- Passkeys page gets an **"Add a new device"** button rendering the QR + clickable link.
- `waForRequest` falls back to a deterministic `DefaultRPID`.
- `run.sh` stops printing the dead boot URL; points to the in-app flow.

KISS: reuses the existing token-gated `/setup` ceremony unchanged; the QR uses
`go-qrcode` (already a dependency, previously unused).

## Implementation

| File | Change |
|---|---|
| `internal/handler/setup_token.go` | `Regenerate()`, `TTL()`; nil-safe |
| `internal/config/config.go` | `ExternalOrigin()` + `isLoopbackOrigin` (handles bracketed IPv6) |
| `internal/handler/auth.go` | `EnrollDevice` handler; `ExternalOrigin`/`DefaultRPID` fields; deterministic `waForRequest` fallback |
| `cmd/dsh/main.go` | always construct setup token; wire `ExternalOrigin`/`DefaultRPID`; route `POST /settings/passkeys/enroll` |
| `cmd/dsh/web/templates/settings_passkeys.html` | "Add a new device" QR section |
| `run.sh` | point to in-app enrollment instead of stale boot URL |

## Tests & verification ("run it or it doesn't count")

**Unit/integration (`go test ./...` — all packages green, no regressions):**
- `config`: `TestExternalOrigin` (localhost/external ordering, 127.0.0.1, `[::1]`, trimming).
- `handler`: `TestSetupTokenRegenerate`, `TestSetupTokenTTLAnchoredToGeneration` (a 2-h-old
  token is expired; Regenerate makes it valid again), `TestSetupTokenNilSafe`.
- `cmd/dsh`: `TestEnrollDevice_RequiresSession` (302), `_RequiresCSRF` (403), `_FullFlow`
  (mint → URL is ts.net + valid QR PNG + TTL 600 → `/setup?token` unlocks → begin via
  ts.net Host yields `rp.id=ts.net` → wrong token 403), `TestWAForRequestDeterministicFallback`
  (unknown Host → `localhost` 10/10), `TestWAForRequest_KnownHostsExact`.

**Live smoke against the compiled binary** (isolated: port 9191, temp DB, never touched the
production container — confirmed its `StartedAt` unchanged):
- `/setup` gated (302→/login) once a passkey exists.
- Authenticated enroll → `{"url":"https://dsh-1.your-tailnet.ts.net/setup?token=…","qr":"data:image/png;base64,…","expires_in":600}`.
- Enroll without CSRF → 403.
- `/setup?token=<fresh>` → 200; `/setup/passkey/begin` via ts.net Host → `rp.id=dsh-1.your-tailnet.ts.net`; wrong token → 403.
- Authenticated enroll → `{"url":"https://dsh-1.your-tailnet.ts.net/setup?token=…","qr":"data:image/png;base64,…","expires_in":600}`.
- Enroll without CSRF → 403.
- `/setup?token=<fresh>` → 200; `/setup/passkey/begin` via ts.net Host → `rp.id=dsh-1.your-tailnet.ts.net`; wrong token → 403.
- QR decodes as a valid `256×256` PNG.

Not coverable without hardware: the phone's actual `navigator.credentials.create()`
(biometric prompt) — that's the review step.

## How Tomas verifies / unblocks himself

1. Rebuild & restart DSH from this branch: `cd projects/DSH-dashboard && ./run.sh`
   (or `make watch`).
2. On the **laptop** (where the `localhost` passkey works via the SSH tunnel), open
   `http://localhost:9090/settings/passkeys` → **Add a new device** → a QR appears.
3. **Scan the QR with the phone**, register a passkey (binds to `dsh-1.your-tailnet.ts.net`).
4. From then on the phone logs in with that passkey at `https://dsh-1.your-tailnet.ts.net`.
3. **Scan the QR with the phone**, register a passkey (binds to `dsh-1.your-tailnet.ts.net`).
4. From then on the phone logs in with that passkey at `https://dsh-1.your-tailnet.ts.net`.

## Decisions

### On-demand device enrollment supersedes the boot-time run.sh token
**Date:** 2026-06-15 11:05
**Phase:** Implementation
**Decided by:** Developer
**Decision:** As [DSH-031]. Add `POST /settings/passkeys/enroll` (session+CSRF) that
mints a fresh, generation-anchored token and returns a QR to the external origin.
**Alternatives considered:** Longer boot-token TTL; re-print on `run.sh`; require reauth.
**Reasoning:** The boot token can't be re-minted without a restart and expires in 10 min;
the "add a device from an authenticated session" pattern is the right, KISS fix.
**Revisit if:** Multi-user, or a device that can't scan a QR.

### Deterministic waForRequest fallback
**Date:** 2026-06-15 11:05
**Phase:** Implementation
**Decided by:** Developer
**Decision:** As [DSH-032]. Fall back to `DefaultRPID`, not a random map entry.
**Reasoning:** Randomized map iteration could non-deterministically flip the rpId.
**Revisit if:** Requests arrive on origins outside `DSH_ORIGIN`.
