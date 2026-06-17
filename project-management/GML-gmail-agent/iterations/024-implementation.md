# GML — Iteration 024: Implementation — Docker-first (no host binaries)

- **Phase:** 2 — Implementation
- **Phase lead:** Developer
- **Start:** 2026-06-17
- **End:** 2026-06-17
- **Input:** Bug report — `gws` not found on host PATH when `run-task.sh` ran pipeline natively
- **Branch/worktree:** `gml-gmail-agent`

---

## What was built

Full Docker-first rewrite: all GML commands now run inside the Docker container.
No host binaries required (no Go binary, no gws, no npm).

### Architecture change
- **`pipelineLoadCreds()`** reads credentials from stdin (was: called `op` CLI directly via `LoadFromOP`)
- **`run-task.sh`** routes ALL commands through `docker compose run` — removed `ensure_binary()`, `ensure_gws()`, native exec
- **`docker-compose.yml`** mounts `./data:/app/data` (rw), passes through `LLP_URL`/`LLP_SOCKET`/`LLP_MODEL`, sets `GML_DSH`
- Container runs as host uid/gid (`user: "${HOST_UID}:${HOST_GID}"`) — `HOST_UID` because bash `UID` is readonly
- `HOME=/tmp` in container — host uid has no passwd entry, HOME defaults to `/` (unwritable); gws needs writable home for discovery cache
- LLP socket auto-mounted into container when present at `~/.llp/control.sock`
- `LLP_URL` defaults to `http://localhost:4000` in run-task.sh (network_mode: host)

### Pipeline log improvements
- `llm.Client.DisplayName(fallback)` returns `LLP/<model>` when proxy active, raw model name otherwise
- All step headers and error messages use `DisplayName` — no more misleading "gemini" when LLP routes the call
- Removed redundant per-call `[LLP] routing via proxy...` line

### DSH config cleanup
- Removed stale DSH `client_id`/`client_secret`/`url` from `rules.yaml` — `dsh.yaml` is now the sole source
- Simplified fallback warning: logs the actual error instead of a generic "using rules.yaml" message

### Dead code
- `internal/creds/op.go` — `LoadFromOP()` + constants no longer called. Can be removed in follow-up.

## run-task.sh command routing

| Command | Credentials | Notes |
|---------|------------|-------|
| analyze, learn, watch-analysis, watch-knowledge | read-only (stdin pipe) | Pipeline + Gmail |
| distill, propose, apply-rules | none | Pipeline, no Gmail |
| run, watch-rules | read-write (stdin pipe) | Archive rules |
| everything else | read-only (stdin pipe) | Building-block |

## Testing

- `go build ./...` success
- Full end-to-end: `./run-task.sh analyze --hours 4` — 6 emails fetched, analyzed via LLP proxy, 4 notifications posted to DSH

## Bugs found & fixed

1. **`gws` not found on PATH** — root cause: pipeline commands ran natively but `gws` only exists in Docker image. Fixed by routing all commands through Docker.
2. **DSH `invalid_client` (HTTP 401)** — `dsh.yaml` mode 600, container ran as `gml` user (different uid from host) → permission denied → silently fell back to stale `rules.yaml` creds. Fixed with uid/gid mapping + explicit `GML_DSH` env var + warning log.
3. **`LLP_URL` empty in container** — host env vars weren't passed through. Fixed by defaulting to `http://localhost:4000` in run-task.sh.
4. **LLP socket not found in container** — `~/.llp/control.sock` resolved to container user's home. Fixed by explicitly mounting socket file and injecting `LLP_SOCKET` env var.
5. **`UID` readonly variable** — bash treats `UID` as readonly, `export UID=...` fails. Fixed by renaming to `HOST_UID`/`HOST_GID`.
6. **gws discovery cache permission denied** — host uid 1002 has no passwd entry in container, `HOME=/` (unwritable). Fixed by setting `HOME=/tmp` in docker-compose.yml.

## Commits (8 on `gml-gmail-agent`, merged to master)

1. `b6536e5` — [GML] Move pipeline commands from bash to Go
2. `48ccc2e` — [GML] Run all commands through Docker — no host binaries
3. `abca910` — [GML] Fix DSH config and LLP socket passthrough in Docker
4. `9beeda8` — [GML] Fix container permissions and LLP defaults
5. `5146f30` — [GML] Fix UID readonly variable error in run-task.sh
6. `7dbc5bc` — [GML] Set HOME=/tmp in container for gws discovery cache
7. `ea7248f` — [GML] Show LLP/model in pipeline step logs instead of raw model name
8. `6588648` — [GML] Remove stale DSH config from rules.yaml — use dsh.yaml only

## Decisions

### Docker-first for all GML commands
**Date:** 2026-06-17
**Phase:** Implementation
**Decided by:** Tomas
**Decision:** All GML commands run inside Docker — no host binaries
**Alternatives considered:** Install gws via npm on the host; hybrid (some commands native, some Docker)
**Reasoning:** Docker image already bundles everything. Host installs add fragile dependencies. Container is the reproducible, portable unit.
**Revisit if:** Performance overhead of Docker becomes measurable for interactive commands

### DSH config in dsh.yaml only
**Date:** 2026-06-17
**Phase:** Implementation
**Decided by:** Tomas
**Decision:** Remove DSH credentials from rules.yaml; dsh.yaml is the sole source
**Alternatives considered:** Keep both with dsh.yaml override (current fallback)
**Reasoning:** Duplicate credentials drift — rules.yaml had stale creds causing 401 errors
**Revisit if:** Never — single source of truth is strictly better
