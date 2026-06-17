# LLP — Assumptions & Design Decisions

Non-obvious decisions with rationale. Answers "why did we do it this way?"

---

## LLP-001 — OpenAI façade over swappable impls; CLI-default backends

**Decision:** The OpenAI-compatible REST API is only the client-facing interface. Under it sit swappable impls, used in failover order:
1. **Gemini CLI** (default) — exec `gemini-cli`, reuse host Cloud-project auth.
2. **Claude CLI** (second) — exec `claude -p`, reuse host subscription.
3. **OpenLLM / OpenAI-compatible HTTP** (third) — configurable base URL → Ollama, a remote API, OpenRouter, or whatever OpenLLM supports.

Two adapter shapes implement the `Provider` interface: `CliProvider` (#1, #2) and `HttpProvider` (#3).

**Rationale:** Impls #1/#2 reuse the **free** host-CLI auth GML already relies on — no per-token cost on the common path, which is the actual point of "avoid token exhaustion." Impl #3 keeps a clean HTTP escape hatch for local/remote OpenAI-compatible models. The façade stays uniform so every client speaks one protocol regardless of which impl serves it. (Supersedes an initial "all backends over direct HTTP APIs" pick, which would have made GML's free usage metered for no gain.)

**Affected areas:** provider adapters (`CliProvider`/`HttpProvider`), deployment model (proxy needs host CLI access — see LLP-005), secret handling (only impl #3→remote needs an API key), cost tracking (metering matters only for the HTTP-remote path).

---

## LLP-002 — OpenAI-compatible API contract

**Decision:** Clients talk to the proxy via `POST /v1/chat/completions` with OpenAI-shaped requests/responses (`messages`, `model`, `choices`, `usage`). Supporting endpoints: `GET /v1/models`, `GET /healthz`.

**Rationale:** Any OpenAI SDK or OpenRouter-style client works as a drop-in, future-proofing for agents beyond GML. The extra request-shape surface (roles/choices/usage) is worth the interoperability.

**Affected areas:** HTTP handlers, request/response normalization, client migration (GML's `run-task.sh`).

---

## LLP-003 — Exhaustion handling: queue + automatic failover

**Decision:** Requests are serialized/throttled per impl (bounded concurrency + token-bucket). On a *retryable* failure (CLI rate-limit / quota / non-zero exec exit / timeout, or HTTP 429/5xx) the router fails over down the chain **Gemini CLI → Claude CLI → OpenLLM**; *terminal* failures (e.g. a malformed request / HTTP 400) are returned as-is, not retried.

**Rationale:** Maximizes availability across a family of agents sharing provider quotas — the stated "session management to avoid token exhaustion." Known tradeoff: the responding model can change mid-batch (see LLP-005 risk for GML's strict-JSON output).

**Affected areas:** router/failover logic, rate limiter, retry classification, model registry (chains).

---

## LLP-004 — v1 scope: full gateway up front

**Decision:** v1 includes all four backends, routing/failover, and usage+cost tracking — not a thin GML-only Gemini slice.

**Rationale:** Tomas's explicit choice. This deliberately overrides the MO KISS "thin slice first" default; recorded here because it's the kind of scope decision MO §4 reserves for Tomas. Smart cost/quality routing, caching, and DSH usage UI remain deferred to later iterations.

**Affected areas:** Phase-1 plan size, test matrix, acceptance criteria.

---

## LLP-005 — (Provisional) Go + SQLite + stdlib HTTP

**Decision (provisional — confirm in Planning):** Implement in Go as a single static binary; persist usage/cost in SQLite on a named volume; use stdlib `net/http`; `network_mode: host` (house style). Any API key needed by impl #3→remote is injected from `op` via env at startup — never in image or on disk.

**Open — deployment (Phase-1 decision, Open Question #6):** Because impls #1/#2 exec `gemini-cli`/`claude`, the proxy needs those binaries **and their host auth**. Either (a) run LLP directly on the host (matches the todo's "runs on host", breaks MO §1 "everything in containers"), or (b) build a container that bundles the CLIs with host auth mounted read-only (honors MO §1, fiddlier). Not yet decided.

**Rationale:** Matches GML's stack and the DSH SQLite precedent; KISS for a handful of endpoints; satisfies the MO safety checklist on secret handling.

**Affected areas:** entire implementation; `docker-compose.yml` / host run script; CLI-auth bootstrap.

**Revisit if:** Planning surfaces a need that Go's stdlib makes awkward (e.g. heavy SSE multiplexing), or the container-with-CLIs auth mounting proves impractical.

---

## LLP-006 — Deployment: run on the host (not containerized)

**Decision:** LLP runs as a single Go binary directly on the host (port `4000`). Agent containers reach it via HTTP over the host network.

**Rationale:** Impls #1/#2 exec the host's `gemini`/`claude` CLIs, which are authenticated by the user's logged-in sessions with refreshing OAuth tokens. Running on the host reuses those sessions with zero auth-mounting and no refresh-into-a-mount issues. Matches the todo's "runs on host". This **knowingly breaks MO §1 "everything in containers"** for this one service — brokering host-authenticated CLIs is its whole purpose.

**Affected areas:** no `docker-compose.yml` for v1; a host run script + (optionally) a systemd unit; `LLP_PORT` env; clients use the host's address.

**Revisit if:** LLP must run off the dev host, or container CLI-auth mounting becomes easy enough to honor MO §1.

---

## LLP-007 — OpenLLM (impl #3) built-but-stubbed in v1 → **superseded by LLP-019**

**Decision:** ~~v1 implements and unit-tests the `HttpProvider` adapter against a mock OpenAI-compatible server and registers impl #3, but wires no live backend.~~ **Superseded:** LLP-019 wires the `HttpProvider` to a live Ollama instance (local GPU container). The generic `openllm` name is replaced by `ollama` in config. The `HttpProvider` code is unchanged.

**Rationale:** Proves the failover path GML uses today without depending on a GPU box or a metered remote key. Going live later is configuration (a base URL, optional key), not new code.

**Affected areas:** `HttpProvider`, model registry (impl #3 ~~registered but unhealthy/disabled~~ now live as `ollama`).

---

## LLP-008 — Gemini CLI invoked without `--approval-mode plan`

> Extended by LLP-014: the command now carries `--approval-mode default` (auto-deny) — a different mode than the broken `plan`.

**Decision:** The gemini impl runs `npx @google/gemini-cli -e none -p ""` (prompt on stdin). It does **not** carry GML's `--approval-mode plan` flag.

**Rationale:** In gemini-cli v0.29.5 that flag errors (`Approval mode "plan" is only available when experimental.plan is enabled`) unless an experimental setting is on — it only appeared to work from some cwds because gemini resolves settings per-directory. Approval mode governs agentic tool-use; a read-only prompt→text completion needs none. (This same flag is the likely cause of GML's own intermittent gemini failures.)

**Affected areas:** `config.example.yaml` / `config.yaml` gemini command; reliability of impl #1.

**Revisit if:** an impl needs gemini tool-use — then enable `experimental.plan` deliberately.

---

## LLP-009 — CLI impls relay model output verbatim (transport, not formatter)

**Decision:** A CliProvider returns the model's stdout as-is (trimmed; optional fence-strip per impl). It does not reshape or extract structured content.

**Rationale:** LLP is transport. Gemini-cli (agentic) sometimes prepends planning prose before the answer; faithfully relaying it keeps the proxy honest and avoids lossy heuristics. Clients that need strict output validate/extract downstream — GML's analyzer already strips fences/prose. Consistent with the planning safety note that injection/format hardening is the caller's job.

**Affected areas:** `provider.CliProvider`; client-side validation expectations.

**Revisit if:** multiple clients want clean output — add an optional per-impl extraction mode rather than baking it in.

---

## LLP-010 — Model selection: OpenRouter-style `impl[/model]`

**Decision:** The `model` field accepts a logical model name (→ chain), a bare impl name (→ pin to that backend, no failover), or `impl/<model-id>` (→ pin + override the underlying model for that request). Split on the first `/`; logical models win on exact match and ignore overrides.

**Rationale:** Lets off-the-shelf OpenAI clients pick a backend *and* a specific model with no extra request fields (the OpenAI surface only has `model`). A bare impl = "this exact backend, fail if down"; a logical model = "best-effort with failover". First-`/` split keeps Ollama-style ids (`llama3.1:8b`) intact.

**Affected areas:** `registry.Resolve` (returns `(chain, override)`), `provider.Request.ModelID`, `CliProvider`/`HttpProvider` (apply override; CLI needs `model_flag`), `server` (plumbs override), `config` (`model_flag`: gemini `-m`, claude `--model`).

**Revisit if:** a request needs both a pin and a custom failover order.

---

## LLP-011 — Rate-limit detection uses specific phrases, not bare keywords

**Decision:** CLI rate-limit classification matches specific phrases (`resource_exhausted`, `quota exceeded`, `too many requests`, `code: 429`, …), never bare `"quota"` or `"429"`.

**Rationale:** Bare keywords match Node **stack-trace filenames** (`googleQuotaErrors.js`) and `:429:` line numbers, so a model-not-found (404) was misclassified as a rate limit and wrongly put gemini on cooldown — benching a healthy default backend for later requests. Missing an oddly-worded rate-limit degrades gracefully (plain retry/failover, no cooldown); a false positive does real harm. Covered by `TestCli_ModelNotFoundIsNotRateLimit`.

**Affected areas:** `provider.DefaultRateLimitPatterns`, cooldown behavior.

**Revisit if:** a real rate-limit message slips through — add the phrase with a test.

---

## LLP-012 — Auth: auto-provisioned session tokens via Unix-socket handshake

**Decision:** No static API keys. The data API binds loopback; agents register at startup over a control Unix socket (`~/.llp/control.sock`, parent dir `0700`), receive a random in-memory session token, and use it as `Authorization: Bearer` on the loopback data API. Tokens are session-scoped; a stale token → 401 → the agent re-registers. Nothing in env, on disk, or in `/proc`.

**Rationale:** Tomas's directive — secrets must be auto-provisioned and mutually exchanged at process start, held in memory only (never env/`/proc`/disk), with zero manual key setup. Mutual trust is rooted in the 0700 owner-only socket dir (agent trusts LLP because the socket is in the user's own dir; LLP records the peer via `SO_PEERCRED`). Mirrors the GML stdin-to-heap credential model. See memory `feedback-service-secrets`.

**Affected areas:** `internal/auth` (dynamic `Store`), `internal/control` (UDS `/register`), `cmd/llp` (bind loopback, start control socket), `config` (drop `clients`, add `bind_host`/`control_socket`); every client (DSH `llpclient`, GML `run-task.sh`, `smoke.sh`).

**Revisit if:** an agent must reach LLP from another host — add a networked credentialed transport (the token model carries over).

---

## LLP-013 — Loopback bind + 0700-dir gate; SO_PEERCRED logged, strict-uid opt-in

**Decision:** Data API binds `127.0.0.1` by default. The control socket's gate is its `0700` owner-only parent dir. `SO_PEERCRED` (uid/pid) is captured and logged on each handshake; same-uid enforcement is opt-in (`control_require_same_uid`, default false).

**Rationale:** The DSH container runs as root and reaches the socket via `CAP_DAC_OVERRIDE` + bind-mount; enforcing peer-uid==llp-uid would break that while adding nothing over the 0700 dir (a same-uid attacker passes the check too; other users are already blocked by the dir). Loopback bind keeps the data API off-host-unreachable.

**Affected areas:** `internal/control` (peercred capture + optional enforcement), `cmd/llp`/`config` (bind host), DSH `docker-compose.yml` (socket bind-mount, container is root).

**Revisit if:** untrusted same-host non-root peers exist — enable strict mode and align uids.

---

## LLP-014 — SECURITY: gemini command carries `--approval-mode default`; workspace settings as second layer

**Decision:** The gemini impl command is `npx @google/gemini-cli -e none --approval-mode default -p ""`. A committed `.gemini/settings.json` in the project dir additionally sets `general.defaultApprovalMode: "default"` (and excludes the user-level `jvm-debugger` MCP server).

**Rationale:** Headless gemini-cli is not read-only by default. Verified in the 0.29.5 source: `-e none` disables only extensions/MCP, **not built-in tools** (`write_file` etc.); in `-p` mode `isWorkspaceTrusted()` returns trusted unconditionally; and the user-level `~/.gemini/settings.json` on this host sets `defaultApprovalMode: auto_edit` — so a completion can silently write files. That is incident MND-011 (2026-06-12: a 304KB prompt pushed gemini into agent mode and it wrote three files into the repo). `--approval-mode default` cannot prompt non-interactively, so every tool call is auto-denied. The workspace settings file is defense in depth: it protects even if the flag is dropped from a regenerated config — which actually happened (the 2026-06-12 20:10 restart re-created `config.yaml` from the then-unfixed example, silently reopening the gap). It applies because `run.sh` starts llp from the project dir and gemini resolves workspace settings per-cwd.

**Affected areas:** `config.example.yaml` + live `config.yaml` (gemini command), committed `.gemini/settings.json`, README regression note.

**Revisit if:** an impl deliberately needs gemini tool-use — give it a separate impl entry with an explicit approval mode, never by relaxing the default impl.

---

## LLP-015 — gemini-cli internal retry disabled (`maxAttempts: 1`); quota-exhausted errors get a longer cooldown

**Decision:** The workspace `.gemini/settings.json` sets `general.maxAttempts: 1`, turning off gemini-cli's internal retry/backoff. LLP classifies long-window quota errors (`TerminalQuotaError`, "daily quota", "quota will reset", "resets after", claude's "usage limit reached") as `QuotaExhausted` and applies the impl's `quota_cooldown` (example config: 30m) instead of the 60s throttle `cooldown`.

**Rationale:** Root cause of the 2026-06-12 evening "2–3 min burns": gemini-cli retries `RetryableQuotaError` internally up to 10 attempts (5s initial delay, ×2 backoff capped at 30s ≈ up to ~4 min), so while gemini quota was exhausted every `auto` request ground inside the CLI until LLP's 180s timeout, then failed over (observed request latencies 167s/241s) — and a timeout sets no cooldown, so the next request burned again. Worse, the terminal-quota stderr matched **none** of `DefaultRateLimitPatterns` ("You have exhausted your daily quota…" ≠ "quota exceeded"/"resource exhausted"), so even fast failures never set cooldown. Retry/failover/cooldown are LLP's whole job — the inner retry loop is redundant and slow in a proxy. With one attempt, quota errors surface in seconds, are classified, and bench the impl for the (long) quota window. Pattern-matching rule from LLP-011 still applies: class names like `terminalquotaerror` are safe (NOT substrings of the stack-frame filename `googleQuotaErrors.js`); covered by `TestCli_ModelNotFoundIsNotQuotaExhausted`.

**Affected areas:** `.gemini/settings.json`, `provider.DefaultQuotaExhaustedPatterns`/`Error.QuotaExhausted`, `registry` (`quota_cooldown`), `router` cooldown selection, `config.example.yaml`.

**Revisit if:** transient network blips through gemini become noisy without the CLI's own retry — raise `maxAttempts` to 2 rather than re-enabling the full backoff ladder, or parse the server-suggested reset delay into a dynamic cooldown.

---

## LLP-016 — /healthz reflects serveability; degradation in the body, always HTTP 200

**Decision:** The router tracks per-impl outcomes (consecutive failures since last success, last error + timestamps). `/healthz` reports per impl `serveable` = configured && not cooling down && `consecutive_failures < 3`, plus the raw signals; top-level `status` is `"degraded"` when no impl is serveable. The endpoint always returns HTTP 200.

**Rationale:** healthz used to reflect config, not reality — it said `ok` while every completion 502'd, so MND's `llp_up` trusted it and burned a full chain walk (todo 2026-06-12). Passive outcome tracking is free and catches what cooldown misses (timeouts deliberately set no cooldown). The threshold (3) tolerates a one-off blip without flapping. HTTP stays 200 because DSH's `llpclient.getJSON` rejects non-200 — degraded must still render per-impl detail in the dashboard; MND's `jq -e '.status == "ok"'` picks up degradation with no client change. An active probe (test completion per impl) was rejected: it burns quota and adds latency for a signal passive tracking already gives after the first real failure.

**Affected areas:** `router` (outcome stats), `server.healthz`, README, MND `llp_up` semantics (no change needed), DSH LLM-proxy tab (new fields available, optional).

**Revisit if:** clients need pre-traffic liveness (nothing failed yet, but the CLI is broken) — add an opt-in `?probe=1` that runs a tiny completion per impl.

---

## LLP-017 — Startup guard: a gemini-cli command without an explicit `--approval-mode` refuses to start

**Decision:** Two enforcement points, same invariant ("the approval mode is chosen in config, never inherited"): (1) `run.sh` greps every `command:` line that execs gemini-cli and aborts loudly if the flag is missing on that line; (2) `registry.Build` validates the parsed argv of every cli impl and returns an error. An impl that deliberately needs tool use passes by carrying its own explicit `--approval-mode <mode>`.

**Rationale:** Tomas's ruling after the regeneration regression: a regenerated/edited config that would drop the flag must **fail or warn loudly, never silently start insecure**. The patched example alone doesn't close the vector — any stale example (an old worktree, a manual copy) re-burns it. The shell guard is line-level (a file-level grep is fooled by the flag appearing in comments — found while testing) and protects deployments running an older binary; the Go guard covers exotic YAML layouts and every launch path that bypasses `run.sh`. Verified against the real pre-fix example from git history: FATAL before build.

**Affected areas:** `run.sh`, `registry.Build` (+ `TestBuildRejectsGeminiWithoutApprovalMode`), the tc-global-restarter worktree's runtime copies (patched out-of-band 2026-06-13 so the next fleet restart is safe pre-merge).

**Revisit if:** the config format moves to multi-line command arrays (shell guard would miss them; the Go guard still catches it — tighten the shell grep or drop it then).

---

## LLP-018 — Streaming is façade-level: completed responses re-emitted as SSE

**Decision:** `stream: true` is honored by running the request through the router exactly as before (failover, per-impl queueing, cooldowns, usage accounting unchanged) and re-emitting the completed response as OpenAI `chat.completion.chunk` SSE events: role delta, content in ≤256-rune chunks, a final `finish_reason:"stop"` chunk carrying `usage`, then `data: [DONE]`. Errors are never streamed — failures return the regular JSON error envelope before any SSE bytes. No upstream pass-through streaming.

**Rationale:** Tomas's "keep it simple" (2026-06-13). The interop win is that OpenAI SDK clients with `stream=true` work at all (previously they mis-received a plain JSON body). True pass-through would need a streaming Provider interface and breaks failover: once the first delta reaches the client, the router cannot switch impls mid-stream. LLP's backends are batch-shaped today anyway — CLI impls return complete stdout, and openllm has no live endpoint wired (enabling it stays config-only, LLP-007) — so pass-through could not improve first-token latency yet.

**Affected areas:** `openai` stream types, `server.writeStream`, README; no router/provider/usage changes.

**Revisit if:** a live streaming-capable backend needs first-token latency — add a `Streamer` interface with fail-over-only-before-first-delta semantics.

---

## LLP-019 — Ollama replaces the stubbed OpenLLM impl: local GPU, container, no API key

**Decision:** The generic `openllm` stub (LLP-007) is replaced by a concrete `ollama` impl: an Ollama container (`docker-compose.yml`, `network_mode: host` → `127.0.0.1:11434`) with GPU passthrough (NVIDIA GTX 1080, 8GB VRAM), using `qwen2.5:7b` as the default model. The impl uses the existing `HttpProvider` unmodified — Ollama speaks the OpenAI `/v1` API. Ollama sits last in both `auto` and `gml-analyze` chains; it serves when all CLI impls are exhausted. The router already skips unavailable HTTP impls (LLP-007), so when Ollama is not running, it is silently passed over.

**Rationale:** Closes the "built-but-stubbed" gap (LLP-007): the failover chain now has a concrete backstop that runs locally with zero API cost. `qwen2.5:7b` fits the GTX 1080's 8GB VRAM comfortably (4.7GB) and handles GML's structured-output (JSON) tasks well. Container + `network_mode: host` follows the house style (MO §1) while staying reachable by the host-bound LLP binary (LLP-006). No API key needed — Ollama has no auth on its local endpoint.

**Affected areas:** `config.example.yaml` (impl rename + base_url), `docker-compose.yml` (new), `setup-ollama.sh` (new), `smoke.sh` (ollama test), failover chains (`auto`, `gml-analyze`), README.

**Revisit if:** the host gets more VRAM (upgrade to a larger default model), or Ollama needs to run on a separate GPU box (change `base_url` and potentially add auth).
