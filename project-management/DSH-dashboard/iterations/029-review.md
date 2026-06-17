# DSH — Iteration 029: Review (Tailscale phone passkey enrollment fix)

- **Phase:** Review
- **Start:** 2026-06-15
- **End:** 2026-06-15

## What was presented

Iteration 028 fix for Tailscale phone passkey enrollment, branch `dsh-taila-fix`:
- `POST /settings/passkeys/enroll` (session+CSRF): mints a fresh, generation-anchored setup token and returns `{url, qr, expires_in}` pointing at the Tailscale external origin
- Passkeys settings page: "Add a new device" button renders QR + clickable link
- `SetupToken.Regenerate()` + `TTL()` — token TTL anchored to generation time, not process start
- `ExternalOrigin()` — picks the first non-loopback `DSH_ORIGIN` for enrollment URLs
- `waForRequest` deterministic fallback to `DefaultRPID` (fixes latent random-map-iteration bug)
- `run.sh` updated to point to in-app enrollment instead of stale boot URL

All unit/integration tests green; live-binary smoke verified end-to-end (production container untouched). See [DSH-031], [DSH-032].

## Tomas's feedback

- Accepted.

## Outcome

Ship it. Merge iteration 028 to master (review gate = merge gate, MO §10).

## Next iteration

None scheduled.
