# Iteration 020 — Implementation: insight provenance (back-tracking)

**Started:** 2026-06-09
**Phase:** Implementation
**Plan:** see 020-planning.md

## What was built (forward-only)

- **Surface IDs + capture attribution** — `FormatDismissedNotifications` (`internal/notify/dsh.go`)
  prefixes each entry with `[insight #<ID>]`; the distill prompt (`internal/prompt/distill.go`)
  adds `source_insights` to the todos schema; `DistilledTodo.SourceInsights` added.
- **Shared normalizer** — factored `notify.NormalizeSearchKey(query)` out of `InsightDedupKey`
  (behavior identical) so the insight Link and the pattern gmail_search canonicalize identically.
- **Knowledge provenance via deterministic join** — `knowledge.Pattern.SourceInsights` +
  `Upsert` unions them; `cmdDistillApply` re-fetches dismissed insights and attributes each
  pattern's source IDs by `InsightDedupKey(Link) == NormalizeSearchKey(gmail_search)` (no LLM).
- **Distill skip-dedup (no ledger)** — `cmdDistillGather` derives the already-distilled set from
  every pattern's `SourceInsights` and skips those insights, logging each skip.
- **Propose/rule provenance** — `Proposal.SourceInsights` (copied by `Generate`);
  `AnnotatedRule.InsightIDs` (unioned through `mergeAnnotatedRules`); `BuildGeneratedRules`
  emits `# insights #…`; `cmdApplyRules` populates from the plan's proposal.
- **Plan dedup by source-insight set** — `structuralDedup` skips a candidate whose `SourceInsights`
  are all covered by a live plan, ahead of the sender+filter floor.
- **Todo back-link** — `cmdDistillApply` appends `(insight #N)` to the todo text from the LLM
  attribution; dedup stays on `todoreader.Add`'s text-floor (DSH side).

## Run log

- `go build ./...`, `go vet ./...` — clean. `go test ./...` — all pass.
- New tests: `notify.TestProvenanceJoin` (Link↔gmail_search join across URL-encoding, time-window,
  quoting, term-order; non-match for different senders); `knowledge.TestUpsertUnionsSourceInsights`;
  `propose.TestBuildGeneratedRulesInsightComment` (unioned `# insights` line); `main`
  `TestStructuralDedup_InsightProvenance` (folded-filter candidate skipped by provenance) +
  `TestFormatInsightSuffix`.
- gml image rebuilt.

## Behavior proof (empirical, gemini on a real distill prompt)

Built a distill prompt with two `[insight #101 / #102]` dismissed entries and ran gemini:
- Patterns came back with `gmail_search` = `from:no-reply@socradar.com -Critical` and
  `from:alerting-noreply@alerts.example.com` — exactly the insight Links, so the deterministic join
  attributes `[101]`/`[102]` (confirmed by `TestProvenanceJoin` on the same cases).
- The todo came back with `source_insights:[102]` → the `(insight #102)` back-link will attach.
- Datamarking collapsed the space (`insight#101`) but the LLM read through it; attribution intact.

Therefore the two-distill-runs guarantee holds deterministically: run 1 populates pattern
`SourceInsights`; run 2's gather skips those insight IDs → no duplicate pattern/todo.

The repeat-killer itself is now unit-tested directly (not just inferred): the gather skip loop was
extracted to a pure `selectUndistilled(dismissed, kf)` and `TestSelectUndistilled` asserts an
already-distilled insight is skipped while a fresh one is gathered, plus an explicit second pass
(after #102/#103 distill) that skips all three. (The live two-pass needs Tomas's creds; the
extraction is the self-contained proof.)

## Decisions
- Carried from 020-planning: no ledger; deterministic join for patterns/plans/rules; LLM only for
  the todo back-link (text-floor backstops dedup); forward-only.

## Next
- Environment cleanup (paused) — knowledge.yaml reset + plans + insights, using text/canonical
  dedup since legacy artifacts have no provenance.
