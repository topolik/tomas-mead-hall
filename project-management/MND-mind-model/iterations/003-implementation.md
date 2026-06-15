# 003 — Implementation

- **Start:** 2026-06-12
- **End:** 2026-06-12
- **Phase:** Implementation

## What was built

`projects/MND-mind-model/` — Go CLI (`mnd`) + Docker + `run-task.sh`, following the GML pattern (Go steps containerized, LLM calls host-side with prompt files).

| Package | Purpose | Tests |
|---|---|---|
| `internal/claude` | Session JSONL parser — human turns only, assistant context attached | T1–T4 |
| `internal/gemini` | chats/session-*.json + logs.json fallback parser | T5–T6 |
| `internal/extract` | Walk both trees, noise filter, near-dup collapse, redact, sort | T7–T8b |
| `internal/redact` | Secret-shape redaction with kind-tagged markers | T9–T10 |
| `internal/distill` | Batching, datamarked prompts, resilient per-item validation, identity-keyed merge | T11–T14 |
| `internal/brain` | insights.yaml persistence, profile prompt/writer | T15 |
| `internal/ask` | BM25 retrieval, ask prompt, answer validation | T16–T17 |

`go test ./...`: **33 tests passing** in 9 packages. Image builds with tests as a build gate (GML pattern).

## Run log (real data — T18–T21)

### Extract (T18) — PASS
- First run: 2916 moments (claude files=239, gemini chats=1204, logs=4), noise=541.
- Leak checks: tool_result leakage **0**, raw secret prefixes **0**, redaction markers 13 (working), system-reminder leakage **0**.
- **Bug found via real data:** 852 moments in one project were piped `--- FILE:` payloads from a batch script; 472 more were templated audit-loop prompts. Machine-fed content arriving as "user" turns would have skewed distillation.
- **Fix:** machine-payload prefixes treated as noise + global near-dup collapse (normalized 200-char key). Tests T7b/T8b added first, then implementation (MND-012).
- Final corpus: **1853 moments** (claude 922 / gemini 931), dup=1014 collapsed, noise=590.

### Distill (T19) — PASS (after one environment fix)
- First attempt: all 3 batches failed — gemini-cli 0.29.5 rejects `--approval-mode plan` without `experimental.plan`. Resilient merge handled it exactly as designed (batches skipped, nothing corrupted). **GML's gemini invocations use the same flag — flagged for review.**
- Fix: drop the flag for MND's text→JSON prompts (MND-011). Did NOT enable experimental flags in Tomas's global gemini settings.
- Re-run: 3 batches (~120 moments), **36 insights accepted, 0 dropped**. Examples:
  - *"Demand strict verification of record counts to prevent silent data omission"* (correction_pattern, evidence: 2026-03-04 session)
  - *"Drop calculated fields if their derivation is unreliable; do not present guesses as facts"*
  - *"Validate tools and evaluate output quality on a minimal sample before executing bulk batch operations"*

### Profile (T20) — PASS
- 36 insights → 3 profiles, every line carries insight-ID citations, weak single-occurrence insights marked "(tentative)".

### Ask (T21) — PASS
- Q: *"I'm an agent processing 5000 security findings in batches. First batch looks good. Run the whole job overnight?"*
- A: *"Do not run the bulk batch job overnight yet. First, write a batch-processing execution plan to a local state file so you can resume… validate output quality on your first batch sample, verify the record counts… establish programmatic ID-matching to mathematically prove completeness."* — confidence: high, 4 citations resolving to real session evidence.
- JSON mode + silence handling: React-vs-Vue question → `confidence: low`, empty citations, Tomas-consistent KISS extrapolation explicitly labeled as such. Agents are told to treat `low` as "ask the human."

## Bugs found & fixed
1. Machine-fed payloads polluting corpus (above) — fixed with tests.
2. gemini-cli approval-mode incompatibility (above) — fixed, GML impact flagged.
3. Datamarker constant initially written as empty string (U+E000 is invisible) — caught by test T11, fixed with explicit `` escape… visible in git as the marker char.

## Full-corpus run (2026-06-12, after Tomas's review: "continue until all is done")

- **Distill pass 1:** 46 batches, 45 OK + 1 transient gemini routing failure → 448 insights (484 total). ~2h with throttling, progress reported every 10 min per Tomas's request.
- **Distill pass 2 (retry):** re-batched the failed batch + previously-uncited moments → 33 batches, 0 failures → 303 more insights. **Final brain: 787 insights** (direction 238 / tech 222 / heuristic 166 / correction 161; 921 of 1853 moments cited as evidence).
- **Incident — gemini wrote into the repo:** the 304KB profile prompt broke gemini-cli's auto-routing classifier; retried with pinned `gemini-2.5-pro`, which went into agent mode and **wrote the three profile files itself** — `-e none` disables extensions but NOT built-in tools, and the user-level `defaultApprovalMode: auto_edit` auto-approved `write_file`. Caught because the files lacked the `mnd profile-write` header. Quarantined, fixed (`--approval-mode default` = auto-deny in non-interactive), regenerated through validation (MND-011 revised).
- **Profiles v2:** 189 lines, 97 insight citations; the profile LLM grouped paraphrase variants into multi-citation lines, recovering the repetition signal that exact-match identity keys miss.
- **Ask re-test (same SAST question):** answer kept all pilot directives and added autonomous batch-script execution + intermediate progress files — confidence high, 7 citations (vs 4 on the 6% brain).
- **Ask validity probe:** asked the orchestrator the *exact* decision Tomas made this morning ("fix GML's broken flag now or stay on task?"). **It contradicted him** — said "fix right away, don't ask" (citing his autonomy/root-cause bias), while real Tomas said "focus on the task at hand, GML works, fix later." The brain captures his drive, not yet his scope discipline. Prime target for iteration 2: feed actual decision outcomes back as corrective insights.

## Live herdr orchestration test (2026-06-12, Tomas: "test it live through herdr")

- Built `orchestrate.sh`: herdr pane tail → mind model → direction back into the pane. Dry-run default; `--send` delivers; refuses on `confidence: low` (MND-013/014).
- Reviewed `hwt` (Tomas's worktree+agent launcher): compatible out of the box — `hwt <branch>` spawns the agent pane, `orchestrate.sh <pane>` directs it. `HWT_AGENT` env override noted for future orchestrated variants.
- **Live test:** scratch worktree `mnd-live-test` (no-focus), claude agent launched via `herdr pane run`, tasked to ask one storage question and wait.
  - Agent asked: *"Should the new agent service's execution logs be stored in a database or in flat files — and what is the main reason?"*
  - Brain answered (high confidence, 4 citations): *"Store the execution logs in flat files. This keeps the project strictly self-contained, follows the KISS principle, and avoids introducing unnecessary external database dependencies."*
  - Agent confirmed: *"OK: Execution logs will be stored in flat files to keep the project self-contained and simple (KISS)…"* — loop closed.
- herdr API notes: `pane read` returns raw text, `agent read` returns a JSON envelope (orchestrate resolves pane first and uses `pane read --source visible`); `recent-unwrapped` can be empty right after status changes.
- Cleanup: test worktree removed via `herdr worktree remove`, branch deleted.

## Limitations (iteration 1, by design)
- Insight occurrences are all 1 — paraphrase variants evade the exact-match identity key; semantic dedup (GML-style LLM dedup pass) is the iteration-2 fix. The profile generator partially compensates by grouping variants at generation time.
- `--skip-insights` infers "processed" from evidence citations, so signal-free moments get re-sent on every distill run (pass 2 was 33 batches for this reason). Iteration 2: persist a processed-moments ledger.
- Profiles regenerate wholesale; Tomas's manual edits don't survive regeneration (correct via review feedback or insights.yaml — header says so).
- Orchestrator knows general patterns but not situational priority calls (see validity probe above).

## Decisions

### Drop --approval-mode plan for MND LLM calls
**Date:** 2026-06-12
**Phase:** Implementation
**Decided by:** Team
**Decision:** Call gemini-cli without approval-mode; do not enable `experimental.plan` globally.
**Alternatives considered:** Enabling experimental.plan in ~/.gemini/settings.json (rejected — global side effect on Tomas's environment for zero benefit to text-only prompts).
**Reasoning:** MND prompts are pure text→JSON; no tools are involved. Touching Tomas's global settings for this would be a side effect outside project scope.
**Revisit if:** MND prompts ever need tool use, or gemini-cli changes non-interactive defaults.

### Machine-payload filter + near-dup collapse in extract
**Date:** 2026-06-12
**Phase:** Implementation
**Decided by:** Team
**Decision:** Treat `--- FILE:` dumps and harness notices as noise; collapse near-duplicate texts globally keeping the first exemplar.
**Alternatives considered:** Per-project moment caps; LLM-side filtering only.
**Reasoning:** 35% of the corpus was mechanical repetition — cheap deterministic filtering beats spending LLM budget on it. The distiller's "skip task mechanics" instruction remains as the second line of defense.
**Revisit if:** Near-dup collapse eats legitimately repeated direction patterns (the repetition signal then lives in occurrence counts instead — which the collapse currently suppresses; acceptable for iteration 1).
