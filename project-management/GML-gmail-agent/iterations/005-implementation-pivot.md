# GML — Iteration 005: Implementation (Pivot — Daemon + Stdin Credentials)

- **Phase:** 2 — Implementation
- **Phase lead:** Developer
- **Start:** 2026-05-27
- **End:** 2026-05-28

---

## What was implemented

Pivot from one-shot + disk credentials to long-running daemon + in-memory credentials (stdin pipe from `op`). All code from iteration 003 carried forward; the credential injection and container orchestration were rewritten.

### Files created/modified

- `internal/creds/creds.go` — OAuth2 credentials in heap only, token refresh via HTTPS
- `internal/scheduler/scheduler.go` — Go ticker scheduler for `gml serve`
- `internal/config/config.go` — added `mode` (readonly/readwrite) and `schedule` config
- `cmd/gml/main.go` — added `serve` command, stdin credential loading, readonly guard
- `internal/gws/gws.go` — all functions take `*creds.Creds`, pass token via `GOOGLE_WORKSPACE_CLI_TOKEN` env var to subprocess
- `internal/stats/stats.go` — updated to use `*creds.Creds`
- `internal/rules/engine.go` — updated to use `*creds.Creds`
- `setup.sh` — complete rewrite: manual GCP Console flow, 1Password storage
- `run.sh` — `op` pipe to `docker compose run -T`
- `docker-compose.yml` — removed `stdin_open: true`
- `rules.yaml` — added `mode: readonly`, `schedule`, rules commented out for later stages

### 1Password structure

Three items:
- **"GML Gmail Agent"** (Login) — `username` = OAuth client_id, `password` = OAuth client_secret
- **"GML Gmail Read-Only Credentials"** (Password) — `credential` = read-only token JSON from `gws auth export`
- **"GML Gmail Read-Write Credentials"** (Password) — `credential` = modify-scoped token JSON from `gws auth export`

---

## Bugs found and fixed

### Bug 1: gws ignores `client_secret.json` in config dir
**Symptom:** `gws auth login` returned "No OAuth client configured" even with a correctly written `client_secret.json` at `/gws-config/client_secret.json`.
**Root cause:** gws does not actually read `client_secret.json` from its config dir despite suggesting it in the error message.
**Fix:** Pass credentials via `GOOGLE_WORKSPACE_CLI_CLIENT_ID` and `GOOGLE_WORKSPACE_CLI_CLIENT_SECRET` env vars instead.

### Bug 2: `docker compose run` allocates TTY by default
**Symptom:** `run.sh` failed with "cannot attach stdin to a TTY-enabled container because stdin is not a terminal".
**Root cause:** Unlike `docker run`, `docker compose run` allocates a TTY by default. Combined with `stdin_open: true` in compose file, piped stdin was rejected.
**Fix:** Use `-T` flag (disable TTY) instead of `-i`, and removed `stdin_open: true` from `docker-compose.yml`.

### Bug 3: `op --reveal` CSV-escapes values containing quotes
**Symptom:** JSON credentials from `op item get --fields credential --reveal` had outer `"` wrapping and doubled `""` internal quotes, causing Go JSON parse failure.
**Root cause:** `op --reveal` applies CSV-style escaping to any value containing quote characters, regardless of content type.
**Fix:** Use `op item get --format json` and extract the `value` field via python3 in `run.sh`.

### Bug 4: `read -rsp` gives no feedback on paste
**Symptom:** User pasted client secret but couldn't tell if it was received (silent input).
**Fix:** Changed to `read -rp` (visible input) — acceptable for interactive terminal setup.

### Bug 5: `gcloud iap oauth-clients create` is deprecated and wrong type
**Symptom:** Attempted to automate OAuth client creation via `gcloud iap oauth-clients create`.
**Root cause:** Command is deprecated (shutdown March 2026), only creates IAP web clients (not Desktop/native), and has no `--type` flag.
**Fix:** Dropped gcloud automation entirely. Manual GCP Console setup is the single path.

---

## Test results (live)

| Test | Result | Notes |
|------|--------|-------|
| `./run.sh profile` | PASS | Returns `user@example.com`, 229682 messages, 82404 threads |
| `./run.sh run --dry-run` | PASS | "No messages matched any rules" (rules commented out — correct) |
| `./run.sh stats` | PASS | 500 unread, top sender: noreply@jira.example.com (94) |
| `setup.sh` idempotency | PASS | Detects existing 1Password item, offers skip or overwrite |
| `setup.sh` client reuse | PASS | Reads client_id/secret from 1Password on re-run |
| 1Password client storage | PASS | Stored immediately before browser login |

---

## Deferred

- Rules definition — deferred to later project stage (rules commented out in `rules.yaml`)
- `gml serve` live test — requires active rules
- Mode 2 (Claude AI analysis) — after DSH can display agent output
- Mode 3 (Claude proposes rules) — after Mode 2

---

## Decisions

### [GML-015] Manual GCP Console setup only — no gcloud automation
**Date:** 2026-05-28
**Phase:** Implementation
**Decided by:** Developer + Tomas
**Decision:** Drop all gcloud-based automation for OAuth client creation. Use manual GCP Console flow exclusively.
**Alternatives considered:** (a) `gcloud iap oauth-clients create` — deprecated, wrong client type; (b) `clientauthconfig.googleapis.com` REST API — user rejected; (c) `gcloud auth application-default login` with default client — no custom OAuth client
**Reasoning:** `gcloud iap` is deprecated and can't create Desktop clients. REST API adds complexity. Manual Console setup is simple, one-time, and reliable.
**Revisit if:** gcloud adds a non-deprecated command for creating Desktop OAuth clients

### [GML-016] Client credentials as 1Password Login item (username/password)
**Date:** 2026-05-28
**Phase:** Implementation
**Decided by:** Developer + Tomas
**Decision:** Store OAuth client_id as username and client_secret as password in a Login-type 1Password item ("GML Gmail Agent"), separate from the token item.
**Alternatives considered:** (a) JSON blob in single field — required python3 to parse on read; (b) Single item with multiple custom fields — more complex op commands
**Reasoning:** Native 1Password field types (username/password) read cleanly with `op item get --fields username/password --reveal` without any JSON parsing.
**Revisit if:** More credential fields are needed beyond client_id and client_secret

### [GML-017] gws credentials via env vars, not client_secret.json file
**Date:** 2026-05-28
**Phase:** Implementation
**Decided by:** Developer
**Decision:** Pass OAuth client credentials to gws via `GOOGLE_WORKSPACE_CLI_CLIENT_ID` and `GOOGLE_WORKSPACE_CLI_CLIENT_SECRET` env vars instead of writing a `client_secret.json` file.
**Alternatives considered:** `client_secret.json` in `GOOGLE_WORKSPACE_CLI_CONFIG_DIR` — gws doesn't actually read it despite error message suggesting otherwise
**Reasoning:** Tested empirically: file approach fails silently, env var approach works. These env vars are only in the setup container (interactive, short-lived), not in the runtime container.
**Revisit if:** gws fixes client_secret.json loading from config dir
