# Iteration 015 — Plan-and-approve pipeline (Mode 3B)

**Started:** 2026-06-01
**Phase:** Implementation

## Goal

Complete the plan-and-approve workflow: knowledge.yaml → propose rules → DSH review UI → approved rules written to rules.yaml. Nothing executes without explicit sign-off.

## What was built

### `gml propose` command
- Reads `knowledge.yaml` + `rules.yaml`, generates proposals, posts to DSH as plans
- `--json` flag for raw JSON output (no DSH posting)
- Deduplicates against existing rules
- Extracts senders from `senders[]` field, falls back to `gmail_search` `from:` prefix
- Auto-generates rule names from sender domain (e.g., `liferay notifications`)
- Carries `refined_action` as constraint for human review
- Sends web push notification for new plans

### `gml apply-rules` command
- Fetches approved plans from DSH, outputs new rules.yaml to stdout
- `run.sh` captures output and writes to rules.yaml on host
- Idempotent: replaces marker block (`# === gml-generated rules ===`) on each run
- Preserves hand-edited rules above the marker

### DSH `/plans` page
- New `plans` table (migration 010) with pending/approved/rejected lifecycle
- API: `POST/GET /api/v1/plans`, `PATCH /api/v1/plans/:id`
- UI: status filter tabs with counts, approve/reject buttons per plan
- Client-side JSON→readable rendering: pattern, rule details, senders
- Constraint warning for refined patterns ("not enforced by rule type — advisory only")
- Nav link added to all DSH templates

### Tests (6 in propose package)
- `TestGenerate` — confirmed + rejected pattern, constraint carried through
- `TestGenerateSkipsDuplicate` — sender already in rules.yaml → skipped
- `TestGenerateFallsBackToGmailSearch` — no senders field → parse from gmail_search
- `TestBuildRuleName` — domain extraction, multi-sender fallback
- `TestBuildGeneratedRules` — marker insertion before analysis: section
- `TestBuildGeneratedRulesIdempotent` — no duplicates on re-run

### Live test (full pipeline)
1. `./run.sh post-plan` → posted 1 plan (Atlassian), skipped 1 (Salesforce rejected)
2. Approved plan via DSH API
3. `./run.sh apply-rules` → generated rule inserted in rules.yaml with marker
4. `./run.sh propose` → correctly sees "rule already exists for all senders"
5. Second apply-rules → idempotent, no duplicates

## Run log

```
$ ./run.sh post-plan
  posted plan #2: liferay notifications
done: 1 plans posted to DSH, 1 skipped

$ ./run.sh apply-rules
  approved: liferay notifications (archive_by_sender) [constraint: Do not auto-archive ...]
done: 1 rules generated (write via run.sh)
[apply-rules] rules.yaml updated

$ ./run.sh propose
  loaded 2 knowledge patterns, 2 existing rules
done: 0 proposals, 2 skipped
  skipped: Atlassian — rule already exists for all senders
  skipped: Salesforce — rejected by user
```

## Decisions

### Marker-based rules.yaml writing
**Date:** 2026-06-01
**Decided by:** Developer
**Decision:** Generated rules are placed in a `# === gml-generated rules ===` block. Re-running replaces the block idempotently. Hand-edited rules above the marker are preserved.
**Rationale:** Tomas hand-edits rules.yaml. yaml.v3 Node API would be complex. Separate file would spread config. Marker is KISS-aligned and git-diffable.

### Container outputs to stdout, shell writes file
**Date:** 2026-06-01
**Decided by:** Developer
**Decision:** `apply-rules` outputs new rules.yaml content to stdout. `run.sh` captures it and writes to the host file.
**Rationale:** Docker compose mounts rules.yaml as `:ro`. Rather than fight volume overrides, keep the container read-only and let the shell do file I/O.

### Merged propose + post-plan into single command
**Date:** 2026-06-01
**Decided by:** Tomas
**Decision:** `propose` posts to DSH by default; `--json` for raw output. Removed `post-plan` as separate command.
**Rationale:** User feedback: "I don't know what to do with it" when propose showed JSON. Default should do the useful thing.

### Constraints are advisory only
**Date:** 2026-06-01
**Decided by:** Developer
**Decision:** Refined patterns with `refined_action` produce a `constraint` field in the proposal. The DSH UI shows a warning ("not enforced by rule type"). The rule itself is unconditional `archive_by_sender`.
**Rationale:** Building a constraint-aware rule type is premature. The human sees the warning and can approve or reject. Future "broader action vocabulary" work can add conditional rules.
