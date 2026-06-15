# Iteration 017 — Unified daemon manager with CLI interval control

**Started:** 2026-06-02
**Phase:** Implementation

## Goal

Replace the broken `watch.sh` (called nonexistent `run.sh watch`) with a unified daemon manager for all three GML pipelines. Move interval control from YAML config to CLI flags. Add persistent logging.

## What was built

### `watch.sh` — full rewrite
- Manages all 3 daemons: `analysis`, `knowledge`, `rules`
- Subcommands: `status`, `start`, `stop`, `restart`, `logs`, `attach`
- Default target is "all" for start/stop/restart, specific daemon required for logs/attach
- tmux sessions as state source of truth (`tmux has-session`)
- `status` shows running/stopped, PID, and log file size

### `--interval N` — CLI interval control
- All three daemons accept `--interval N` (minutes) on the command line
- Precedence: CLI flag > YAML config > code default (5 min)
- `run-task.sh watch-analysis/watch-knowledge`: `--interval` parsed in bash, overrides `read_config`
- `run-task.sh watch-rules`: `--interval` forwarded to Go binary
- Go scheduler (`scheduler.Run`) accepts `intervalOverride` parameter
- Interval config keys removed from `rules.yaml` (local-only change, skip-worktree)

### Persistent logging
- Daemon output tee'd to `~/.local/share/gml/<daemon>.log`
- Log rotation on each start: current log renamed to `<daemon>-<start>--<end>.log` (ISO 8601)
- Start timestamp written as `# started YYYYMMDDTHHMMSS` marker in first line
- `logs <daemon>` reads from file (works after session death)
- `logs <daemon> --follow` does `tail -f`

### `run.sh` → `run-task.sh` rename
- Clarifies role: `run-task.sh` runs single tasks, `watch.sh` manages daemon lifecycle
- All references updated across README, setup.sh, docker-compose.yml, Go source, watch.sh
- `watch-*` commands remain in `run-task.sh` (they are valid tasks)

## Default interval change

All daemon defaults changed from 60/360/360 minutes to **5 minutes**:
- `watch-analysis`: 60 → 5
- `watch-knowledge`: 360 → 5
- `watch-rules`: 360 → 5

## Run log

```
$ go test ./...
All 107 tests pass (no new tests — management layer is bash)

$ docker compose build
Image rebuilt with new Go binary (--interval support in scheduler)

$ ./watch.sh start rules
  rules: starting...
  rules: started (log: ~/.local/share/gml/rules.log)

$ ./watch.sh logs rules
# started 20260602T213035
[scheduler] starting — interval 5 min, rules: 2

$ ./watch.sh restart rules
  rules: stopped
  rules: starting...
  rules: started

$ ls ~/.local/share/gml/
rules-20260602T213035--20260602T213039.log   # rotated with start--end
rules.log                                     # current run
```

## Decisions

### CLI-first interval control
**Date:** 2026-06-02
**Decided by:** Tomas
**Decision:** Daemon intervals are controlled via `--interval N` CLI flag, not YAML config.
**Alternatives considered:** Keep intervals in rules.yaml (status quo), environment variables
**Reasoning:** watch.sh is the operational entry point — intervals belong where you start daemons, not buried in config. YAML fallback preserved for backwards compatibility but config keys removed from rules.yaml.

### 5-minute default for all daemons
**Date:** 2026-06-02
**Decided by:** Tomas
**Decision:** All three daemons default to 5-minute intervals when no --interval flag or config is provided.
**Alternatives considered:** Keep previous defaults (60/360/360), different defaults per daemon
**Reasoning:** Fast feedback loop during active development and use.
**Revisit if:** LLM API costs become a concern at high frequency.

### Log rotation with start+end timestamps
**Date:** 2026-06-02
**Decided by:** Tomas
**Decision:** Rotated logs named `<daemon>-<start>--<end>.log` using ISO 8601 format (YYYYMMDDTHHMMSS).
**Alternatives considered:** Truncate on start, rotate with single timestamp, keep N files
**Reasoning:** Both timestamps needed to identify the run window. ISO 8601 with `--` separator is standard for time intervals.

### run.sh renamed to run-task.sh
**Date:** 2026-06-02
**Decided by:** Tomas
**Decision:** Rename run.sh to run-task.sh to clarify it runs single tasks.
**Alternatives considered:** gml.sh, task.sh
**Reasoning:** "run-task" explicitly communicates single-execution purpose vs watch.sh's daemon management role.
