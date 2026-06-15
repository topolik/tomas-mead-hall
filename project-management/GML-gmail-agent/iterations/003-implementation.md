# GML — Iteration 003: Implementation

- **Phase:** 2 — Implementation
- **Phase lead:** Developer
- **Start:** 2026-05-27
- **End:** 2026-05-27

---

## What Was Built

Mode 1 Rule Engine — complete.

### Files created

```
projects/GML-gmail-agent/
├── Dockerfile                    # Multi-stage: Go 1.25 builder + Node/gws installer + debian runtime
├── docker-compose.yml            # Mounts rules.yaml + credentials, passes env vars
├── run.sh                        # Pulls credentials from 1Password, runs container
├── rules.yaml                    # Example rules (archive_by_age and commented examples)
├── README.md                     # Setup, auth, commands, security notes
├── go.mod / go.sum               # Go module
├── cmd/gml/main.go               # CLI entrypoint: profile, stats, run commands
└── internal/
    ├── config/config.go          # YAML rule loading + validation
    ├── gws/gws.go                # gws subprocess wrapper + Gmail types
    ├── rules/engine.go           # Rule engine: query builder + post-filter + archive
    └── stats/stats.go            # Inbox stats collector
```

### Commands implemented
- `gml profile` — auth smoke test: prints email + message count
- `gml stats [--json] [--pages N]` — inbox stats: unread, age distribution, categories, top 10 senders
- `gml run [--dry-run] [--pages N]` — applies all rules from rules.yaml

### Rule types implemented
- `archive_by_age` — params: `days`, `state` (read|unread|any)
- `archive_by_sender` — params: `patterns[]` (email substring or regex)
- `archive_by_label` — params: `label`

---

## Run Log

### Build
```
go build ./...         ✓ clean
go vet ./...           ✓ no warnings
docker build           ✓ first attempt failed (golang:1.23 doesn't support go 1.25.9 module); fixed → golang:1.25-bookworm
docker build (retry)   ✓ all 3 stages built cleanly
```

### Container validation
```
docker run gml-test           → usage printed, exit 0  ✓
docker run gml-test foo       → "unknown command foo", exit 1  ✓
docker run gml-test run --dry-run (bad rules) → validation error, exit 1  ✓
```

### T5 (unknown rule type rejected)
- Config with `type: unknown_type` → clear error message, no rules run ✓

### Auth-dependent tests (T1–T4, T6)
Cannot run without credentials. These must be run by Tomas after completing the auth setup (gws auth setup + gws auth login + store in 1Password).

---

## Bugs Found and Fixed

1. **Dockerfile used golang:1.23** — go.mod requires 1.25.9. Fixed: changed to `golang:1.25-bookworm`.
2. **Dockerfile CMD ["--help"]** — binary doesn't handle `--help` flag, printed "unknown command --help". Fixed: removed CMD, binary prints usage on no args and exits 0.
3. **gws NDJSON pagination parsing** — `--page-limit` outputs one JSON object per page as NDJSON, not a single array. Fixed: parse line by line, fall back to single-object parse if no newlines.

---

## Deferred Items

- T1–T4, T6: require live credentials — blocked until Tomas completes auth setup
- `gws auth setup` and headless export flow: documented in README, not automated
- Parallel archive operations: sequential is fine for MVP
- `--page-all` support: present in gws.go but default is 5 pages to keep stats fast

---

## Decisions

### [GML-010] gws NDJSON parsing strategy
**Date:** 2026-05-27 18:45
**Phase:** Implementation
**Decided by:** Developer
**Decision:** Parse gws paginated output line by line (NDJSON), fall back to single-object JSON if no newlines
**Alternatives considered:** Always parse as NDJSON; always re-aggregate via --page-all
**Reasoning:** gws outputs either one JSON object (no pagination) or one per page (NDJSON). Both formats must be handled. The fallback covers the common single-page case.
**Revisit if:** gws changes its output format

### [GML-011] Sender pattern matching: Gmail query first, regex post-filter
**Date:** 2026-05-27 18:45
**Phase:** Implementation
**Decided by:** Developer
**Decision:** Simple email patterns use Gmail `from:` query syntax; regex patterns fall back to fetching all inbox and filtering client-side
**Alternatives considered:** Always fetch all and filter client-side
**Reasoning:** Gmail query-level filtering is faster for simple cases (common case). Regex post-filter is a fallback for power users only.
**Revisit if:** Client-side filtering becomes a performance problem on large inboxes
