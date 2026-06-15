# GML — Iteration 022: Implementation — Adopt DSH Threads processed-tracking

- **Phase:** 2 — Implementation
- **Phase lead:** Developer
- **Start:** 2026-06-13
- **Input:** `022-planning.md` (skip = provenance ∪ resolved-thread; mark forward-only, gap set only)
- **Branch/worktree:** `gml-distill-threads` (off master `33a44d4`)

---

## What was built

### DSH client (`internal/notify/dsh.go`)
- `ListResolvedNotificationThreads() (map[int64]bool, error)` — one batched
  `GET /api/v1/threads?ref_type=notification&status=resolved` (ref_id omitted);
  parses the string `ref_id` of each `ThreadSummary` into an int64 set,
  skipping non-numeric. Reuses the existing OAuth Bearer `token()` flow.
- `PostResolvedThread(refID int64, subject, body string) error` — two calls:
  `POST /api/v1/threads {subject, body, ref_type:"notification", ref_id:"N"}`
  then `PATCH /api/v1/threads/{id} {status:"resolved"}` using the id from the
  create response. New `ThreadSummary` struct (string `ref_id`, matching DSH).

### Distill selection (`cmd/gml/main.go`)
- Extracted `provenanceDistilledSet(kf)` and `isDistillable(n)` helpers (single
  source for the provenance set and the distillable predicate).
- `selectUndistilled(dismissed, kf, resolved)` — new `resolved` param; skip =
  `provenance ∪ resolved-thread`. A nil set degrades to provenance-only.
- `cmdDistillGather` — fetches the resolved set once (best-effort) and passes it.
- `gapInsightsToMark(dismissed, distilled, resolved)` — the gap set
  (`distillable ∧ ¬provenance ∧ ¬resolved`); `threadMarkerText` (subject ≤200);
  `markDistilledGap(dsh, dismissed, kf)` posts a resolved thread per gap insight,
  best-effort.
- `cmdDistillApply` — restructured: hoisted the `dsh` client + `dismissed` fetch
  (shared by provenance attribution and marking); the empty-result early-return
  removed so marking runs even when the LLM produced nothing (the
  distilled-to-nothing gap); knowledge re-loaded **after** pattern writes so this
  cycle's pattern-producing insights count as provenance-covered and are excluded
  from marking (no thread, no push).

## Tests added
- `internal/notify/dsh_test.go` — `TestListResolvedNotificationThreads` (batched
  query shape, string→int64, non-numeric skipped), `TestPostResolvedThread`
  (create→resolve sequence + payload shapes), via `httptest`.
- `cmd/gml/main_test.go` — updated `TestSelectUndistilled` for the new signature;
  added `TestSelectUndistilled_ResolvedThreadUnion` (gap insight skipped by a
  resolved thread alone) and `TestGapInsightsToMark` (exactly
  `distillable ∧ ¬provenance ∧ ¬resolved`).
- `internal/notify/accept_threads_test.go` — env-gated live acceptance driving
  GML's own client against a running DSH.

## Verification (ran it)

- `go build ./...` — success. `go vet ./...` — no issues.
- `go test ./...` — **196 pass in 15 packages** (incl. the 4 new/updated tests,
  verified by name with `-v`).
- **Live isolated round-trip — PASSED.** Spun up the **real DSH binary** on a
  scratch SQLite DB (migrations applied incl. `011_threads`), provisioned an
  OAuth2 client, seeded a dismissed GML insight (#1), and drove it with GML's
  `notify.DSHClient`: probe resolved set → `PostResolvedThread(#1)` (create +
  PATCH resolve) → probe again → #1 present. Proves the GML client ↔ real DSH
  wire contract end-to-end. Nothing live touched; throwaway provisioning helper
  removed; worktree clean afterward.
  ```
  --- PASS: TestAcceptance_GMLThreadsRoundTrip (0.07s)
  OK: GML client ↔ live DSH threads round-trip for notification #1
  ```

## Bugs found & fixed during implementation
- None — build/vet/tests green on first full run after the edits.

## Decisions
Recorded in `ASSUMPTIONS.md`: **GML-084** (threads close the residual gap;
provenance retained; skip = union), **GML-085** (forward-only marking of the gap
set only — bounds web-push), **GML-086** (marking is best-effort, never fatal).

## Not in this iteration / review gate
- **Full-stack real-data acceptance** — `gml distill-gather | <llm> | gml
  distill-apply` against a **copy of the live `dsh.db`** with the real LLM
  pipeline. Like DSH iter-026, this is Tomas-assisted (he produces the copy +
  runs the DSH/LLP stack). The isolated round-trip above already proves the wire
  contract against the real DSH binary; this gate adds the live data + LLM legs.
- **todo.txt L75** closes on merge to master (review gate = merge gate, MO §10).
