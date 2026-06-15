# GML — Iteration 023: Implementation — Local distilled-ledger (replaces threads)

- **Phase:** 2 — Implementation
- **Phase lead:** Developer
- **Start:** 2026-06-14
- **Input:** `023-planning.md`
- **Branch/worktree:** `gml-distill-ledger` (off master `8e190e5`)

---

## What was built

- **`internal/knowledge/knowledge.go`** — added `DistilledInsights []int64`
  (`distilled_insights`, omitempty) to `KnowledgeFile`: the local "already
  distilled" ledger for the residual gap.
- **`cmd/gml/main.go`:**
  - `distilledSet(kf)` = pattern provenance ∪ ledger; `selectUndistilled(dismissed,
    kf)` reverts to 2 args and uses it. `cmdDistillGather` no longer calls DSH for
    threads.
  - `appendDistilledLedger(kf, dismissed)` appends the residual-gap IDs
    (`distillable ∧ ¬provenance ∧ ¬ledgered`) and returns them.
  - `cmdDistillApply` loads `knowledge.yaml` once, upserts patterns, appends the
    ledger (runs even on an empty LLM result), saves once if changed.
  - Removed `gapInsightsToMark`, `threadMarkerText`, `markDistilledGap`.
- **`internal/notify/dsh.go`** — removed `ThreadSummary`,
  `ListResolvedNotificationThreads`, `PostResolvedThread`, and the `strconv`
  import. (DSH Threads API itself is untouched — GML just stops using it.)
- **Tests** — `main_test.go`: `TestSelectUndistilled` reverted to 2-arg;
  `TestSelectUndistilled_LedgerUnion` + `TestAppendDistilledLedger` added.
  `dsh_test.go`: thread httptest tests removed, imports reverted.
  `accept_threads_test.go` deleted.

## Testing

- `go build ./...` success; `go vet ./...` clean.
- `go test ./...` — **194 pass in 15 packages** (new ledger tests verified by
  name with `-v`).
- No DSH-thread symbols remain in GML (grep clean).

## Real-data verification (RAN AND PASSED, 2026-06-14)

Against a **copy** of the live `dsh.db` (`docker cp` from the running container;
live DB untouched, copy discarded after), with a provisioned test OAuth client and
one injected synthetic gap insight (#1322), driving the **iter-023 gml binary**:

```
CYCLE 1 distill-gather:  found 1 dismissed insights ... (27 skipped as already distilled)
distill-apply (empty):   [ledger] recorded insight #1322 distilled (residual-gap)
knowledge.yaml:          distilled_insights:\n    - 1322
CYCLE 2 distill-gather:  no new dismissed notifications to distill (28 already distilled)
```

The gap insight is found, recorded **locally** (no `[thread]` line, no DSH write),
and skipped next cycle. Confirms the ledger closes the gap with zero DSH
dependency. (Harness note: first attempt accidentally built `gml` from the master
checkout — old thread code — and printed `[thread] marked`; fixed by pointing the
build at this worktree.)

## Bugs found & fixed

- None in product code. One test-harness bug (wrong binary source) caught and
  fixed during verification.

## Decisions

`ASSUMPTIONS.md` **GML-087** (local ledger replaces threads) — supersedes
GML-084/085/086, which are annotated accordingly.

## Not in this iteration

DSH-side changes (threads remain in DSH, unused by GML); the deferred LLM
cross-linker; analyze-path dedup.
