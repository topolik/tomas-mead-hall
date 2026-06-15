# DSH — Assumptions & Design Decisions

Captures non-obvious decisions and their rationale. Each entry answers "why did we do it this way?"

---

### [DSH-001] Go + Docker container
**Date:** 2026-05-27
**Phase:** Ideation
**Decided by:** Tomas
**Decision:** The dashboard is a Go application packaged as a Docker container.
**Alternatives considered:** Python/FastAPI, Node.js/Express, static HTML
**Reasoning:** Single binary, fast, no runtime deps. Container makes it portable across home server / VPS without environment coupling. Tomas expressed explicit preference.
**Revisit if:** Go expertise becomes a bottleneck or the container runtime adds operational friction.

---

### [DSH-002] Hosted on home server (dedicated always-on machine)
**Date:** 2026-05-27
**Phase:** Ideation
**Decided by:** Tomas
**Decision:** For PoC/MVP the container runs on a home server reachable from both laptop and desktop (LAN or VPN).
**Alternatives considered:** Cloud VPS; decided-later
**Reasoning:** Simplest setup for the MVP. Home server already exists and is always on.
**Revisit if:** Access from outside home network becomes frequent, or home server reliability causes issues. Migration to VPS is a deployment change, not an architecture change.

---

### [DSH-003] Dual ingest model — REST API + file watch
**Date:** 2026-05-27
**Phase:** Ideation
**Decided by:** Tomas
**Decision:** Operational/real-time data is pushed via HTTP REST API. Long-term / batch data can be provided as files (format TBD).
**Alternatives considered:** REST API only; shared DB volume (rejected: home-server-only)
**Reasoning:** REST is the right default for cross-machine data push. File-based ingest covers cases where agents produce structured output files that get committed to repos — simpler than requiring an API call from every agent.
**Revisit if:** File format contract becomes messy; consider REST-only if file watch creates more problems than it solves.

---

### [DSH-004] Agent run outputs deferred
**Date:** 2026-05-27
**Phase:** Ideation
**Decided by:** Tomas
**Decision:** "Agent run outputs" feature is explicitly out of scope for this iteration.
**Alternatives considered:** Include from the start
**Reasoning:** Not defined yet; premature to build a container for undefined data. Focus on backlog + project management progress first.
**Revisit if:** A specific agent type and output format is defined; then model it and add to the API.

---

### [DSH-006] Auth: passkey-only for UI, OAuth 2.1 client credentials for API
**Date:** 2026-05-27 (updated 2026-05-27)
**Phase:** Planning → revised in Review (004)
**Decided by:** Tomas
**Decision:** UI login: passkey (WebAuthn) only — no password, no TOTP. First-run setup at `/setup`. API: OAuth 2.1 client credentials flow (JWT).
**Alternatives considered:** No auth; static bearer token; external auth provider (Zitadel/Authentik); keep password as fallback
**Reasoning:** Single user, single machine. Passkey is phishing-resistant and simpler. Password + TOTP adds complexity with no benefit for a personal tool. OAuth 2.1 client credentials is the right grant type for machine-to-machine API calls.
**Revisit if:** Need to add another user or access from a device without passkey support.

---

### [DSH-007] Frontend: ASCII-style HTMX + Go templates
**Date:** 2026-05-27
**Phase:** Planning
**Decided by:** Tomas
**Decision:** Server-rendered HTML with Go `html/template` + HTMX. Monospace font, ASCII box-drawing, `[STATUS]` labels. All assets embedded in binary. No external CDN dependencies.
**Alternatives considered:** React/Svelte SPA
**Reasoning:** Tomas explicitly requested UI readable by LLM agents — semantic HTML, all data visible in source. HTMX gives dynamic updates without a build step or JS framework.
**Revisit if:** Complex client-side state management becomes necessary.

---

### [DSH-008] SQLite on named Docker volume
**Date:** 2026-05-27
**Phase:** Planning
**Decided by:** PM + Performance
**Decision:** SQLite at `/data/dsh.db`, persisted on a named Docker volume.
**Alternatives considered:** Postgres (overkill for single user)
**Reasoning:** Single user, small data set. Easy to back up, no extra container. Named volume survives restarts.
**Revisit if:** Concurrent writers needed or data grows beyond SQLite's comfort zone.

---

### [DSH-009] DSH project seeded at startup (bootstrap)
**Date:** 2026-05-27
**Phase:** Planning
**Decided by:** Tomas + PM
**Decision:** DSH project row is inserted/updated in the projects table on every startup. Day-one dogfood.
**Alternatives considered:** Push via API only (chicken-and-egg problem)
**Reasoning:** The dashboard cannot call its own API before it's running. Startup seed resolves the bootstrap problem cleanly.
**Revisit if:** Self-reporting fully via API is preferred in a later iteration.

---

### [DSH-010] Passkey-only authentication
**Date:** 2026-05-27
**Phase:** Review (004)
**Decided by:** Tomas
**Decision:** Password login and TOTP removed entirely. Passkey is the only authentication method.
**Alternatives considered:** Keep password as emergency fallback
**Reasoning:** Single user. Passkey is more secure and simpler to operate. Password/TOTP adds code complexity with no real benefit.
**Revisit if:** Multi-user or passkey-incompatible device access is needed.

---

### [DSH-011] todo.txt as primary backlog storage
**Date:** 2026-05-27
**Phase:** Review (004)
**Decided by:** Tomas
**Decision:** Backlog items stored in repo-root `todo.txt` (bind-mounted), not the database.
**Alternatives considered:** Keep DB-backed backlog
**Reasoning:** File already existed as manual backlog before DSH. One canonical source in the repo is simpler than DB sync. Human-readable and diffable.
**Revisit if:** Multi-user editing or rich item metadata is needed.

---

### [DSH-012] Projects from filesystem, not database
**Date:** 2026-05-27
**Phase:** Review (004)
**Decided by:** Tomas
**Decision:** Projects page reads `project-management/*/PROJECT.md` via bind mount. No DB writes for projects.
**Alternatives considered:** API-pushed DB rows (original design)
**Reasoning:** Files are already the authoritative source. Removes a sync problem. `pmreader` is ~90 lines with no DB schema dependency.
**Revisit if:** Projects need computed fields not in PROJECT.md.

---

### [DSH-013] network_mode: host for real client IP
**Date:** 2026-05-27
**Phase:** Review (004)
**Decided by:** Developer
**Decision:** `docker-compose.yml` uses `network_mode: host`. Container binds directly to host network stack.
**Alternatives considered:** Reverse proxy with X-Real-IP header
**Reasoning:** Simplest fix for a single-container single-host setup. Docker bridge NAT replaced client IP with container gateway IP, breaking audit log usefulness.
**Revisit if:** Multiple containers need to communicate, or host networking causes port conflicts.

---

### [DSH-014] Multi-value comma-separated filter syntax
**Date:** 2026-05-29
**Phase:** Review (008)
**Decided by:** Tomas
**Decision:** All notification filters accept comma-separated values. Prefix with `!` to negate. URL: `?priority=Q1,Q2` or `?priority=!Q3,!Q4`.
**Alternatives considered:** Multiple select elements, JavaScript-only filtering
**Reasoning:** Tomas requested multi-select and negation during review. Comma-separated URL params are bookmarkable and consistent across all filter dimensions.
**Revisit if:** Filter expressions become complex enough to need a query language.

---

### [DSH-015] Message text search with LIKE
**Date:** 2026-05-29
**Phase:** Review (008)
**Decided by:** Tomas
**Decision:** Message filter uses SQL `LIKE '%term%'` (case-insensitive in SQLite). Multiple terms OR'd, excluded terms use NOT LIKE.
**Alternatives considered:** Full-text search (FTS5), regex
**Reasoning:** LIKE is sufficient for current notification volume (~50-200 active). FTS5 adds complexity for no benefit yet.
**Revisit if:** Notification volume grows large enough that LIKE scans become slow.

---

### [DSH-016] Structured priority column for notifications
**Date:** 2026-05-29
**Phase:** Ideation (005)
**Decided by:** Analyst
**Decision:** Add nullable `priority TEXT` column to notifications table rather than parsing priority from message text.
**Alternatives considered:** Parse from message regex, client-side filtering
**Reasoning:** Structured data enables SQL filtering and sorting. Nullable column is backwards-compatible. Decouples UI from GML's message format.
**Revisit if:** Notifications become fully schema-free.

---

### [DSH-017] Priority validated in Go, not DB CHECK
**Date:** 2026-05-29
**Phase:** Planning (006)
**Decided by:** Developer
**Decision:** Validate priority (Q1-Q4 or empty) in Go handler. No DB CHECK constraint.
**Alternatives considered:** DB CHECK like the `type` column
**Reasoning:** Adding a CHECK that allows NULL + Q1-Q4 is awkward in SQLite ALTER TABLE. Go validation returns clear HTTP 400. Single write path means validation can't be bypassed.
**Revisit if:** Multiple code paths write notifications.

---

### [DSH-018] One comment per notification (1:1)
**Date:** 2026-05-29
**Phase:** Ideation (009)
**Decided by:** Developer (default)
**Decision:** Single editable comment field per notification, not a thread.
**Alternatives considered:** 1:N comment thread with timestamps
**Reasoning:** KISS. The use case is annotation, not conversation.
**Revisit if:** Tomas wants to track changes over time or multiple actors comment.

---

### [DSH-019] Inline click-to-edit comment
**Date:** 2026-05-29
**Phase:** Review (012)
**Decided by:** Tomas
**Decision:** Comment displays as plain text. Click to edit — reveals textarea + Save/Cancel. Empty comments show `[+ comment]` label.
**Alternatives considered:** Always-visible textarea (rejected by Tomas — white input box was visually noisy)
**Reasoning:** Keeps the notification table clean. Most notifications won't have comments, so the default state should be minimal.
**Revisit if:** Inline editing becomes awkward for longer comments.

---

### [DSH-020] Comments persist after dismiss
**Date:** 2026-05-29
**Phase:** Ideation (009)
**Decided by:** Developer (default)
**Decision:** Dismissed notifications retain their comments. API returns comments on dismissed notifications.
**Alternatives considered:** Clear comment on dismiss; hide dismissed from API
**Reasoning:** The stated purpose is GML re-learning — that learning happens after the notification is handled.
**Revisit if:** Storage growth becomes a concern.

---

### [DSH-021] Push is fire-and-forget
**Date:** 2026-05-31
**Phase:** Planning (014)
**Decided by:** Developer
**Decision:** Notification is always stored in DB regardless of push delivery. Push sending runs in a goroutine — failure is logged, not surfaced to API caller.
**Alternatives considered:** Synchronous push with error reporting, retry queue
**Reasoning:** Single-user dashboard with low volume. If push fails, the notification is still visible in the UI. Complexity of delivery guarantees isn't justified.
**Revisit if:** Push reliability becomes a concern or multiple users need delivery tracking.

---

### [DSH-022] VAPID keys stored in SQLite config table
**Date:** 2026-05-31
**Phase:** Planning (014)
**Decided by:** Developer
**Decision:** VAPID keypair auto-generated on first start, stored in config table. Same pattern as jwt_secret.
**Alternatives considered:** Environment variable, file on disk
**Reasoning:** Consistent with existing key management. No env var to configure. Rotate by deleting rows.
**Revisit if:** Need to share VAPID keys across multiple instances.

---

### [DSH-023] Multi-origin WebAuthn via WAMap
**Date:** 2026-05-31
**Phase:** Implementation (015)
**Decided by:** Developer
**Decision:** One `webauthn.WebAuthn` instance per unique RPID, selected by request Host header. Token-gated setup for registering passkeys on new origins.
**Alternatives considered:** Single WebAuthn with multiple RPOrigins (doesn't work — RPID must be singular per spec)
**Reasoning:** Passkeys are RPID-bound. Phone accessing via Tailscale hostname needs its own passkey. WAMap is the minimal fix.
**Revisit if:** go-webauthn adds native multi-RPID support.

---

### [DSH-024] Tailscale handles HTTPS
**Date:** 2026-05-31
**Phase:** Implementation (015)
**Decided by:** Tomas
**Decision:** DSH runs plain HTTP on port 9090. Tailscale sidecar provides Let's Encrypt HTTPS via `tailscale serve`. No self-signed cert management.
**Alternatives considered:** NordVPN Meshnet (tried, failed), self-signed CA certs (implemented then superseded), Cloudflare Tunnel
**Reasoning:** Tailscale worked immediately. Automatic Let's Encrypt is simpler than managing self-signed certs and phone trust stores.
**Revisit if:** Tailscale pricing changes or need public (non-Tailscale) access.

---

### [DSH-025] Setup token written to file, not logs
**Date:** 2026-06-01
**Phase:** Implementation (017)
**Decided by:** Tomas
**Decision:** Setup token written to `/data/setup-token` (0600 perms), read by `run.sh` and displayed once. Never logged to stdout/docker logs.
**Alternatives considered:** Log to stdout (original), pass via env var (visible in `docker inspect`)
**Reasoning:** Tomas flagged that tokens should not be in logs. File on the data volume is readable only by the container and `run.sh`.
**Revisit if:** Need token access without `run.sh` (e.g., remote API-based token retrieval).

---

### [DSH-026] Setup token is one-time use with 10-minute TTL
**Date:** 2026-06-01
**Phase:** Implementation (017)
**Decided by:** Tomas + Developer
**Decision:** Token consumed after first successful passkey registration. Expires 10 minutes after container start regardless. Restart container for a fresh token.
**Alternatives considered:** Multi-use within TTL window, persistent token in DB
**Reasoning:** Single-user dashboard — registering a second device from a new origin is rare. One-time + TTL minimizes exposure window.
**Revisit if:** Need to register multiple devices in one session.

---

### [DSH-005] Team in formation — minimal persona bootstrap
**Date:** 2026-05-27
**Phase:** Ideation
**Decided by:** Analyst (phase lead)
**Decision:** Created minimal Analyst and Developer persona files to satisfy MO requirement that active personas have files. No skill content yet — skills accumulate from project work.
**Alternatives considered:** Proceed without persona files
**Reasoning:** MO states "don't silently act as personas that have no file." Thin files are better than missing files.
**Revisit if:** Other personas are needed for Phase 1 (PM, QA, Security are likely).

---

## DSH-027 — Notifications API `limit` clamps instead of falling back

**Decision:** `ListNotifications` clamps an out-of-range `limit` to a 200 max rather than silently reverting to the default (20). Any positive `limit` is honored up to the ceiling.

**Rationale:** GML callers pass `limit=200` for their dedup guard; the previous `n > 0 && n <= 100` check failed for 200 and fell through to the default 20, silently truncating the guard to the 20 most-recent items (a contributor to GML's repeated-insight bug). Clamping is least-surprising: more is capped, not discarded.

**Affected areas:** `internal/handler/api.go` (`ListNotifications`), `cmd/dsh/integration_test.go` (`TestNotification_LimitClampedNotDropped`)

---

### [DSH-028] Threads use a polymorphic ref, validated at the API layer; no `todo` refs
**Date:** 2026-06-12
**Phase:** Planning (025)
**Decided by:** Developer
**Decision:** `threads.ref_type/ref_id` is a single nullable polymorphic pair (`notification|plan|project`), existence-checked in the handler on create. `todo` is deliberately not a valid ref_type.
**Alternatives considered:** Per-type FK columns; a `thread_refs` join table (M:N)
**Reasoning:** Iteration 1 needs exactly one ref per thread (KISS). Todo IDs are `todo.txt` line indexes that shift on every edit — a ref would silently re-point to a different item.
**Revisit if:** The LLM cross-linking iteration needs one thread on many notifications (add `thread_refs` then), or todos get stable IDs.

---

### [DSH-029] Thread authorship comes from the authenticated identity, never the payload
**Date:** 2026-06-12
**Phase:** Planning (025)
**Decided by:** Developer
**Decision:** `RequireJWT` resolves the OAuth client's display name into the request context; thread/message `author`/`created_by` always come from there (UI: session username). The JSON payload has no author field.
**Alternatives considered:** `author` in the request body
**Reasoning:** Payload authorship is spoofable by any credentialed agent; the client name ("gml", "mnd") is already authenticated and human-meaningful.
**Revisit if:** One client legitimately posts on behalf of multiple identities.

---

### [DSH-030] Threads push on creation only; nav badge counts open threads
**Date:** 2026-06-12
**Phase:** Implementation (026)
**Decided by:** Developer
**Decision:** Web-push fires when a thread is created (mirrors plans). Replies don't push; awareness comes from the nav badge (open-thread count). Per-agent unread tracking is deferred.
**Alternatives considered:** Push on every message; per-user read markers
**Reasoning:** Reply-pushes get noisy fast in a discussion; unread semantics have no consumer yet (ideation 024) — GML needs ref+status lookup, not an inbox.
**Revisit if:** Agent↔agent messaging becomes chatty enough that Tomas misses replies.

---

### [DSH-031] On-demand device enrollment from the Passkeys page (supersedes the run.sh token for adding devices)
**Date:** 2026-06-15
**Phase:** Implementation (028)
**Decided by:** Developer
**Decision:** An authenticated user mints a device-enrollment link on demand from `/settings/passkeys` ("Add a new device" → `POST /settings/passkeys/enroll`, session+CSRF). The endpoint regenerates the setup token (TTL now anchored to *generation*, not process start) and returns a QR code + URL pointing at the **external** origin (`Config.ExternalOrigin()` — first non-loopback `DSH_ORIGIN`). The user scans it on the new device and registers a passkey bound to that origin's RPID. This supersedes [DSH-026] as the way to add a device: the boot-time token printed by `run.sh` expires 10 min after container start and is useless on a long-running container (it's up for days), so the printed URL never works in practice. `run.sh` no longer prints a phone URL; it points to this in-app flow.
**Alternatives considered:** (a) Extend the boot token's TTL to hours — still requires a container restart to re-mint, and a long-lived static token on disk is a worse exposure; (b) re-print on every `run.sh` — `run.sh` doesn't restart an already-running container, so no new token is minted; (c) require a passkey-reauth before minting — rejected to match the existing posture, where add/remove passkey already use session+CSRF only.
**Reasoning:** The user is already authenticated on a trusted device (laptop, localhost passkey via SSH tunnel); "add a device" is the standard pattern. QR delivery removes the error-prone manual entry of a 32-hex-char URL on a phone. Token stays one-time (`Consume()` on successful registration) and short-lived (10 min from generation), so the exposure window is small.
**Affected areas:** `internal/handler/setup_token.go` (`Regenerate`, `TTL`), `internal/handler/auth.go` (`EnrollDevice`, `waForRequest`), `internal/config/config.go` (`ExternalOrigin`), `cmd/dsh/main.go`, `cmd/dsh/web/templates/settings_passkeys.html`, `run.sh`.
**Revisit if:** Multi-user, or a device that can't scan a QR needs enrolling (add copy-link/manual entry — already shown as a clickable link).

---

### [DSH-032] `waForRequest` falls back to a deterministic default RP
**Date:** 2026-06-15
**Phase:** Implementation (028)
**Decided by:** Developer
**Decision:** When the request Host matches no configured origin, `waForRequest` returns the RP for `DefaultRPID` (RPID of the first `DSH_ORIGIN` entry) instead of an arbitrary `for range` map pick. Exact host matches are unchanged.
**Alternatives considered:** Keep the map-iteration fallback; return 400 on unknown host.
**Reasoning:** Go map iteration order is randomized, so the old fallback could hand a client a `localhost` rpId on one request and a `ts.net` rpId on the next — a latent way to break a WebAuthn ceremony non-deterministically. A fixed default is predictable and debuggable. (Extends [DSH-023].)
**Revisit if:** A request legitimately arrives on an origin not in `DSH_ORIGIN`.
