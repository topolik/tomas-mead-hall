# Iteration 019 — One archive rule per sender: fold at propose, deterministic guard at apply

**Started:** 2026-06-09
**Phase:** Implementation

## Goal

Tomas reported "multiple conflicting plans but the conflict-finding feature doesn't find them."
Investigation reframed the problem twice and landed on a safety invariant.

### What the investigation found (empirical)

- The conflict-detection LLM **works**: rebuilding the exact merge prompt from the current DB
  (29 approved plans after superseding) and running it through gemini returned the 4 real
  conflicts ([59,106] snyk, [68,81,83,103] socradar, [99,104] secops, [56,66,91] confluence),
  and the real output passes `ParseAndValidateMerge` cleanly. So the LLM/prompt/parser/dedup are
  fine.
- The conflicts never reach the DSH conflicts tab because the merge/conflict step runs **only**
  in the manual `apply-rules` flow — no daemon invokes it — and `rules.yaml` had not been
  regenerated since the relevant plans were approved.
- **Live safety bug.** Two `archive_by_sender` rules for one sender apply independently and
  **union** at the mailbox, so `-Critical` ∪ `-VIP` archives ~everything. The Jun-3 `rules.yaml`
  already had such pairs — most dangerously **snyk had a no-filter catch-all (#54)** archiving
  *all* Snyk mail, plus confluence (`"Meeting notes"` ∪ `-"mentioned you"`) and grafana.

### Reframing with Tomas

1. Don't clean up conflicts among approved plans — **prevent** creating a conflicting plan.
2. Plans trace back to LLM insights; the same sender accrues differently-worded insights over
   time, and `knowledge.yaml` keys entries by `gmail_search`, so each becomes a distinct plan →
   distinct rule. The fix is the invariant **one archive_by_sender rule per sender**, folding new
   constraints in (AND for exclusions, OR for inclusions, ambiguous → human-reviewable).
3. **KISS:** reconciliation was happening twice (propose dedup gate + merge/conflict LLM). If
   propose folds, the merge LLM is redundant in the cycle. Collapse to one reconciliation
   (propose) + a **deterministic** per-sender guard at apply (stronger than a non-deterministic
   LLM that can slip). Nothing deleted — the merge cluster stays as a manual diagnostic.

## What was built

### Change 1 — Fold at propose (the single reconciliation LLM step)
- `internal/prompt/propose_dedup.go`: `BuildProposeDedup` → **`BuildProposeReconcile`**. Enforces
  one-rule-per-sender; for each candidate decides NEW / DUPLICATE / FOLD. A FOLD emits one
  combined proposal with the folded filter and `knowledge_ref:"merge_conflict:[ids]"` citing the
  plan ids it supersedes (reuses the existing supersession marker).
- `cmd/gml/main.go` `cmdProposeGather`: now passes `cfg.Rules` (live `rules.yaml`) to the gate via
  new `formatExistingRules`, so folding is against current archiving reality, not just DSH plans.
- `cmdProposeApply`: logs folds and their superseded ids. A folded plan's `merge_conflict:` marker
  is consumed by the existing `superseded` logic in `cmdApplyRules` on approval.

### Change 2 — Deterministic per-sender guard at apply (the backstop)
- `internal/propose/propose.go`: `SameSenderConflicts(rules []config.Rule) map[string][]string`
  (pure detector; senders in ≥2 archive rules with distinct `CanonicalQuery` filters) and
  `GuardSameSender([]AnnotatedRule)` (strips offending senders from all rules, drops emptied
  rules, preserves clean co-senders).
- `cmd/gml/main.go`: `guardSameSender` wrapper runs in **both** `cmdApplyRules` and
  `cmdMergePlansApply` before `BuildGeneratedRules`, withholding the footgun from `rules.yaml`
  and reporting what it withheld. Makes the OR-union impossible to write even if the LLM slips.
- Tests: `internal/propose/samesender_test.go`.

### Change 3 — Deterministic apply wired into the knowledge cycle
- `run-task.sh`: extracted `run_apply_rules()` (no-LLM; wraps `gml apply-rules`; `return` not
  `exit`, no `trap`, explicit `rm -f`). Wired into the `apply-rules --no-llm` command and added as
  **step 4/4** of `watch-knowledge` (renumbered 1/4…4/4, banner updated). The LLM
  `apply-rules --model` path stays as a manual diagnostic, untouched.

### Change 4 — `rules` daemon hot-reloads rules.yaml
- `internal/scheduler/scheduler.go`: `Run` takes a `rulesPath`; each tick reloads
  `config.Load(rulesPath)`, swapping in the fresh config (recomputing `dryRun`) and falling back
  to the last good config on a read error (guards against a half-written file). `cmd/gml/main.go`
  `cmdServe` passes the path. Regenerated rules now apply with no `./watch.sh restart rules`.

## Net effect

Fully hands-off under `./watch.sh start`: the `knowledge` daemon proposes **folded** (one-per-
sender) plans and regenerates `rules.yaml` deterministically each cycle; the `rules` daemon
hot-reloads it. The only human touchpoints stay in DSH (dismiss/comment insights, approve plans).

## Backlog cleanup (Change 5, falls out of the above)

The current `rules.yaml` footguns (snyk/socradar/confluence) are **withheld** by the guard on the
first apply (fails safe — over-archiving stops immediately). All backlog senders are still in
`knowledge.yaml` (socradar 14 refs, snyk 5, confluence 8, grafana 6, secops 6), so the reconcile
gate will re-propose them as **folded** plans on the next cycle → Tomas approves → `rules.yaml`
regenerates one-rule-per-sender. No manual `apply-rules` run required.

**Behavior-change note to surface:** until the folded plans are approved, the withheld senders get
*no* archiving rule — mail that was being (over-)archived will stay in the inbox. That is the
intended safe direction (important Critical/VIP alerts were being archived by the OR-union).

## Run log

- `go build ./...` — OK. `go test ./...` — all packages pass (new `samesender_test.go` green).
- `bash -n run-task.sh` — OK.
- `docker compose build gml` — (rebuilt this iteration).
- Empirical pre-work: gemini on the live merge prompt → 4 conflicts; `ParseAndValidateMerge` on
  that output → 17 merged_rules, 4 conflicts, no error; confirmed `rules.yaml` over-archives snyk.

## Decisions

- **Fold, don't keep two rules** (Tomas): two same-sender archive rules union and over-archive;
  the only correct shape is one rule per sender. A folded plan *replaces* the current rule on
  approval.
- **Prevent at propose, not clean up at merge** (Tomas): conflicts shouldn't be created in the
  first place.
- **KISS — one reconciliation, deterministic guard:** dropped the merge LLM from the cycle and the
  durable-rejected-conflict-suppression machinery from the earlier plan (no conflict plans posted
  in the cycle → nothing to suppress). Unwired, not deleted — the merge cluster shares only
  `parseConflictPlanIDs` (in `main.go`, kept) with the deterministic path.
- **Deterministic guard over LLM-only:** the OR-union footgun is too sharp to rest on a
  non-deterministic step; the guard makes it impossible to write to the mailbox.
