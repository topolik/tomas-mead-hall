# Developer — Skills

Distilled capabilities gained from project work.

---

## Go — Web Services

- Build self-contained Go HTTP servers with `net/http` ServeMux, embedded assets (`embed.FS`), and `html/template` — no framework needed
- Serve static files and templates from `embed.FS` with `fs.Sub`
- Use Go's pattern-based routing (`"GET /path/{id}"`) with `PathValue` for clean route definitions
- Apply layered middleware (session auth, CSRF, JWT) as `http.HandlerFunc` wrappers without a framework
- Write CSRF-protected forms: generate token in session, validate in `CheckCSRF` middleware

## Go — SQLite & Migrations

- Use `modernc.org/sqlite` for pure-Go SQLite (no CGO, works in scratch/Alpine containers)
- Track schema migrations with a `schema_migrations` table — each migration runs exactly once, safe across restarts and version upgrades
- Handle `sql.ErrNoRows`, `sql.NullString`, and nullable columns cleanly in scan loops

## Authentication — Passkey / WebAuthn

- Implement full WebAuthn flow with `go-webauthn/webauthn` v0.17.4: registration ceremony, discoverable login (usernameless), credential storage and retrieval
- Store `CredentialFlags` as a single protocol byte (`cred.Flags.ProtocolValue()`) and restore with `NewCredentialFlags(protocol.AuthenticatorFlags(b))` — required to avoid BackupEligible mismatch on iCloud Keychain / Touch ID
- Handle first-run setup (no passkeys exist): public `/setup` endpoint that registers the first credential and auto-creates a session; `RequireSession` redirects to `/setup` on clean DB

## Authentication — OAuth 2.1 / JWT

- Implement OAuth 2.1 client credentials flow: client creation, argon2id secret hashing, JWT issuance (HS256), token validation with revocation check against DB
- Revoke tokens immediately by checking `revoked_at` in DB on every API call — not just at issuance
- Audit all token events (issued, rejected, revoked) with actor, IP, and detail

## Security

- Unified audit log: write all sensitive events (auth, MFA, tokens, admin, API) to a single `audit_log` table with event, actor, remote_ip, detail
- Real client IP extraction: `X-Real-IP` → `X-Forwarded-For` → `RemoteAddr`; use `network_mode: host` in Docker when no reverse proxy is present
- argon2id password hashing via `golang.org/x/crypto/argon2`
- CSRF token per session, rotated on each settings page load

## Backup & Restore

- Hot SQLite backup inside a running container: `sqlite3 .backup` is safe under concurrent reads/writes
- Encrypted compressed backup pipeline: `sqlite3 .backup` → `gzip -c` → `openssl enc -aes-256-cbc -pbkdf2 -iter 600000` — output is a `.db.gz.enc` file
- Restore: decrypt → decompress → `docker compose stop` → `docker cp` into named volume → `docker compose start`
- Secure temp file deletion with `shred -u` (falls back to `rm -f`)

## File-backed Data

- `pmreader`: scan a directory for `*/PROJECT.md` files, parse `# Title` and `- **Key:** Value` markdown metadata — clean alternative to DB-backed project data
- `todoreader`: read/write a `todo.txt` file with status markers (`[ ]`, `[~]`, `[x]`, `[-]`), inline suffix format (`#Q2 #date`), and legacy multi-line fallback (continuation-line parsing for Priority/Added fields)
- `updateLine`: atomic in-place line rewrite via read-all → modify → write-all — suitable for small files
- Delete a multi-line `todo.txt` item safely: remove the item line *and* the indented continuation lines that follow it (loop while `isContinuation`) so nothing is orphaned
- **Path-traversal-free directory lookup from a URL value**: never `filepath.Join(base, userInput)`; instead scan the dir, parse each candidate's own metadata, and match the parsed id against the requested one (case-insensitive). The user string never touches the filesystem path, so `../` etc. is structurally impossible — no sanitization needed

## Go — LLM Pipeline Orchestration

- Build three-step container↔host pipelines: container produces prompt (stdout) → host calls LLM CLI → container validates/posts results
- Parse and validate LLM JSON output with strict Go struct unmarshaling, enum validation, and code fence stripping for Gemini compatibility
- Strip LLM preamble/postamble from both JSON arrays (`[...]`) and objects (`{...}`) — detect opening delimiter and find matching close
- Implement DSH OAuth2 client credentials flow in Go: token acquisition, caching with expiry, JWT bearer auth for API calls
- Build Gmail search URLs from LLM-provided query strings with `url.QueryEscape`
- Design two-array LLM output schemas (`{"patterns": [...], "todos": [...]}`) to route a single LLM response to multiple downstream systems
- YAML-based persistent knowledge files: Load/Save with graceful handling of nonexistent files, Upsert dedup by key field, human-readable Format for LLM prompt context
- Thread provenance (source IDs) through a multi-stage LLM pipeline by a **deterministic field-copy join** instead of trusting the LLM: when an upstream artifact's key is derivable from a downstream field (e.g. a knowledge pattern's `gmail_search` is mandated to equal the source insight's search Link), canonicalize both sides with one shared normalizer and match — attribute provenance with zero LLM reliance, reserving LLM attribution only for artifacts with no deterministic anchor
- Keep dedup off the non-deterministic path: derive an "already processed" set from stored provenance (no separate ledger that drifts) so re-running an LLM step over the same input emits nothing new; key entity dedup on the source-ID set (robust when the entity's other fields, e.g. a folded filter, have changed)
- Prevent a multi-rule footgun at rule-formation: fold overlapping same-key LLM candidates into one rule at propose time, with a deterministic detect-and-refuse guard at apply (group by key, withhold any key with ≥2 conflicting variants) as the backstop — prevention in the LLM step, safety in deterministic code
- Dedup LLM-generated artifacts on a **coarse identity key** (the stable attributes) not the volatile full output: an LLM re-derives the same item each cycle with reworded wording, so key on what stays fixed (sender-set + category) and treat the wording as the structural floor only — collapses semantic duplicates a string-equality key never could
- **Derive the identity key symmetrically on both sides** — a stored row exposes only what it persisted (its rendered message/link), so a freshly-generated candidate must compute its key through the *same* parse of its own rendered form, guaranteeing the key it computes equals the key it will have once stored. Keying the candidate off a richer in-memory field the stored side can't see silently under-dedups
- Confine the LLM to the one judgment that's irreducibly semantic (is this candidate genuinely new vs a reword of a dismissed one?) behind a deterministic gate; do everything else — active-match → update-in-place, exact repost → skip — in code. Re-introducing an LLM dedup *decision* is the exact failure mode that produced the duplicates
- Update-in-place safely with a DB guard: `UPDATE … WHERE id=? AND dismissed_at IS NULL` (0 rows affected → 404) makes "refresh the live one" impossible to turn into "resurrect a dismissed one"; caller falls back to posting new on the 404

## Go — Email Processing

- Fetch full Gmail message bodies via `gws` CLI subprocess, extract body from multipart MIME
- HTML→text conversion with `jaytaylor.com/html2text` (strips style/script tags, handles hidden elements)
- Datamarking: insert U+E000 between words via `strings.Fields`/`strings.Join` for prompt injection defense
- Input sanitization pipeline: HTML strip → base64 removal → invisible character stripping → truncation → datamarking → injection flagging
- Gmail label management via gws CLI: list labels, create labels (with visibility settings), idempotent ensure-or-create pattern with namespace prefix enforcement
- Atomic multi-operation `messages.modify` calls: combine `addLabelIds` and `removeLabelIds` in a single API request to avoid partial-state mutations

## Shell — Daemon Orchestration

- Build shell-based daemon with credential caching: fetch 1Password credentials once at startup, cache in shell variable (memory-only)
- State-file catch-up: write timestamp after success, compute window from `max(interval, time_since_last_success)` on next cycle
- Temp file management with script-level variables and `trap EXIT` cleanup (local variables in functions lose scope before trap fires)
- tmux session management scripts: detached start, stop, status, logs, attach subcommands

## Go — Dynamic Query Building

- Build parameterized SQL WHERE clauses from URL query params: parse comma-separated values into include/exclude lists, generate `IN (?,...)`/`NOT IN (?,...)`/`LIKE` clauses with `?` placeholders
- Handle NULL-safe exclusion: `col NOT IN (?,?) OR col IS NULL` prevents NULLs from being silently filtered out
- Sort with NULLs last in SQLite: `ORDER BY col IS NULL, col` pushes NULL values to the end
- Preserve filter state across page reloads: hidden form fields for composite values, `fo` param for `<details>` open state, `autofocus` for active text inputs
- **One filter type, two backends**: give the filter spec both a SQL emitter (`applySQL`/`applyLikeSQL`) and an in-memory matcher (`Accept` exact include/exclude, `AcceptLike` case-insensitive substring with OR-includes). DB-backed screens (notifications) and file-backed screens (todo.txt) then share the same `parseFilter`, the same `Raw()`/`Has()`/`IsNegated()` template helpers, and the same filter-panel HTML+JS — port a screen's filtering by swapping the data source, not the filter logic
- **Bulk edits on a line-indexed file**: do all selected operations in one read-all → modify → write-all pass keyed on a set of indices, never a delete-one-at-a-time loop — rewriting once sidesteps the index-shifting bug where deleting line N invalidates every later index
- Multiple bulk actions from one selection: a single hidden form + several `type=submit` buttons that each carry `name="action" value="…"`; the clicked button's value names the action server-side (no JS needed to track mode)

## Threads / polymorphic refs (DSH)

- **Polymorphic ref without FK**: a nullable `ref_type`/`ref_id` pair + an API-layer existence check (switch on type → COUNT the target table) gives one table N attach-points without per-type FK columns; deliberately exclude ref targets whose IDs aren't stable (todo.txt line indexes)
- **Authenticated authorship via middleware context**: resolve the OAuth client's display name once in the JWT middleware, stash it in the request context, and have handlers read authorship only from there — the payload carries no author field, so authorship is unspoofable by construction; UI posts use the session username the same way
- **Acceptance-test against a prod copy**: a `*_test.go` that skips unless an env var (`DSH_ACCEPT_DB`) points at a `sqlite3 .backup` copy of the live DB, then boots the real mux via `httptest` + a real OAuth token and exercises the full client flow on real rows — repeatable real-data verification with zero risk to production

## Web Push (VAPID)

- Implement Web Push with `webpush-go`: generate VAPID keypair (ECDSA P-256), store in DB, send encrypted notifications to browser push services (FCM/APNs)
- Service worker push handling: `push` event → `showNotification`, `notificationclick` event → focus existing tab or `openWindow`
- Browser-side push subscription: `pushManager.subscribe({userVisibleOnly: true, applicationServerKey})`, send subscription JSON (endpoint + keys) to server
- Clean up stale subscriptions on 410/404 responses from push services

## Authentication — Multi-origin WebAuthn

- Create separate `webauthn.WebAuthn` instances per RPID when serving multiple origins (e.g. localhost + Tailscale hostname) — passkeys are RPID-bound by spec
- Select the correct WebAuthn instance per request by matching `r.Host` to RPID
- Token-gated setup: allow passkey registration on new origins after first passkey exists, using a random token logged on startup
- **`tailscale serve` preserves the original `Host` header** (the MagicDNS name), so `r.Host`-based RP selection Just Works behind it — verified by reading `publicKey.rpId` straight off `/auth/passkey/login/begin` through the real `.ts.net` path. (Probe rpId via the begin endpoints: it's `publicKey.rpId` for assertions/login, `publicKey.rp.id` for creation/register.)
- **Make the unknown-`Host` fallback deterministic** — `for wa := range waMap { return wa }` picks a *random* RP (Go map order), which can hand a client a `localhost` rpId on one request and `ts.net` on the next and break the ceremony intermittently. Fall back to a fixed `DefaultRPID` (RPID of the first configured origin) instead.

## Authentication — On-demand passkey device enrollment (DSH iter 28)

- **A multi-origin deployment needs per-device enrollment, on demand.** A passkey is RPID-bound, so a phone over Tailscale (`https://x.ts.net`) can never use a `localhost`-registered passkey (even one synced via 1Password) — it must register its own. The enrollment path therefore has to be reachable *whenever* a new device shows up, not just at first run.
- **Don't anchor an enrollment token's TTL to process start.** A boot-time token with a 10-min TTL is dead on any long-running container (up for days), and `docker compose up -d` won't re-mint it because it doesn't recreate an already-running container — so the "setup URL" is always expired. Anchor the TTL to *generation* and expose a `Regenerate()` that resets the clock.
- **Mint enrollment links from an authenticated session, deliver as a QR.** `POST /settings/passkeys/enroll` (session + CSRF) regenerates a fresh one-time token and returns `{url, qr, expires_in}`; the URL points at the **external** origin (first non-loopback `DSH_ORIGIN`), and the QR is a PNG data-URL via `skip2/go-qrcode` (`qrcode.Encode(url, qrcode.Medium, 256)` → base64). The user is already trusted on their laptop; "add a device" + scan beats typing a 32-hex URL on a phone. Token stays one-time (`Consume()` on success) + short-lived.
- **Pick the external origin structurally**: `ExternalOrigin()` = first `DSH_ORIGIN` entry whose host isn't loopback (`localhost`/`127.0.0.1`/`::1`); strip scheme, and special-case the bracketed IPv6 form `[::1]:port` before splitting on `:` (a naive `IndexAny(":/" )` truncates inside the brackets).
- **Verify the full flow without an authenticator**: integration-test enroll → `/setup?token=` unlocks → `/setup/passkey/begin` (Host = ts.net) yields `rp.id = ts.net` → wrong token 403, by forging a session row (`auth.CreateSession`) + a seeded passkey. Only the final `navigator.credentials.create()` (biometric) needs real hardware — leave that as the human review step.

## Tailscale — Container Sidecar

- Run Tailscale as a Docker sidecar with `network_mode: host`, `NET_ADMIN`+`SYS_MODULE` caps, `/dev/net/tun` device, and persistent state volume
- Use `tailscale serve --bg --https=443 http://localhost:9090` for automatic Let's Encrypt HTTPS proxying to a plain HTTP backend
- Custom entrypoint (`tailscaled --state=... & sleep infinity`) avoids containerboot auth timeout issues

## Web UI — small terminal-style patterns

- **App-wide favicon with no per-template edits**: serve one `GET /favicon.ico` handler returning an SVG with `Content-Type: image/svg+xml`. Browsers auto-request `/favicon.ico` on every page and honor the response content-type over the `.ico` extension, so a single route covers the whole app — no `<link rel="icon">` in N duplicated `<head>`s, no binary asset
- **Render newlines in escaped text**: `white-space: pre-line` makes `\n` in `html/template`-escaped content show as line breaks while still collapsing other whitespace (no `<br>` injection, no `pre`)
- **Reveal a `[more]` toggle only when text actually overflows**: CSS-clamp with `max-height`+`overflow:hidden`, then in JS compare `el.scrollHeight` vs `el.clientHeight` per element and show the toggle only on overflow; toggle an `.expanded` class (`max-height:none`) to expand
- Show raw markdown docs verbatim in `pre` with `white-space: pre-wrap; word-break: break-word` — readable in a monospace UI without adding a markdown-rendering dependency or an HTML-injection surface

## Docker

- **Auto-rebuild on source change**: `develop.watch` with `action: rebuild` (ignore `**/*.md`) in `docker-compose.yml` + `docker compose watch` — recreates the container whenever a watched file changes; kills the "stale image because I forgot `--build`" class of bug
- `network_mode: host` preserves real client IPs without a reverse proxy
- Bind-mount files into a container read-only (`ro`) for config/data that lives in the repo
- Bind-mount files read-write for container output (e.g. knowledge.yaml) — ensure `chmod 666` before mounting when container runs as a different UID than the host user
- Add CLI tools to Alpine images via `apk add` in Dockerfile (e.g. `sqlite3` for backup scripts)
- Run containers as non-root system user (`useradd -r -m`) — some CLIs (gws) require writable `$HOME` for discovery cache

## Session-History Mining (MND)

- Parse Claude Code session JSONL: `type:user` records mix human turns with tool_results — filter `message.content` arrays containing `tool_result` blocks (203/295 records in a typical session are tool results, and that is where secrets live)
- Parse Gemini CLI history: `tmp/<project>/chats/session-*.json` holds both conversation sides; checkpoint files snapshot the same session repeatedly — dedup by content, not just (session, timestamp) identity
- Collapse templated batch loops with a normalized-prefix near-dup key (lowercase, collapse whitespace, first 200 chars) — 35% of a raw session corpus was script-fed repeats arriving as "user" turns
- Redact secret shapes (ghp_/sk-/AKIA/AIza/JWT/op:///PEM/password-assignments) with kind-tagged markers `[REDACTED:<kind>]` before data leaves the extraction stage; keep assignment key names, drop only values
- Build pure-Go BM25 retrieval (k1=1.2, b=0.75) over short structured docs — no vector DB needed for corpora of hundreds of items; index in-memory per invocation
- LLM-extract behavioral insights with identity-keyed dedup (category + normalized statement hash) so re-runs update occurrence counts instead of duplicating claims; every insight carries evidence refs (moment ID, timestamp, verbatim quote) for auditability
- Drive incremental corpus processing by skipping moments already cited as evidence in the output — `--limit N` per run walks a large corpus across cheap sessions
- gemini-cli 0.29.5 gates `--approval-mode plan` behind `experimental.plan` settings flag — text→JSON prompts need no approval mode at all

## herdr Agent Orchestration (MND)

- Drive herdr agents programmatically: `herdr agent list` (states: idle/working/blocked), `herdr pane read <id> --source visible --lines N` (raw text), `herdr pane send-text <id> <text>` + `send-keys <id> Enter` to answer an agent prompt
- `herdr pane read` returns raw text but `herdr agent read` wraps it in a JSON envelope — resolve the pane via `herdr agent get <target> | jq .result.agent.pane_id` and use pane read
- `--source recent-unwrapped` can return empty right after an agent status change — `--source visible` is the reliable read
- Block until an agent needs input: `herdr wait agent-status <pane> --status idle|blocked|done --timeout MS`
- Spawn disposable test agents without disturbing the user: `herdr worktree create --cwd <repo-parent> --branch <name> --no-focus --json` → `herdr pane run <pane> "claude --remote-control"`; clean up with `herdr worktree remove --workspace <id> --force` + `git branch -D`
- `hwt <branch> [agent-cmd]` (shell function) = worktree create/open + agent launch in one step; `HWT_AGENT` overrides the default agent command
- Closed-loop agent direction: pane tail → LLM-with-persona (mind model) → send direction back; gate auto-send on answer confidence and default to dry-run

- Discriminate self/other content in mixed training corpora at turn level: an invisible datamark (U+E000) injected as prompt-injection defense doubles as a perfect self-authorship marker; ledger outbound messages by normalized-text sha256 and pin shell↔Go hash parity with a fixture test
- Make LLM corpus processing idempotent with a processed-ledger (IDs appended only after successful merge) — re-runs converge to "nothing new" instead of re-spending LLM calls on silent items
## LLM Gateway / Proxy (LLP)

- Build an OpenAI-compatible gateway (`/v1/chat/completions`) that hides multiple backends behind one façade; resolve a client-facing **logical model name** to an ordered **failover chain** of impls via config (unknown model ⇒ default chain)
- Model backends behind a single `Provider` interface so CLI-backed and HTTP-backed impls are interchangeable; expose an optional `Available()` so a config-disabled impl (empty base URL) is transparently skipped by the router
- Classify provider failures as **retryable** (rate-limit/quota, non-zero exit, timeout, HTTP 429/5xx ⇒ fail over) vs **terminal** (HTTP 400/401/403 ⇒ return to caller); put a rate-limited impl on a **cooldown** so it's skipped until it clears
- Bound per-impl concurrency with a buffered-channel semaphore (cap 1 = serialize) — a simple, effective guard against shared-quota/token exhaustion across many agents
- Track usage/cost in SQLite (one row per request: agent, impl, tokens, cost, latency, status); aggregate with `GROUP BY substr(ts,1,10)` for per-day rollups; cost = tokens × per-1M price (free CLI impls = 0)
- Expose model selection through the OpenAI `model` field itself (no extra request fields): a logical name → chain, a bare impl name → pin (no failover), `impl/<model-id>` → pin + override the backend's model. Split on the *first* `/` so override ids keep `:`/`/` (Ollama `llama3.1:8b`). Per-request override needs a configured CLI flag (gemini `-m`, claude `--model`)
- Distinguish a momentary throttle from a long-window quota exhaustion (daily limit / "resets after 3h49m") and give the latter its own much longer `quota_cooldown` — a 60s cooldown on an hours-long outage means every minute-spaced request re-pays the failed probe
- Make a gateway's `/healthz` reflect **serveability, not config**: track per-impl outcomes passively (consecutive failures since last success, last error/ok timestamps — free, no probe traffic), report per-impl `serveable` and a top-level `ok|degraded`, and signal degradation in the **body** while keeping HTTP 200 when existing clients hard-fail on non-200 responses
- Implement **façade-level SSE streaming** on a batch-mode gateway: run the request through the full router (failover, queueing, cooldowns, usage unchanged), then re-emit the completed response as OpenAI `chat.completion.chunk` events (role delta → content in ≤256-rune chunks → `finish_reason:"stop"` with `usage` → `[DONE]`); errors return the JSON envelope before any SSE bytes — avoids a streaming Provider interface that would break mid-stream failover

## Service-to-service auth (secure by default, no key setup)

- Two local services can authenticate with **zero manual key setup and no secret in env/disk/`/proc`**: the provider binds its data API to **loopback** and runs a **Unix control socket** in a `0700` owner-only dir; a consumer registers there at startup (`POST /register {agent}`), gets a random per-session token, holds it **in memory**, and sends it as `Authorization: Bearer` on the loopback API. Stale token (e.g. provider restart) → 401 → re-register automatically
- The `0700` parent dir is the real access gate; capture `SO_PEERCRED` (uid/pid) via `conn.SyscallConn().Control` + `syscall.GetsockoptUcred` for logging/identity. Enforcing peer-uid==self adds nothing over the 0700 dir (a same-uid attacker passes too) and breaks bind-mounted containers — make it opt-in
- A containerized consumer reaches a host socket by bind-mounting the dir; if it runs as **root** (default Docker), `CAP_DAC_OVERRIDE` lets it open the owner-only socket without uid matching. HTTP-over-UDS in Go = `http.Transport{DialContext: dial "unix" socketPath}`. Mirrors the GML stdin-to-heap credential ethos (see memory `feedback-service-secrets`)

## Go — robust subprocess exec

- `exec.CommandContext` only kills the direct child; a grandchild (e.g. `npx`→node) holding the stdout pipe makes `Wait()` block past the deadline. Fix: `SysProcAttr{Setpgid:true}` + a `cmd.Cancel` that `syscall.Kill(-pid, SIGKILL)`s the whole group, plus `cmd.WaitDelay` as a backstop
- Capture stdout (content) and stderr (diagnostics) into separate buffers; pipe the prompt via `cmd.Stdin`; merge `os.Environ()` with per-call env overrides
- gemini-cli: `-e none -p ""` (prompt on stdin) for non-interactive completion; do NOT pass `--approval-mode plan` (errors unless `experimental.plan` is enabled). It resolves settings per-cwd, so a flag can "work" from one directory and fail from another. It also emits noise on stderr and may prepend agentic prose to stdout — relay verbatim and let the caller extract
- Headless gemini-cli (`-p`) treats **every** workspace as trusted (`isWorkspaceTrusted()` short-circuits) and `-e none` does NOT disable built-in tools (`write_file`…), so a user-level `defaultApprovalMode: auto_edit` lets a "completion" write files — always pass `--approval-mode default` (auto-denies all tool calls non-interactively) when exec'ing it as a text backend
- Configure gemini-cli per-deployment without touching user settings: a workspace `.gemini/settings.json` in the exec cwd is honored in headless mode. Useful keys: `general.maxAttempts: 1` disables the internal retry ladder (default 10 attempts, 5s ×2 backoff capped at 30s ≈ up to ~4 min of silent grinding per quota-hit request — deadly behind a proxy that owns failover); `general.defaultApprovalMode` overrides the user-level mode (defense in depth); `mcp.excluded: [name]` stops user-level MCP servers loading (~5s startup saved per completion)
- gemini-cli quota errors come in two classes: `RetryableQuotaError` (per-minute throttle, retried internally) vs `TerminalQuotaError` (daily/long-window quota or server-suggested delay > 5 min, thrown immediately). Text mode prints `[API Error: <message>]` plus a re-thrown stack whose first line carries the class name — classify on phrases like `terminalquotaerror` / "daily quota" / "quota will reset" and bench the backend on a long quota-specific cooldown, separate from the short throttle cooldown
- gemini-cli `-m`/`--model`, claude `--model` select the model; an invalid id exits non-zero (verify a flag works before relying on it)
- When classifying a subprocess failure from its stderr, match **specific phrases**, not bare keywords: Node stderr includes stack traces, so `"quota"` matches the filename `googleQuotaErrors.js` and `"429"` matches a `:429:` line number — a bad-keyword match misclassified a 404 as a rate limit and wrongly triggered a cooldown (error **class names** are safe: `terminalquotaerror` is not a substring of `googlequotaerrors.js`)

## Shell ↔ HTTP integration

- Route an existing shell pipeline's LLM call through an HTTP service non-destructively: gate on an env var (`LLP_URL`), build the JSON body with `jq -Rs` (raw-slurp a file into a JSON string — injection-safe), POST with `curl --data @-`, extract with `jq -r '.choices[0].message.content // empty'`. Unset var ⇒ original code path unchanged

## herdr watch-mode orchestration (MND iteration 4)

- A "react to any agent needing input" daemon over herdr is a **poll loop**, not an event wait: `herdr wait agent-status` watches one pane for one status, so poll `herdr agent list | jq` and filter on `agent_status` ∈ {blocked, idle}
- Loop protections that made it safe live: (1) answer once per `(pane, normalized-tail-hash)` — JSONL ledger, same normalization as the sent-ledger; (2) the same tail reappearing **after** a delivered answer means the direction failed → escalate to the human, never resend; (3) per-pane cooldown (60s) bounds LLM spend on panes whose tail keeps changing; (4) skip agents whose cwd is the orchestrator's own worktree (self-orchestration recursion)
- Gate sends on an explicit `pending: question|none` field from the LLM (it reads the tail anyway) — idle ≠ asking; an idle agent mid-build got correctly ruled "let it run" on the first live pass
- **stdin-slurp trap**: any child that reads stdin (`docker compose run -T`, `curl`, `ssh`) inside a `while read` loop eats the loop's remaining input — every item after the first silently vanishes. Feed children `</dev/null`. Found live; the dry-run pass looked fine because the first pane masked it
- Pane tail hashing is stable enough to dedup on (statuslines show session-start time, not a ticking clock) — but verify per-UI with two reads a few seconds apart before trusting it

## Self-distilling brain: contradiction resolution & reliable retrain (MND iter 5–7)

- **Retire stale beliefs by provenance, don't average them.** When an LLM-distilled knowledge base accumulates contradictions, have the LLM only *identify* conflicting sets and classify each: genuine `contradiction` (same context, opposite — retire the loser) vs `context_split` (both true in different situations — keep both, add a scope). Pick the winner in code by a deterministic rule (direct-correction > inferred, newer > older, stronger > weaker) so every retirement is explainable; mark losers `superseded` (keep for audit), exclude from use via an `Active()` filter. Default an ambiguous verdict to the *non-destructive* side.
- **Loop-until-dry beats single-pass for LLM set-discovery.** One sweep's coverage is non-deterministic — the LLM surfaces different conflicts each pass. Repeat until N consecutive passes change nothing, capped for cost. But every looping operation must be **idempotent** or it never converges: `context_split` re-worded the same contexts cosmetically every pass until we made it scope-once (found live — unit tests passed, the live loop exposed it).
- **The brain is production data, not code (MO §10).** It lives on master and updates there directly (a learning run commits its data to master, never pushed); worktrees isolate *code* dev/test, and test-generated data never rides a code merge. A learning daemon belongs on master committing each update — not in a worktree leaving uncommitted output.
- **Mark the orchestrator's own output so retraining can't relearn it** (recursion guard): a delivered direction carries a fixed prefix that doubles as an extract-time exclusion marker, and is hashed into a send-ledger. Verified live: a prefixed direction in a real session is dropped by extract (`self=N`).
- **gemini-cli emits invalid JSON** (raw newlines inside string values) often enough that any parser of its output needs an escape-and-retry repair pass — don't trust a single strict `Unmarshal`. Verify model ids against the live CLI before pinning; ids vanish (claude-fable-5 pulled) and "X.Y Pro" rarely maps to the obvious slug (Gemini 3.1 Pro = `gemini-3.1-pro-preview`, not `gemini-3.1-pro`).

## Measuring an LLM clone's fidelity (MND iter 8)

- To know if a "decide like X" clone is trustworthy, build a **blind-replay eval**: take real situations where X actually decided, have the clone answer with the decision withheld, and an *independent* LLM judge agreement (agree/partial/disagree). Score = (agree + ½·partial)/n. The headline number matters less than the **disagreement list** — the concrete misses that target the next training pass.
- **Calibration is the safety question**: bucket fidelity by the clone's own stated confidence. If confidence doesn't separate right from wrong, any "withhold on low-confidence" gate is useless. First real run caught the clone at 100% `high` while 41% wrong — the gate couldn't protect anything. Flag degenerate (single-bucket) confidence explicitly in the report.
- **Leakage**: testing against a brain distilled from the same moments is in-sample (optimistic upper bound) — label it; a held-out train/test eval-brain gives the unbiased number. The disagreement list is valid either way.
- **Per-category breakdown finds the real gap**: averages hide it. The split that mattered — concrete *preferences* distill well (tech 80%) but contextual *judgment* doesn't (decision-heuristics 38%) — only showed up per-category.
