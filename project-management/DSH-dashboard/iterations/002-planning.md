# DSH — Iteration 002: Planning

- **Phase:** 1 — Planning
- **Phase leads:** PM + QA
- **Supporting:** Security, Performance
- **Start:** 2026-05-27
- **End:** 2026-05-27

---

## QA: Acceptance Criteria (written first)

These criteria define "done" for iteration 1. Implementation is not complete until every item below passes.

### Auth — UI (username + password + MFA)

- [ ] `GET /` unauthenticated → HTTP 302 redirect to `/login`
- [ ] `GET /login` returns login form (HTML)
- [ ] `POST /login` with valid username + password, no MFA configured → session created, redirect to `/`
- [ ] `POST /login` with valid username + password, TOTP enrolled → redirect to `/login/mfa`, session not yet active
- [ ] `POST /login/mfa` with valid TOTP code → session created, redirect to `/`
- [ ] `POST /login/mfa` with invalid TOTP code → error shown, session not created
- [ ] `POST /login` with wrong password → error message shown, no session
- [ ] Session cookie is `HttpOnly`, `SameSite=Lax`
- [ ] `GET /settings/mfa` (authenticated) shows TOTP enrollment form with QR code
- [ ] `POST /settings/mfa/totp` with correct verification code → TOTP enrolled for user
- [ ] `GET /settings/mfa/passkey` (authenticated) shows Passkey registration flow
- [ ] Passkey registration completes and credential is stored
- [ ] `POST /login` → Passkey authentication flow → session created (no password needed)
- [ ] `GET /logout` → session destroyed, redirect to `/login`
- [ ] Rate limit: 5 failed login attempts → temporary lockout (429 or redirect with delay)

### Auth — API (OAuth 2.1 client credentials)

- [ ] `POST /oauth/token` with valid `client_id` + `client_secret` + `grant_type=client_credentials` → returns `access_token` (JWT), `token_type=Bearer`, `expires_in`
- [ ] `POST /oauth/token` with wrong `client_secret` → HTTP 401 `{"error":"invalid_client"}`
- [ ] `POST /oauth/token` with unknown `client_id` → HTTP 401 `{"error":"invalid_client"}`
- [ ] JWT is verifiable with the server's public key (or shared secret)
- [ ] `GET /api/v1/health` without token → HTTP 200 (health check is public)
- [ ] `POST /api/v1/projects` without token → HTTP 401
- [ ] `POST /api/v1/projects` with valid token → HTTP 200/201
- [ ] `POST /api/v1/projects` with expired token → HTTP 401
- [ ] Admin UI (authenticated as Tomas): create OAuth2 client → client_id + client_secret issued
- [ ] Admin UI: list OAuth2 clients
- [ ] Admin UI: revoke/delete OAuth2 client

### Dashboard — Backlog

- [ ] `GET /backlog` (authenticated) → shows all backlog items grouped by Eisenhower quadrant (Q1–Q4)
- [ ] Each item shows: text, priority, status (open/in_progress/done/parked), added date
- [ ] `POST /backlog` (form submit) creates a new backlog item → appears in list
- [ ] Tomas can change status of an item via the UI (HTMX or form)
- [ ] Tomas can change priority of an item via the UI
- [ ] Empty quadrant shows a placeholder ("nothing here") not an error

### Dashboard — Projects

- [ ] `GET /projects` (authenticated) → shows all projects
- [ ] Each project shows: code, name, status, priority, phase, last updated, lead
- [ ] **DSH project is in the list** (day-one dogfood: pushed via API or seeded at startup)
- [ ] `POST /api/v1/projects` with DSH project payload → project appears/updates in `/projects`

### Dashboard — Notifications

- [ ] `GET /notifications` (authenticated) → shows active (non-dismissed) notifications
- [ ] Each notification shows: message, project code, created time
- [ ] Tomas can dismiss a notification → it disappears from the list
- [ ] `POST /api/v1/notifications` with valid token + payload → notification created, appears in UI
- [ ] Dismissed notifications are not deleted, just hidden (dismissal is recorded)

### UI / Accessibility

- [ ] All pages use monospace font
- [ ] Layout uses ASCII-style structure (box-drawing, `[STATUS]` labels, `---` separators)
- [ ] Navigation links are plain `<a>` tags (not JS-driven)
- [ ] HTML source uses semantic elements: `<table>`, `<ul>`, `<nav>`, `<main>`, `<header>`, `<form>`
- [ ] Page renders and is navigable with JavaScript disabled (except HTMX-powered inline updates)
- [ ] No external CSS/JS resources — all assets served by the Go binary

### API contract

- [ ] `GET /api/v1/health` → `{"status":"ok","version":"..."}`
- [ ] `POST /api/v1/projects` body: `{"code":"DSH","name":"Dashboard","status":"Planning","priority":"Q2","lead":"Developer","phase":"Planning","last_updated":"2026-05-27"}`
- [ ] `POST /api/v1/notifications` body: `{"project_code":"DSH","message":"...","type":"action_needed"}`
- [ ] All API errors return JSON: `{"error":"...","message":"..."}`
- [ ] API versioned at `/api/v1/`

---

## PM: Implementation Checklist

Ordered by dependency. Each item is a concrete deliverable.

### Foundation
- [ ] Initialize Go module (`go.mod`), project layout: `cmd/`, `internal/`, `web/` (templates + static)
- [ ] Dockerfile: multi-stage build, final image Alpine-based
- [ ] `docker-compose.yml`: single container, named volume for SQLite, configurable port
- [ ] SQLite database connection with migrations (use `golang-migrate` or embed SQL migration files)
- [ ] Environment config: `DSH_PORT`, `DSH_DB_PATH`, `DSH_JWT_SECRET` (generated at first run if absent)

### Database schema
- [ ] `users` table: id, username, password_hash (argon2id), created_at
- [ ] `totp_credentials` table: user_id, secret (encrypted), verified, created_at
- [ ] `passkey_credentials` table: user_id, credential_id, public_key, sign_count, created_at
- [ ] `sessions` table: id (token), user_id, created_at, expires_at
- [ ] `oauth2_clients` table: client_id, client_secret_hash, name, created_at, revoked_at
- [ ] `backlog_items` table: id, text, priority (Q1–Q4), status, added_date, updated_at
- [ ] `projects` table: code (PK), name, status, priority, lead, current_phase, last_updated
- [ ] `notifications` table: id, project_code, message, type, created_at, dismissed_at

### Auth — UI
- [ ] Login page (`/login`): username + password form
- [ ] Session middleware: validate session cookie on every protected route
- [ ] `POST /login` handler: verify password (argon2id), check MFA enrollment, issue or advance session
- [ ] MFA page (`/login/mfa`): TOTP input form
- [ ] `POST /login/mfa` handler: verify TOTP code, issue final session
- [ ] `GET /logout` handler: destroy session
- [ ] CSRF token on all POST forms
- [ ] Rate limiter on `/login` (in-memory, per IP, 5 attempts / 10 min window)
- [ ] TOTP enrollment: generate secret, render QR code (use `skip2/go-qrcode`), verify code
- [ ] Passkey registration: WebAuthn ceremony (`go-webauthn/webauthn`)
- [ ] Passkey login: WebAuthn authentication ceremony

### Auth — API (OAuth 2.1 client credentials)
- [ ] `POST /oauth/token` handler: validate client_id + client_secret_hash, issue JWT
- [ ] JWT: HS256 or RS256, claims: `sub=client_id`, `iss=dsh`, `exp`, `iat`
- [ ] API middleware: validate JWT on all `/api/v1/` routes except `/api/v1/health`
- [ ] Admin UI: `GET /admin/clients` — list clients
- [ ] Admin UI: `POST /admin/clients` — create client (generate client_id + secret, show secret once)
- [ ] Admin UI: `POST /admin/clients/{id}/revoke` — revoke client

### Dashboard UI
- [ ] Base layout template: nav bar (`Backlog | Projects | Notifications | Settings`), monospace font, ASCII style
- [ ] `GET /backlog`: render items by quadrant; HTMX form to add item and change status
- [ ] `GET /projects`: render project table
- [ ] `GET /notifications`: render active notifications; HTMX dismiss button
- [ ] `GET /settings/mfa`: TOTP + Passkey enrollment UI
- [ ] Seed DSH project at startup (or first API call from the project itself)

### API endpoints
- [ ] `GET /api/v1/health`
- [ ] `POST /api/v1/projects` (upsert by code)
- [ ] `POST /api/v1/notifications`

### Tests
- [ ] Unit tests: password hashing, TOTP verification, JWT issue/validate
- [ ] Integration tests (using `httptest`): login flow, OAuth2 token flow, API ingest, backlog CRUD
- [ ] `make test` runs all tests, `make docker-build` builds the image

### First run / bootstrap
- [ ] On first startup with empty DB: create admin user (username `tomas`, password from `DSH_ADMIN_PASSWORD` env var)
- [ ] DSH project seeded in the projects table at startup

---

## Security Review

_Review by Security persona against standing orders._

**Flagged items — must be addressed in implementation:**

1. **Password storage**: Argon2id required (not bcrypt — argon2id is the current recommendation for new implementations). Checklist already specifies this. ✓
2. **TOTP seeds**: Stored encrypted in the DB. Encryption key = `DSH_TOTP_KEY` env var. Must not be stored in plaintext. **Add `DSH_TOTP_KEY` to env config checklist.**
3. **OAuth2 client secrets**: Hashed (argon2id) before storage, shown in plaintext exactly once at creation. Checklist specifies this. ✓
4. **JWT signing key**: Generated at first startup if `DSH_JWT_SECRET` is absent; stored in DB or written to volume. Never hardcoded. ✓
5. **CSRF**: All state-mutating form endpoints need CSRF token validation. In checklist. ✓
6. **Rate limiting**: Login endpoint rate-limited. In checklist. ✓ Token endpoint should also be rate-limited. **Add to checklist.**
7. **Session cookie flags**: `HttpOnly`, `SameSite=Lax`, `Secure` if behind TLS. In acceptance criteria. ✓ — Note: MVP is HTTP via SSH tunnel, so `Secure` flag cannot be set. Acceptable for now.
8. **Passkey credential binding**: `go-webauthn/webauthn` handles RP ID and origin validation — ensure `DSH_ORIGIN` env var is set and validated. **Add to env config.**
9. **Admin routes**: `/admin/` endpoints must be protected by session auth (not just API JWT). Verify this in implementation.
10. **DB file permissions**: Docker volume for SQLite should be mounted with restrictive permissions (600). Add note to docker-compose.

**Cleared items**: No secrets in code, no external auth dependencies with unclear trust boundaries, single-user scope reduces attack surface significantly.

---

## Performance Review

_Review by Performance persona._

Single-user home tool — no scale concerns. Two things worth flagging:

1. **Notifications query**: `SELECT * FROM notifications WHERE dismissed_at IS NULL` — add index on `dismissed_at`. Simple.
2. **Projects and backlog queries**: Both are small tables (tens of rows). No pagination needed for MVP; add a `LIMIT 500` guard to prevent runaway queries if data grows unexpectedly.
3. **HTMX partial renders**: Prefer returning minimal HTML fragments on HTMX requests (not full page re-renders). Keeps responses snappy and reduces DOM churn.

Nothing blocks shipping.

---

## Architecture Diagram

See `diagrams/architecture.md`.

---

## Open Decisions Resolved in This Phase

### [Decision: Auth model]
**Date:** 2026-05-27
**Phase:** Planning
**Decided by:** Tomas
**Decision:** UI uses username + password + MFA (TOTP or Passkey). API uses OAuth 2.1 client credentials flow.
**Alternatives considered:** No auth (home network only); static bearer token
**Reasoning:** Tomas explicitly requested this. Dashboard will be accessible via SSH tunnel from any network — proper auth is appropriate. OAuth 2.1 client credentials is the right fit for machine-to-machine API calls from other projects.
**Revisit if:** External auth provider (Zitadel/Authentik) is preferred over in-house implementation; revisit in iteration 2.

### [Decision: OAuth 2.1 scope — client credentials only for MVP]
**Date:** 2026-05-27
**Phase:** Planning
**Decided by:** PM + Security
**Decision:** MVP implements OAuth 2.1 client credentials flow only. No authorization code flow, no refresh tokens in v1.
**Alternatives considered:** Full OAuth 2.1 server with authorization code + PKCE
**Reasoning:** Other projects (agents, scripts) are machine clients — client credentials is the right grant type. Authorization code flow adds complexity with no benefit for this use case.
**Revisit if:** A human-in-the-browser OAuth flow is needed.

### [Decision: Frontend — ASCII-style HTMX + Go templates]
**Date:** 2026-05-27
**Phase:** Planning
**Decided by:** Tomas
**Decision:** Server-rendered HTML using Go `html/template` + HTMX for inline updates. CSS: monospace font, ASCII box-drawing characters, `[STATUS]` labels. No external assets.
**Alternatives considered:** React/Svelte SPA
**Reasoning:** Tomas explicitly requested ASCII-like UI readable by LLM agents. HTMX + server templates achieves this: semantic HTML, no JS required to read the page, all data visible in source.
**Revisit if:** Rich client-side interactivity becomes a real requirement.

### [Decision: SQLite + named Docker volume]
**Date:** 2026-05-27
**Phase:** Planning
**Decided by:** PM + Performance
**Decision:** SQLite stored in a named Docker volume at `/data/dsh.db`.
**Alternatives considered:** Postgres (overkill), flat files (no query capabilities)
**Reasoning:** Single user, dozens of rows per table — SQLite is the right fit. Named volume survives container restarts. Easy to back up with `cp`.
**Revisit if:** Multiple concurrent writers, or data size exceeds SQLite comfort zone (~50GB).

### [Decision: Bootstrap — DSH project seeded at startup]
**Date:** 2026-05-27
**Phase:** Planning
**Decided by:** Tomas + PM
**Decision:** On first startup, DSH project is seeded into the projects table. Day-one dogfood.
**Alternatives considered:** Push via API only (chicken-and-egg: dashboard must exist before the API call can be made)
**Reasoning:** The dashboard can't push data to itself before it's running. Seed at startup resolves the bootstrap problem.
**Revisit if:** Self-reporting via API is cleaner in a later iteration.

### [Decision: Network topology — out of scope]
**Date:** 2026-05-27
**Phase:** Planning
**Decided by:** Tomas
**Decision:** How Tomas accesses the home server (SSH port forwarding) is a deployment detail — not modeled or documented in this project.
**Alternatives considered:** Tailscale integration, mDNS
**Reasoning:** Tomas accesses via SSH tunnel; this is operational knowledge, not application logic.
**Revisit if:** Dashboard needs to know its own public URL (e.g., for OAuth2 redirect URIs — not needed for client credentials).

---

## Phase 1 Sign-off

**Tomas reviews this plan and confirms:**
- [ ] Acceptance criteria are correct and complete
- [ ] Implementation checklist covers all requirements
- [ ] Security concerns are acceptable
- [ ] Architecture diagram matches expectations

_Phase 2 (Implementation) starts after Tomas confirms the plan._
