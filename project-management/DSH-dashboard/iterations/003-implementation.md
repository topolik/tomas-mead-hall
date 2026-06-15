# DSH — Iteration 003: Implementation

- **Phase:** 2 — Implementation
- **Phase leads:** Developer + QA
- **Start:** 2026-05-27
- **End:** (in progress)

## Environment
- Go 1.25.9
- Docker 29.4.2 / Docker Compose v5.1.3

## Build log

### Slice 1 — Skeleton (health endpoint + Docker)
- [x] `go.mod`, `cmd/dsh/main.go`, Dockerfile, docker-compose.yml, Makefile
- [x] Verify: `curl :18080/api/v1/health` → `{"status":"ok","version":"0.1.0"}`

### Slice 2 — DB + migrations
- [x] SQLite (modernc.org/sqlite, pure Go) + embedded SQL migrations at startup
- [x] Verify: schema applied on first start, idempotent on restart

### Slice 3 — Password auth (no MFA)
- [x] argon2id password hashing, session cookies, CSRF tokens, login rate limiter
- [x] Bootstrap: admin user `tomas` on first run; password printed to stdout if not set via env
- [x] Verify: login → 302 → /backlog; protected page with cookie → 200

### Slice 4 — Dashboard UI + API
- [x] Backlog (Q1-Q4 groups, add/status/priority update), Projects, Notifications, Admin Clients pages
- [x] `/api/v1/health`, `/api/v1/projects` (upsert), `/api/v1/notifications`
- [x] DSH project seeded at startup (day-one dogfood)
- [x] Verify: POST via API → project appears in /projects; notification appears in /notifications

### Slice 5 — TOTP
- [x] Enrollment: TOTP secret generated, QR code rendered, code verified and stored (AES-GCM encrypted)
- [x] Login: MFA pending session → /login/mfa → TOTP verify → full session

### Slice 6 — Passkey
- [x] WebAuthn registration ceremony (go-webauthn/webauthn)
- [x] WebAuthn discoverablelogin authentication ceremony
- [x] Credentials stored in passkey_credentials table

### Slice 7 — OAuth 2.1 (client credentials)
- [x] POST /oauth/token: validate client_id + argon2id secret hash → issue HS256 JWT
- [x] API middleware: validate JWT on all /api/v1/ routes except health
- [x] Admin UI: create client (secret shown once), list clients, revoke client
- [x] Verify: create client → get token → POST /api/v1/projects with Bearer → 200

### Slice 8 — Tests + README
- [x] Unit tests: password hash/verify, TOTP encrypt/decrypt, OAuth token issue/validate
- [x] Integration tests: health, unauth redirect, login flow, OAuth2 token flow
- [x] Docker image builds and runs: `docker build -t dsh:latest .` → health 200
- [x] README.md complete

## Bugs found and fixed

1. **Go template inheritance broken**: All page templates shared one `{{define "content"}}` block — last file parsed won for all pages. Fixed by making each template self-contained (no inheritance).

2. **embed paths relative to source file**: `//go:embed web/templates` in `cmd/dsh/main.go` resolved relative to `cmd/dsh/`, not the module root. Fixed by moving `web/` under `cmd/dsh/`.

3. **Auth handler: unused variable `sess`**: Compiler error in TOTPEnrollSubmit. Fixed by renaming to `_, sesData, _`.

4. **Integration test: leftover `cookiejar` import**: From an unused snippet. Removed.

5. **Port 8080 in use by Liferay DXP**: All local test runs redirected to port 18080.

## Decisions

### [Decision: Standalone templates instead of inheritance]
**Date:** 2026-05-27
**Phase:** Implementation
**Decided by:** Developer
**Decision:** Each HTML template is self-contained (nav duplicated). No template inheritance.
**Alternatives considered:** Per-request template parsing (avoids namespace collision); named content blocks per page
**Reasoning:** Go html/template puts all parsed templates in a shared namespace — `{{define "content"}}` from the last file wins for all pages. Per-request parsing works but adds overhead. Standalone templates are the simplest fix and maintainability cost is low (nav is ~2 lines).
**Revisit if:** Many pages added — extract nav as a Go-level wrapper function.

### [Decision: web/ directory under cmd/dsh/]
**Date:** 2026-05-27
**Phase:** Implementation
**Decided by:** Developer
**Decision:** `web/` lives at `cmd/dsh/web/` rather than the project root.
**Alternatives considered:** Root-level embed package; symlinks
**Reasoning:** Go's `//go:embed` resolves paths relative to the source file directory, not the module root. `cmd/dsh/main.go` needs `web/` to be a subdirectory of `cmd/dsh/`. Moving it there is the cleanest solution.
**Revisit if:** Multiple binaries need the same assets — then a shared embed package at the right level makes sense.

### [Decision: TOTP key auto-generated and persisted to DB]
**Date:** 2026-05-27
**Phase:** Implementation
**Decided by:** Developer + Security
**Decision:** If `DSH_TOTP_KEY` env var is absent, a 32-byte key is generated and stored in the `config` table.
**Alternatives considered:** Always require `DSH_TOTP_KEY` (fail loudly if absent)
**Reasoning:** For a single-user home server, auto-generation with DB persistence is simpler than managing yet another env var. The key is in the same SQLite volume as the data it protects. If `DSH_TOTP_KEY` is set, it takes precedence — allowing key rotation or hardening.
**Revisit if:** Threat model changes (key and data in same volume is a concern if volume is exported).

### [Decision: JWT secret auto-generated and persisted to DB]
**Date:** 2026-05-27
**Phase:** Implementation  
**Decided by:** Developer + Security
**Decision:** JWT signing secret generated on first start, stored in `config` table.
**Alternatives considered:** Require `DSH_JWT_SECRET` env var
**Reasoning:** Same reasoning as TOTP key — home server, single user. Tokens become invalid when container is recreated without a backup of the volume, which is acceptable.
