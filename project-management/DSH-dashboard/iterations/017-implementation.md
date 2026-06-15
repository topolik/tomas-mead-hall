# DSH — Iteration 017: Implementation (run.sh automation + setup token hardening)

- **Start:** 2026-06-01
- **End:** 2026-06-01
- **Phase:** Implementation

## What was built

### run.sh automation

`run.sh` now handles the full startup sequence — no manual `docker compose exec` commands:

1. Builds DSH image (`--build`, cached when unchanged)
2. Starts containers
3. Waits for Tailscale daemon, then waits for BackendState to settle (handles reconnection from saved state)
4. First-run only: prompts with Tailscale login URL (interactive `tailscale up`)
5. Configures HTTPS proxy (`tailscale serve`, idempotent)
6. Reads setup token from file and prints a ready-to-use phone setup URL
7. Prints local + Tailscale URLs

### Setup token security hardening

Tomas identified three issues with the setup token: not one-time, not time-scoped, logged to docker logs.

Fix: new `SetupToken` type in `internal/handler/setup_token.go`:
- **One-time use** — `Consume()` called after successful passkey registration, invalidates token
- **10-minute TTL** — `Valid()` rejects and cleans up after expiry
- **Not in logs** — token written to `/data/setup-token` file (0600), never to stdout
- **Constant-time comparison** via `crypto/subtle`
- `run.sh` reads the file and displays the URL; file is deleted after use or expiry

### README cleanup

Removed stale references to 1Password password handling (removed in passkey-only migration) and manual Tailscale setup commands. Updated phone setup instructions to reference `run.sh` output.

### Test fixes

- Integration tests updated for new `buildMux` signature (map-based WAMap + `*SetupToken`)
- Fixed pre-existing `ValidateToken` arg count mismatch in `oauth_test.go`

## Decisions

### [DSH-025] Setup token written to file, not logs
**Date:** 2026-06-01
**Phase:** Implementation (017)
**Decided by:** Tomas
**Decision:** Setup token written to `/data/setup-token` (0600 perms), read by `run.sh` and displayed once. Never logged to stdout/docker logs.
**Alternatives considered:** Log to stdout (original), pass via env var (visible in `docker inspect`)
**Reasoning:** Tomas flagged that tokens should not be in logs. File on the data volume is readable only by the container and `run.sh`.
**Revisit if:** Need token access without `run.sh` (e.g., remote API-based token retrieval).

### [DSH-026] Setup token is one-time use with 10-minute TTL
**Date:** 2026-06-01
**Phase:** Implementation (017)
**Decided by:** Tomas + Developer
**Decision:** Token consumed after first successful passkey registration. Expires 10 minutes after container start regardless. Restart container for a fresh token.
**Alternatives considered:** Multi-use within TTL window, persistent token in DB
**Reasoning:** Single-user dashboard — registering a second device from a new origin is rare. One-time + TTL minimizes exposure window.
**Revisit if:** Need to register multiple devices in one session.
