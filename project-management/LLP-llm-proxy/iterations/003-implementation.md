# LLP — Iteration 003: Implementation

- **Phase:** 2 — Implementation
- **Phase lead:** Developer
- **Start:** 2026-06-12
- **End:** 2026-06-12

---

## What shipped

A working Go binary (`projects/LLP-llm-proxy/`) implementing the full v1 gateway from the plan:
OpenAI-compatible façade, CLI + HTTP providers, failover router with per-impl queue + cooldown,
SQLite usage/cost tracking, bearer auth, host launcher. **40 tests pass (race-clean); vet clean.**
All three live acceptance runs pass against the running service.

Build order followed `002-planning.md` exactly (T1→T9). Packages:
`openai`, `registry`, `provider` (`cli`/`http`), `router`, `usage`, `auth`, `server`, `cmd/llp`.

## Live acceptance runs (real, not synthetic — MO §1)

- **L1 — real gemini:** `POST /v1/chat/completions {model:auto}` → served by **gemini** (`PONG`), usage row recorded. ✓
- **L2 — forced failover:** config with gemini deliberately rate-limited → router failed over to **claude** (`PONG`); `/healthz` then showed `gemini cooling_down=true`. ✓
- **L3 — GML migration:** sourced the real `llp_complete` from `run-task.sh`, fed a GML-style analyze prompt through the proxy → returned a valid, extractable `Q1` concern JSON array (what GML's analyzer consumes). ✓
- Usage tracking verified live via `/admin/usage` (per day/agent/impl, with token sums and error counts).

## Bugs found & fixed

1. **CLI timeout hang (orphaned pipe).** `exec.CommandContext` killed the direct child but a grandchild (`npx`→node) kept the stdout pipe open, so `Wait()` blocked past the timeout. Fix: run the CLI in its own process group (`Setpgid`) and kill the whole group via `cmd.Cancel`, plus a `WaitDelay` backstop. `TestCli_Timeout` now returns in ~150ms instead of 5s.
2. **Gemini `--approval-mode plan` is broken in CLI v0.29.5.** Copied verbatim from GML, the flag errors `Approval mode "plan" is only available when experimental.plan is enabled` (exit 1, empty stdout) — it only "worked" earlier from `/tmp` because gemini resolves settings per-cwd. This is *also* why GML's own gemini path has been flaky (its `run-task.sh` has a "DIAGNOSTIC" block fighting exactly this). Fix: LLP's gemini impl uses `npx @google/gemini-cli -e none -p ""` (no approval-mode). After the fix, L1 was served by gemini directly. See Decision LLP-I1.
3. **`go mod tidy` prunes unimported deps.** Early tidy removed yaml/sqlite from `go.mod` until the importing packages existed; re-tidy after each new external import. (Workflow note, no code impact.)

## Observed behavior (not a bug — documented)

- **CLI impls relay output verbatim.** Gemini-cli (agentic) can prepend planning prose (e.g. "I will search for GEMINI.md files…") before the answer. The proxy faithfully returns it; clients needing strict formats validate/extract downstream — GML's analyzer already strips fences/prose. Recorded as the transport boundary (planning §Safety). A future iteration may add optional per-impl output extraction.

## Cross-project change

`projects/GML-gmail-agent/run-task.sh`: added `llp_complete()` and a non-destructive `LLP_URL` toggle on the **analyze** step. With `LLP_URL` unset, GML's CLI path is byte-for-byte unchanged.

## Decisions

### [Decision LLP-I1: gemini impl drops `--approval-mode plan`]
**Date:** 2026-06-12 · **Phase:** Implementation · **Decided by:** Developer
**Decision:** LLP's gemini CLI impl invokes `npx @google/gemini-cli -e none -p ""` (prompt via stdin), without `--approval-mode plan`.
**Alternatives considered:** Keep GML's exact flags (fails); enable `experimental.plan` in gemini settings (extra global config, unnecessary for a read-only completion).
**Reasoning:** `--approval-mode plan` governs agentic tool-approval and errors without an experimental flag; a plain prompt→text completion needs none. Verified: with the flag removed, gemini returns exit 0 + clean stdout.
**Revisit if:** A future impl needs gemini tool-use (then enable the experimental setting deliberately).

### [Decision LLP-I2: deployment uses a host process started by run.sh]
**Date:** 2026-06-12 · **Phase:** Implementation · **Decided by:** Developer
**Decision:** No `docker-compose.yml`. `run.sh` builds `./llp` and runs it on the host (LLP-006); client keys come from a git-ignored `.env` (generated on first run), provider auth from the host CLIs' sessions.
**Reasoning:** Consistent with LLP-006; keeps CLI auth trivial. `config.yaml` and `.env` are git-ignored; `config.example.yaml` is the committed template.
**Revisit if:** LLP needs to run off the host (then containerize with mounted CLI auth, per LLP-006's alternative).
