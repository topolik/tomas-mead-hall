# DSH — Dashboard

Local web dashboard aggregating idea backlog, project management progress, and notifications. Single Go binary in a Docker container, accessible via SSH port-forward from any machine.

## What it does

- **Backlog** — idea/todo list from `todo.txt`: add, edit, set status, and delete items; filter by priority / status / free-text (include & `!`exclude), sort, status-count badges, and bulk select → mark done / park / delete
- **Projects** — status of all active projects; click a project to drill down into its `PROJECT.md`, `ASSUMPTIONS.md`, and every iteration file
- **Notifications** — action items waiting for review (pushed via API); long messages clamp with a `[more]` toggle and multi-line bodies render their line breaks
- **Threads** — durable M:N discussions between you and the agents, attachable to notifications/plans/projects; agents post via API (e.g. GML marks a dismissed insight "processed" with a resolved thread), you reply in the UI
- **API Clients** — manage OAuth 2.1 credentials for projects/agents that push data

## How to run

```sh
./run.sh
```

`run.sh` handles everything:
- Starts the DSH and Tailscale containers
- Waits for Tailscale daemon to be ready
- On **first run**: prompts for Tailscale authentication (opens a login URL in your browser — one-time only)
- Configures Tailscale HTTPS proxy (Let's Encrypt) pointing to DSH
- Prints both the local and Tailscale URLs

Requires `jq` for parsing Tailscale status. Tailscale state persists in a Docker volume — authentication is only needed once.

### SSH access from another machine

```sh
ssh -L 9090:localhost:9090 homeserver
# then open http://localhost:9090 in your browser
```

WebAuthn (Passkey) requires `DSH_ORIGIN=http://localhost:9090` — this is the default and works correctly through an SSH tunnel.

## Configuration

All settings via environment variables (set in `docker-compose.yml`):

| Variable | Default | Description |
|---|---|---|
| `DSH_PORT` | `8080` | HTTP listen port |
| `DSH_DB_PATH` | `/data/dsh.db` | SQLite file path (in named volume) |
| `DSH_ORIGIN` | `http://localhost:9090` | WebAuthn RP origin(s) — must match the URL in the browser. Comma-separated for multiple (e.g. `http://localhost:9090,https://dsh-1.xxx.ts.net`); one RP per unique host. The first non-loopback entry is used for device-enrollment links. |
| `DSH_VAPID_CONTACT` | `mailto:admin@localhost` | Contact email for Web Push VAPID |

The JWT signing key and VAPID keys are auto-generated on first start and stored in the DB. To rotate them, delete the `config` table rows.

## Authentication

**UI login**: Passkey (WebAuthn). Register at `/setup` on first run.

**Adding a phone or another device**: Passkeys are bound to the origin's host
(RPID), so a passkey registered on `localhost` does **not** work over Tailscale
(`https://<host>.ts.net`) and vice-versa — each origin needs its own passkey.
To enroll a new device:

1. Log in on a device that already has a passkey (e.g. the laptop on `localhost`).
2. Go to **Passkeys → "Add a new device"**. It mints a fresh, one-time enrollment
   link (valid 10 min) pointing at the external (Tailscale) origin and shows it as
   a **QR code**.
3. Scan the QR with the new device and register a passkey there — it binds to the
   Tailscale hostname, so it works from the phone going forward.

(The boot-time `setup-token` is for genuine first-run only; it expires 10 minutes
after the container starts. Use the in-app QR flow above to add devices later.)

**API access**: OAuth 2.1 client credentials flow.

```sh
# 1. Create a client at /admin/clients in the UI — copy the secret shown once.

# 2. Get a token:
curl -X POST http://localhost:9090/oauth/token \
  -d "grant_type=client_credentials&client_id=dsh_xxx&client_secret=yyy"

# 3. Call the API:
curl -X POST http://localhost:9090/api/v1/projects \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"code":"ABC","name":"My Project","status":"Planning","priority":"Q2","lead":"Dev","phase":"Planning","last_updated":"2026-05-27"}'
```

## API reference

All API routes require `Authorization: Bearer <token>` except health.

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/health` | Health check (public) |
| `POST` | `/api/v1/projects` | Create or update a project |
| `GET` | `/api/v1/projects` | List all projects |
| `POST` | `/api/v1/notifications` | Create a notification |
| `POST` | `/api/v1/threads` | Create a thread (+ first message) |
| `GET` | `/api/v1/threads` | List threads (`?ref_type=&ref_id=&status=`) |
| `GET` | `/api/v1/threads/{id}` | Thread with messages |
| `POST` | `/api/v1/threads/{id}/messages` | Reply to a thread |
| `PATCH` | `/api/v1/threads/{id}` | Set status (`open`/`resolved`) |

### POST /api/v1/projects

```json
{
  "code": "DSH",
  "name": "Dashboard",
  "status": "Implementation",
  "priority": "Q2",
  "lead": "Developer",
  "phase": "Implementation",
  "last_updated": "2026-05-27"
}
```

### POST /api/v1/notifications

```json
{
  "project_code": "DSH",
  "message": "Review ready for iteration 3",
  "type": "action_needed"
}
```

`type` is either `action_needed` or `info`.

### Threads

```sh
# Mark a dismissed insight "processed" (the GML pattern):
curl -X POST .../api/v1/threads -H "Authorization: Bearer <token>" \
  -d '{"subject":"processed: insight #42","body":"folded into knowledge.yaml","ref_type":"notification","ref_id":"42"}'
curl -X PATCH .../api/v1/threads/<id> -H "Authorization: Bearer <token>" -d '{"status":"resolved"}'

# The skip-check: non-empty result => insight 42 already processed
curl ".../api/v1/threads?ref_type=notification&ref_id=42&status=resolved" -H "Authorization: Bearer <token>"
```

`ref_type` ∈ `notification|plan|project` (optional). Message/thread authorship is
taken from the authenticated OAuth client name (UI posts use the session user) —
the payload carries no author field.

## Push notifications

DSH sends push notifications to your phone/browser when new notifications arrive via the API. Uses Web Push (VAPID) over a Tailscale tunnel — no ports exposed to the internet.

### Tailscale setup

Handled automatically by `run.sh`. On first run, the script prompts with a Tailscale login URL — open it in your browser to authenticate. After that, Tailscale state persists in a Docker volume and no further authentication is needed.

DSH is then accessible at `https://<hostname>.<tailnet>.ts.net` with automatic Let's Encrypt HTTPS.

### Phone setup

1. **Install Tailscale** on your phone and join the same tailnet
2. **Run `./run.sh`** — it prints a phone setup URL with a one-time token (expires in 10 min)
3. **Open the URL** on your phone to register a passkey
4. **Go to Notifications** and tap **[Enable Push]** — allow notifications when prompted
5. Done — new notifications will push to your phone

If the token expired, restart DSH (`docker compose restart dsh`) and run `./run.sh` again for a fresh one.

VAPID keys are auto-generated on first start. Push is fire-and-forget — if delivery fails, the notification is still stored in the DB.

## How to test

```sh
make test          # run unit + integration tests
make docker-build  # build the Docker image
```

## Development

```sh
make watch         # start the stack, then rebuild + recreate on any source change
```

`make watch` uses Docker Compose Watch (`develop.watch` in `docker-compose.yml`,
`action: rebuild`). Edit any Go/template/static file and the container is rebuilt
and recreated automatically — no more stale images from forgetting `--build`.
Markdown changes are ignored so editing docs doesn't trigger a rebuild.

## Data persistence

SQLite at `/data/dsh.db` in a named Docker volume (`dsh-data`).

### Backup & restore

```sh
./backup.sh                # encrypted backup (works whether DSH is running or stopped)
./restore.sh <file.enc>    # restore from backup (stops DSH if running, restarts after)
```

Passphrase via `$BACKUP_PASSPHRASE` env var or interactive prompt. Backups go to `~/.local/share/dsh/backups/` by default (override with `$BACKUP_DIR`). Old backups are pruned after 30 days.

When DSH is running, backup uses SQLite's hot-backup API (`docker exec`). When stopped, it copies the db file directly from the Docker volume — no container needed.

### Setup & client provisioning

```sh
./setup.sh                           # build + start DSH containers
docker compose exec -T dsh /app/dsh create-client GML   # provision an OAuth2 client (JSON output)
```

The global `setup.sh` and per-project `setup.sh` scripts handle client provisioning automatically.
