# LLP — Iteration 004: Model selection (review feedback → implement)

- **Phase:** Review → Implementation
- **Phase lead:** Developer
- **Start:** 2026-06-12
- **End:** 2026-06-12

---

## Review feedback (Tomas)

Reviewing v1: *"does the api support choosing the model? to not only switch gemini or claude models but also switch from gemini to claude to openllm?"* → then *"go and test it end to end."*

v1 only let clients pick a **logical model name** mapped to a failover chain; an impl name like `claude` silently fell back to the default chain (verified live: `model:"claude"` was served by gemini). No per-request choice of a specific backend, and no way to choose the underlying model id (gemini flash↔pro, claude opus↔sonnet).

## What shipped

OpenRouter-style `model` field, three forms:

| `model` value | Behavior |
|---|---|
| `auto`, `gml-analyze` | defined logical model → its failover chain *(existing)* |
| `gemini`, `claude`, `openllm` | **pin to that single backend** (1-element chain, no failover) |
| `gemini/gemini-2.5-pro`, `claude/sonnet` | **pin backend + override the underlying model** for that request |

Split on the first `/` only (so override ids may contain `:` or `/`, e.g. Ollama's `llama3.1:8b`). A defined logical model always wins on exact match; an override on a logical model is ignored (chains span heterogeneous impls). Unknown → default chain.

Changes: `provider.Request.ModelID` (override); `CliProvider` applies it via the impl's `model_flag` (errors terminally if none configured); `HttpProvider` sends it as the model; `registry.Resolve` returns `(chain, override)`; `server` plumbs it. Config gains `model_flag` for gemini (`-m`) and claude (`--model`) — both verified to accept a model id and to fail on an invalid one.

## Live verification (against the running service, real CLIs)

| Request | Result |
|---|---|
| `auto` | served by **gemini** (chain) |
| `gemini` | **gemini** (pinned) |
| `claude` | **claude** (pinned — was gemini before this change) |
| `gemini/gemini-2.5-flash` | gemini, `PONG` (override applied) |
| `claude/haiku` | claude, `PONG` (override applied) |
| `gemini/not-a-real-model` | error, **no failover** (pinned), **gemini not cooled down** |

## Bug found & fixed (during live test)

**Rate-limit misclassification via stack-trace filename.** A gemini `ModelNotFoundError` (HTTP 404) was classified as a **rate limit** because the bare substring `"quota"` matched the Node stack-trace filename `googleQuotaErrors.js` — which then wrongly put gemini on **cooldown**, so a bad-model request would bench the default backend for subsequent calls. Fixed by tightening `DefaultRateLimitPatterns` to specific phrases (`resource_exhausted`, `quota exceeded`, `too many requests`, `code: 429`, …) that don't match filenames or `:429:` line numbers. Added regression test `TestCli_ModelNotFoundIsNotRateLimit` using the real stderr. Re-verified live: bad model → plain error, gemini stays available.

**Tests:** 46 pass (race-clean) — added registry pin/override/ignore-override, CLI override-applies-flag / override-without-flag-errors / model-not-found-not-rate-limit, HTTP override, server pin+override-plumbing.

## Decisions

### [Decision LLP-010: model selection syntax = OpenRouter-style `impl[/model]`]
**Date:** 2026-06-12 · **Phase:** Implementation · **Decided by:** Developer (on Tomas's feedback)
**Decision:** `model` accepts a logical name, a bare impl name (pin), or `impl/<model-id>` (pin + override). Split on first `/`. Logical models keep priority and ignore overrides.
**Alternatives considered:** `impl:model` separator (rejected — collides with Ollama ids like `llama3.1:8b`); a separate `provider` request field (rejected — breaks OpenAI-compat clients that only send `model`).
**Reasoning:** Mirrors OpenRouter, so off-the-shelf OpenAI clients select backends/models with no extra fields. Pin = "this exact thing, fail if down"; logical model = "best-effort with failover".
**Revisit if:** clients need both a pin and a custom failover order in one request.

### [Decision LLP-011: rate-limit detection uses specific phrases, not bare keywords]
**Date:** 2026-06-12 · **Phase:** Implementation · **Decided by:** Developer
**Decision:** CLI rate-limit classification matches specific phrases; bare `"quota"`/`"429"` are NOT used (they match stack-trace filenames / line numbers).
**Reasoning:** False positives wrongly trigger cooldown, benching a healthy backend. Better to miss an oddly-worded rate-limit (degrades to plain retry/failover, no cooldown) than to bench a backend on a 404. See regression test.
**Revisit if:** a real rate-limit message is observed that none of the phrases catch — add it (with a test).
