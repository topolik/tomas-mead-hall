# 011 — Ideation (Iteration 4: smart LLM routing via LLP + orchestrate watch mode)

- **Start:** 2026-06-12
- **Phase:** Ideation
- **Trigger:** Tomas's go (2026-06-12): *"I want this orchestrator to be very smart so it should use the highest models on gemini and claude. In the ideal world it checks the quotas on both gemini and claude and chooses the best."* Plus his question: does MND call LLMs through the LLP project? (Answer: not yet — that's this iteration.)

## Problem

1. **Model smartness.** MND calls the CLIs directly (`run-task.sh llm()`, GML pattern) with hardcoded pins: gemini default (2.5-pro for profiles), `claude-opus-4-6` fallback (stale id). Today gemini-2.5-pro quota exhausted mid-work (~4h cooldown) and the fallback needed a manual `--model claude`. Tomas wants: highest models on both sides, quota-aware selection, automatic.
2. **Orchestration is manual.** `orchestrate.sh` answers one agent on demand. Tomas wants agents answered when they go `blocked`/`idle` without him (or anyone) polling. `herdr wait agent-status` watches one pane for one status — there is no "any agent needs input" signal, so a watch loop is needed.

## Facts established live (2026-06-12)

- **LLP is running** on `127.0.0.1:4000` (healthz: gemini available, claude available, openllm disabled) with the control-socket handshake at `~/.llp/control.sock`. GML is already migrated (opt-in via `LLP_URL`).
- LLP's router already does exactly the quota logic Tomas described: per-impl queue, rate-limit detection, **cooldown + failover to the next impl**. `model: auto` = chain `[gemini, claude, openllm]`; `impl/model-id` form pins a backend with a model override (no failover).
- **Highest model ids verified via the host CLIs:** `claude-fable-5` answers (also `claude-opus-4-8`); `gemini-3-pro-preview` is accepted (failed only with TerminalQuotaError "resets after 3h49m" — id valid, quota shared-exhausted).
- **The running LLP instance is fragile:** it was started from the `tomas-clone-new-project` worktree which has since been removed — the process lives on deleted inodes; restart from that path is impossible. Filed in todo.txt (LLP ops; restart from the parent workspace master checkout).
- **Security gap found (LLP side):** LLP's gemini command lacks `--approval-mode default` — the exact flag whose absence let gemini write files into this repo this morning (MND-011). Any client routing large/untrusted prompts through LLP-gemini re-opens that incident class. Filed in todo.txt for LLP; MND's prompts remain datamarked+framed regardless.

## Proposal

### A. Route MND's LLM calls through LLP (non-destructive, like GML)

- `run-task.sh llm()` gains an LLP path: handshake at the control socket (in-memory token, per the service-secret rule), `POST /v1/chat/completions`. **Auto-detect**: socket present + healthz ok → LLP; otherwise (or `MND_LLP=off`) → the existing direct-CLI code unchanged.
- **Highest-models chain, client-side for now:** try `gemini/gemini-3-pro-preview`, then `claude/claude-fable-5` (env-tunable `MND_LLP_CHAIN`). Each entry is LLP-pinned, so LLP still provides queueing, cooldown fast-fail, and usage accounting; MND advances the chain on failure. When LLP grows per-chain model overrides (filed in todo.txt), this collapses to one logical model server-side.
- Direct-CLI fallback pins updated too: claude `claude-opus-4-6` → `claude-fable-5`.
- Profile regeneration (300KB prompts) keeps its pinned `MND_GEMINI_MODEL` path until gemini-3-pro-preview is proven on large prompts — model upgrades there are config, not code.

### B. Orchestrate watch mode (`orchestrate-watch.sh`)

Poll `herdr agent list` (default 30s, `MND_WATCH_INTERVAL`); for every agent with status `blocked` or `idle`:

1. **Skip self + excluded panes** (`MND_WATCH_EXCLUDE`; agents whose cwd is this worktree are excluded by default — the orchestrator must not orchestrate itself).
2. **Skip no-question tails:** the ask response gains `pending: question|none` — an idle agent that finished cleanly and asks nothing gets nothing sent.
3. **Answer once per question:** answered-ledger keyed (pane, normalized-tail hash). The same question appearing again after our answer means the direction didn't unblock the agent → **escalate to DSH instead of resending** (loop protection).
4. Then the existing `orchestrate.sh` semantics: send on medium/high confidence (ledgered), DSH escalation on low.

### C. Deferred to iteration 5: contradiction resolution

Still the key quality finding (stale insights coexist with corrections), but Tomas's directive steers this iteration to model smartness + hands-off orchestration. Carried explicitly, not dropped.

## Why this shape

- **LLP, not bespoke quota probing.** "Check the quotas on both and choose the best" is LLP's router (cooldown = observed-quota memory). Duplicating it in MND would be a second, divergent implementation of infrastructure the team already shipped and live-verified today. MND becomes LLP's second customer; both projects harden one router.
- **Graceful degradation everywhere:** LLP down → CLIs; gemini cooling → claude; low confidence → Tomas via DSH. No mode where agents silently get nothing.
- **KISS on the watch loop:** a shell poll over `herdr agent list` — no daemon framework, no new binary; same auditability as orchestrate.sh.

## Open questions → ASSUMPTIONS.md

- MND-020 (LLP routing + auto-detect), MND-021 (client-side highest-model chain), MND-022 (watch loop protections), MND-023 (pending-question gate).
