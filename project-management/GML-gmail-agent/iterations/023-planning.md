# GML — Iteration 023: Planning — Local distilled-ledger (replaces threads)

- **Phase:** 1 — Planning
- **Phase lead:** Developer
- **Start:** 2026-06-14
- **Input:** `023-ideation.md` (simplify processed-tracking off DSH threads → local ledger)

---

## Approach

Replace iteration 022's DSH-thread markers with a local append-only ledger
`distilled_insights: []int64` in `knowledge.yaml`. Skip-set = pattern provenance
(iter 020) ∪ ledger. No DSH calls in the skip/record path.

## Test requirements (TDD — written FIRST)

1. **`selectUndistilled` ledger union** — an insight in `kf.DistilledInsights` is
   skipped even when provenance does not cover it; provenance-covered ones still
   skip; a fresh insight is still gathered. (2-arg signature, no `resolved` param.)
2. **`appendDistilledLedger`** records exactly `distillable ∧ ¬provenance ∧
   ¬already-ledgered` IDs (and returns them); excludes provenance-covered,
   already-ledgered, and non-distillable rows; no in-batch dupes.
3. **Existing tests stay green** — `TestSelectUndistilled` (now 2-arg),
   provenance/identity/dedup suites untouched.
4. **Regression:** full `go test ./...` green; `go vet` clean; no DSH-thread
   symbols remain in GML.

## Plan / checklist

1. **`internal/knowledge/knowledge.go`** — add `DistilledInsights []int64
   yaml:"distilled_insights,omitempty"` to `KnowledgeFile`.
2. **`cmd/gml/main.go`:**
   - `distilledSet(kf)` = provenance ∪ ledger; `selectUndistilled(dismissed, kf)`
     uses it (drop the `resolved` param and the DSH thread fetch in
     `cmdDistillGather`).
   - `appendDistilledLedger(kf, dismissed)` — append the residual-gap IDs, return
     them. Replaces `gapInsightsToMark`/`threadMarkerText`/`markDistilledGap`.
   - `cmdDistillApply` — load `knowledge.yaml` once; upsert patterns; append the
     ledger (runs even on empty result); save once if changed. Keep `dismissed`
     fetch (provenance attribution + ledger) and todo posting.
3. **`internal/notify/dsh.go`** — remove `ThreadSummary`,
   `ListResolvedNotificationThreads`, `PostResolvedThread`, the `strconv` import.
4. **Tests** — update `main_test.go` (tests 1–2), revert `dsh_test.go` (drop the
   httptest thread tests), delete `accept_threads_test.go`.
5. **Docs** — `ASSUMPTIONS` GML-087 (+ supersede 084/085/086), `PROJECT`, `README`,
   todo L75 note.

## Verification (run it)

- `go build ./... && go vet ./... && go test ./...`.
- **Real-data:** clone the live `dsh.db` (container `docker cp`), provision a test
  OAuth client + inject one synthetic gap insight on the copy, run a throwaway DSH,
  and exercise the **iter-023 gml binary**: `distill-gather` (finds the gap) →
  `distill-apply` empty result (records it in `knowledge.yaml`, no DSH write) →
  `distill-gather` (now skips it). Confirm `distilled_insights` in the local file
  and no `[thread]`/DSH write. (Live DB untouched; copy discarded.)

## Decisions (→ `ASSUMPTIONS.md`)

- **GML-087 — local distilled-ledger replaces DSH-thread processed-tracking;
  supersedes GML-084/085/086.** No LLM/agent reads threads and the discussion
  feature is unused, so processed-state belongs where its only reader (GML) is.
  The ledger is the minimal explicit state the residual gap requires (provenance
  can't derive it); append-only, local, no DSH dependency, no web-push.

## Out of scope

DSH-side changes (threads remain in DSH, unused by GML); analyze-path dedup; the
deferred LLM cross-linker.
