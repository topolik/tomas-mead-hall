# GML — Iteration 023: Ideation — Simplify processed-tracking off DSH threads

- **Phase:** 0 — Ideation
- **Phase lead:** Developer
- **Start:** 2026-06-14
- **Input:** post-merge review of iteration 022 (threads processed-tracking)

---

## Trigger

Reviewing iteration 022 (merged), Tomas asked: *"Are the threads useful alone?
Does ever any LLM look at them?"* — then noted the only existing thread
(`#1296`, `created_by=tomas`) was tomas-only with no agent interaction.

## Finding (verified in code)

- **No LLM reads DSH threads.** The distill prompt is built from dismissed
  notifications + `knowledge.yaml` patterns only (`BuildDistill`); thread content
  never enters any prompt. The deferred "LLM cross-linker" that would read/group
  threads is unbuilt (zero cross-link code in DSH).
- **GML's iter-022 skip-check reads only `ref_id` + `status`** — never the thread
  body. It was using the threads table as a key-value "processed" flag.
- **DSH Threads has no real discussion usage** — no agent has ever held a
  back-and-forth; the one human thread is tomas-only.

So iteration 022's markers were "a flag wearing a thread costume": degenerate
one-message threads, read by nobody as text, that also cluttered the human
`/threads` view and (on first run) would web-push per marker. DSH Threads'
discussion value is currently unrealized; its only *functioning* consumer is the
GML processed-flag — which needs a boolean, not an M:N discussion system.

## Decision: simplify to a local ledger

Move GML's "already distilled" state **off DSH threads** into a local ledger in
`knowledge.yaml` (`distilled_insights: [ids]`). Skip-check becomes provenance
(iter 020) ∪ that ledger. This:

- removes the thread dependency entirely (no DSH calls in the skip/mark path —
  distill gets faster and more robust);
- puts the state where its only reader (GML) is — no DSH-side data nobody reads;
- no web-push, no `/threads` clutter;
- keeps the gap closed for the cases provenance can't derive (todo-only /
  distilled-to-nothing / non-matching query).

## Options considered

- **Local ledger in `knowledge.yaml`** — **chosen.** Simplest; GML-only; the file
  GML already owns.
- **`distilled_at` field on the DSH notification** — properly-shaped vs a thread,
  but still a cross-project change putting state in DSH that only GML reads;
  rejected given the finding that DSH-side processed-state has no consumer.
- **Drop gap-tracking entirely (provenance only)** — the real-data run showed the
  current residual gap is 0, so this was viable; rejected because Tomas chose to
  keep gap-closing, and the ledger is cheap/forward-preventive.

## Tension with GML-020

GML-020 said "no separate ledger to drift — derive 'distilled' from provenance."
The residual gap (insights producing no pattern) **cannot** be derived from
provenance, so closing it needs explicit state. The ledger is the minimal such
state: append-only, low-risk (a stale entry just means an insight isn't
re-distilled — the goal), and local. Recorded as GML-087, superseding the
threads-based GML-084/085/086.

## Scope

- **In:** remove GML's thread usage; add the `knowledge.yaml` ledger; skip + record.
- **Out:** DSH-side changes (threads stay in DSH, just unused by GML); the analyze
  path; the deferred cross-linker.
