# 005 — Planning (Iteration 2: retraining + self/other discrimination)

- **Start:** 2026-06-12
- **End:** 2026-06-12
- **Phase:** Planning

## Problem

The brain must keep learning from Tomas's NEW conversations — but once MND orchestrates agents and pipelines call LLMs, the session stores fill with **agent-generated content masquerading as "user" turns**: MND's own distill/profile prompts (gemini sessions), orchestrator-sent directions (typed into agent panes), GML's analysis prompts. Retraining on those = the brain eating its own output. Tomas: *"you must distinguish between your own conversations that should be ignored and my claude/gemini sessions that can be used."*

## Discrimination design (turn-level, layered)

| Layer | Catches | Mechanism |
|---|---|---|
| Datamark fingerprint | MND distill/profile prompts, GML email prompts — any pipeline prompt using the U+E000 spotlighting defense | user turn contains U+E000 → drop (it's our own injection defense doubling as a self-marker) |
| Pipeline phrase markers | our prompt templates | "OUTPUT SCHEMA:" + known system-prompt openers → drop |
| Send-ledger | orchestrator directions appearing as "user" turns in agent sessions | `orchestrate.sh --send` logs sha256(normalized text) to `brain/sent-ledger.jsonl`; extract drops matches |
| Project-dir exclusions | pipeline working dirs in `~/.gemini/tmp` | configurable list, default: MND-mind-model, GML-gmail-agent |

Interpretation note: discrimination is **turn-level**, not session-level — Tomas's directions typed into agent panes are the highest-value signal and must NOT be dropped with the session. Recorded in ASSUMPTIONS (MND-015).

## Retrain design

- **Processed-moments ledger** (`brain/processed.yaml`): every moment ID sent to distillation is recorded after a successful batch merge — whether or not it yielded insights. Fixes iteration-1's re-sending of silent moments AND gives retrain its increment.
- **`run-task.sh retrain`**: extract → distill (only unprocessed moments) → profile (only when new insights landed; `MND_GEMINI_MODEL` defaults to gemini-2.5-pro for the large profile prompt).
- **`watch.sh`** (GML pattern): daemon looping retrain on an interval (default 24h), logs per run.

## Test requirements (first)

- **T22** user turn containing U+E000 → excluded (claude + gemini parsers/finalize).
- **T23** user turn containing pipeline phrase markers ("OUTPUT SCHEMA:" etc.) → excluded.
- **T24** user turn whose normalized sha256 is in the send-ledger → excluded; ledger read tolerates missing file.
- **T25** gemini project on the exclusion list → no moments from it.
- **T26** distill-prompts skips moment IDs in processed ledger; distill-merge appends the batch's IDs only for successfully merged batches.
- **T27 (live)** `retrain` run 1 distills only unprocessed moments; immediate run 2 reports nothing new (convergence). Run against real session stores.
- **T28 (live)** `orchestrate.sh --send` appends to the ledger; a follow-up extract excludes exactly that text.

## Checklist

- [ ] `internal/exclude` — fingerprints, markers, ledger (T22–T24)
- [ ] wire into extract finalize + gemini walker exclusion list (T25)
- [ ] processed ledger in distill-prompts/distill-merge (T26)
- [ ] `orchestrate.sh` ledger append on send (T28)
- [ ] `run-task.sh retrain` + `watch.sh` daemon (T27)
- [ ] live test, run log, docs, commit

## Safety checklist review (§7)

Secrets: unchanged paths (ledgers store hashes, not content). Input validation: unchanged. Quality: TDD per above; live convergence test mandatory. No new endpoints/SQL/HTML.

## Decisions

### Turn-level discrimination via fingerprints + ledger, not session-level exclusion
**Date:** 2026-06-12
**Phase:** Planning
**Decided by:** Team (interpretation of Tomas's directive)
**Decision:** Drop individual machine-authored turns; keep Tomas's turns wherever they appear — including inside agent-pane sessions.
**Alternatives considered:** Excluding whole sessions by cwd (would discard Tomas's in-pane directions — the best orchestration signal); allowlisting only specific dirs (brittle as projects move).
**Reasoning:** The boundary "who authored this text" is precise at turn level; the U+E000 datamark we already inject is a perfect self-marker.
**Revisit if:** Self-content leaks through (add markers) or Tomas's real turns get dropped (loosen).
