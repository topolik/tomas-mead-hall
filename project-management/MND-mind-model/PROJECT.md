# MND — Mind Model

- **Code:** MND
- **Status:** Iteration 11 ready for review — embedding retrieval + evidence gate. 91% fidelity, 57% coverage, 0 judgment leaks. LLM classifier eliminated.
- **Priority:** Q2
- **Lead:** Developer
- **Created:** 2026-06-12
- **Last updated:** 2026-06-16
- **Current phase started:** 2026-06-16

## Overview
Distills Tomas's decision-making "brain" from his Claude + Gemini session history into readable Markdown profiles and an evidence base, then exposes it through an orchestrator command (`mnd ask`) that answers agent questions — directions, priorities, corrections — the way Tomas would. End goal: herdr agents get Tomas-style steering without interrupting Tomas.

## Architecture
```mermaid
graph LR
    C["~/.claude/projects<br/>session JSONL"] --> E["mnd extract<br/>(decision moments)"]
    G["~/.gemini/tmp<br/>session JSON"] --> E
    E -->|redacted moments| D["mnd distill<br/>(LLM batches)"]
    D -->|insights + evidence| P["mnd profile<br/>(LLM merge)"]
    P --> B["data/<br/>profiles *.md + insights.yaml"]
    H["herdr agents"] -->|question| EMB["embed<br/>(Ollama GPU)"]
    EMB -->|cosine top-k| GATE["evidence gate"]
    GATE -->|"auto: safe + high similarity"| A["mnd ask<br/>(embedding top-k + LLM)"]
    GATE -->|"escalate: judgment / sparse"| DSH["DSH escalation"]
    B --> A
    A -->|Tomas-style direction<br/>+ evidence citations| H
```

## Current State

**Merged to master (iterations 1–8):**
- Iter 1: full corpus distilled (787 insights / 1853 moments), profiles v2, live herdr orchestration loop
- Iter 2: self-excluding retraining (turn-level discrimination, datamark, phrase markers)
- Iter 3: DSH low-confidence feedback loop (escalation → comment → corrective insight)
- Iter 4: LLP gateway routing + watch mode (auto-answer blocked/idle agents, loop protection)
- Iter 5: contradiction resolution (three-way verdict), attribution prefix `[MND orchestrator]`
- Iter 6–7: loop-until-dry contradiction sweep, eval-brain deferred, retrain daemon fixes
- Iter 8: fidelity eval (`mnd eval`) — 59% in-sample, confidence non-discriminating (100% high while 41% wrong), tech_preference 80% vs decision_heuristic 38%

**Merged to master (iterations 9–10):**
- Iter 9: three attacks on the tech-vs-judgment gap all **failed** (ask-side prompt, computed confidence from retrieval, semantic dedup). Conclusion: the split is a structural ceiling. Shipped: `eval-rerun` A/B tool, `eval-calibration`, `mnd dedup`. Reverted all fidelity attempts.
- Iter 10: **competence-boundary routing** — classify incoming question by category (cheap LLM), auto-answer where measured-safe, escalate the rest. Default policy: `correction_pattern,direction_pattern` → 87% fidelity at 38% coverage, 0 judgment leaks. Mandatory post-retrain fidelity eval with 75% threshold.

**On branch, pending review (iteration 11):**
- Iter 11: **embedding retrieval + evidence gate** — replaces BM25 + LLM classifier with Ollama GPU (nomic-embed-text, 768-dim) semantic retrieval. Evidence-derived routing inspects retrieval metadata (no LLM call). tech_preference re-included → 91% fidelity at 57% coverage, 0 judgment leaks. LLM calls per question: 2→1.

**Deferred:** held-out eval-brain validation (MND-031).
