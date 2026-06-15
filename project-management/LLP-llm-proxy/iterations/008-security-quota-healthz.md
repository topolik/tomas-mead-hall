# LLP Iteration 008 — approval-mode hardening, quota-burn fix, healthz serveability

- **Start:** 2026-06-12 22:15
- **End:** 2026-06-12 23:30
- **Phase:** Implementation (todo.txt items 71 Q1 + 72 Q2 + MND's open TerminalQuotaError investigation, bundled — worktree `todo-70`)

## What happened

Three LLP items in one pass:

1. **Q1 SECURITY — `--approval-mode default` (LLP-014).** The gemini impl command now carries
   `--approval-mode default`; without it headless gemini-cli inherits the user-level
   `defaultApprovalMode: auto_edit` and can write files (incident class MND-011).
   **Live regression found during the work:** the 2026-06-12 20:10 restart of the shared
   :4000 instance ran from a fresh worktree, found no `config.yaml`, and regenerated it
   from the then-unfixed `config.example.yaml` — silently reopening the gap the earlier
   manual fix had closed. Verified in gemini-cli 0.29.5 source: in `-p` mode
   `isWorkspaceTrusted()` returns trusted *unconditionally* and `-e none` does not disable
   built-in tools. Fix is layered: flag in the example config + committed
   `.gemini/settings.json` (workspace `defaultApprovalMode: default`, honored because
   `run.sh` starts llp from the project dir — verified live).

2. **TerminalQuotaError / 2–3 min burns (LLP-015).** Root cause of the evening's 167s/241s
   request latencies while gemini quota was exhausted: gemini-cli internally retries
   `RetryableQuotaError` up to **10 attempts** (5s → ×2 backoff capped at 30s ≈ up to ~4 min),
   so every `auto` request ground inside the CLI until LLP's 180s timeout, then failed over —
   and a timeout sets no cooldown, so the next request burned again. Separately, the
   terminal-quota stderr ("You have exhausted your daily quota…", "resets after 3h49m")
   matched **none** of `DefaultRateLimitPatterns`, so even fast quota failures never set a
   cooldown. Fixes: `general.maxAttempts: 1` in the workspace `.gemini/settings.json`
   (fail fast; retries/failover/cooldown are LLP's job), new `QuotaExhausted` error class
   with its own patterns, and per-impl `quota_cooldown` (30m in the example) so a
   known-long quota outage benches the impl properly. This closes MND's open
   "[LLP] TerminalQuotaError/cooldown" todo (in their unmerged worktree todo.txt).

3. **healthz serveability (LLP-016).** The router now tracks per-impl outcomes
   (consecutive failures, last error/ok timestamps); `/healthz` reports per-impl
   `serveable` (+ raw signals) and a top-level `status: ok|degraded` (no serveable impl).
   Always HTTP 200 — DSH's `getJSON` rejects non-200; MND's `llp_up`
   (`jq '.status == "ok"'`) now sees degradation with no client change.

## Changes

- `internal/provider`: `Error.QuotaExhausted`, `DefaultQuotaExhaustedPatterns`
  (`terminalquotaerror`, `daily quota`, `quota will reset`, `resets after`,
  `usage limit reached`), quota-first classification; `retryablequotaerror` +
  `suggested retry after` added to rate-limit patterns. LLP-011's no-bare-keyword rule kept
  (class names are not substrings of the stack-frame filename `googleQuotaErrors.js`).
- `internal/registry`: `quota_cooldown` impl config → `Impl.QuotaCooldown`.
- `internal/router`: quota cooldown selection; per-impl outcome stats + `StatsFor`.
- `internal/server`: healthz rewrite (serveable, consecutive_failures, last_error[_at],
  last_ok_at, degraded status; threshold 3).
- `config.example.yaml`: gemini `--approval-mode default`, `quota_cooldown: 30m`
  (gemini+claude), security comments.
- `.gemini/settings.json` (new, committed): `maxAttempts: 1`,
  `defaultApprovalMode: default`, `mcp.excluded: [jvm-debugger]` (the user-level MCP server
  was being loaded — and failing — on every completion; excluding it cut ~5s startup).
- README: security regression note, fail-fast note, healthz semantics.
- ASSUMPTIONS: LLP-014/015/016 (+ pointer from LLP-008).

## Run log

- `go test ./...` and `go test -race ./...`: **59 pass** (was 52; +7 new: quota classification ×3,
  router quota-cooldown/stats ×3, healthz serveability ×1).
- Workspace-settings pickup proven live: with `.gemini/settings.json` present, the
  user-level `jvm-debugger` MCP discovery error disappeared from gemini stderr (and the
  PONG round-trip dropped ~20s → ~14.5s).
- Side instance :4100 (real CLIs): handshake + `auto` completion → gemini served PONG in
  ~18s **with the approval-mode flag**; healthz shows `last_ok_at`, `serveable: true`.
- Side instance :4101 (both impls faking quota-exhausted stderr): completion → 502
  `quota exhausted`; healthz flips to `status: degraded`, both impls
  `cooling_down: true` (30m quota cooldown), `serveable: false`, `last_error` visible —
  exactly the case item 72 complained about (healthz said "ok" while everything 502'd).
- gemini-3-pro-preview probe: quota had reset (~3h49m window elapsed) — PONG in 16s, so
  the live TerminalQuotaError could not be re-triggered; classification is covered by unit
  tests built from the CLI source's exact message formats instead.

## Ops state — RESOLVED (Tomas approved the restart, 2026-06-13)

Initially the agent was denied restarting the shared :4000 service; Tomas approved it
("a live Q1 gap outranks the disruption; don't ask again — apply and test") and the restart
ran 2026-06-13 00:09. **Runtime verification, not assertion:**

- Startup log clean — no regeneration (`config.yaml` already present), no errors.
- `/proc/<llp-pid>` confirms cwd = the restarter worktree's LLP dir and `--config config.yaml`;
  that file's gemini command carries the flag.
- During a live `auto` completion through :4000 the spawned child was sampled from the
  process table: `npm exec @google/gemini-cli -e none --approval-mode default -p` —
  **the flag is on the real process**. Gemini served PONG in ~20s; `/healthz` ok.
- Side observation reported to Tomas: an unrelated interactive gemini session on the host
  runs with `--yolo` (pid 1478019) — not LLP's, but it bypasses all approval gates.

## Follow-up (same review cycle): regeneration vector closed (LLP-017)

Tomas's ruling: regeneration must **fail or warn loudly** if it would drop the flag.

- `run.sh`: line-level guard — any `command:` line execing gemini-cli without
  `--approval-mode` on that line ⇒ FATAL before build. (A file-level grep was tried first
  and proved fooled by the flag appearing in *comments* — caught during guard testing.)
- `registry.Build`: validates the parsed argv of every cli impl — covers exotic YAML
  layouts and launches that bypass `run.sh`. + `TestBuildRejectsGeminiWithoutApprovalMode`.
- Verified against the real pre-fix example from git history (`git show HEAD~1:...`):
  FATAL. Good config still builds. **60 tests pass (race-clean).**
- The `tc-global-restarter` worktree's runtime copies patched out-of-band (Tomas-approved):
  fixed `config.example.yaml`, guarded `run.sh`, `.gemini/settings.json` — so the next
  fleet restart is safe even before this branch merges. Contents are identical to this
  branch's files, so the eventual master merge into that worktree is a no-op.
- Not merged/pushed — iteration 008 waits for Tomas's review per MO.

## Decisions

### Layered fix for approval-mode (flag + workspace settings)
**Date:** 2026-06-12 22:50
**Phase:** Implementation
**Decided by:** todo-70 agent
**Decision:** Carry `--approval-mode default` in the impl command AND commit a workspace `.gemini/settings.json` with `defaultApprovalMode: default`.
**Alternatives considered:** Flag only (lost once already via config regeneration); editing the user-level `~/.gemini/settings.json` (would change Tomas's interactive gemini behavior — rejected).
**Reasoning:** The regeneration regression proved a single point of failure; the workspace file survives config.yaml regeneration and rides along in git.
**Revisit if:** gemini-cli changes workspace-settings resolution for headless mode.

### Disable gemini-cli internal retry rather than lengthen LLP timeout
**Date:** 2026-06-12 22:55
**Phase:** Implementation
**Decided by:** todo-70 agent
**Decision:** `general.maxAttempts: 1` for LLP's invocations only.
**Alternatives considered:** Keep 10 attempts + raise LLP timeout (makes burns worse); maxAttempts 2 (still 5s+ penalty per failed request for little gain); parsing "resets after Xs" into dynamic cooldown (over-engineering now).
**Reasoning:** A proxy that owns failover/cooldown gets nothing from an inner retry ladder except multi-minute blind spots. Fail fast → classify → bench via quota_cooldown.
**Revisit if:** transient gemini network blips become noisy — bump to 2 before re-enabling backoff.

### Startup guard at two layers (shell line-grep + Go argv validation)
**Date:** 2026-06-13 00:15
**Phase:** Implementation
**Decided by:** Tomas (requirement) / todo-70 agent (design)
**Decision:** Enforce "gemini-cli command must carry an explicit `--approval-mode`" in both `run.sh` (line-level grep, FATAL) and `registry.Build` (parsed argv, error).
**Alternatives considered:** Shell-only (doesn't cover direct `./llp --config` launches, and old binaries elsewhere need the shell layer anyway — so both); file-level grep (fooled by the flag in comments — rejected after testing); checksum-pinning the example (blocks legitimate edits).
**Reasoning:** The invariant is "mode chosen in config, never inherited" — an impl that deliberately wants tool use declares `--approval-mode <mode>` explicitly and passes. Two layers cover both the old-binary deployment window and every launch path.
**Revisit if:** config moves to multi-line command arrays (shell grep misses them; Go guard still catches).

### healthz: passive signal, body-only degradation, threshold 3
**Date:** 2026-06-12 23:05
**Phase:** Implementation
**Decided by:** todo-70 agent
**Decision:** Track outcomes passively; report `serveable` per impl and `degraded` top-level; keep HTTP 200 always.
**Alternatives considered:** Active probe per healthz hit (burns quota, adds latency); HTTP 503 on degraded (breaks DSH's `getJSON` non-200 check); threshold 1 (flaps on a single blip).
**Reasoning:** Passive is free and truthful after the first real failure; MND's existing `jq '.status == "ok"'` benefits with zero client change.
**Revisit if:** pre-traffic liveness is needed — opt-in `?probe=1`.
