# LLP — LLM Proxy

A self-hosted, **OpenAI-compatible** HTTP gateway in front of several LLM backends.
One API (`POST /v1/chat/completions`) for every agent; the proxy queues/rate-limits
per backend, **fails over automatically**, and tracks per-agent token usage and cost.

GML Mode 2 is the first customer: instead of `run-task.sh` invoking the gemini/claude
CLIs directly, GML calls the proxy.

## What it is

- **Façade:** `POST /v1/chat/completions` (+ `GET /v1/models`, `GET /healthz`, `GET /admin/usage`).
- **Backends (impls), in failover order:**
  1. **Gemini CLI** (default) — execs `npx @google/gemini-cli`, reuses the host's Cloud-project auth (free).
  2. **Claude CLI** — execs `claude -p`, reuses the host subscription (free).
  3. **OpenLLM / OpenAI-compatible HTTP** — *stubbed in v1* (empty `base_url` ⇒ disabled). Point it at Ollama, a remote API, or OpenRouter to enable.
- **Failover:** on a retryable failure (rate-limit/quota, non-zero exit, timeout, HTTP 429/5xx) the router tries the next impl and puts a rate-limited impl on cooldown. A **quota-exhausted** failure (gemini's `TerminalQuotaError` daily limit, claude's "usage limit reached") gets the longer `quota_cooldown` (30m in the example config) instead of the 60s throttle cooldown. Terminal errors (HTTP 400/401/403) are returned as-is.
- **Queue:** per-impl concurrency cap (default 1 ⇒ serialized) — the guard against token/quota exhaustion.
- **Usage:** one SQLite row per request (agent, impl, tokens, cost, latency, status); aggregated at `/admin/usage`.
- **Auth (secure by default, no keys to manage):** the data API binds **loopback** (`127.0.0.1`). Agents register at startup over a **Unix control socket** (`~/.llp/control.sock`, parent dir `0700`) and receive a per-session token held only in memory — nothing in env, on disk, or `/proc`. See `ASSUMPTIONS.md` LLP-012/013.
- Runs **on the host** (see `ASSUMPTIONS.md` LLP-006) because the CLI impls reuse the host's logged-in sessions.

## Run it

```bash
cd projects/LLP-llm-proxy
./run.sh           # builds ./llp, copies config.yaml, starts on 127.0.0.1:4000 + control socket
```

No keys to set up: provider auth (gemini/claude) uses the host CLIs' sessions; client auth is the
control-socket handshake. Edit `config.yaml` (from `config.example.yaml`) to change models, failover
chains, timeouts, the bind host, the control-socket path, or to enable the OpenLLM backend.

**Run in the background** (tmux daemon, same pattern as GML's `watch.sh`):

```bash
./watch.sh start            # start as a detached tmux session (survives your shell)
./watch.sh status           # running? healthz? control socket?
./watch.sh logs --follow    # tail the log (~/.local/share/llp/llp.log)
./watch.sh restart          # rotate log + restart;  also: stop | attach
```

### Call it

Get a session token via the handshake, then call the loopback API:

```bash
TOKEN=$(curl -s --unix-socket ~/.llp/control.sock -X POST http://unix/register \
  -H 'Content-Type: application/json' -d '{"agent":"me"}' | jq -r .token)

curl http://localhost:4000/v1/chat/completions \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"model":"auto","messages":[{"role":"user","content":"Reply with one word: PONG"}]}'
```

(`./smoke.sh` does the handshake + a full matrix for you.)

### Choosing a model

The `model` field accepts three forms (OpenRouter-style):

| `model` | Meaning |
|---|---|
| `auto`, `gml-analyze` | a **logical model** → its failover chain (`config.yaml` → `models`); `auto` = `[gemini, claude, openllm]` |
| `gemini`, `claude`, `openllm` | **pin to one backend** (no failover) |
| `gemini/gemini-2.5-pro`, `claude/sonnet` | **pin a backend + override its underlying model** for that request |

Split on the first `/`, so override ids may contain `:`/`/` (e.g. `openllm/llama3.1:8b`). A defined
logical model wins on exact match; an unknown model falls back to `default_model`. The response's
`model` field reports which impl actually served it. Overriding a model requires the impl to have a
`model_flag` configured (gemini `-m`, claude `--model` — both set in `config.example.yaml`).

### Streaming

Set `"stream": true` and LLP answers with OpenAI-style SSE (`chat.completion.chunk`
events ending in `data: [DONE]`; the final chunk carries `usage`):

```bash
curl -sN localhost:4000/v1/chat/completions \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"model":"auto","stream":true,"messages":[{"role":"user","content":"hi"}]}'
```

Streaming is **façade-level** (see `ASSUMPTIONS.md` LLP-018): the router first completes
the request as usual — failover, queueing, and usage accounting are identical to the
non-streaming path — then replays the result as chunk events. Works for every impl
(gemini, claude, openllm). Errors are never streamed: failures return the regular JSON
error envelope.

### Inspect

```bash
curl -s localhost:4000/healthz | jq                                  # per-impl serveability (open, no token)
curl -s localhost:4000/admin/usage    -H "Authorization: Bearer $TOKEN" | jq   # usage/cost by day/agent/impl
curl -s "localhost:4000/admin/requests?limit=20" -H "Authorization: Bearer $TOKEN" | jq  # recent calls (newest first)
```

`/healthz` reflects **serveability, not just config**: each impl reports `serveable`
(configured && not cooling down && fewer than 3 consecutive failures) plus
`consecutive_failures`, `last_error`, `last_error_at`, `last_ok_at`. The top-level
`status` is `"ok"` while at least one impl is serveable, `"degraded"` otherwise —
clients should key off that field (`jq -e '.status == "ok"'`). The endpoint always
answers HTTP 200; degradation is signaled in the body only.

## Test it

```bash
go test ./...            # unit + integration, race-clean
```

**Live / failover** (requires authenticated gemini/claude CLIs on the host):

```bash
# L1 — real completion via gemini (model auto → gemini serves)
# L2 — forced failover: a config where gemini is broken; the router uses claude
LLP_CONFIG=config-failover-test.yaml ./run.sh        # gemini deliberately rate-limits → claude serves, gemini cools down
```

## GML integration

`run-task.sh` gained a non-destructive toggle: set `LLP_URL` (optional `LLP_SOCKET`, `LLP_MODEL`)
and GML's analyze step routes through the proxy via `llp_complete`, which does the control-socket
handshake itself (no key). Unset `LLP_URL`, and GML uses the CLIs as before.

```bash
LLP_URL=http://localhost:4000 LLP_MODEL=gml-analyze ./run-task.sh analyze
```

## Notes / known behavior

- **SECURITY — the gemini command must keep `--approval-mode default` (LLP-014).** Headless
  gemini-cli does **not** sandbox its built-in tools: `-e none` only disables extensions/MCP, and
  in `-p` mode every workspace counts as trusted, so the user-level `defaultApprovalMode: auto_edit`
  lets a completion **write files** (incident MND-011, 2026-06-12 — a large prompt pushed gemini
  into agent mode and it wrote into the repo). `default` auto-denies all tool calls non-interactively.
  **Regression risk:** `run.sh` regenerates a missing `config.yaml` from `config.example.yaml` — that
  is exactly how the flag was lost once (restart from a fresh worktree, 2026-06-12 20:10). Both files
  carry the flag now, and a **startup guard enforces it** (LLP-017): `run.sh` and `registry.Build`
  both refuse to start a gemini-cli command without an explicit `--approval-mode`, so a stale example
  fails loudly instead of silently emitting an insecure config. The committed `.gemini/settings.json`
  layers `defaultApprovalMode: default` as defense in depth (it applies because `run.sh` starts llp
  from this directory).
- **gemini-cli's internal retry is disabled** (`.gemini/settings.json` → `general.maxAttempts: 1`,
  LLP-015). With the default 10 attempts, a quota-exhausted gemini grinds 2–4 minutes inside the CLI
  before failing — every `auto` request burned LLP's full 180s timeout before failing over (observed
  167–241s latencies). Failover, cooldown, and retries are LLP's job; the CLI should fail fast.
- **CLI backends relay model output verbatim.** Gemini-cli (being agentic) can prepend planning
  prose before the answer; clients needing strict formats validate/extract downstream (GML's
  analyzer already strips fences/prose). A future iteration may add optional per-impl output extraction.
- **Token usage for CLI impls is estimated** (chars/4); HTTP impls report real usage. CLI cost = 0 (free).
- See `project-management/LLP-llm-proxy/` for design decisions (`ASSUMPTIONS.md`) and the iteration log.

## Layout

```
cmd/llp/main.go            entrypoint (load config → wire → ListenAndServe)
internal/openai/           OpenAI-compatible request/response types
internal/registry/         config load + logical-model → failover-chain resolution
internal/provider/         Provider interface; CliProvider (gemini/claude), HttpProvider (openllm)
internal/router/           failover walk + per-impl concurrency + cooldown
internal/usage/            SQLite usage/cost store + aggregation
internal/auth/             bearer-token → agent
internal/server/           HTTP handlers
config.example.yaml        sample config (copy → config.yaml)
.gemini/settings.json      workspace settings for the exec'd gemini-cli: approval-mode guard + fail-fast (LLP-014/015)
run.sh                     host launcher
```
