# 013 — Implementation (Iteration 4: LLP routing + watch mode)

- **Start:** 2026-06-12
- **End:** 2026-06-12
- **Phase:** Implementation

## What was built

- **LLP routing** (`run-task.sh`): `llp_up` (socket + healthz probe) / `llp_complete` (control-socket handshake; token held in a shell var and passed to curl via process-substituted config — never argv/env/disk). `llm()` walks the highest-models chain `gemini/gemini-3-pro-preview → claude/claude-fable-5` (`MND_LLP_CHAIN`, `MND_GEMINI_MODEL`, `MND_CLAUDE_MODEL`; `--model claude` flips order), every entry LLP-pinned so LLP applies queue/cooldown/usage. Chain exhausted or LLP down/off → direct CLI unchanged. Direct claude pin bumped `claude-opus-4-6` → `claude-fable-5` (verified live). **Profiles stay direct-CLI** (`MND_LLP=off`): the ~300KB profile prompt is the MND-011 incident vector and LLP's gemini impl lacks `--approval-mode default` (todo, Q1).
- **`pending` gate** (T35/T36): ask answers carry `pending: question|none`; missing/junk coerces to `question`. `orchestrate.sh` exits 3 on `none` — no fabricated direction to healthy agents. **48 Go tests.**
- **`orchestrate.sh`**: `--escalate` flag (force DSH escalation regardless of confidence — watch uses it when a question survives our answer); `MND_ASK_MODEL` env (watch steers the model non-interactively); exit-code contract 0/2/3.
- **`orchestrate-watch.sh`**: poll `herdr agent list` (30s default), react to `blocked`/`idle`; self-worktree + `MND_WATCH_EXCLUDE` skipped; per-(pane, normalized-tail-hash) ledger `data/watch-ledger.jsonl` — answer once, repeat-after-sent ⇒ escalate, `none`/`escalated` remembered (zero re-cost); per-pane cooldown (60s) bounds LLM spend on busy panes; `--once`/`--dry-run` for testing.

## Live verification (real agents, real LLP, real DSH)

- **T37**: ask through LLP → both chain entries 502 → **direct-CLI fallback answered** with citations. Root cause of the 502s is an LLP find: the running instance was started from the since-deleted `tomas-clone-new-project` worktree and spawns CLIs with that deleted cwd — claude: "current working directory was deleted"; gemini: npm bootstrap crash. **Healthz says ok while every completion fails** (impl serveability not probed — filed). So: LLP-routed serving remains unverified until the instance is restarted from the master checkout; MND's degradation design is what got proven instead.
- **T38**: `MND_LLP=off` → pure direct-CLI path, no LLP traffic.
- **T39** (scratch hwt-style worktree + live claude agent asking "YAML or SQLite?"):
  - a) dry-run pass: selection correct, own pane self-excluded; **bonus**: the DXP audit agent went idle mid-pass and the brain correctly ruled `pending: none` — "agent is mid-work… let it run" — reading its build state accurately from the tail.
  - b) seeded repeat-question → **forced DSH escalation** (notification 1291 `[MND ask 18d73f5fcb1a]`, update-not-repost verified; drill artifact — dismiss without comment).
  - c) real pass → direction sent in Tomas style ("YAML files… greppable, dependency-free… KISS"), high confidence, 4 citations; **agent acknowledged and proceeded**; ledgered `sent`.
  - d) after completion → natural `pending: none`, ledgered.
  - Scratch worktree + branch removed.

## Found and fixed live

- **stdin-slurp bug**: `docker compose run -T` inside the watch loop ate the agent-list stream — every pane after the first silently dropped. Fix: `handle_pane … </dev/null`.
- **Model-steering gap**: watch had no way to express model preference → `MND_ASK_MODEL`.
- Tail-hash stability verified (statusline shows session-start time, not a clock); busy panes re-hash each pass — cooldown caps that at ~1 ask/min/pane.

## Blocked on Tomas

Restarting LLP from the master checkout (and patching its runtime config with the gemini `--approval-mode default` flag) was blocked by the permission classifier as cross-project shared-runtime scope — correctly. One command when you want it:
`cd /path/to/repo/projects/LLP-llm-proxy && ./watch.sh restart`
(after `cp config.example.yaml config.yaml` + adding `"--approval-mode", "default"` to the gemini command). Until then MND runs on direct CLIs by design.

## Decisions

### Profiles never route through LLP until its gemini impl auto-denies tools
**Date:** 2026-06-12 · **Decided by:** team (security)
**Decision:** `profile` forces `MND_LLP=off`.
**Reasoning:** Exact MND-011 incident vector (300KB prompt, gemini tool use); LLP's gemini command lacks the auto-deny flag (Q1 todo).
**Revisit if:** LLP ships the flag — then profiles can join the chain.

### Watch-mode loop protection is hash-ledger + cooldown, not semantic dedup
**Date:** 2026-06-12 · **Decided by:** team
**Decision:** Answer once per (pane, normalized-tail-hash); repeat-after-sent escalates; 60s/pane cooldown.
**Reasoning:** KISS; exact-tail repeats catch the "send didn't register / agent truly stuck" case. Semantically-rephrased repeats produce new hashes — bounded by cooldown, acceptable v1.
**Revisit if:** real ping-pong observed — then qhash-level dedup on the extracted question.

## T37 closed (2026-06-12, after Tomas restarted LLP)

Tomas restarted LLP from the master checkout with the **capability-ladder config** (his design: tiered impls `gemini`/`claude`/`gemini2`/`claude2`/`gemini3`/`claude3`, most-capable-first `auto` chain, per-tier cooldowns, `--approval-mode default` on all gemini tiers, `gml-analyze` kept on the cheap tier). MND's default switched to a single `model: auto` request — LLP owns the rotation; the client-side chain remains only as the `MND_LLP_CHAIN` escape hatch.

Live result: `[llp:auto] ok` — the ladder tried gemini-3-pro-preview (quota exhausted → 600s tier cooldown, visible in healthz), **claude-fable-5 served**; LLP request log shows agent `mnd`, status ok. Quota-aware best-model selection verified end-to-end.
