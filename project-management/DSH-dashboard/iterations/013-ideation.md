# DSH — Iteration 013: Ideation (Push Notifications)

- **Start:** 2026-05-31
- **Phase:** Ideation

## Goal

Tomas wants push notifications on his phone when DSH notifications arrive (action items from agents, reviews waiting, etc.). Must be privacy-respecting and fit the "DSH as central hub" philosophy.

## Requirements

1. When a notification is created via `POST /api/v1/notifications`, Tomas gets a push notification on his phone
2. Privacy-first: no services hosted by third parties that see plaintext data
3. Self-contained in DSH — no extra services to maintain (no ntfy, no Telegram bots)
4. Secure access from phone without exposing the server to the internet

## Options Explored

### Option A: ntfy.sh (public server)
- Simplest (~10 lines of code), but Tomas doesn't trust third-party hosting
- **Rejected:** privacy concern

### Option B: Self-hosted ntfy
- Docker container alongside DSH + Tailscale for connectivity
- Adds another service to maintain, Firebase/APNs complexity for phone push delivery
- **Rejected:** violates KISS, adds unnecessary component when Web Push can do the job

### Option C: Native mobile app
- Massive overkill for personal dashboard, weeks of work, app store hassle
- **Rejected:** effort-to-value ratio

### Option D: Tailscale + Web Push built into DSH (**Selected**)
- Tailscale for secure phone-to-server connectivity (WireGuard mesh VPN, no public exposure)
- Web Push API with VAPID keys built directly into DSH
- Service worker in DSH frontend handles push events
- Push payload encrypted end-to-end with VAPID keys
- Browser push service (Google/Apple) delivers the push but cannot read the content
- Everything self-contained — DSH is both the notification source and the push server

## Scope

### In scope
- VAPID key generation and storage (SQLite, like existing TOTP key)
- Service worker for push notification display
- Push subscription management (subscribe/unsubscribe in DSH settings UI)
- Push subscription storage in SQLite
- Trigger push on `POST /api/v1/notifications` (and UI-created notifications if any)
- Go library for Web Push (e.g., github.com/SherClockHolmes/webpush-go)
- Tailscale setup documentation

### Out of scope
- Tailscale installation automation (manual one-time setup)
- HTTPS/TLS for DSH (Tailscale MagicDNS + HTTPS certs handles this)
- Per-notification-type push filtering (all notifications push; filter later if needed)
- Multiple user push subscriptions (single-user dashboard)

## Key Technical Notes

- Web Push requires HTTPS. Tailscale provides automatic HTTPS certs via `tailscale cert` for MagicDNS hostnames. DSH will need a TLS listener option, or Tailscale's built-in HTTPS proxy can handle it.
- VAPID keys are an ECDSA P-256 keypair. Generate once, store in DB config table.
- Service workers require same-origin. Already satisfied since DSH serves its own static files.
- Push subscriptions contain an endpoint URL + encryption keys. Store per-device in SQLite.
- The `CreateNotification` handler in `internal/handler/api.go:74` is the integration point — after inserting the notification, fan out web push to all subscriptions.

## Decisions

### [Selected Approach: Tailscale + Web Push in DSH]
**Date:** 2026-05-31
**Phase:** Ideation
**Decided by:** Tomas
**Decision:** Use Tailscale for secure connectivity + Web Push API built into DSH
**Alternatives considered:** ntfy.sh (public), self-hosted ntfy, native mobile app
**Reasoning:** Privacy-first (payload encrypted, no third-party hosting of data), self-contained in DSH (no extra services), KISS (Web Push is a browser standard, Tailscale is one install)
**Revisit if:** Web Push proves unreliable on iOS Safari, or if push volume grows beyond single-user needs
