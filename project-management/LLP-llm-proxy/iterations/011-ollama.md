# Iteration 011 — Ollama backend (wiring the HTTP provider to a live local model)

**Start:** 2026-06-17
**Phase:** Implementation (combined ideation/planning/implementation — proportional to scope)

## What

Wire the existing `HttpProvider` (built-but-stubbed since LLP-007) to a live Ollama instance running
as a Docker container with GPU passthrough. Renames the config impl from `openllm` → `ollama`.

## Changes

- `docker-compose.yml` — Ollama container, `network_mode: host`, NVIDIA GPU, persistent volume
- `setup-ollama.sh` — start container + pull model (default `qwen2.5:7b`)
- `config.example.yaml` — `openllm` → `ollama` with live `base_url: http://127.0.0.1:11434/v1`; added `ollama` to `gml-analyze` chain as backstop
- `config-ollama-test.yaml` — standalone test config (ollama-only, port 4002)
- `smoke.sh` — tests `ollama` and `ollama/qwen2.5:7b` when Ollama is running
- `README.md` — updated for live ollama (setup instructions, model table, layout)
- `ASSUMPTIONS.md` — LLP-019 (ollama decision), LLP-007 superseded

## No Go code changes

The `HttpProvider` worked as-is. Ollama's `/v1/chat/completions` is fully OpenAI-compatible. Zero
code changes — this was a config + infrastructure addition only.

## Live verification

Tested on port 4002 (ollama-only config, side instance):

| Test | Result |
|------|--------|
| `auto` → ollama completion | ✅ `served_by: ollama`, content "PONG", usage `{prompt:37, completion:3}` |
| Streaming (`stream: true`) | ✅ SSE chunks, role delta, content, finish_reason=stop, usage in final chunk |
| Model override `ollama/qwen2.5:7b` | ✅ Pinned + served |
| Usage tracking | ✅ Per-agent per-impl rows in SQLite |
| Unit tests | ✅ 63 pass (race-clean, no changes to Go code) |

## Decisions

### [LLP-019: Ollama replaces the stubbed openllm]
**Date:** 2026-06-17
**Phase:** Implementation
**Decided by:** Developer (Tomas review pending)
**Decision:** Wire HttpProvider to local Ollama (container, GPU, qwen2.5:7b default), rename openllm → ollama
**Alternatives considered:** (a) Keep `openllm` name — rejected, confusing now that the backend is concrete; (b) Run Ollama on host — rejected, MO §1 "everything in containers"; (c) Remote API — rejected, adds cost and latency, local GPU is free
**Reasoning:** Closes the v1 stub gap with zero code changes. The router already skips unavailable HTTP impls, so ollama degrades gracefully when the container is down.
**Revisit if:** More VRAM available → upgrade default model; Ollama needs to run remote → change base_url + potentially add auth
