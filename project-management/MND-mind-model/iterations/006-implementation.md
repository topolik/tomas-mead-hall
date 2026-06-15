# 006 — Implementation (Iteration 2: retraining + self/other discrimination)

- **Start:** 2026-06-12
- **End:** 2026-06-12
- **Phase:** Implementation

## What was built

- `internal/exclude` — the self/other discriminator (turn-level, layered): datamark fingerprint (U+E000 — our own injection defense doubles as a self-marker), pipeline phrase markers, orchestrator send-ledger (sha256 of normalized text). Includes a **shell↔Go hash-parity test** pinning both implementations to the same digest.
- Extract: `Options{LedgerPath, ExcludeGeminiProjects}`; drops self-content before anything else (new stat: `self=N`); gemini pipeline working dirs (MND, GML) excluded wholesale.
- Processed-moments ledger (`brain/processed.yaml`): distill-prompts skips processed IDs; distill-merge appends a batch's IDs only after its response merges successfully (`.ids` sidecars). Fixes iteration-1's re-sending of silent moments.
- `orchestrate.sh --send` now ledgers every delivered direction to `brain/sent-ledger.jsonl`.
- `run-task.sh retrain` (incremental learn; profiles regenerate only when insights.yaml hash changes; `MND_GEMINI_MODEL` defaults to gemini-2.5-pro for the 300KB+ profile prompt) and `watch-retrain` (daemon, `MND_RETRAIN_INTERVAL`, default 24h).

`go test ./...`: **41 tests passing** in 10 packages.

## Run log (real data — T22–T27)

- **Bootstrap:** seeded `brain/processed.yaml` with all 1853 iteration-1 moment IDs (they had all been through distillation passes 1+2).
- **Extract with discrimination (T22–T25):** 1874 moments, **self=9** — today's MND pipeline prompts (datamarked distill/profile content, the `<terminal-tail>` orchestrate question) correctly dropped; 21 genuinely new moments (Tomas's turns from today's sessions) admitted.
- **Retrain run 1 (T27):** 1 batch (25 unprocessed moments) → 12 new insights → **799 total**; ledger += 25; profiles auto-regenerated through validation (315KB prompt, pinned model, headers present).
- **Retrain run 2:** picked up the single moment generated between runs, distilled, regenerated.
- **Retrain run 3 (convergence):** "skipping 1875 already-processed moments … nothing new to distill … profiles unchanged." Self-stabilizing: retraining's own session residue is excluded by construction.
- **T28 (send-ledger):** unit-tested end-to-end (ledger write format ↔ extract drop, hash parity shell↔Go). The append-on-send path is live in orchestrate.sh; the next real orchestrated session validates it organically — noted for iteration-3 verification.

## Iteration 3 (planned, per Tomas's directive 4)

DSH low-confidence feedback loop — when `orchestrate.sh` refuses (`confidence: low`), post the question + proposed answer as a DSH notification; Tomas's comment comes back through a learn step as corrective insights (GML-insights pattern). Also: datamark terminal tails (MND-013 hardening), validate T28 against a live orchestrated session.

## Decisions

### Bootstrap processed ledger from the existing corpus rather than re-distilling
**Date:** 2026-06-12
**Phase:** Implementation
**Decided by:** Team
**Decision:** Seed processed.yaml with all 1853 iteration-1 moment IDs.
**Reasoning:** All of them went through distillation in passes 1+2; re-sending ~930 evidence-uncited moments would cost ~23 LLM calls for content the brain already saw.
**Revisit if:** Evidence emerges that pass-1/2 batches were under-extracted (then drop selected IDs from the ledger to re-queue them).

### Profile regeneration gated on insights.yaml content hash
**Date:** 2026-06-12
**Phase:** Implementation
**Decided by:** Team
**Decision:** retrain regenerates profiles only when the brain actually changed.
**Reasoning:** Profile generation is the most expensive and fragile LLM step (300KB+ prompt, pinned model); skipping it on no-op runs makes the daemon cheap.
