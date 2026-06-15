# GML — Iteration 002: Planning

- **Phase:** 1 — Planning
- **Phase lead:** PM
- **Start:** 2026-05-27
- **End:** 2026-05-27

---

## Scope: Iteration 1 — Rule Engine MVP

This plan covers only **Iteration 1** (Mode 1: Rule Engine). Modes 2 and 3 are planned but not scoped here.

---

## PM Checklist

### Auth & Container Setup
- [ ] `Dockerfile` — installs `gws` binary, exposes commands
- [ ] `docker-compose.yml` — mounts credentials file, passes config path
- [ ] `run.sh` — pulls credentials from 1Password, starts container
- [ ] Auth verified: `gws gmail users getProfile` returns Tomas's account

### Config System
- [ ] `rules.yaml` — rule file format defined and documented
- [ ] Rule types supported in v1:
  - [ ] `archive_by_age` — archive read emails older than N days
  - [ ] `archive_by_sender` — archive all from matching sender patterns
  - [ ] `archive_by_label` — archive all with a given label
- [ ] Rule file loaded and validated at startup
- [ ] `--dry-run` flag: show what would be archived, don't act

### Stats Command
- [ ] `gml stats` — prints inbox summary:
  - Total unread count
  - Top 10 senders by message volume
  - Inbox age distribution (0-7d, 7-30d, 30-90d, 90d+)
  - Count by Gmail category (Primary, Social, Promotions, Updates, Forums)
- [ ] Output: human-readable table + optional JSON (`--json` flag)

### Rule Engine Command
- [ ] `gml run [--dry-run]` — applies all rules in config, logs actions
- [ ] Logs each archive action: message-id, sender, subject, rule that triggered
- [ ] Summary at end: N messages archived, N would be archived (dry-run)

### Tests
- [ ] QA-defined acceptance criteria (see below)
- [ ] `--dry-run` against real inbox: produces output, no mutations
- [ ] At least one archive rule fires correctly on known test data

### Documentation
- [ ] `projects/GML-gmail-agent/README.md` — setup, auth, run instructions

---

## QA: Test Requirements (TDD — defined before implementation)

### T1: Auth smoke test
- Container starts, `gws gmail users getProfile` returns email matching `user@example.com`
- **Pass:** profile JSON contains correct email
- **Fail:** any auth error, missing scope, or wrong account

### T2: Stats command — non-empty inbox
- `gml stats` runs against real inbox, produces output with all 4 sections (unread count, top senders, age distribution, categories)
- **Pass:** all sections present, counts are non-negative integers
- **Fail:** empty output, error, or missing section

### T3: Dry-run — no mutations
- `gml run --dry-run` runs with a rule that would match at least one message
- **Pass:** output shows matched messages; no messages are moved/archived when the run completes; second `gml stats` shows identical inbox counts
- **Fail:** any inbox mutation during dry-run

### T4: Archive rule fires
- Config contains `archive_by_age: {days: 365, state: read}` (archive read emails >1 year old)
- `gml run` (no dry-run) executes
- **Pass:** at least one matching message is archived (no longer in inbox); logged in output
- **Fail:** no action taken, or rule fails with error
- **Note:** run T3 first to confirm the rule matches before T4 executes mutations

### T5: Unknown rule type rejected
- Config contains an unrecognized rule type
- **Pass:** tool exits with a clear error message, no rules run
- **Fail:** silent skip or partial execution

### T6: JSON output flag
- `gml stats --json` produces valid JSON
- **Pass:** `jq '.' <<< "$(gml stats --json)"` exits 0
- **Fail:** malformed JSON or non-JSON output

---

## Security Review (pre-implementation)

**Credentials handling:**
- Credentials file must NOT be baked into the Docker image — always injected at runtime
- `gws auth export --unmasked` output must be stored in 1Password, not in the repo
- `run.sh` must read from `op` and pass via env var, same pattern as DSH `run.sh`
- Container must have `--read-only` filesystem where possible (credentials mounted as read-only volume)

**Scope:**
- Gmail scopes requested must be minimal for Mode 1: read + modify (archive = add ARCHIVE label, remove INBOX label)
- Do NOT request send scope in Mode 1 — Mode 2 only needs draft scope, not send
- `gws auth login -s gmail` to select only Gmail scopes

**Risks:**
- OAuth token stored in credentials file — file permissions must be 600
- `archive_by_sender` rule with a wildcard could archive everything — validate rules before running
- Accidental archive is recoverable (Gmail keeps archived mail, accessible via All Mail) — flag this in README

---

## Performance

- `gws --page-all` on a large inbox (10k+ messages) can be slow — default to `--page-limit 5` for stats, document override
- Archive operations are sequential by default — acceptable for MVP; no parallelism needed

---

## Architecture Diagram

See `diagrams/architecture.md`.

---

## Language / Stack Decision

**Go** — consistent with DSH, single binary in container, good subprocess handling for `gws` calls.

No LLM dependency in Mode 1 — the rule engine is pure Go logic calling `gws` subprocesses.

---

## Decisions

### [GML-007] Go as implementation language
**Date:** 2026-05-27 18:30
**Phase:** Planning
**Decided by:** PM + Developer
**Decision:** Implement Mode 1 in Go (single binary)
**Alternatives considered:** Python (easier AI integration later), shell script (minimal effort)
**Reasoning:** Go matches DSH, compiles to a single binary suitable for containers, and subprocess calling of `gws` is straightforward. Mode 2/3 AI work can call Claude API from Go or wrap a Python subprocess — decide when that iteration starts.
**Revisit if:** Claude API Go SDK support is insufficient for Mode 2

### [GML-008] Stats scope limited to what gws supports directly
**Date:** 2026-05-27 18:30
**Phase:** Planning
**Decided by:** PM
**Decision:** Stats use `gws gmail users messages list` with label/query filters; no custom parsing of message bodies
**Alternatives considered:** Full message fetch for richer stats
**Reasoning:** Header-level data (sender, date, labels) is sufficient for inbox stats; fetching bodies is expensive and unnecessary for Mode 1.
**Revisit if:** Body-level stats are requested

### [GML-009] Archive = "remove INBOX label" (not delete, not trash)
**Date:** 2026-05-27 18:30
**Phase:** Planning
**Decided by:** Security
**Decision:** Archive via Gmail's archive mechanism (remove INBOX label). Never trash or delete.
**Alternatives considered:** Trash, delete
**Reasoning:** Archive is reversible; trash is recoverable for 30 days; delete is permanent. Mode 1 must be safe to run autonomously — only reversible operations permitted.
**Revisit if:** Tomas explicitly wants trash/delete rules (requires explicit opt-in flag in config)
