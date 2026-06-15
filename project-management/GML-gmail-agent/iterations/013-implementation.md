# GML — Iteration 013: Knowledge File & Distillation Implementation

- **Phase:** 2 — Implementation
- **Phase lead:** Developer
- **Start:** 2026-05-30

---

## What Was Built

### Schema Changes
- Added `gmail_search` field to `InsightAnalysis` struct (required, validated)
- Updated LLM prompt in `history.go` to include `gmail_search` in output schema with Gmail operator examples
- Updated `InsightsToNotifications()` to use LLM-provided `gmail_search` for link instead of deriving from `affected_senders`
- Added knowledge context section to `BuildHistory()` prompt (new 5th parameter)
- Added knowledge-aware instructions to LLM prompt (respect confirmed/rejected/refined patterns)

### New Packages
- `internal/knowledge/` — Knowledge file CRUD
  - `KnowledgeFile` struct with `last_distilled_at` and `Patterns` array
  - `Pattern` struct: gmail_search (key), pattern, status, category, senders, first_seen, last_updated, comment_summary, refined_action
  - `Load(path)` / `Save(path)` — YAML read/write, handles nonexistent file gracefully
  - `Upsert()` — deduplicates by gmail_search, preserves first_seen on update
  - `Format()` — human-readable output for LLM prompt context
  - `ValidatePattern()` — status validation, refined requires refined_action
  - 8 unit tests

### New Distillation Pipeline
- `internal/notify/distill.go` — `DistillResult` struct with `Patterns` + `Todos` arrays
  - `DistilledPattern` for knowledge entries, `DistilledTodo` for action items
  - `ParseAndValidateDistilled()` — handles two-array format and legacy flat array
  - `DistilledToKnowledge()` — converts patterns to knowledge entries
  - Reuses `stripCodeFence` (updated to handle both `[...]` and `{...}` formats)
  - 12 unit tests

- `internal/prompt/distill.go` — `BuildDistill()` prompt builder
  - System prompt distinguishes pattern feedback from action items
  - LLM can emit both a pattern and a todo from a single comment
  - Datamarked dismissed insights section, existing knowledge section
  - 3 unit tests

### DSH Todo API (cross-project: DSH-dashboard)
- `POST /api/v1/todos` — creates todo via `todoreader.Add()`, JWT auth
  - Supports `text`, `priority` (Q1-Q4, default Q2), `project_code` (prefixed to text)
- `GET /api/v1/todos` — lists all todos, optional `status` and `priority` filters
- `APIHandler` extended with `TodoPath` field
- GML's `DSHClient` extended with `PostTodo()` method
- 4 integration tests: create+list, requires text, requires auth, default priority

### New Commands
- `gml distill-gather` — fetches dismissed `[Insight:]` notifications with comments from DSH, loads existing knowledge.yaml, builds distillation prompt (stdout)
- `gml distill-apply` — reads LLM distillation JSON (stdin), validates, splits: patterns → knowledge.yaml, todos → DSH todo API

### Shell
- `run.sh distill [--model gemini|claude]` — 3-step pipeline: distill-gather → LLM → distill-apply
  - Mounts `knowledge.yaml` into container for read/write
- `run.sh learn` updated to mount `knowledge.yaml` (read-only) so `gml history` includes knowledge context

### Blocker Fix: Link in Dismissed Notifications
- `FormatDismissedNotifications()` now includes `Link:` line for each notification
- Distill prompt updated: LLM instructed to URL-decode the Link as authoritative `gmail_search` source
- `stripCodeFence` updated to handle both JSON arrays `[...]` and objects `{...}`
- 3 new `dsh_test.go` tests: link included, no-link skipped, empty input

---

## Run Log

### Test Results
- knowledge: 8/8 pass
- notify: 44 tests pass (28 base + 11 distill + 3 dsh + 2 insight additions)
- prompt: 6 tests pass (3 history + 3 distill)
- sanitize: 31 pass
- behavior: 5 pass
- Full GML test suite: 94 total, all pass
- DSH integration tests: 4 new todo tests, all pass (full DSH suite passes)
- Docker image builds cleanly.

### Known Limitations
- Todo posting has no idempotency: if `distill-apply` fails mid-loop (e.g. network error), re-running duplicates already-posted todos.

### Pending Live Test
Live test requires:
1. Existing insight notifications with comments in DSH (from iter 012 live test — 10 insights posted)
2. Tomas to dismiss some with comments via DSH web UI
3. Run `./run.sh distill` to produce `knowledge.yaml` + post action-item todos to DSH
4. Run `./run.sh learn --days 7` to verify knowledge appears in prompt
5. Check DSH todo page for any action items created by distill

---

## Test Requirements Status

### Offline
- [x] `insights.go`: `gmail_search` required — validation fails when empty
- [x] `insights.go`: valid insight with `gmail_search` passes
- [x] `InsightsToNotifications`: link uses LLM-provided `gmail_search`
- [x] `knowledge/`: Load/Save roundtrip preserves all fields
- [x] `knowledge/`: Upsert by `gmail_search` — new pattern added, existing updated
- [x] `knowledge/`: Status transitions (confirmed → refined, etc.)
- [x] `knowledge/`: Validate pattern (status, refined_action)
- [x] `prompt/distill.go`: distill prompt sections present, datamarked
- [x] `prompt/distill.go`: empty data handled
- [x] `prompt/distill.go`: action item instructions present
- [x] `prompt/history.go`: knowledge section present in output
- [x] `prompt/history.go`: empty knowledge handled
- [x] `notify/distill.go`: valid distilled JSON parses (two-array format)
- [x] `notify/distill.go`: refined without action fails
- [x] `notify/distill.go`: invalid status fails
- [x] `notify/distill.go`: code fence handling
- [x] `notify/distill.go`: DistilledToKnowledge mapping
- [x] `notify/distill.go`: mixed patterns + todos
- [x] `notify/distill.go`: todo validation (missing text, invalid priority)
- [x] ~~`notify/distill.go`: legacy flat array backward compat~~ (removed — prefer hard failure for prompt regression)
- [x] `dsh.go`: FormatDismissedNotifications includes Link line
- [x] `dsh.go`: FormatDismissedNotifications skips Link when empty
- [x] `dsh.go`: FormatDismissedNotifications handles empty input
- [x] DSH: POST /api/v1/todos creates item, GET /api/v1/todos lists it
- [x] DSH: POST /api/v1/todos rejects empty text
- [x] DSH: POST /api/v1/todos requires JWT auth
- [x] DSH: POST /api/v1/todos defaults to Q2

### Live
- [x] Full loop: dismiss insight with comment → `run.sh distill` → `knowledge.yaml` created/updated + todos posted → next `run.sh learn` prompt includes knowledge
