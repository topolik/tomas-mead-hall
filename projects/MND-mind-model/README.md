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
./run-task.sh ask --json "..."        # machine-readable: {"answer", "confidence", "citations"}
./run-task.sh pipeline [--limit N]    # extract + distill + profile
```

All commands accept `--model gemini|claude` (default: gemini). Go steps run in Docker; LLM calls run host-side via `npx @google/gemini-cli` / `claude -p` with prompt files (GML pattern).

`distill` is incremental: moments in the processed ledger (`data/processed.yaml`) or already cited as evidence are skipped.

## Retraining (continuous learning)

```bash
./run-task.sh retrain          # learn from NEW conversations only; regen profiles if the brain changed
./run-task.sh watch-retrain    # daemon — MND_RETRAIN_INTERVAL seconds between runs (default 86400)
```

Retraining **never learns from the agent team's own output** (turn-level discrimination):
- pipeline prompts carry the U+E000 datamark and known template phrases → dropped (`self=N` in extract stats)
- directions the orchestrator sent into agent panes are hashed in `data/sent-ledger.jsonl` → dropped
- pipeline working dirs in `~/.gemini/tmp` (MND, GML) are excluded wholesale (`--exclude-gemini`)
- Tomas's own turns survive everywhere — including what he types into agent panes

Live-verified 2026-06-12: three consecutive `retrain` runs — 12 new insights from the day's real conversations, then convergence ("nothing new to distill") with the pipeline's own session residue continuously excluded.

## Feedback loop (DSH)

When the orchestrator can't answer (`confidence: low`), it escalates instead of guessing:

```
orchestrate.sh (confidence: low)
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

Returns `{"answer": "...", "confidence": "high|medium|low", "citations": ["insight-id", ...]}`. Citations resolve in `data/insights.yaml` — each carries the session evidence that backs the direction. Treat `confidence: low` as "mind model is silent — ask the human."

## Orchestrating herdr agents

```bash
herdr agent list                                  # find the agent waiting for direction
./orchestrate.sh <pane-or-terminal-id>            # dry-run: print the direction Tomas would give
./orchestrate.sh <pane-or-terminal-id> --send     # deliver it into the agent's pane
```

`orchestrate.sh` reads the tail of the agent's terminal, extracts its pending question, asks the mind model, and (with `--send`) types the direction into the pane. It **refuses to send on `confidence: low`** — that's the "ask the real Tomas" signal. Live-tested 2026-06-12: a scratch agent asked "database or flat files for execution logs?", the brain answered "flat files, KISS, no unnecessary dependencies" with 4 evidence citations, and the agent proceeded on that direction.

Works with `hwt`-spawned agents out of the box (`hwt <branch>` → agent pane → `orchestrate.sh <pane>`).
