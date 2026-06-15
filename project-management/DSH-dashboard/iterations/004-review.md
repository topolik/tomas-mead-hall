# DSH — Iteration 004: Review

- **Phase:** 3 — Presentation & Review
- **Phase lead:** PM
- **Start:** 2026-05-27
- **End:** 2026-05-27

## What was presented

Full working implementation of iteration 1. See `003-implementation.md` for the complete run log.

Summary of what was built entering this review:
- Login (username + password), TOTP second factor, Passkey (WebAuthn)
- Backlog page: add items, Q1–Q4 grouping, status changes (DB-backed)
- Projects page: populated by REST API push
- Notifications page: populated by REST API push, dismissible
- OAuth 2.1 client credentials: admin UI to create/revoke clients, JWT token endpoint
- ASCII terminal-style UI, HTMX, all assets embedded in binary
- Docker Compose single-command run
- DSH project self-seeded at startup

## Bugs found during review (fixed before Tomas could log in)

1. **`run.sh` used hardcoded vault name "Personal"** — not valid in Tomas's account. Fixed: removed `--vault` flag.
2. **Host port 8080 conflict with Liferay DXP** — container failed to start. Fixed: changed to 9090.
3. **`op item get --fields password` returns concealed placeholder** — Fixed: added `--reveal`.
4. **`DSH_ADMIN_PASSWORD` never reached the container** — commented out in `docker-compose.yml`. Fixed: uncommented as a pass-through env var.
5. **Bootstrap skipped password update on existing DB** — Fixed: bootstrap now updates hash whenever `DSH_ADMIN_PASSWORD` is set.

## Tomas's feedback

Login confirmed working (2026-05-27).

### Bugs reported after logging in

6. **TOTP settings page shows new QR after enrollment** — Fixed: check `auth.GetTOTPSecret` first; render enrolled state if already enrolled.
7. **Passkey login fails: BackupEligible flag mismatch** — go-webauthn v0.17.4 checks stored `BackupEligible` matches authenticator. Fixed: added `flags` column (migration 002), store `cred.Flags.ProtocolValue()` on register, restore via `NewCredentialFlags` on load. Migration runner upgraded to track applied migrations via `schema_migrations` table.

### Features added during review

8. **Remove TOTP/Passkey buttons** — `[Remove TOTP]` and `[Remove Passkey]` added to `/settings/mfa`.
9. **Password re-auth before MFA changes** — `RequireMFAReauth` middleware added; password confirmation required before MFA settings (10-minute window).
10. **Encrypted offsite backup** — `backup.sh`: sqlite3 hot backup → gzip → AES-256-CBC (openssl, 600k PBKDF2 iterations). Passphrase in 1Password. Temp files shredded. `restore.sh`: decrypt → decompress → `docker cp` into volume.
11. **OAuth2 revocation is immediate** — `ValidateToken` now checks `revoked_at` in DB; revoked tokens rejected on next API call.
12. **Unified audit log** — All sensitive events (auth, MFA, tokens, API calls, admin actions) written to `audit_log` table with actor, IP, detail. Admin UI at `/admin/audit`.
13. **Real client IP** — `network_mode: host` in docker-compose; `realIP()` checks X-Real-IP → X-Forwarded-For → RemoteAddr.
14. **Failed API calls in audit log** — `RequireJWT` logs `api_call_failure` with error detail.
15. **Projects from filesystem** — `pmreader` package scans `project-management/*/PROJECT.md`; `DSH_PM_PATH` bind-mounted. DB-backed projects table is no longer used for UI.
16. **todo.txt replaces DB backlog** — `todoreader` package reads/writes `todo.txt` at `DSH_TODO_PATH`. Supports legacy multi-line format (continuation-line fallback) and new single-line `- [s] text  #Q2 #date` format. UI renamed from Backlog to todo.txt.
17. **Passkey-only auth** — Password login, TOTP, and MFA re-auth gate removed entirely. First-run handled by `/setup` (public, auto-redirected when no passkeys exist); registers passkey and creates session in one flow. `/login` is passkey-only.
18. **Named passkeys** — `name` column added (migration 005). Name entered before registration. Settings page lists each passkey with name, date, and individual `[Remove]` button.
19. **Edit todo items** — `[Edit]` per row in todo list; opens pre-filled form at `/todo/{id}/edit` for text + priority. Preserves status and original added date.
20. **Consistent button style** — `a.btn` CSS class added so link-buttons (`[Edit]`, `[Cancel]`) match `<button>` style.

## Final state

- Auth: passkey-only. Setup at `/setup` on clean DB.
- todo.txt: bind-mounted at `/todo.txt`, read/written by dashboard.
- Projects: read from `project-management/*/PROJECT.md` via bind mount.
- Backup: `backup.sh` / `restore.sh` scripts for encrypted compressed snapshots.
- Audit: all sensitive events logged to `audit_log`.

## Decisions

### [DSH-010] Passkey-only authentication
**Date:** 2026-05-27
**Phase:** Review
**Decided by:** Tomas
**Decision:** Remove password login and TOTP entirely. Authentication is passkey-only.
**Alternatives considered:** Keep password as fallback
**Reasoning:** Single user, single machine. Passkey is phishing-resistant and simpler. Password + TOTP adds complexity with no benefit when only one person uses the dashboard.
**Revisit if:** Need to add another user or access from a device without passkey support.

### [DSH-011] todo.txt as primary backlog storage
**Date:** 2026-05-27
**Phase:** Review
**Decided by:** Tomas
**Decision:** Backlog stored in repo-root `todo.txt`, not the database.
**Alternatives considered:** Keep DB-backed backlog
**Reasoning:** File is human-readable, diffable, and already existed as a manual backlog before DSH. Having one canonical source in the repo is simpler than syncing DB and file.
**Revisit if:** Multi-user editing or rich item metadata is needed.

### [DSH-012] Projects from filesystem, not database
**Date:** 2026-05-27
**Phase:** Review
**Decided by:** Tomas
**Decision:** Projects page reads `project-management/*/PROJECT.md` via bind mount; no DB writes.
**Alternatives considered:** API-pushed DB rows (original design)
**Reasoning:** The files already exist and are the authoritative source. Removing the API-push path eliminates a sync problem. `pmreader` is ~90 lines and requires no DB schema changes.
**Revisit if:** Projects need computed fields not present in PROJECT.md.

## Next iteration plan

(to be decided)
