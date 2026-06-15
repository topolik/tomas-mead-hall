# Iteration 020 — Planning: insight provenance (back-tracking) through the pipeline

**Started:** 2026-06-09
**Phase:** Planning

## Problem

Every GML artifact (knowledge entry → plan → rule, and todos) originates from an LLM-generated
insight (a DSH notification Tomas dismisses-with-comment), but that link is implicit (via the
Gmail-search string) and discarded once the artifact exists. Tomas asked to record **which
insight #ID** each artifact came from — for traceability, and to make dedup deterministic.

The visible symptom that motivated it: the `distill` step re-processes the *same dismissed
notification* every cycle, so the LLM re-emits the same todo (one was posted 11×) and the same
knowledge pattern.

## Design

**Thread `source_insights` (DSH notification IDs) through every layer**, mostly by deterministic
field-copy, never relying on the LLM for anything dedup depends on:

- **insight → knowledge:** the distill prompt already mandates `pattern.gmail_search` = the
  insight's `Link`, so `NormalizeSearchKey(Link) == NormalizeSearchKey(gmail_search)` is a
  deterministic join. `cmdDistillApply` re-fetches dismissed insights and attributes IDs by this
  join — no LLM attribution for patterns.
- **knowledge → plan:** `Generate` copies `Pattern.SourceInsights` → `Proposal.SourceInsights`
  (stored in the plan's detail JSON).
- **plan → rule:** `cmdApplyRules` copies into `AnnotatedRule.InsightIDs`; `BuildGeneratedRules`
  emits a `# insights #…` comment beside the existing `# plan #N`.
- **insight → todo:** todos have no query anchor, so the LLM attributes `source_insights` (prompt
  shows each entry as `[insight #N]`); `cmdDistillApply` appends a `(insight #N)` back-link suffix.
  Dedup for todos stays on the deterministic text-floor (`todoreader.Add`), never the LLM.

**Dedup gains from provenance:**
- `cmdDistillGather` skips insights whose ID is already in some pattern's `SourceInsights` —
  prevents re-distillation (the 11× repeat) deterministically.
- `structuralDedup` skips a candidate whose `SourceInsights` are all covered by a live plan —
  robust even when a folded plan's filter no longer matches the raw candidate's key.

## Two dedup layers (named so they don't get conflated)
- **insight #ID** → catches re-*processing* the same dismissed row each cycle.
- **`InsightDedupKey`** (iteration 018) → catches a re-*posted* insight (new auto-increment id,
  same Link). ID-provenance does NOT replace it.

## Rejected alternatives
- **Separate `distilled_insight_ids` ledger** — a second source of truth that drifts from the
  provenance already stored; derive the "distilled" set from pattern `SourceInsights` instead.
- **LLM-attributed dedup** — the dedup-critical path must be deterministic; the LLM slipping a
  source ID would silently re-create duplicates. LLM attribution is used only for the todo
  back-link (traceability), backstopped by the text-floor.
- **Backfilling legacy artifacts** — forward-only by Tomas's choice; the existing 38 plans /
  knowledge / 63 insights stay un-tagged, and the paused environment cleanup still uses the
  text/canonical dedup.

## Decisions
- Forward-only; provenance built **before** the environment cleanup (cleanup sequenced after).
- No ledger; provenance is the single source of truth for "distilled."
- Deterministic join for patterns/plans/rules; LLM attribution only for the todo suffix.
- Advisor-reviewed; the design separates traceability (the deliverable) from dedup (floors mostly
  pre-existed; provenance adds the source-insight-set plan key as the one real new dedup).
