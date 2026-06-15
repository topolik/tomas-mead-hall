# 024 — Iteration 9: close the tech-vs-judgment fidelity gap

- **Start:** 2026-06-14
- **Trigger:** Tomas — "attack the split of technical vs judgement disproportionate" (tech_preference 80% vs decision_heuristic 38%, iter-8 eval).

## Diagnosis (from the eval disagreements + brain inspection)

**Behavior:** the clone reflexively applies a few broadly-applicable maxims — KISS, "start small & validate", "run it empirically", "don't overcomplicate" — even when Tomas's actual judgment was more specific or opposite:
- non-deterministic build → clone used KISS to *accept* it; Tomas wanted **root-cause investigation** (check the git hash).
- VPN testing → clone said abandon for clean infra; Tomas wanted to **keep the realistic setup to observe behavior**.
- conflict resolution → clone said "start small, then process"; Tomas wanted a **preemptive design fix**.

**Root cause:** the brain has **179 active decision_heuristic insights, all at occurrence=1** — paraphrases were never merged (the deferred semantic-dedup item). The space is flat and bloated, so neither occurrence-weighting nor the whole-profile summary nor top-k BM25 can surface the *situationally-apt* heuristic; the answering LLM defaults to the maxims that match almost any prompt. The apt heuristics (root-cause, simplify, preserve-realistic-setup) exist but are lost in the flat 179.

## Attack (two levers, measured against the eval)

1. **Ask-side (this iteration, cheap, fast to measure):**
   - Ask prompt: add a judgment-selection rule — identify what KIND of move fits THIS situation (root-cause vs design-fix vs simplify vs just-run-it) and apply it; do NOT reflexively default to broadly-applicable maxims when a specific judgment fits.
   - Retrieval: raise ask top-k so the apt heuristic is more likely in context.
2. **Structural (likely iteration 10): semantic insight dedup** — merge the 179 paraphrases so occurrence counts mean something and the profile/retrieval can prioritize. The eval reclassified this from "polish" to load-bearing.

## Measurement (the eval is the judge)
- Fast loop: re-ask + re-judge the existing 8 decision_heuristic cases (baseline 37.5%) with the changed brain — directional signal, ~9 LLM calls.
- Honest proof: a FRESH full `mnd eval` after the change — confirms decision_heuristic rises AND tech_preference/others don't regress (a judgment-prompt change could hurt the 80%). Net fidelity must not drop.

## Risk
- Overfitting to the 8 known cases → the change is principled (targets the failure *pattern*, not the cases); validated on a fresh sample.
- Regression on tech_preference → caught by the full-eval regression check; the new rule is scoped to "judgment calls."

## RESULT — ask-side lever FAILED (controlled A/B, same 43 cases via `eval-rerun`)

| category | iter-8 | iter-9 (ask-side) | Δ |
|---|---|---|---|
| decision_heuristic (target) | 38% | **31%** | **−6** |
| correction_pattern | 67% | 57% | −10 |
| direction_pattern | 57% | 67% | +10 |
| tech_preference | 80% | 80% | 0 |
| **overall** | 59% | 58% | −1 |
| confidence dist. | 100% high | 100% high | unchanged |

The prompt rule + top-k bump **hurt the target category** and only reshuffled wins between categories (net −1, within noise). The confidence-honesty nudge had **zero effect** — still 100% `high`. **Reverted** (`ask.go`/`main.go` back to iter-8); kept only the `eval-rerun` A/B tool.

## What the negative result tells us (the real finding)
1. **The judgment gap doesn't yield to prompt-tweaking** — telling the answering LLM "pick the right heuristic" doesn't make it judge like Tomas. Likely structural/fundamental: concrete *preferences* distill (tech 80%); contextual *judgment* (root-cause-vs-accept, design-vs-process) depends on situational reading that generic maxims can't reconstruct.
2. **LLM self-reported confidence can't be prompt-fixed** — it stays uniformly high. To get a real signal it must be **computed** (from retrieval strength/agreement), not asked.

## A/B of the two structural levers (Tomas: "try first and second as a/b test again")

Both measured on the same 43 cases via `eval-rerun` / `eval-calibration`:

**A — computed confidence from retrieval: FAILED.** Retrieval signals are identical for right vs wrong (topScore ~24–25, nStrong ~10 across agree/partial/disagree) — even on the deduped brain. Root cause: 800+ insights ⇒ every question pulls ~10 strong matches; the brain is *saturated*, so retrieval density can't discriminate (same reason the LLM always says "high").

**B — semantic dedup: FAILED, and hurt the target.** Merged 104 duplicates (842→738), but decision_heuristic dropped **38%→25%** (overall 59%→56%). Giving the loud heuristics (KISS/run-it) more occurrence weight made the clone reach for them harder — the amplification risk, confirmed. Calibration stayed flat too.

**Conclusion (3 attempts, all failed): the tech-vs-judgment split is not closable by prompt / retrieval / representation tweaks in this architecture.** Concrete *preferences* distill and replay well (tech 80%); contextual *judgment* depends on situational reading that distilled-insight + retrieve + LLM-answer cannot reconstruct. It's a structural ceiling, not a tuning miss.

## Recommended path (for Tomas to steer)
The clone is **trustworthy on preferences (80%), not on judgment (38%)**, and can't tell which is which (confidence broken). The product-correct response to "make me redundant" is **safe redundancy, not forced fidelity**:
- **(A) Competence-boundary routing + computed confidence (recommended):** derive confidence from retrieval (strong+agreeing insights ⇒ high; sparse/conflicting ⇒ low), and/or treat `decision_heuristic`-shaped questions as escalate-by-default. The orchestrator auto-answers where it's reliable and escalates judgment calls to Tomas — redundant where safe, in-the-loop where not.
- **(B) Semantic dedup (brain hygiene, uncertain for this gap):** merge the flat-179 so retrieval/profile can prioritize. Helps cleanliness; may or may not lift judgment fidelity (the failure is application, not frequency).
- **(C) Accept the limit:** chasing judgment fidelity toward 80% may be a losing battle; lean fully on (A).

## Updated recommendation (after both levers failed)
Stop chasing judgment fidelity — three attempts failed, one made it worse. The eval reliably tells us WHERE the clone is trustworthy (tech 80%, correction 67%, direction 57%) vs not (judgment 38%). **Competence-boundary routing by question category** is the safe, honest answer: classify the incoming question, auto-answer the high-fidelity categories, and **escalate judgment-shaped questions to Tomas**. The router must be the *question's category* (cheap LLM classify), NOT retrieval-confidence (lever A proved that doesn't discriminate). This makes the clone redundant where it's measured-safe and keeps Tomas in the loop exactly where it isn't — which is what "make me redundant" actually needs.

## Shipped this iteration (despite no fidelity gain)
Reusable measurement + hygiene tooling, all tested (65 Go tests): `eval-rerun` (controlled A/B on fixed cases), `eval-calibration` (retrieval-signal vs verdict), `mnd dedup` + `internal/dedup` (works — merged 104 — but **do NOT auto-run on production**: it amplifies loud heuristics and lowered judgment fidelity in testing). Plus the headline finding: the tech-vs-judgment ceiling is structural → pivot to competence routing.


