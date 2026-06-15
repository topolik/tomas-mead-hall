# Iteration 016 — Gmail traceability labels

**Started:** 2026-06-02
**Phase:** Implementation

## Goal

Apply Gmail labels (e.g. `GML/archived`) before any archive action so Gmail itself is the audit trail. Users can search `label:GML-archived` in Gmail to see everything GML touched.

## Scope

- `GML/archived` label only (matches current archive-only action vocabulary)
- Forward-only: new archives get labeled, no backfill
- Single `messages.modify` call: `addLabelIds` + `removeLabelIds` atomically
- `--dry-run` applies no labels (zero mutations, consistent with existing semantics)

## Out of scope

- `GML/filtered` and other action-type labels (belongs to broader-action-vocabulary todo)
- Encoding "why" in label hierarchy — that's what execution logs are for
- Backfilling historical archives

## Decisions

### Atomic label + archive in single API call
**Date:** 2026-06-02
**Decided by:** Developer
**Decision:** Combine `addLabelIds: ["GML/archived"]` and `removeLabelIds: ["INBOX"]` in one `messages.modify` call rather than two separate calls.
**Rationale:** Avoids half-state where INBOX is removed but label isn't applied. One API call per message instead of two.

### Security: narrow the allowlist, don't open it
**Date:** 2026-06-02
**Decided by:** Developer
**Decision:** Add `users.labels.list` and `users.labels.create` to the gws allowlist. Update the modify test to allow `addLabelIds` only with the `tracingLabelID` variable, not arbitrary labels. Keep the no-send/no-delete/no-trash checks.
**Rationale:** The existing security tests enforce minimal blast radius. Adding label support should narrow the allowed scope to tracing labels only, not open unrestricted label mutation.

### Label ensured once per run, cached
**Date:** 2026-06-02
**Decided by:** Developer
**Decision:** `EnsureTracingLabel` is called once at the start of a rules run. It lists existing labels, creates if missing, and returns the label ID. The ID is passed through to per-message archive calls.
**Rationale:** KISS — one API call to list, at most one to create. No per-message overhead.

### Graceful degradation when label unavailable
**Date:** 2026-06-02
**Decided by:** Developer
**Decision:** If label creation fails (e.g., scope issue), archiving proceeds without the label and a warning is logged. No hard failure.
**Rationale:** Tracing is an audit enhancement, not a prerequisite for archiving. Existing credentials may need re-auth with proper scopes — don't block normal operation.

## What was built

### `gws.go` — label operations
- `ListLabels()`: lists all Gmail labels
- `CreateLabel()`: creates a new label with name, visible in label list and message list
- `EnsureTracingLabel()`: idempotent label setup — lists, creates if missing, returns ID. Enforces `GML/` prefix.
- `ArchiveWithLabel()`: atomic `messages.modify` with both `addLabelIds` and `removeLabelIds`

### `gws_test.go` — narrowed security
- `users.labels.list` and `users.labels.create` added to method allowlist
- New `TestTracingLabelConstraints`: verifies `addLabelIds` only appears in `ArchiveWithLabel`, enforces `GML/` prefix, blocks `labels.delete` and `labels.patch`
- Original `TestArchiveOnlyRemovesInbox` retained for TRASH/SPAM/delete checks

### `rules/engine.go` — tracing integration
- `TracingLabelName` constant: `"GML/archived"`
- `RunWithSenderFilter` accepts `tracingLabelID` parameter
- Uses `ArchiveWithLabel` when label ID available, falls back to `Archive`

### `scheduler/scheduler.go` — label setup on startup
- `EnsureTracingLabel` called once on scheduler start
- Label ID cached and reused for all subsequent runs

### `cmd/gml/main.go` — label setup in `run` command
- Label ensured before run (skipped in dry-run mode)
- Warning logged if unavailable, proceeds without labels

## Scope note

`gmail.modify` scope covers `labels.create` per Google docs. If the live test shows scope errors, the OAuth token needs re-auth (fresh consent flow). No code change needed.

## Run log

```
$ go test ./...
ok   gws     0.005s    (3 tests pass including new TestTracingLabelConstraints)
ok   rules   0.003s

$ docker compose build
Image built successfully, tests pass in container

$ ./run.sh run --dry-run --since 24
[DRY RUN] No messages will be archived.
[would archive] [liferay notifications] ... — 2 messages
(no label setup in dry-run — correct)
```
