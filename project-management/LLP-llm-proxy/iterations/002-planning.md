# LLP — Iteration 002: Planning

- **Phase:** 1 — Planning
- **Phase lead:** Developer
- **Start:** 2026-06-12
- **End:** 2026-06-12

---

## Goal of v1

A Go binary running on the host (port `4000`) that exposes `POST /v1/chat/completions` (OpenAI-compatible) and serves it via a failover chain of impls: **Gemini CLI → Claude CLI → OpenLLM(stubbed)**. It queues/rate-limits per impl, records usage/cost in SQLite, and authenticates clients by bearer key. Acceptance is proven by migrating one real GML pipeline step onto it.

---

## Component Design

### 1. HTTP surface
| Method | Path | Auth | Purpose |
|---|---|---|---|
| POST | `/v1/chat/completions` | bearer | OpenAI-compatible completion (non-streaming v1; `stream:true` = stretch) |
| GET | `/v1/models` | bearer | List logical model names from registry |
| GET | `/healthz` | open | Liveness + per-impl availability/cooldown state |
| GET | `/admin/usage` | bearer | Usage/cost aggregation (by agent / impl / day) |

### 2. Config (`config.yaml`, loaded at startup)
```yaml
port: 4000
db_path: ./data/llp.db
clients:                      # bearer key -> agent name (keys themselves come from env, see run.sh)
  - { key_env: LLP_KEY_GML, agent: gml }
  - { key_env: LLP_KEY_DSH, agent: dsh }
impls:
  gemini:                     # impl #1, default
    type: cli
    command: ["npx", "@google/gemini-cli", "-e", "none", "--approval-mode", "plan", "-p", ""]
    env:   { GOOGLE_CLOUD_PROJECT: "your-gcp-project-id" }
    timeout: 180s
    cooldown: 60s             # after a rate-limit, skip this impl this long
    price: { in: 0, out: 0 }  # free via host CLI
  claude:                     # impl #2
    type: cli
    command: ["claude", "-p", "--output-format", "text"]
    model_flag: "--model"
    model_id: ""              # configurable; verify current id before pinning (see Decision LLP-P3)
    timeout: 180s
    cooldown: 60s
    price: { in: 0, out: 0 }
  openllm:                    # impl #3, stubbed (empty base_url => disabled)
    type: http
    base_url: ""
    model_id: ""
    api_key_env: OPENLLM_API_KEY
    price: { in: 0, out: 0 }  # set when wired to a metered remote
models:                       # logical name -> ordered failover chain of impl keys
  auto:        { chain: [gemini, claude, openllm] }
  gml-analyze: { chain: [gemini, claude] }
default_model: auto           # used when client sends an unregistered model name
```

### 3. Packages (`projects/LLP-llm-proxy/`)
```
cmd/llp/main.go            # flags, load config, wire deps, http.ListenAndServe
internal/
  openai/    types.go      # Request, Message, Response, Choice, Usage (OpenAI shape) + (un)marshal
  registry/  registry.go   # load config.yaml; resolve logical model -> []ImplRef; validate
  provider/  provider.go   # Provider interface + NormalizedRequest/Response
             cli.go         # CliProvider  (impls #1, #2): render messages->prompt, exec, parse stdout, stripCodeFence
             http.go        # HttpProvider (impl #3): OpenAI-compatible HTTP client to base_url
  router/    router.go      # walk failover chain; classify errors; per-impl semaphore + cooldown
  usage/     store.go       # SQLite: insert + aggregate; cost = tokens * price
  auth/      auth.go        # bearer -> agent; middleware
  server/    server.go      # handlers, routing, request validation, OpenAI response assembly
config.example.yaml
run.sh                      # host launcher: load client keys (+ any impl keys) from op/env, exec binary
README.md
go.mod
```

### 4. Provider interface
```go
type Provider interface {
    Name() string
    Generate(ctx context.Context, req NormalizedRequest) (NormalizedResponse, error)
}
// NormalizedRequest: messages []Message, model string, params (temp, maxTokens)
// NormalizedResponse: content string, promptTokens, completionTokens int (estimated for CLI)
```
- **CliProvider** (#1/#2): flatten `messages` → a single prompt (system text first, then turns), pipe to the CLI via **stdin** (matches GML), capture stdout, `stripCodeFence`, return content. Token counts **estimated** (chars/4 heuristic) — CLIs don't report usage in text mode (see Decision LLP-P2). Timeout via `exec.CommandContext`.
- **HttpProvider** (#3): POST OpenAI-shaped body to `base_url`; parse `choices[0].message.content` + real `usage`. Disabled when `base_url == ""`.

### 5. Router & failover
- For the resolved chain, try each impl in order. Skip an impl that is **cooling down** or **disabled**.
- **Error classification:**
  - *Retryable → fail over:* context timeout; CLI non-zero exit whose stderr matches rate-limit patterns (`quota`, `RESOURCE_EXHAUSTED`, `rate limit`, `429`, `overloaded`); HTTP 429 / 5xx. A rate-limit hit also starts that impl's `cooldown`.
  - *Terminal → return as-is:* HTTP 400/401/403; a request the proxy itself rejects (bad body). (Pure CLI invocations are proxy-controlled, so "bad request" is rare.)
  - *Other CLI non-zero exit:* treated as retryable→failover but logged distinctly (could be transient).
- If all impls in the chain fail, return the **last** error as an OpenAI-shaped error response with the appropriate status.
- **Rate limiting:** per-impl semaphore, default concurrency **1** (serialize — the simplest defense against "token exhaustion"); configurable later.

### 6. Usage store (SQLite, WAL)
`usage(id, ts, agent, requested_model, impl_used, prompt_tokens, completion_tokens, est_cost_usd, latency_ms, status, error)`.
Cost = `prompt_tokens*price.in + completion_tokens*price.out` (0 for the free CLI impls). `/admin/usage` returns sums grouped by agent, impl, and day.

---

## Test Requirements (TDD — write FIRST, per §4 Phase 1)

| # | Package | Test |
|---|---|---|
| T1 | openai | Request/Response marshal↔unmarshal round-trip; minimal + full payloads |
| T2 | registry | Load config; resolve `gml-analyze`→[gemini,claude]; unknown model→`default_model` chain; `openllm` with empty `base_url` resolves as disabled |
| T3 | provider/cli | Fake CLI (testdata script): messages→prompt rendering; stdout→content; `stripCodeFence` for fenced + prose-prefixed output; non-zero exit→error; context timeout→error; rate-limit stderr→retryable error |
| T4 | provider/http | httptest upstream: request mapping; parse content+usage; 429→retryable; 400→terminal; disabled when base_url empty |
| T5 | router | chain[A,B]: A retryable→B serves; A terminal→propagate (no failover); both fail→last error; A rate-limited→A skipped until cooldown expires; cooling impl skipped |
| T6 | router | Rate limiter: cap=1 ⇒ N concurrent calls serialize (observe via a gated fake provider) |
| T7 | usage | Insert rows; aggregate by agent/impl/day; cost calc for a priced impl = tokens×price |
| T8 | auth | Valid key→agent; missing/invalid bearer→401; `/healthz` open |
| T9 | server | Integration with fake impls: POST `/v1/chat/completions`→200 OpenAI shape + usage; `/v1/models`; `/admin/usage` requires auth; oversized body→400 |

**Live / real-data tests (MO "run it or it doesn't count", not synthetic):**
- L1: start LLP on host; `curl` a completion that drives the **real gemini-cli** end-to-end → real output in OpenAI shape; row in `usage`.
- L2: **forced failover** — temporarily misconfigure `gemini` (bad command) so it fails; confirm **claude** serves the request and `impl_used=claude`.
- L3: **GML migration proof** — repoint one GML pipeline step (analyze) in `run-task.sh` to `curl http://localhost:4000/v1/chat/completions` instead of invoking the CLI directly; confirm GML analyze still produces valid output end-to-end.

---

## Build Order (checklist)

- [ ] 0. Scaffold module, layout, README skeleton, `config.example.yaml`
- [ ] 1. `openai` types  (T1)
- [ ] 2. `registry` loader + resolution  (T2)
- [ ] 3. `provider` interface + `CliProvider`  (T3)
- [ ] 4. `HttpProvider`  (T4)
- [ ] 5. `router` failover + rate limiter + cooldown  (T5, T6)
- [ ] 6. `usage` SQLite store + cost  (T7)
- [ ] 7. `auth` bearer→agent middleware  (T8)
- [ ] 8. `server` handlers wiring all endpoints  (T9)
- [ ] 9. `run.sh` host launcher (keys from op/env) + `main.go`
- [ ] 10. Live verification L1 (real gemini-cli) + L2 (forced failover)
- [ ] 11. GML migration proof L3
- [ ] 12. Docs: README (what/run/test), update `team/developer/SKILLS.md`, ASSUMPTIONS if changed
- [ ] (stretch) 13. `stream:true` SSE passthrough

---

## Safety Checklist Review (§7)

**Security**
- *Secrets:* client bearer keys come from **env** (`LLP_KEY_*`), injected by `run.sh` from `op`; never in code/config-committed. CLI impls use the host's logged-in sessions (not our secret). Impl #3 remote key (when wired) from `op`→env. ✔
- *Parameterized SQL:* all `usage` inserts/queries use placeholders. ✔
- *HTML escaping:* N/A — JSON API only, `encoding/json`. ✔
- *CSRF:* N/A — bearer-token API, no cookies/browser sessions. ✔
- *Input validation:* validate JSON body, require `messages`, cap total prompt bytes (configurable, e.g. 1 MB), validate model name. ✔
- *Auth on endpoints:* bearer required on `/v1/*` and `/admin/*`; `/healthz` open by design. ✔
- *Prompt-injection boundary:* **LLP is transport, not defense.** It relays prompts verbatim; injection hardening stays in the caller (GML already has its 5-layer defense). Recorded so no one assumes the proxy sanitizes. ✔

**Quality**
- Tests written first (T1–T9) before each component; no merge of a component without its green tests. ✔
- Live runs L1–L3 required before "done". ✔

**Performance**
- *Hot path = CLI subprocess startup.* `npx @google/gemini-cli` boots Node per call (slow). Flag: prefer a globally-installed `gemini` over `npx` if available; measure call latency in L1. GML already tolerates this today. ⚑
- *Serialized per-impl (cap 1)* bounds throughput — acceptable for agent cadence (GML ~36 calls/hr), not high-QPS. Revisit cap if a client needs concurrency. ⚑
- *SQLite WAL* for concurrent read (usage endpoint) during writes. ✔

---

## Decisions

### [Decision LLP-P1: Stack confirmed — Go + stdlib net/http + SQLite]
**Date:** 2026-06-12 · **Phase:** Planning · **Decided by:** Developer
**Decision:** Confirm the provisional LLP-005 stack. Go single binary, stdlib `net/http`, `mattn/go-sqlite3` (or `modernc.org/sqlite` pure-Go to avoid cgo) for usage.
**Alternatives considered:** A framework (chi/gin) — unnecessary for ~4 routes.
**Reasoning:** Matches GML/DSH; minimal deps; easy host binary. Pure-Go sqlite avoids cgo cross-compile pain — pick during scaffold.
**Revisit if:** Streaming or middleware needs outgrow stdlib.

### [Decision LLP-P2: CLI token usage is estimated]
**Date:** 2026-06-12 · **Phase:** Planning · **Decided by:** Developer
**Decision:** For CLI impls, report estimated token counts (chars/4 heuristic) in the `usage` field and store; exact counts only for HTTP impls that return real `usage`.
**Alternatives considered:** Run a real tokenizer per provider (adds deps + per-model drift); omit usage for CLI (breaks OpenAI shape).
**Reasoning:** CLIs don't emit token usage in text mode. Cost for CLI impls is 0 anyway (free), so estimation only affects volume stats, where chars/4 is good enough.
**Revisit if:** A client needs accurate CLI token accounting.

### [Decision LLP-P3: Model ids are config, verified before any metered/HTTP wiring]
**Date:** 2026-06-12 · **Phase:** Planning · **Decided by:** Developer
**Decision:** Keep provider model ids and prices in `config.yaml`, not code. GML currently pins a stale `claude-opus-4-6`; LLP leaves `claude.model_id` empty (CLI default) for v1. Before wiring the metered HTTP impl (later iteration) or pinning a Claude id, **verify current model ids + pricing via the `claude-api` skill** — do not copy from memory or from GML's stale value.
**Reasoning:** Honors the standing rule never to state model ids/pricing from memory; v1's live impls are free CLIs so no price table is needed yet.
**Revisit if:** Impl #3 is wired to a metered remote — populate + verify the price table then.
