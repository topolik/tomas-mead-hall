# DSH — Iteration 015: Implementation (Push Notifications)

- **Start:** 2026-05-31
- **End:** 2026-05-31
- **Phase:** Implementation

## What was built

All items from the 002-planning checklist were implemented: push subscription table, VAPID key generation, `PushHandler` (subscribe/unsubscribe/vapid-key), `SendWebPush` fire-and-forget goroutine in `CreateNotification`, service worker, notifications.html push toggle UI, README update.

## What wasn't in the plan

### Multi-origin WebAuthn (WAMap + setup token)

Passkeys are bound to an RPID (hostname). A passkey registered on `localhost` doesn't work on `dsh-1.your-tailnet.ts.net`. This wasn't anticipated in planning.

Fix: `NewWebAuthnMap()` creates a separate `webauthn.WebAuthn` instance per unique RPID. `waForRequest(r)` picks the right one based on `r.Host`. A one-time setup token (random 32 hex chars, logged on startup) gates passkey registration on new origins when a passkey already exists.

### TLS support in main.go

Added `DSH_TLS_CERT`/`DSH_TLS_KEY` config, `ListenAndServeTLS`, and a `/ca.crt` endpoint. This was built for self-signed cert access before Tailscale's Let's Encrypt was configured. Now vestigial — Tailscale handles TLS. Left in place as it's harmless and could be useful for non-Tailscale deployments.

### NordVPN Meshnet attempt (abandoned)

Before Tailscale, tried NordVPN Meshnet for phone-to-server connectivity. Phone showed "disconnected" persistently, and NordVPN's VPN blocked Tailscale's control plane connections. Abandoned after ~1 hour. All NordVPN was disconnected; switched entirely to Tailscale.

### setup.html token passthrough bug

The setup page's JavaScript called `/setup/passkey/begin` and `/setup/passkey/finish` without forwarding the `?token=` parameter from the URL. Found during live testing on phone — "setup not allowed" error. Fixed by extracting token from `window.location.search` and appending to fetch URLs.

## Run log

1. Built and deployed DSH with push support — build succeeded, VAPID keys auto-generated
2. Tailscale sidecar container configured (`network_mode: host`, `tailscale serve --bg --https=443 http://localhost:9090`)
3. Tomas authenticated Tailscale from phone, accessed `https://dsh-1.your-tailnet.ts.net`
4. Hit "setup not allowed" on passkey registration → found and fixed token passthrough bug
5. Passkey registered successfully from phone via Tailscale HTTPS
6. Push enabled on notifications page — browser prompted for permission, subscription stored in DB
7. Test notification sent via API (`POST /api/v1/notifications`) — FCM returned 201
8. Push notification received on phone after brief delay

## Test coverage

The 10 test requirements from planning were verified manually (curl + phone browser). No automated tests were written — this is a single-user feature with browser-dependent behavior (service workers, push permission prompts) that doesn't lend itself to unit tests. The end-to-end test (API notification → FCM 201 → phone notification) is the meaningful verification.

## Decisions

### [Multi-origin WebAuthn via WAMap]
**Date:** 2026-05-31
**Phase:** Implementation
**Decided by:** Developer
**Decision:** One `webauthn.WebAuthn` instance per unique RPID, selected by request Host header
**Alternatives considered:** Single WebAuthn with multiple RPOrigins (doesn't work — RPID must be singular), separate passkey per origin with no cross-origin support
**Reasoning:** Passkeys are RPID-bound by spec. The phone needs its own passkey for the Tailscale hostname. WAMap + setup token is the minimal fix.
**Revisit if:** go-webauthn adds multi-RPID support natively

### [Tailscale over NordVPN Meshnet]
**Date:** 2026-05-31
**Phase:** Implementation
**Decided by:** Tomas
**Decision:** Use Tailscale for phone-to-server connectivity, abandon NordVPN Meshnet
**Alternatives considered:** NordVPN Meshnet (tried, failed — phone never connected reliably)
**Reasoning:** Tailscale worked immediately after auth. Provides automatic Let's Encrypt HTTPS via `tailscale serve`. NordVPN's VPN interfered with Tailscale control plane.
**Revisit if:** Tailscale pricing changes or NordVPN Meshnet becomes reliable
