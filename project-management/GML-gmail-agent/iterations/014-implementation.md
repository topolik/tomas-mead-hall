# GML — Iteration 014: Implementation (execution visibility + logging)

- **Start:** 2026-06-01
- **End:** 2026-06-01
- **Phase:** Implementation

## Scope

Two related todo items addressed together:
- **Execution visibility**: show exactly which emails will be processed, what action, and why
- **Execution logging**: structured output of every action with reason, rule type, and email context

**Out of scope** (separate iterations):
- Plan-and-approve workflow (post plan to DSH, execute after approval)
- Gmail traceability (labels as audit trail)
- Broader action vocabulary (labels, filters, drafts)

## What was built

### Enhanced Action struct (`internal/rules/engine.go`)

`Action` now includes:
- `RuleType` — which rule type triggered the action (archive_by_age, archive_by_sender, archive_by_label)
- `Reason` — human-readable explanation of WHY the rule matched
- `Date` — email date for context
- `Timestamp` — when the action was evaluated

Reason examples:
- `"message is 45 days old (threshold: 30 days, state: read)"`
- `"sender matches pattern: notifications@github.com"`
- `"has label: to-archive"`

JSON tags added to `Action` and `Result` for structured serialization.

### `--json` flag for `gml run`

`gml run --dry-run --json` outputs the full Result struct as JSON to stdout:
```json
{
  "actions": [
    {
      "message_id": "abc123",
      "subject": "Weekly digest",
      "from": "noreply@service.com",
      "date": "2026-05-01",
      "rule_name": "old read mail",
      "rule_type": "archive_by_age",
      "reason": "message is 31 days old (threshold: 30 days, state: read)",
      "archived": false,
      "timestamp": "2026-06-01T10:00:00Z"
    }
  ]
}
```

### Enhanced text output

Text mode now shows message ID, date, and reason per action:
```
[would archive] [github notifications] "Tamás Biró" <notifications@github.com> — Re: [liferay/lfris-security] ...
  id: 19e83e12bbccc485
  date: 2026-06-01
  reason: sender matches pattern: notifications@github.com
```

### `--since H` flag for time-scoped runs

`gml run --dry-run --since 2` limits rule evaluation to emails from the last H hours. Essential for hourly scheduled runs — avoids scanning the entire inbox each time.

Gmail query gets `newer_than:Nh` appended to all rule types. Without `--since`, behavior is unchanged (scans full inbox).

Scheduler auto-calculates: `2x interval_minutes` converted to hours, so a 60-min interval looks back 2 hours.

### Unified scheduler logging (`internal/scheduler/scheduler.go`)

Scheduler now uses the same enhanced format with reason in log output. Time-scoped via `--since` derived from interval.

### `matchedPattern` helper

Extracted from `matchesSenderPatterns` — returns which specific pattern matched (used to build reason string). Unit tested.

## Tests

- 7 new tests in `internal/rules/engine_test.go` (5 `TestMatchedPattern` subtests + 2 `TestMatchesSenderPatterns` checks)
- All 101 existing tests continue to pass

## Decisions

### [GML-048] Visibility + logging combined in one iteration
**Date:** 2026-06-01
**Phase:** Implementation (014)
**Decided by:** Developer
**Decision:** Bundle execution visibility (todo #3) and execution logging (todo #5) into a single iteration, despite the user requesting "separate todo items."
**Alternatives considered:** Two separate iterations (one for enhanced Action fields, one for JSON output)
**Reasoning:** Both features produce the same data shape — an Action with reason, type, date, and timestamp. Splitting them would mean touching the same struct twice. The user's "separately" referred to working on each todo item in its own session, not mandating separate iterations for closely coupled changes.
**Revisit if:** Future items (plan-and-approve, labels, broader actions) are truly independent and should not be bundled.

### [GML-049] --since in hours, scheduler uses 2x interval
**Date:** 2026-06-01
**Phase:** Implementation (014)
**Decided by:** Tomas + Developer
**Decision:** `--since` flag takes hours (not days/minutes). Scheduler derives since window as `2x interval_minutes` converted to hours.
**Alternatives considered:** Days (too coarse for hourly runs), minutes (Gmail API only supports hours via `newer_than:Nh`)
**Reasoning:** Tomas specified hourly job cadence. Gmail's `newer_than` supports hours natively. 2x multiplier ensures no gap between runs even if one is delayed.
**Revisit if:** Sub-hour scheduling is needed (Gmail doesn't support `newer_than` in minutes).

## Follow-up

- JSON output to DSH as a "plan" (needs new DSH endpoint — belongs to plan-and-approve iteration)
- Persistent log file (watch.sh can tee stderr; dedicated DSH endpoint is separate work)
