# 012 — Planning (Iteration 4)

- **Start:** 2026-06-12
- **Phase:** Planning
- **Scope:** (A) LLM routing via LLP with highest-models chain; (B) orchestrate watch mode. (C) contradiction resolution deferred to iteration 5 (011-ideation).

## Design

### A. LLP routing (`run-task.sh`)

- New `llp_complete <llp-model> <prompt-file> <response-file>`: register over `~/.llp/control.sock` (token in shell memory only — never env/disk, per the service-secret rule), `POST /v1/chat/completions` with a jq-built body (prompt file → one user message), extract `.choices[0].message.content`.
- `llm()` dispatch order: if `MND_LLP=off` → direct CLI (unchanged). Else if control socket + healthz OK → walk `MND_LLP_CHAIN` (default `gemini/gemini-3-pro-preview claude/claude-fable-5`; `--model claude` flips claude first). Every chain entry is LLP-pinned ⇒ LLP still applies queue/cooldown/usage; MND advances on failure. Chain exhausted or LLP unreachable → direct CLI fallback (never a dead end).
- Direct-CLI claude pin: `claude-opus-4-6` → **`claude-fable-5`** (verified live).
- Profile regeneration keeps `MND_GEMINI_MODEL` pinning (300KB prompts; gemini-3-pro-preview unproven there — config change later, not code).

### B. Watch mode (`orchestrate-watch.sh`)

Loop every `MND_WATCH_INTERVAL` (default 30s):
1. `herdr agent list` → agents with `agent_status` in {`blocked`, `idle`} (set via `MND_WATCH_STATUSES`).
2. Skip: panes whose cwd is this worktree (self), `MND_WATCH_EXCLUDE` panes.
3. Tail-hash dedup against `data/watch-ledger.jsonl` (`{ts,pane,hash,action}`; normalization identical to the sent-ledger): unseen → `orchestrate.sh <pane> --send`; seen with `action:sent` (same question survived our answer) → one DSH escalation (`action:escalated`); seen otherwise → skip.
4. `pending: none` answers (agent asks nothing) → no send, ledger `action:none`.

### Ask schema change

`Answer` gains `pending: "question"|"none"`; the system prompt instructs `none` only when a terminal tail shows no pending question/decision. Missing/junk value coerces to `question` (old behavior; confidence still gates sending). Flows through `ask-parse --json` automatically.

## Tests (written first)

| ID | What | How |
|----|------|-----|
| T35 | `ParseAnswer` pending: `question`/`none` pass; missing or junk → `question` | Go unit |
| T36 | Ask prompt instructs the `pending` field and its tail-only semantics | Go unit |
| T37 | LLP routing live: gemini entry fails fast (cooldown), chain advances, claude-fable-5 serves; request visible in LLP `/admin/requests` as agent `mnd` | live |
| T38 | `MND_LLP=off` and LLP-down both fall back to direct CLI | live |
| T39 | Watch mode live on a scratch hwt agent: question answered + ledgered; same tail second pass → skipped; seen-after-sent → escalates (simulated tail) | live |

## Risks

- LLP's gemini impl lacks `--approval-mode default` (filed Q1 in todo.txt). Until fixed, MND's LLP-gemini calls carry the MND-011 incident-class risk; mitigations: prompts datamarked/framed, gemini currently quota-cooling (claude serves), and the chain can be set claude-first via `MND_LLP_CHAIN`.
- The running LLP instance lives on deleted inodes (todo Q2) — auto-detect + CLI fallback (MND-020) makes its death a non-event for MND.
