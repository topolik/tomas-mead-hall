# LLP Iteration 009 — SSE streaming on /v1/chat/completions

- **Start:** 2026-06-13 00:25
- **End:** 2026-06-13 00:50
- **Phase:** Implementation (Tomas: "proceed with wiring OpenLLM streaming but keep it simple")
- **Note:** Per Tomas's ruling (2026-06-13), iteration 008 was merged to master and this
  iteration was developed directly on master in the parent workspace. No push to origin —
  Tomas pushes after his own runtime verification of :4000.

## What happened

`stream: true` was already accepted by the request type but silently ignored — OpenAI
SDK clients requesting a stream got a plain JSON body. LLP now honors it with
**façade-level streaming** (LLP-018): the router still runs the full completion
(failover, per-impl queueing, cooldowns, and usage accounting completely unchanged),
and the finished response is re-emitted as OpenAI `chat.completion.chunk` SSE events —
role delta first, content in ≤256-rune chunks (rune-safe), a final `finish_reason:"stop"`
chunk carrying `usage`, then `data: [DONE]`.

This works identically for every impl — gemini, claude, and **openllm**, which stays
config-only to enable (set `base_url`, LLP-007). Errors are never streamed: a failed
request returns the regular JSON error envelope before any SSE bytes.

## Changes

- `internal/openai`: `Delta`, `StreamChoice`, `StreamChunk` types.
- `internal/server`: `writeStream` (SSE headers, chunked re-emit, flush per event);
  `chatCompletions` branches on `req.Stream` after usage is recorded.
- Tests ×3: happy path (deltas concatenate to the full content, role first, usage in the
  final chunk, `[DONE]` last, usage row recorded), long multi-byte content splits across
  chunks and reassembles exactly, error path stays JSON (502, `application/json`).
- README (streaming section), ASSUMPTIONS LLP-018, PROJECT.md.

## Run log

- `go test -race ./...`: **63 pass** (60 + 3 streaming).
- Live (side instance :4100, real gemini CLI): `curl -N` with `"stream": true` →
  `data:` events in order (role → `STREAMING WORKS` content delta → stop+usage → `[DONE]`),
  `Content-Type: text/event-stream`. Token handshake unchanged.
- :4000 deploy: restarted from the parent workspace (master) after commit — see Run log
  addendum below.

## Decisions

### Façade-level streaming, not upstream pass-through
**Date:** 2026-06-13 00:30
**Phase:** Implementation
**Decided by:** todo-70 agent (within Tomas's "keep it simple")
**Decision:** Stream by re-emitting the completed response as SSE chunks; do not stream incrementally from upstreams.
**Alternatives considered:** True pass-through streaming (HTTP impl `stream:true` upstream relay; CLI impls via `--output-format stream-json`): requires a streaming Provider interface and breaks failover semantics — once the first delta is sent to the client, the router can no longer fail over mid-stream; per-impl queueing and usage accounting would need rework. Rejected for now as the opposite of "keep it simple".
**Reasoning:** The interop win is that OpenAI SDK clients with `stream=true` work against LLP at all (they currently mis-receive JSON). LLP's backends are batch-shaped today (CLI impls return full stdout; openllm has no live endpoint wired), so first-token latency cannot actually be improved by pass-through yet.
**Revisit if:** a live OpenLLM/Ollama backend lands AND a client measurably needs first-token latency — then add a `Streamer` interface with fail-over-before-first-delta semantics.
