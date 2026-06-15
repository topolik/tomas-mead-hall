# 017 — Implementation (Iteration 5B: contradiction resolution)

- **Start:** 2026-06-13
- **End:** 2026-06-13
- **Phase:** Implementation

## What was built

- **`internal/contradiction`** — `BuildPrompt` (sweep over active insights, grouped by category), `ParseResponse` (prose-tolerant; ids must be known + active; <2 members dropped), `MoreAuthoritative` (provenance: feedback > distill, newer evidence ts > older, strong > weak, stable id tiebreak), `Resolve` (winner stays active, losers marked `status: superseded` + `superseded_by` + `superseded_reason`, kept in the file; idempotent).
- **`distill.Insight`** gained `Status` / `SupersededBy` / `SupersededReason` (all `omitempty`, backward-compatible) and helpers `distill.Active()` + `(Insight).LatestTS()`.
- **Active-only downstream**: profile generation, ask BM25 retrieval, and the sweep itself build over `distill.Active()` — superseded insights stay in `insights.yaml` for audit but never reach a profile or an answer (4 `Active()` call sites).
- **`mnd contradiction-prompt` / `contradiction-merge`**; `run-task.sh contradictions`, folded into `retrain` after `learn` (so fresh feedback insights win the conflicts they settle; the existing before/after hash gate regenerates profiles when a retirement changes the brain).
- **53 Go tests** (T41–T45 new). Division of labour mirrors distill: LLM finds conflicts, Go decides winners (MND-003).

## Live run (T46) over the real 801-insight brain

`[llp:auto] ok` (prompt 179 KB, gemini served). **4 contradictions resolved**, all by recency/strength among same-source pairs:

1. `delegate deep-dive analysis to sub-agents` (04-10) → superseded by `process large contexts directly, avoid sub-agents` (04-19). Recency.
2. **`store OAuth creds in a credential manager, pass as env vars` (05-28) → superseded by `never use env vars for sensitive keys; in-memory only` (06-12).** Recency — and the winner **matches Tomas's standing service-secrets rule** (memory `feedback-service-secrets`). Strong validation that provenance picks the right belief.
3. `prefer minimal top-level docs, avoid nested READMEs` → superseded by `keep root brief, move detail into nested READMEs` (same day, +9 min; winner also stronger).
4. `run all services/infra in containers` (05-31) → superseded by `default to local builds over Docker unless requested` (06-03). Recency.

### For Tomas's review (the false-contradiction risk, materialized)
**#4 is debatable** — "run services in containers" (deployment) vs "default to local builds" (build process) are arguably *different contexts*, not a true contradiction; the LLM over-flagged and recency retired the container preference. Note the brain advised "run everything in containers" during today's DXP orchestration. Trivially reversible: flip `status` back on `6e46e7d05cda`. This is exactly the risk flagged in planning — recorded here as the first real example.

### Acceptance case — honest negative
The iteration-4 relic *"Work directly on the master branch; do not create branches or PRs"* is **still active** — the sweep correctly did **not** touch it, because **no insight contradicts it**. The push-ruling feedback insight is about *pushing to origin*, a different concern; and the team's actual branch-per-project workflow (MO §10) was adopted via instruction + research, never distilled into an insight. **Finding: contradiction resolution only retires a stale belief when the corrected belief exists as an insight.** The branches relic needs the new workflow captured as an insight (via the feedback loop or a targeted distill) before it can lose — filed for iteration 5 follow-up.

## Decisions

### Superseded, not deleted
**Date:** 2026-06-13 · **Decided by:** team
**Decision:** Conflicts mark the loser `status: superseded` with `superseded_by` + reason; the insight stays in `insights.yaml`, excluded from profiles/ask via `Active()`.
**Reasoning:** Auditability and reversibility — a false contradiction (#4) is recoverable by flipping one field, and the evidence trail survives.

### LLM finds conflicts, Go picks winners
**Date:** 2026-06-13 · **Decided by:** team
**Decision:** The LLM only identifies conflicting sets; the winner is the deterministic provenance rule (feedback > distill, newer > older, strong > weak).
**Reasoning:** The authority ordering is Tomas's policy, not an LLM judgment call — keeping it in Go makes every retirement explainable and reproducible.

## Update — three-way verdict (Tomas's #4 feedback, 2026-06-13)

Tomas flagged #4: "run vs deploy — ensure the context is clear when to use which." The binary conflict→retire was missing a category. Added a **three-way verdict** per flagged set:
- `contradiction` — same context, opposite → retire by provenance (unchanged).
- `context_split` — both valid in different situations → **keep both**, sharpen each `context`; nothing retired. Unknown/missing verdict defaults here (non-destructive).
- not-a-conflict → the LLM omits it.

`ask` gained rule 1a: insights are context-scoped; when two seem to conflict, apply the one whose context matches the agent's situation. T47 added; 54 tests.

**Re-run** (restored the brain to its pre-sweep 801-active state, swept fresh):
- **2 retired** (genuine contradictions): sub-agents (delegate vs process-directly), README structure (top-level vs nested).
- **4 context-split pairs scoped** (8 insights, all kept active): containers↔local-builds (new-service isolation vs mature-env local paths — **#4, now scoped not retired**); autonomous-momentum↔clarify-on-ambiguity; encrypted-temp-files↔in-memory-secrets; lock-package-branches↔latest-stable.

Two honest limitations, both filed in MND-025:
1. **Non-deterministic coverage** — the env-var-credentials genuine contradiction was caught in the first (binary) run but NOT this run; the LLM flags different sets each pass. Repeated `retrain` converges; a single pass does not guarantee. A future refinement: loop the sweep until two consecutive passes find nothing new (the "loop-until-dry" pattern).
2. The branches relic (017 above) still needs the workflow captured as an insight before anything can defeat it.
