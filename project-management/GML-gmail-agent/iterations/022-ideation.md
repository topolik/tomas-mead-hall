# GML — Iteration 022: Ideation — Adopt DSH Threads processed-tracking in distill

- **Phase:** 0 — Ideation
- **Phase lead:** Developer
- **Start:** 2026-06-13
- **Input:** todo.txt L75; DSH iterations 024–027 (Threads shipped); GML iterations 013 (gap), 020 (provenance), 021 (identity dedup)

---

## Trigger

todo.txt L75:

> `[GML]` adopt DSH threads processed-tracking in distill: before re-processing a
> dismissed insight, check `GET /api/v1/threads?ref_type=notification&ref_id=N&
> status=resolved` — non-empty means processed, skip it. Post a resolved thread
> after each successful distill. DSH side shipped in iteration 026.

The DSH half shipped and passed real-data acceptance (insight #1294, iteration
026). This iteration is the GML-side adoption.

## The ideation question — is this still needed as written?

The todo descends from the **013-review gap**: *"distill reprocesses same
dismissed insights — known limitation … acceptable until discussions project
provides 'processed' tracking"* (`013-review.md:36-37`). But that gap was written
2026-05-31. Since then, **iteration 020 (2026-06-09) already built a 'processed'
tracker** — *insight provenance*. So before adopting the todo literally, the
honest question is: **what, if anything, does provenance still miss?**

### Current-state inventory — what iteration 020 already does

`selectUndistilled()` (`cmd/gml/main.go:731`) builds a `distilled` set from every
knowledge pattern's `SourceInsights` and skips any dismissed insight already in
it. Attribution happens in `cmdDistillApply` via the deterministic join
`NormalizeSearchKey(pattern.GmailSearch) == InsightDedupKey(insight.Link)`
(`main.go:804`). The team called this *"the single source of truth for
'distilled' — no separate ledger to drift"* (`020-planning.md`, decisions) and
explicitly rejected a separate ledger. It killed the headline "11× repeated todo"
symptom deterministically.

So provenance is **not** nothing — it closed the *common* case. The 013-review
gap is already ~80% solved. Adopting the todo as "build the missing tracker"
would be re-building something that exists.

### What provenance structurally cannot reach (the residual gap)

Provenance only marks an insight processed when it produced a **pattern whose
query normalizes to the insight's link**. It misses:

1. **Todo-only distill** — yields a todo, no pattern. Todos carry `SourceInsights`
   (`main.go:833`) but are not persisted to `knowledge.yaml`; `selectUndistilled`
   scans only `kf.Patterns` (`main.go:734`). → re-distilled every cycle.
2. **Distills to nothing** — empty LLM result early-returns (`main.go:769`),
   records nothing. → re-fed forever.
3. **Non-matching query** — LLM broadens/merges the query; the join yields no ID.
4. **Best-effort join** — skipped if the DSH fetch fails (`main.go:790`).

Each `watch-knowledge` cycle re-feeds the growing pile of uncovered insights to
Gemini-via-LLP. **That is a live LLP quota burn**, and it is invisible to Tomas
(it lives in `knowledge.yaml`, not DSH).

### What threads add over provenance

- **ID-keyed, not query-derived** — a resolved thread is keyed on the *immutable*
  DSH notification ID, so it covers all four gap cases regardless of what (or
  whether) the insight produced. No fragile query-normalization join.
- **Cross-service visible** — Tomas can see in the DSH UI that GML processed an
  insight; provenance is buried in a YAML file.
- **The contract already exists** — DSH shipped + acceptance-tested exactly this
  query shape; GML's existing OAuth Bearer client already authenticates to it.

**Conclusion:** re-scope from "build processed-tracking" → **"close the residual
gap provenance can't reach, with the thread contract; keep provenance for the
common case + traceability."**

## Requirements (what "done" means)

- A dismissed insight that has been through distill — *whatever it produced* — is
  durably marked processed and never re-distilled.
- The fragile query-normalization join is no longer the only thing preventing
  infinite reprocessing.
- No regression to the provenance-covered common case; no new auth; no DSH change.
- Web-push volume to Tomas stays bounded (CreateThread pushes on every creation).
- Touches only the distill/learn path — the analyze-side dedup (018/021) is left
  alone.

## Options considered

- **A — Literal todo: a full threads ledger replacing provenance.** Rejected:
  re-introduces a second source of truth that drifts, the exact thing GML-020
  rejected; throws away a working deterministic mechanism.
- **B — Do nothing; provenance is good enough.** Rejected: the residual gap is a
  *live* LLP quota burn that grows with the dismissed backlog, and it is invisible
  to Tomas.
- **C — Replace the provenance skip entirely with a thread check.** Rejected:
  loses the no-network local fast-path, forces a one-time re-distill of the whole
  already-covered backlog, and maximizes web-push noise.
- **D — Union skip (provenance ∪ resolved-thread), forward-only mark of the gap
  set only.** **Chosen.** Zero regression for the covered case (no thread, no
  push), threads handle exactly what provenance misses, and the first-run push
  burst is bounded by the *current* uncovered backlog — then steady-state is one
  push per genuinely-new uncovered insight.
- **Back-fill all dismissed vs forward-only marking.** Forward-only — matches the
  team's established precedent (GML-020 "forward-only by Tomas's choice") and is
  what bounds the push burst.

## Scope

- **In:** `distill-gather` (skip-check), `distill-apply` (mark gap set), 2 new
  DSH-client methods.
- **Out:** analyze-side dedup; removing provenance/`SourceInsights` (kept for
  traceability + fast-path); any DSH-side change; LLM cross-linking / per-agent
  inbox (DSH's deferred backlog, not GML's).

## Decisions (high-level; detailed rationale → planning + ASSUMPTIONS)

- Re-scoped: threads close the **residual** gap, not the whole gap — provenance
  (GML-020) already owns the common case and is retained.
- Skip = **provenance ∪ resolved-thread** (union, zero-regression).
- Mark **forward-only**, gap set only (exclude provenance-covered) to bound
  web-push volume.
- Thread marking is **best-effort** — never fatal to distill.

## Open consideration for review

`CreateThread` web-pushes Tomas on every creation (`api_threads.go:152`).
Option D bounds this, but the first run still pushes once per currently-uncovered
insight. If even that bounded burst is unwanted, a follow-up could ask DSH for a
"silent/agent-created" flag — out of scope here (DSH is frozen for this work).
