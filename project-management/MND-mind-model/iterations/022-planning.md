# 022 — Planning (Iteration 8: fidelity eval)

- **Start:** 2026-06-14
- **Phase:** Planning (TDD — tests listed before code)
- **Status:** DRAFT for Tomas's review (no implementation until sign-off)

## Design — a 4-stage file-in/file-out pipeline (LLM host-side, MND-003)

```
moments.jsonl ─► eval-build ─►(LLM)─► eval-cases.jsonl ─► eval-ask ─►(LLM ×N, the clone)─►
   answered.jsonl ─► eval-judge ─►(LLM)─► scored.jsonl ─► eval-report ─► data/eval/report.md
```

New package `internal/eval`; subcommands `mnd eval-build | eval-ask | eval-judge | eval-report`; orchestrated by `run-task.sh eval`. Same division as distill: LLM does judgment, Go does selection/parsing/aggregation.

### Stage 1 — `eval-build` (select + frame ground-truth cases)
- Deterministically sample `MND_EVAL_N` (default 40) candidate decision moments from `moments.jsonl` (stable: sort by id, stride-sample — reproducible, no `Math.random`).
- One batched LLM call: for each candidate `(context, Tomas-text)`, emit an eval case `{id, category, situation, gold_decision}` OR mark `skip` (not a clean decision — chit-chat, pure task mechanics). Datamarked input (untrusted), schema-validated output.
- Output `data/eval/cases.jsonl` (only the kept, clean cases).

### Stage 2 — `eval-ask` (the clone answers blind)
- For each case, build the ask prompt from the **situation only** (Tomas's gold decision is never in the prompt) over the brain, run the clone (`ask`), capture `{id, answer, confidence, citations}`.
- N LLM calls (the clone answering — inherent). Capped by N. Writes `data/eval/answered.jsonl`.

### Stage 3 — `eval-judge` (independent agreement scoring)
- Batched LLM call(s): given `(situation, gold_decision, clone_answer)` per case, score `verdict ∈ {agree, partial, disagree}` + one-line `reason`. The judge sees gold + clone answer but is a separate pass from the answering (no self-grading in one shot). Datamarked. Resilient per-item parse. Writes `data/eval/scored.jsonl`.

### Stage 4 — `eval-report` (Go aggregation, no LLM)
- `data/eval/report.md`: overall agreement (`agree` + ½·`partial` / total), per-category table, **confidence-calibration table** (agreement rate within each of high/medium/low buckets — the check that low-confidence really means "would be wrong"), and the **full disagreement list** (situation, gold, clone answer, judge reason) — the actionable core.
- Also emit `report.json` for trend tracking across runs.

### Leakage handling (the planning decision)
- **v1 default `--in-sample`**: test the current production brain; the report header labels it *"in-sample — optimistic upper bound."* Cheap (~N+4 LLM calls), immediate, and the disagreement list is fully valid (real failures regardless of leakage).
- **`--held-out` (flag, rigorous):** `eval-build` reserves the newest K decision moments as the test set; a one-off **eval-brain** is distilled from the train remainder (`distill` into `data/eval/brain/`), and `eval-ask` answers from that brain. Unbiased, but costs a full train-split distill.
- **Proposed:** ship both; run `--in-sample` now for a fast signal, run `--held-out` once to measure the leakage gap. ← **confirm with Tomas.**

## Tests (TDD — written first)
| ID | What | How |
|----|------|-----|
| T50 | `eval-build` parse: valid cases kept; `skip`/empty-gold/unknown-category dropped; per-item resilient | Go unit |
| T51 | deterministic sampling: same moments+N ⇒ same case ids (reproducible runs) | Go unit |
| T52 | blind-ask prompt **never contains the gold decision** (leak guard) | Go unit |
| T53 | `eval-judge` parse: agree/partial/disagree pass; junk verdict ⇒ `disagree` (conservative); resilient | Go unit |
| T54 | `eval-report` aggregation: overall score math, per-category counts, calibration buckets, disagreement list completeness | Go unit |
| T55 | report flags `in-sample` vs `held-out` provenance in the header | Go unit |
| T56 | live: full `mnd eval` over the real brain — produces a report with a number + non-empty disagreement list; sanity-read the disagreements | live |

## Risks / mitigations
- **Judge is itself an LLM** → scores are approximate. Mitigation: conservative parsing (junk⇒disagree), one-line reasons surfaced so Tomas can spot-check; the disagreement list is human-readable, not just a number.
- **Cost**: ~N+4 calls/run (N=40 → ~44). Mitigation: N is a knob; eval is periodic, not per-request; judge batched.
- **Not-a-decision moments** pollute the set → `eval-build` skips them; T50 covers it.
- **Leakage** inflates the in-sample number → labeled honestly; held-out flag available.

## Deliverables
`internal/eval` (+tests), 4 subcommands, `run-task.sh eval`, a committed sample `report.md`, ASSUMPTIONS MND-031 (fidelity eval design + leakage stance), SKILLS entry. Code only on the branch → review-gated merge (the eval *report* is data, not committed as production brain).
