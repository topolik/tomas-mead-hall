# GML — Iteration 013: Knowledge File & Distillation

- **Phase:** 1 — Planning
- **Phase lead:** Developer
- **Start:** 2026-05-30

---

## Goal

Add persistent learning that accumulates across runs. When Tomas comments on insight notifications in DSH and dismisses them, a distillation step reads those comments, sends them to an LLM for interpretation, and writes confirmed/rejected/refined patterns to a `knowledge.yaml` file committed to the repo. Future Mode 3A learning runs include this knowledge so they don't repeat resolved patterns and respect Tomas's decisions.

## Scope

In scope:
1. Add `gmail_search` to insight schema (required field, LLM-provided, validated)
2. Knowledge file format (`knowledge.yaml`, committed to repo)
3. `gml distill` command + `run.sh distill` pipeline
4. Include knowledge as context in Mode 3A prompt (`BuildHistory`)

Out of scope:
- Auto-applying confirmed patterns as Mode 1 rules (Stage B)
- Wiring knowledge into Mode 2 analysis prompt
- Backfilling `gmail_search` into existing 15 insight notifications in DSH

## Knowledge File Schema

```yaml
last_distilled_at: "2026-05-30T12:00:00Z"   # informational only
patterns:
  - gmail_search: "from:jira@project.atlassian.net"
    pattern: "Ignores JIRA notifications"
    status: confirmed       # confirmed | rejected | refined
    category: ignore_pattern
    senders:
      - jira@project.atlassian.net
    first_seen: "2026-05-29"
    last_updated: "2026-05-30"
    comment_summary: "Confirmed: these are noise, auto-archive"
    refined_action: ""      # populated when status=refined
```

Key: `gmail_search` — dedupe and upsert by this field. Repeat distill runs are idempotent: always processes all dismissed insight notifications with comments, LLM output keyed by gmail_search, existing entries updated rather than duplicated.

## Implementation Plan

1. **`InsightAnalysis` schema** — add `gmail_search` (required string), update prompt instructions in `history.go`, update validation in `insights.go`, update `InsightsToNotifications` to use LLM-provided search, fix existing tests
2. **`internal/knowledge/`** — new package: `KnowledgeFile` struct, `Load(path)`, `Save(path)`, upsert by gmail_search
3. **`gml distill`** — new command: reads dismissed `[Insight:]` notifications with comments from DSH, builds distillation prompt, validates LLM output, writes/updates `knowledge.yaml`
4. **`run.sh distill`** — 3-step pipeline: gather from DSH → LLM → write file
5. **`BuildHistory`** — add `<knowledge>` section with confirmed/rejected/refined patterns
6. **Tests and live test**

## Test Requirements

### Offline
- [ ] `insights.go`: `gmail_search` required — validation fails when empty
- [ ] `insights.go`: valid insight with `gmail_search` passes
- [ ] `InsightsToNotifications`: link uses LLM-provided `gmail_search`
- [ ] `knowledge/`: Load/Save roundtrip preserves all fields
- [ ] `knowledge/`: Upsert by `gmail_search` — new pattern added, existing updated
- [ ] `knowledge/`: Status transitions (confirmed → refined, etc.)
- [ ] `prompt/history.go`: knowledge section present in output
- [ ] `prompt/history.go`: empty knowledge handled

### Live
- [ ] Full loop: dismiss insight with comment → `run.sh distill` → `knowledge.yaml` created/updated → next `run.sh learn` prompt includes knowledge

---

## Decisions

### GML-051: Idempotent distillation (no watermark)
**Date:** 2026-05-30
**Phase:** Planning
**Decided by:** Developer
**Decision:** Distill always processes all dismissed insight notifications with comments, deduplicates by `gmail_search` key
**Alternatives considered:** Timestamp watermark in knowledge.yaml to skip already-processed notifications
**Reasoning:** KISS — idempotent approach is simpler, avoids state tracking bugs, and the volume is small enough that re-processing is cheap
**Revisit if:** Dismissed insight count exceeds ~200 or LLM costs become a concern

### GML-052: Knowledge file committed to repo
**Date:** 2026-05-30
**Phase:** Planning
**Decided by:** Tomas
**Decision:** `knowledge.yaml` is committed to the repo (not gitignored)
**Alternatives considered:** Gitignored, stored in DSH database
**Reasoning:** Visible history via git, reviewable, no external dependency
**Revisit if:** File grows very large or contains sensitive patterns

### GML-053: Separate LLM distillation step
**Date:** 2026-05-30
**Phase:** Planning
**Decided by:** Tomas
**Decision:** Distillation is a separate LLM call (`gml distill` / `run.sh distill`), not part of the learn pipeline
**Alternatives considered:** Single-call (learn + distill combined), keyword matching without LLM
**Reasoning:** Tomas's comments are free-text; LLM interpretation handles nuance. Separate step keeps learn pipeline unchanged and allows running distill on its own schedule.
**Revisit if:** The two-step workflow feels redundant in practice
