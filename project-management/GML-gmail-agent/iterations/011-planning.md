# GML — Iteration 011: Mode 3 Stage A Planning

- **Phase:** 1 — Planning
- **Phase lead:** Analyst
- **Start:** 2026-05-29

---

## Scope

Build the behavioral learning pipeline (Stage A): collect signals from DSH notification history and Gmail thread analysis, feed them to an LLM, post behavioral insights as DSH notifications.

**Not in scope:** Rule proposals (Stage B), new rule engine types, rule approval workflow.

---

## Implementation Plan

### Step 1: DSH — Expose dismissed notifications (gating dependency)

Add `include_dismissed=true` and `has_comment=true` query params to `GET /api/v1/notifications`. When `include_dismissed=true`, return all notifications (active + dismissed) with `dismissed_at` in response. Backward compatible — default behavior unchanged.

**Files:** `DSH-dashboard/internal/handler/api.go`, `DSH-dashboard/cmd/dsh/integration_test.go`

### Step 2: GML — Gmail thread operations

Add `ListThreads()` and `GetThread()` to `internal/gws/gws.go`. These follow the existing pattern (exec gws subprocess, parse JSON). `GetThread` returns all messages in a thread with their labels — used to detect replies (SENT label present = Tomas replied).

**Files:** `GML-gmail-agent/internal/gws/gws.go`

### Step 3: GML — Enhanced DSH client

Extend `PreviousNotification` struct with Comment, Priority, Link, DismissedAt fields. Add `GetDismissedNotifications()` method calling the new DSH endpoint.

**Files:** `GML-gmail-agent/internal/notify/dsh.go`

### Step 4: GML — Behavior aggregation package

New `internal/behavior/` package. Defines `GmailClient` interface (for testability), `SenderBehavior` struct (email, counts, read rate, reply rate), `BehaviorSummary` struct. `CollectSenderBehavior()` scans top 30 senders in window, checks thread states.

**Files:** `GML-gmail-agent/internal/behavior/behavior.go`, `behavior_test.go`

### Step 5: GML — Insight validation

New `internal/notify/insights.go`. `InsightAnalysis` struct (pattern, evidence, signal_strength, category, affected_senders, suggested_action). `ParseAndValidateInsights()` with strict schema. `InsightsToNotifications()` maps to DSH notifications prefixed with `[Insight: category]`.

**Files:** `GML-gmail-agent/internal/notify/insights.go`, `insights_test.go`

### Step 6: GML — History prompt builder

New `internal/prompt/history.go`. `BuildHistory()` constructs LLM prompt with sender behavior table, dismissed notifications with comments (datamarked), active rules, and previous insights for dedup.

**Files:** `GML-gmail-agent/internal/prompt/history.go`, `history_test.go`

### Step 7: GML — Config + command wiring

Add `LearnConfig` to config. Add `history` and `insights` commands to `cmd/gml/main.go`. Add `learn` subcommand to `run.sh`.

**Files:** `GML-gmail-agent/internal/config/config.go`, `cmd/gml/main.go`, `run.sh`

---

## Test Requirements

### Offline (no credentials needed)
- [ ] DSH: `include_dismissed` returns dismissed notifications with `dismissed_at` field
- [ ] DSH: `has_comment` filters to notifications with non-empty comments
- [ ] DSH: default behavior unchanged (backward compat)
- [ ] GML: insight JSON validation (valid, invalid signal_strength, missing fields, code fences)
- [ ] GML: InsightsToNotifications mapping (message format, priority, [Insight:] prefix)
- [ ] GML: behavior aggregation with mock GmailClient (read rate, reply rate, top-N, min-emails filter)
- [ ] GML: history prompt structure (sections present, comments datamarked, empty data handling)

### Live (credentials needed — deferred to Tomas's return)
- [ ] gws thread list/get works via Docker container
- [ ] DSH dismissed notifications round-trip (post → dismiss → fetch)
- [ ] Full pipeline: `./run.sh learn --days 7 --model gemini`

---

## Cost Controls

- `--days` default 30, hard max 90
- Top senders capped at 30, minimum 3 emails per sender
- Thread lookups capped at 50 per sender
- Dismissed notifications capped at 100

---

## Security

- DSH comments sent to LLM get datamarking + injection detection (GML-019)
- No auto-apply — insights are informational only
- Credential handling unchanged (stdin pipe)

---

## Decisions

### [GML-049] Behavior analysis uses GmailClient interface
**Date:** 2026-05-29
**Phase:** Planning
**Decided by:** Team
**Decision:** `behavior` package depends on a `GmailClient` interface, not directly on `gws` functions. Production wraps gws; tests use mock implementation.
**Alternatives considered:** Direct gws dependency (untestable without live API), full httptest server (overkill)
**Reasoning:** Unit tests must run without Gmail credentials. Interface is minimal (4 methods).
**Revisit if:** Never

### [GML-050] Insights use existing info notification type
**Date:** 2026-05-29
**Phase:** Planning
**Decided by:** Team
**Decision:** Insights posted as `info` notifications with `[Insight: category]` message prefix. No new DSH notification type.
**Alternatives considered:** New `insight` type in DSH CHECK constraint (requires SQLite table rebuild migration)
**Reasoning:** KISS. Prefix-based distinction is sufficient. Stage B may need a `proposal` type — defer schema changes until then.
**Revisit if:** Stage B needs type-level filtering
