# GML — Iteration 004: Pivot Planning (Long-Running Container + In-Memory Credentials)

- **Phase:** 1 — Planning (Pivot)
- **Phase lead:** Developer + PM
- **Start:** 2026-05-27
- **Pivot tag:** `gml-v1-oneshot` (one-shot architecture preserved)

---

## Pivot Trigger

Tomas requested: "the container can run all the time and cron job inside it or just a scheduled task written in go — keep the creds in memory in the container, don't ever write them to disk."

Additionally: env vars are rejected as a credential injection mechanism because they are visible via `docker inspect` and `/proc/1/environ`. Credentials must never appear in process arguments or env vars — only in Go heap memory.

---

## Architecture Change

### Previous (gml-v1-oneshot)
- `docker compose run --rm gml <command>` — one-shot invocations
- Credentials on host disk at `~/.config/gml/credentials.json` (mode 600)
- Injected via bind mount + `GOOGLE_WORKSPACE_CLI_CREDENTIALS_FILE` env var

### New (gml-v2-daemon)
- `gml serve` daemon — long-running container with Go-based scheduler
- Credentials injected via **stdin pipe** at container startup (from `op` on host)
- Credentials stored exclusively in Go heap (`*Creds` struct)
- Never written to disk inside container; never in env vars
- gws subprocesses receive only `GOOGLE_WORKSPACE_CLI_TOKEN=<access_token>` — short-lived, in-process env, cleared after subprocess exits

### Credential injection flow
```
op (host, already signed in)
  └─ stdout (pipe) ──────────────────────────────────────────┐
                                                              ↓
docker compose run -i gml serve  ←── reads stdin ←── JSON credentials
                                       │
                                       ↓
                               Go heap: *Creds struct
                               os.Unsetenv on anything sensitive
                               gws subprocess: GOOGLE_WORKSPACE_CLI_TOKEN only
```

### One-shot commands (stats, run --dry-run)
Same pipe pattern — credentials injected via stdin, processed, container exits:
```bash
op item get "GML Gmail Read-Only Credentials" --fields credential --reveal \
  | docker compose run -i gml stats
```

---

## Checklist for Iteration 005 (Implementation)

### Core changes

- [ ] `internal/creds/creds.go` — `Creds` struct; `LoadFromReader(r io.Reader)` reads JSON from stdin; `Token()` returns valid access token (refreshes if <5 min remaining); token refresh via HTTP POST to `https://oauth2.googleapis.com/token`
- [ ] `internal/gws/gws.go` — accept token string; pass as `GOOGLE_WORKSPACE_CLI_TOKEN` env var to subprocess; remove credentials-file path param
- [ ] `internal/stats/stats.go` — thread `*Creds` through all gws calls
- [ ] `internal/rules/engine.go` — thread `*Creds` through all gws calls
- [ ] `cmd/gml/main.go` — add `serve` command (daemon mode); read credentials from stdin for all commands; remove file-based credential loading
- [ ] `internal/scheduler/scheduler.go` — ticker-based scheduler (no external dep); configurable interval from rules.yaml; runs `gml run` logic on schedule
- [ ] `docker-compose.yml` — `stdin_open: true`; `restart: unless-stopped` for `serve` mode; remove credential file volume mount and credential env vars
- [ ] `setup.sh` — remove `~/.config/gml/credentials.json` write; verify `op` is signed in and test-run credential export; print correct `run.sh serve` command
- [ ] `run.sh` — `serve` subcommand: `op item get ... | docker compose run -i gml serve`; one-shot subcommands: `op item get ... | docker compose run -i gml <cmd>`; remove `GML_CREDENTIALS_FILE` logic
- [ ] `rules.yaml` — add `schedule:` block (interval in minutes)
- [ ] `README.md` — update all commands; remove disk-credential references; document stdin injection
- [ ] `.gitignore` — add `.env` (defensive; no .env file in new design but guard anyway)

### Security constraints (non-negotiable)

- Credentials JSON never written to any file inside the container
- Credentials never in env vars (container or subprocess)
- gws subprocesses receive only `GOOGLE_WORKSPACE_CLI_TOKEN` — the access token alone, no refresh token
- Refresh token stays in Go heap only
- Access token cleared from Go heap after use (replaced by next refresh)

---

## Acceptance Tests

### T7 — Credential injection via stdin
**Precondition:** Valid credentials JSON available  
**Action:** `echo "$CREDS_JSON" | docker compose run -i gml profile`  
**Expected:** Prints email + message count; exits 0  
**Verifies:** stdin pipe injection works; no file writes needed

### T8 — No credentials file written inside container
**Precondition:** Container ran T7  
**Action:** `docker compose run -i gml sh -c 'find / -name "*.json" 2>/dev/null | grep -v proc'`  
**Expected:** No credentials JSON files found inside container filesystem  
**Verifies:** Hard no-disk constraint

### T9 — Daemon scheduler fires on schedule
**Precondition:** `gml serve` running with `schedule.interval_minutes: 1` in rules.yaml  
**Action:** Wait 2 minutes; observe logs  
**Expected:** Log lines showing rules evaluated at ~1-minute intervals  
**Verifies:** Go scheduler fires correctly; token refresh works across runs

### T10 — Container restart requires re-injection (expected behavior)
**Precondition:** Running `gml serve` daemon  
**Action:** `docker compose restart gml` without re-piping credentials  
**Expected:** Container exits (no credentials); user must re-run `op ... | docker compose run -i gml serve`  
**Verifies:** No credential persistence between container lifetimes (desired property)

### T11 — Token refresh
**Precondition:** `gml serve` running with an access token that is about to expire  
**Action:** Force token expiry (or wait); trigger a `gml run --dry-run`  
**Expected:** Run succeeds; log shows token refreshed; gws subprocess received new token  
**Verifies:** `Creds.Token()` refresh path works

### T12 — One-shot stats via stdin
**Precondition:** Valid credentials available via `op`  
**Action:** `op item get "GML Gmail Read-Only Credentials" --fields credential --reveal | ./run.sh stats`  
**Expected:** Inbox stats printed; container exits 0  
**Verifies:** One-shot mode works alongside daemon mode

### T1–T6 (carried from iteration 002/003)
T1 (profile), T2 (stats), T3 (run --dry-run), T4 (run live), T6 (archive reversible) — still required; now use stdin injection instead of credential file

---

## Decisions

### [GML-012] Credential injection via stdin pipe (not env var)
**Date:** 2026-05-27  
**Decided by:** Tomas  
**Decision:** Credentials are injected via stdin pipe from `op` on the host. Go reads from stdin at startup, parses JSON, stores in heap. No env vars, no files.  
**Rejected:** Env vars — visible in `docker inspect` and `/proc/1/environ`; `op` inside container — requires service account token (same env var problem, one level up); host disk file — persists across container restarts  
**Revisit if:** 1Password service accounts become the standard team setup (then `OP_SERVICE_ACCOUNT_TOKEN` inside container could be acceptable)

### [GML-013] One-shot commands also use stdin injection
**Date:** 2026-05-27  
**Decided by:** Developer  
**Decision:** Even one-shot invocations (`gml stats`, `gml run --dry-run`) read credentials from stdin. This means each invocation requires `op` on the host — a conscious tradeoff for security consistency.  
**Rejected:** Separate credential source for one-shot vs daemon (more code, more attack surface)  
**Revisit if:** One-shot friction becomes a daily pain point

### [GML-014] Go ticker scheduler (no external library)
**Date:** 2026-05-27  
**Decided by:** Developer  
**Decision:** Use `time.NewTicker` with interval from `rules.yaml`. No `robfig/cron` dependency.  
**Reasoning:** Interval-based firing is sufficient for daily/hourly inbox maintenance. Cron expression power not needed. Fewer dependencies = simpler Dockerfile.  
**Revisit if:** User needs time-of-day scheduling (e.g., "only run at 02:00")
