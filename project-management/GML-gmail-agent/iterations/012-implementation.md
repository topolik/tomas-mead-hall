# GML — Iteration 012: Mode 3 Stage A Implementation

- **Phase:** 2 — Implementation
- **Phase lead:** Developer
- **Start:** 2026-05-29

---

## What Was Built

### DSH Changes
- Enhanced `GET /api/v1/notifications` with `include_dismissed=true` and `has_comment=true` query parameters
- When `include_dismissed=true`: returns all notifications (active + dismissed) with `dismissed_at` in response as nullable field
- Backward compatible: default behavior unchanged, `dismissed_at` omitted from response when not requested
- 3 new integration tests: IncludeDismissed, HasComment, BackwardCompat

### GML Changes

**New packages:**
- `internal/behavior/` — Sender behavior aggregation via `GmailClient` interface
  - `CollectSenderBehavior()`: scans inbox, aggregates per-sender read rate, thread count, reply rate
  - Reply detection via `GetThread()` — checks for SENT label in thread messages
  - 5 unit tests (basic stats, min-emails filter, top-N limit, empty inbox, extractEmail)

- `internal/notify/insights.go` — Insight validation and notification mapping
  - `InsightAnalysis` struct (pattern, evidence, signal_strength, category, affected_senders, suggested_action)
  - `ParseAndValidateInsights()` with strict schema validation (reuses `stripCodeFence`)
  - `InsightsToNotifications()` maps insights to DSH notifications with `[Insight: category]` prefix
  - 10 unit tests (valid, code fence, preamble, invalid fields, empty input, notification mapping)

- `internal/prompt/history.go` — Learning prompt builder
  - `BuildHistory()` constructs LLM prompt with sender behavior table, dismissed notifications (datamarked), active rules, previous insights
  - 3 unit tests (with data, empty data, datamarking)

**Modified packages:**
- `internal/gws/gws.go` — Added `ListThreads()`, `GetThread()`, ThreadRef, ThreadListResult, Thread types
- `internal/notify/dsh.go` — Extended `PreviousNotification` struct (added Priority, Link, Comment, DismissedAt), added `GetDismissedNotifications()`, added `FormatDismissedNotifications()`
- `internal/config/config.go` — Added `LearnConfig` (days, max_days, top_senders, min_emails) with defaults

**New commands:**
- `gml history [--days N]` — collects behavioral data, builds LLM prompt (stdout). Needs credentials.
- `gml insights` — validates LLM insight JSON (stdin), posts to DSH. No credentials needed.

**Shell:**
- `run.sh learn [--days N] [--model gemini|claude]` — full pipeline: history → LLM → insights

---

## Run Log

### Test Results
- DSH: 3/3 new integration tests pass. Full suite (except pre-existing auth test failure) passes.
- GML: 18 new tests + 30 existing sanitize tests = 48 total, all pass.
- Docker image builds cleanly.

### Live Test (real Gmail data, real DSH)
```
$ cat token | docker compose run --rm -T gml history --days 7
collecting behavioral data: 7-day window, top 30 senders (min 3 emails)...
  found 16 senders above threshold
  included 84 dismissed notifications with comments

$ gemini-cli < prompt > response

$ cat response | docker compose run --rm -T gml insights
  posted: [info] 🔵 Q3 [Insight: ignore_pattern] Unread security alerts ignored as SOC handles them
  posted: [info] 🔵 Q3 [Insight: ignore_pattern] Neglected SOC group emails due to delegation
  posted: [info] 🔵 Q3 [Insight: archive_candidate] Low engagement with Atlassian notifications
  ... (10 total insights posted)
done: 10 insights posted to DSH
```

Pipeline works end-to-end: behavioral data collection → Gemini analysis → validated insight posting.

---

## Test Requirements Status

### Offline
- [x] DSH: `include_dismissed` returns dismissed notifications with `dismissed_at` field
- [x] DSH: `has_comment` filters to notifications with non-empty comments
- [x] DSH: default behavior unchanged (backward compat)
- [x] GML: insight JSON validation (valid, invalid signal_strength, missing fields, code fences)
- [x] GML: InsightsToNotifications mapping (message format, priority, [Insight:] prefix)
- [x] GML: behavior aggregation with mock GmailClient (read rate, reply rate, top-N, min-emails filter)
- [x] GML: history prompt structure (sections present, comments datamarked, empty data handling)

### Live
- [x] gws thread list/get works via Docker container (implicitly tested via `gml history`)
- [x] DSH dismissed notifications round-trip (84 dismissed notifications with comments fetched)
- [x] Full pipeline: history → Gemini → insights → DSH (10 insights posted)
