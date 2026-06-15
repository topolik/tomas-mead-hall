# LLP — Iteration 001: Ideation

- **Phase:** 0 — Ideation
- **Phase lead:** Developer
- **Start:** 2026-06-12
- **End:** 2026-06-12

---

## The Idea

A unified HTTP gateway for LLM access — one API in front of Claude, Gemini, a local GPU, and OpenRouter. Inspired by LiteLLM but self-hosted and KISS-scoped. It abstracts provider differences behind a single endpoint so every agent in the family speaks one protocol, and it does **session management to avoid token exhaustion**. It runs as a service; all containers reach it via HTTP. **GML Mode 2 is the first migration customer.**

### Why now (leverage)
GML already calls LLMs, but does so by shelling out to host CLIs from `run-task.sh`:
- Default: `GOOGLE_CLOUD_PROJECT=… npx @google/gemini-cli -e none --approval-mode plan -p "" < prompt`
- Optional: `claude -p --model claude-opus-4-6 --output-format text < prompt`

There is **no retry, no rate-limiting, no session management, no cost tracking** anywhere in GML today. GML's own `ASSUMPTIONS.md` flags the unblock: *"Revisit when LLM proxy project provides HTTP API."* Every future agent will have the same need. The proxy is the one piece of infrastructure that makes all subsequent agent work cheaper and more reliable to build.

---

## Core Problem

Two problems, one gateway:
1. **Fragmentation** — each agent re-implements provider quirks (CLI vs API, auth, output cleanup like Gemini's markdown fences, model selection). One protocol kills the duplication.
2. **Token / quota exhaustion** — 3 GML daemons on 5-minute intervals already do ~36 LLM calls/hour; multiple agents will collide on shared provider quotas. The gateway serializes, throttles, and fails over so no single agent starves the others or trips a hard limit.

---

## What It Does (v1 scope — "full gateway up front", per Tomas)

Tomas chose the ambitious cut over a thin GML-only slice. v1 targets the real gateway:

### Confirmed features
- **OpenAI-compatible API as a façade** — `POST /v1/chat/completions` (+ `GET /v1/models`, `GET /healthz`) is *just the stable client interface*. Any OpenAI SDK works as a drop-in. Under it sit several swappable impls.
- **Pluggable backend impls** behind a `Provider` interface, in failover order:
  1. **Gemini CLI** (default) — shell out to `gemini-cli`, reusing the host's Cloud-project auth (free tier).
  2. **Claude CLI** (second) — shell out to `claude -p`, reusing the host subscription (free tier).
  3. **OpenLLM / OpenAI-compatible HTTP** (third) — a configurable base URL; can later front Ollama, a remote API, OpenRouter, or whatever OpenLLM supports. Added when needed.
- **Model registry** — maps a client-facing logical model name → `{impl, upstream model id, base URL/command}`, plus the ordered **failover chain** above per logical alias.
- **Queue + rate limiting** — bounded per-provider concurrency + token-bucket throttle. This is the heart of "session management to avoid token exhaustion."
- **Auto-failover** — on retryable failure (429 / quota / 5xx / timeout) from the primary, try the next provider in the chain; recover when it clears. Terminal errors (400 bad request) are NOT retried.
- **Usage & cost tracking** — persist per request: timestamp, calling agent (API key), requested model, provider used, prompt/completion tokens, estimated cost, latency, status. Queryable via an admin endpoint; later surfaced in DSH.
- **Per-agent auth** — clients present a bearer key so usage is attributable per agent (single human user, many agents).
- **Containerized** — Docker, `network_mode: host` (house style), SQLite on a named volume, provider keys injected from `op` via env at startup (never in image/code).

### Stretch within v1 (sequence last)
- **Streaming** — `stream: true` SSE passthrough. GML doesn't need it (it wants whole-JSON), but OpenAI-compat clients expect it. Implement after the non-streaming path is solid.

### Explicitly deferred (later iterations)
- Smart cost/quality-based routing (auto-pick cheapest adequate model) — v1 uses explicit model + static failover chains.
- Response caching / dedup.
- Prompt/response logging UI in DSH (v1 exposes raw usage; DSH integration is its own iteration).
- Multi-tenant beyond "one human, many agents."

---

## What It's Not
- Not a re-implementation of LiteLLM's full surface — KISS, only what the agent family needs.
- Not an agent framework — it relays chat completions; it does not own prompts, tools, or orchestration.
- Not a secrets manager — keys come from `op`/env; the proxy only consumes them.

---

## Architecture Sketch (exploratory — refined in Phase 1)

```
client (agent) ──Bearer key──▶ [ Auth ] ──▶ [ Queue + per-impl rate limiter ]
                                                        │
                                                        ▼
                                              [ Router / failover chain ] ──reads──▶ Model registry (config)
                                                        │                     ──writes─▶ Usage+cost store (SQLite)
                                   ┌────────────────────┼─────────────────────┐
                                   ▼                    ▼                     ▼
                              Gemini CLI           Claude CLI            OpenLLM / OpenAI-compat HTTP
                              (exec, default)      (exec, 2nd)           (3rd → Ollama / remote / OpenRouter)
```

Provider interface (sketch):
```
type Provider interface {
    ChatCompletion(ctx, NormalizedRequest) (NormalizedResponse, error)
    Name() string
}
```
- `CliProvider` covers impls #1 and #2 — it builds the prompt, execs the CLI (`gemini-cli`, `claude -p`), and parses stdout (reusing GML's `stripCodeFence` learning for Gemini's markdown fences).
- `HttpProvider` covers impl #3 — an OpenAI-compatible HTTP client pointed at a configurable base URL (OpenLLM, Ollama's `/v1`, a remote API, or OpenRouter — all speak the same protocol).

---

## Stack (proposed — confirm in Planning)
- **Go** — matches GML, single static binary, excellent stdlib HTTP server. Team already has Go expertise.
- **SQLite** — KISS usage/cost store, single file on a named volume, no DB container (same call DSH made).
- **stdlib `net/http`** — no heavy framework needed for a handful of endpoints.
- **Host CLI access** — because impls #1/#2 exec `gemini-cli` and `claude`, the proxy needs those binaries *and their host auth*. The todo's "runs on host" line fits this: either run LLP directly on the host, or build a container image bundling the CLIs with their host auth mounted read-only (the same host-vs-container tension GML resolved by keeping CLI calls in `run-task.sh`). **Key Phase-1 decision — see Open Question #6.**

---

## Open Questions (need Tomas's input or Phase-1 exploration)

### 1. ~~API keys & cost~~ → mostly resolved by the CLI-default architecture
Wrapping the **Gemini CLI (default)** and **Claude CLI (second)** reuses the existing **free** host-CLI auth — so the normal path has **no per-token cost**. Metered keys are only needed for impl #3 when OpenLLM is pointed at a *remote* API. Remaining: confirm whether the OpenLLM→remote path is in v1 at all, and if so where that key lives (`op` item / `gml-creds.enc` pattern).

### 2. OpenLLM backend target (impl #3) — RESOLVED
**Resolved (Tomas, 2026-06-12):** built-but-stubbed in v1. The `HttpProvider` adapter is implemented and tested against a mock OpenAI-compatible server, but no live target is wired. It goes live in a later iteration when pointed at Ollama / a remote endpoint. See decision below.

### 3. Failover chain & GML output consistency
- Order is now fixed by the impl ranking: **Gemini CLI → Claude CLI → OpenLLM**. Confirm this matches the standing "Gemini primary" preference (it does).
- Failover risk for GML: its output is a strict JSON schema; a mid-batch switch from Gemini to Claude could change formatting. GML already normalizes both (it ran dual-LLM before) — likely fine. Confirm, or pin specific GML steps to a single impl.

### 4. Quota/budget ceilings
- Even on free CLIs there are session/rate limits. What should the rate limiter enforce per impl (requests/min? concurrency? a cooldown after a CLI rate-limit error)?

### 5. Port
- DSH=9090, GML uses none. Proxy default proposed: **`4000`** (`LLP_PORT`, overridable). Confirm or pick.

### 6. Host vs container deployment — RESOLVED
**Resolved (Tomas, 2026-06-12):** run LLP directly on the host as a single Go binary. The `gemini`/`claude` CLIs and their refreshing OAuth sessions already work there — no auth mounting. Matches the todo's "runs on host". Deliberately breaks MO §1 "everything in containers" for this one service. See decision below.

### 7. Defaults applied (no input needed unless Tomas objects)
- **Port:** `4000` (`LLP_PORT`, overridable).
- **Queue/rate limit (Open Q#4):** per-impl serialized requests (concurrency 1) + a cooldown when a CLI reports a rate-limit, before failing over. Tunable; refined in Planning.

---

## Test Requirements (preview — formalized in Phase 1, TDD)
- `httptest` mock upstreams per provider → adapter request/response mapping correct.
- Failover: primary returns 429 → request succeeds on fallback; terminal 400 does NOT fail over.
- Queue/concurrency: N concurrent requests respect the per-provider cap; throttle holds the rate.
- Usage accounting: tokens + estimated cost recorded per request, attributable to the calling key.
- Auth: missing/invalid bearer key rejected.
- **Run it for real (MO):** drive a real request through the Gemini-CLI impl end-to-end, and migrate one GML pipeline step to call the proxy (instead of `run-task.sh` invoking the CLI directly) as the acceptance proof — not synthetic data.

---

## Candidate Project Name
`LLP-llm-proxy` (code `LLP`). Keeps artifact/commit names aligned with the `todo.txt` item ("LLM Proxy") even though scope grew to a full gateway. Alternatives considered: `LMG-llm-gateway`, `GTW-gateway`. Free of collision with DSH/GML/MOP.

---

## Decisions

### [Decision: Project name and code]
**Date:** 2026-06-12
**Phase:** Ideation
**Decided by:** Developer (confirmable by Tomas)
**Decision:** `LLP-llm-proxy`, code `LLP`
**Alternatives considered:** `LMG-llm-gateway`, `GTW-gateway`
**Reasoning:** Matches the backlog item's name ("LLM Proxy"); keeps the mental model and commit prefixes consistent. Renaming later is cheap if Tomas prefers "gateway".
**Revisit if:** Tomas prefers the "gateway" naming to reflect the broadened scope.

### [Decision: Provider access model = CLI-default impls behind an OpenAI façade]  *(supersedes the initial "Direct HTTP APIs" pick)*
**Date:** 2026-06-12
**Phase:** Ideation
**Decided by:** Tomas
**Decision:** The OpenAI-compatible REST API is *only the client-facing interface*. Under it are swappable impls in failover order: **① Gemini CLI (default), ② Claude CLI, ③ OpenLLM / OpenAI-compatible HTTP** (configurable base URL → Ollama, remote API, OpenRouter, or whatever OpenLLM supports).
**Alternatives considered:** Tomas first selected "Direct HTTP APIs for all backends," then refined to this after the cost flag — the all-API model made GML's free CLI usage metered for no benefit on the common path.
**Reasoning:** Impls #1/#2 reuse the existing **free** host-CLI auth (no per-token cost, directly reuses what GML already runs), while impl #3 keeps a clean HTTP path for local/remote OpenAI-compatible models when wanted. Façade stays OpenAI-compatible so clients are uniform regardless of impl. Tradeoff: the proxy must reach host CLIs + their auth (see Open Question #6).
**Revisit if:** Maintaining CLI subprocess auth inside/alongside a container proves more painful than just paying for metered HTTP APIs.

### [Decision: API contract = OpenAI-compatible]
**Date:** 2026-06-12
**Phase:** Ideation
**Decided by:** Tomas
**Decision:** Expose `POST /v1/chat/completions` (OpenAI-compatible) as the client contract.
**Alternatives considered:** Minimal custom `POST /generate {prompt} → {text}` tailored to GML's stdin→text model.
**Reasoning:** Any OpenAI SDK / OpenRouter-style tool works as a drop-in client; future-proof for agents beyond GML. Worth the extra request-shape surface (messages/roles/choices/usage).
**Revisit if:** The OpenAI surface proves heavier than its interop value for this single-user family.

### [Decision: Exhaustion behavior = Queue + auto-failover]
**Date:** 2026-06-12
**Phase:** Ideation
**Decided by:** Tomas
**Decision:** Serialize/throttle requests; on retryable provider failure (429/quota/5xx/timeout), automatically fail over to the next provider in the chain, then recover.
**Alternatives considered:** Queue + wait/retry same provider (consistent model, slower); Queue-only fail-fast (simplest, caller retries).
**Reasoning:** Maximizes availability across the agent family. Tradeoff noted: the responding model can switch mid-batch — see Open Question #3 for GML's strict-JSON output.
**Revisit if:** Model-switch-mid-batch causes output-schema inconsistency for GML.

### [Decision: v1 scope = Full gateway up front]
**Date:** 2026-06-12
**Phase:** Ideation
**Decided by:** Tomas
**Decision:** v1 ships all four backends + routing/failover + cost tracking, not a thin GML-only Gemini slice.
**Alternatives considered:** Thin slice — one endpoint replacing GML's Gemini CLI call, defer the rest.
**Reasoning:** Tomas wants the real gateway. Explicitly overrides the MO KISS thin-slice default; recorded as a deliberate scope choice (MO §4 — scope is Tomas's to set).
**Revisit if:** v1 proves too large to land in a reasonable iteration — fall back to the thin slice and grow.

### [Decision: Deployment = run LLP on the host]
**Date:** 2026-06-12
**Phase:** Ideation
**Decided by:** Tomas
**Decision:** LLP runs directly on the host as a single Go binary (port `4000`), not in a container. Agent containers (GML, …) reach it via HTTP over the host network.
**Alternatives considered:** Container bundling `gemini-cli`/`claude` with host auth dirs mounted read-write for OAuth refresh.
**Reasoning:** The CLI impls (#1/#2) reuse the host's logged-in `gemini`/`claude` sessions; running on the host means zero auth-mounting and no token-refresh-into-a-mount complications. Matches the todo's "runs on host". Knowingly breaks MO §1 "everything in containers" for this single service — the service's whole job is to broker host-authenticated CLIs.
**Revisit if:** A future deployment needs LLP off the dev host, or container auth-mounting becomes easy enough to honor MO §1.

### [Decision: OpenLLM (impl #3) built-but-stubbed in v1]
**Date:** 2026-06-12
**Phase:** Ideation
**Decided by:** Tomas
**Decision:** v1 implements and tests the `HttpProvider` adapter (against a mock OpenAI-compatible server) and registers impl #3, but wires no live backend. Gemini CLI + Claude CLI are the live impls in v1.
**Alternatives considered:** Wire impl #3 live to local Ollama, or to a remote OpenAI-compatible API (OpenRouter/other) in v1.
**Reasoning:** v1 proves the path GML actually uses today (Gemini→Claude failover) without depending on a GPU box or a metered remote key. The adapter is ready so going live later is config, not code.
**Revisit if:** A local Ollama box or a remote endpoint becomes available and worth wiring.
