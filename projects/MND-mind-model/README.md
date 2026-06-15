# MND — Mind Model

Distills Tomas's decision-making "brain" from Claude + Gemini session history and answers agent questions the way Tomas would — so herdr agents get directions without interrupting him.

```
sessions (ro) → extract → moments.jsonl → distill (LLM) → data/insights.yaml
                                                  → profile (LLM) → data/profiles/*.md
agent question → ask (BM25 retrieve + LLM) → Tomas-style direction + evidence citations
```

- **`data/profiles/*.md`** — the readable core: decision-making, technical preferences, direction style. Every line cites insight IDs.
- **`data/insights.yaml`** — the evidence base: structured insights, each tied to real session moments (session ID, timestamp, quote).
- **`data/`** — gitignored: raw extracted moments, prompt/response files. Raw session content never gets committed.

Sessions are mounted **read-only**; tool results are skipped at parse time and secret shapes are redacted before anything is written to disk or sent to an LLM.

## How to run

```bash
./run-task.sh extract                 # mine ~/.claude/projects + ~/.gemini/tmp → data/moments.jsonl
./run-task.sh stats                   # corpus statistics
./run-task.sh distill [--limit N]     # moments → data/insights.yaml (N batches per run; re-run to continue)
./run-task.sh profile                 # insights → data/profiles/*.md
./run-task.sh ask "Should the new agent service use Postgres or a flat file?"
./run-task.sh ask --json "..."        # machine-readable: {"answer", "confidence", "citations", "pending"}
./run-task.sh pipeline [--limit N]    # extract + distill + profile
./run-task.sh retrain                 # learn + regen + mandatory fidelity eval + threshold check
./run-task.sh classify --question "..." # classify a question's category (for routing)
./run-task.sh eval                    # fidelity eval: sample → blind clone answer → judge → report
./run-task.sh route-eval              # sweep routing policies: coverage vs fidelity frontier
./run-task.sh contradictions          # loop-until-dry contradiction sweep
./run-task.sh learn                   # ingest DSH feedback → corrective insights
```

All commands accept `--model gemini|claude` (default: gemini). LLM calls route through **LLP** when it's up (auto-detected); direct-CLI fallback when LLP is down. `MND_LLP=off` to opt out.

`distill` is incremental: moments in the processed ledger (`data/processed.yaml`) or already cited as evidence are skipped.

## Retraining (continuous learning)

```bash
./run-task.sh retrain          # learn + regen + mandatory fidelity eval + threshold check
./run-task.sh watch-retrain    # daemon — MND_RETRAIN_INTERVAL seconds between runs (default 86400)
```

Every `retrain` run:
1. Extract new moments, distill, learn from DSH feedback, contradiction sweep
2. Regenerate profiles if the brain changed
3. **Mandatory fidelity eval** — `eval` + `route-eval` against the updated brain
4. **Threshold check** — if auto-set delivered fidelity drops below `MND_FIDELITY_MIN_AUTO` (default **75%**), or judgment questions leak into auto-answer, a **DSH `action_needed` alert fires** (Q1 priority)

| Env var | Default | Effect |
|---|---|---|
| `MND_FIDELITY_MIN_AUTO` | `75` | Minimum fidelity (%) for auto-answered categories |
| `MND_ROUTE_AUTO` | `correction_pattern,direction_pattern` | Categories checked against the threshold |

Retraining **never learns from the agent team's own output** (turn-level discrimination):
- pipeline prompts carry the U+E000 datamark and known template phrases → dropped (`self=N` in extract stats)
- directions the orchestrator sent into agent panes are hashed in `data/sent-ledger.jsonl` → dropped
- pipeline working dirs in `~/.gemini/tmp` (MND, GML) are excluded wholesale (`--exclude-gemini`)
- Tomas's own turns survive everywhere — including what he types into agent panes

## Feedback loop (DSH)

When the competence gate classifies a question outside the auto-answer set, the orchestrator escalates instead of guessing:

```
orchestrate.sh (competence gate: category not in MND_ROUTE_AUTO)
  → DSH notification [action_needed/Q1]: agent's question + brain's best guess
  → you dismiss it WITH A COMMENT containing your direction
  → ./run-task.sh learn  (also runs inside retrain)
  → comment becomes a corrective insight (strength: strong, source: feedback)
```

Setup: run `./setup.sh` — it auto-provisions a DSH OAuth2 client and writes `data/dsh.yaml`. Or manually: `cp dsh.yaml.example data/dsh.yaml`, fill credentials from DSH `/admin/clients`, `chmod 600`. Without `data/dsh.yaml`, escalation and learn steps skip gracefully.

Feedback insights carry `source: feedback` and evidence `dsh:<notification-id>` with your comment as the quote — they outrank distilled inference by design (a direct correction from Tomas is definitionally authoritative).

## Backup & Restore

```sh
./backup.sh                # encrypted backup of data/ (insights, profiles, ledgers, dsh.yaml)
./restore.sh <file.enc>    # restore from backup
```

Passphrase via `$BACKUP_PASSPHRASE` or interactive prompt. Backups go to `~/.local/share/mnd/backups/` by default. Data is not committed to git — use `backup.sh` to persist after retraining.

## How to test

```bash
go test ./...        # unit tests — parsers, redaction, batching, validation, merge, BM25, prompts
```

Live verification (real data, no synthetic fixtures):

```bash
./run-task.sh extract && ./run-task.sh stats
grep -c -E 'ghp_[A-Za-z0-9]{20}|sk-[A-Za-z0-9_-]{16}|AKIA[0-9A-Z]{16}' data/moments.jsonl   # must be 0
./run-task.sh distill --limit 1 && head -40 data/insights.yaml
```

## For agents (herdr integration)

```bash
./run-task.sh ask --json "<question you would ask Tomas>"
```

Returns `{"answer": "...", "confidence": "high|medium|low", "pending": "question|none", "citations": ["insight-id", ...]}`. Citations resolve in `data/insights.yaml` — each carries the session evidence that backs the direction. `pending: none` means the agent isn't actually asking anything. Note: confidence is **not reliable for routing** (uniformly `high` in eval) — the competence gate uses question category instead.

## Orchestrating herdr agents

```bash
herdr agent list                                  # find the agent waiting for direction
./orchestrate.sh <pane-or-terminal-id>            # dry-run: print the direction Tomas would give
./orchestrate.sh <pane-or-terminal-id> --send     # deliver it into the agent's pane
```

`orchestrate.sh` reads the tail of the agent's terminal, extracts its pending question, and runs a **two-stage safety gate**:

1. **Competence gate** — classifies the question's category via a cheap LLM call. Categories in `MND_ROUTE_AUTO` (default: `correction_pattern,direction_pattern`) proceed to answer; all others escalate to Tomas via DSH. This replaced the broken confidence-based gate (confidence was uniformly `high` while 41% wrong).
2. **Answer + deliver** — asks the mind model, delivers direction into the pane (with `--send`).

| Env var | Default | Effect |
|---|---|---|
| `MND_ROUTE_AUTO` | `correction_pattern,direction_pattern` | Categories safe to auto-answer (78% fidelity, 0 judgment leaks) |
| `MND_ROUTE` | `on` | Set `off` for legacy answer-everything behavior |

Works with `hwt`-spawned agents out of the box (`hwt <branch>` → agent pane → `orchestrate.sh <pane>`).
