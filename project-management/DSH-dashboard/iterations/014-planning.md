# DSH — Iteration 014: Planning (Push Notifications)

- **Start:** 2026-05-31
- **Phase:** Planning

## Implementation Checklist

### Backend
- [ ] Migration `009_push_subscriptions.sql` — new table for browser push subscriptions
- [ ] VAPID key auto-generation on first start (stored in `config` table, like `jwt_secret`)
- [ ] Add `webpush-go` dependency
- [ ] New `internal/handler/push.go`:
  - `PushHandler.VAPIDKey(w, r)` — serve public key to JS client
  - `PushHandler.Subscribe(w, r)` — store push subscription
  - `PushHandler.Unsubscribe(w, r)` — remove push subscription
  - `SendWebPush(db, vapidPub, vapidPriv, contact, notif)` — fan-out to all subscriptions
- [ ] Modify `APIHandler.CreateNotification` — call `SendWebPush` after DB insert (fire-and-forget goroutine)
- [ ] Add VAPID keys + contact to `APIHandler` struct
- [ ] Add `PushSubscription` model
- [ ] Add `DSH_VAPID_CONTACT` config (default: `mailto:admin@localhost`)
- [ ] Routes: `GET /push/vapid-key`, `POST /push/subscribe`, `POST /push/unsubscribe` (all session-protected)
- [ ] Serve `sw.js` at root path `/sw.js` for root service worker scope

### Frontend
- [ ] `cmd/dsh/web/static/sw.js` — service worker handling push + notificationclick events
- [ ] Update `notifications.html` — add push toggle button + service worker registration JS
- [ ] CSRF token sent via `X-CSRF-Token` header in fetch() calls

### Documentation
- [ ] Update `README.md` — new config var, Tailscale setup, push subscription instructions

## Test Requirements

1. Build succeeds with `make docker-build`
2. Server starts without error, VAPID keys auto-generated on first boot
3. `/push/vapid-key` returns the public key (session required)
4. Service worker registers on notifications page load
5. "Enable Push" button triggers browser permission prompt + subscription
6. Push subscription stored in DB
7. Creating a notification via API triggers push to all subscribers
8. Push notification shows correct title/body, click opens DSH
9. "Disable Push" removes subscription from DB and browser
10. Failed push (expired subscription, 410 Gone) cleans up stale subscription

## Architecture

```
API Client → POST /api/v1/notifications → CreateNotification
                                              │
                                              ├─ INSERT into notifications table
                                              │
                                              └─ goroutine: SendWebPush
                                                    │
                                                    ├─ SELECT * FROM push_subscriptions
                                                    │
                                                    └─ for each subscription:
                                                         webpush.SendNotification(payload, sub, vapid)
                                                              │
                                                              └─ Browser Push Service (FCM/APNs)
                                                                    │
                                                                    └─ Service Worker → showNotification()
```

## Key Design Decisions

### Push is fire-and-forget
Push sending runs in a goroutine after the notification DB insert. If push fails, the notification is still created. This keeps the API fast and reliable — push is best-effort.

### VAPID keys in config table
Same pattern as `jwt_secret`. Auto-generated on first start, persisted in SQLite. No env var needed.

### Service worker at /sw.js
Served from root path so it has root scope. Read from the embedded static FS but served at a dedicated route, not under /static/.

### Push subscribe/unsubscribe via session auth + CSRF
These are browser-facing endpoints (called from JS on the notifications page). They use session cookies like the rest of the UI, not JWT like the API.

## Decisions

### [Implementation Approach]
**Date:** 2026-05-31
**Phase:** Planning
**Decided by:** Developer
**Decision:** Minimal integration into existing DSH codebase — one new handler file, one migration, one service worker, modifications to two existing files (main.go, api.go, notifications.html)
**Alternatives considered:** Separate push microservice, webhook-based approach
**Reasoning:** KISS — DSH is a single binary, keep it that way. Low notification volume (personal dashboard) doesn't justify a separate service.
**Revisit if:** Push sending becomes a bottleneck (unlikely for single-user)
