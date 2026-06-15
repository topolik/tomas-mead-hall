# Iteration 021 — Implementation: insight dedup (identity-keyed update / skip / re-surface)

**Started:** 2026-06-09
**Phase:** Implementation

## What shipped

**Part A — deterministic identity update (no LLM), kills the visible 3-dup bug**
- `internal/notify/identity.go` (new): `InsightIdentityKey(senders, category)` (sorted, lowercased,
  deduped + `|category`); `ParseInsightIdentity` / `identityFromMessageLink` (category from
  `[Insight: <cat>]`, senders from `from:` tokens in the decoded Link; `ok=false` with no sender);
  `ClassifyInsights(candidates, existing) → (posts, updates, skips)` — structural floor skip,
  active-identity match → update, else post. Candidate keys derived symmetrically through the
  rendered notification (`InsightToNotification` → `identityFromMessageLink`).
- `internal/notify/insights.go`: extracted `InsightToNotification(r)` (shared by post + update).
- `internal/notify/dsh.go`: `UpdateNotification(id, message, link, priority)` → `PATCH`.
- `cmd/gml/main.go` `cmdInsights`: classify → execute (skip / PATCH-update / post); on a 404 from a
  raced dismissal, fall back to posting as new.

**Part B — re-surface genuine refinements of dismissed insights (2b)**
- `internal/prompt/dedup.go`: `BuildInsightDedup` — drops reworded duplicates of dismissed insights,
  keeps genuinely-new same-topic ones with an `Update:` pattern prefix. Strict analyze `BuildDedup`
  left untouched.
- `cmd/gml/main.go`: `insight-dedup` verb (`dedupCmd(prompt.BuildInsightDedup)`; `cmdDedup` now
  shares the body via `dedupCmd(build)`).
- `run-task.sh` `run_learn`: inserted the `insight-dedup` LLM stage ([3/4]) before posting,
  mirroring the analyze dedup wiring.

**Part C — grouping discipline**
- `internal/prompt/history.go`: OUTPUT-GROUPING rule — group same-category + same-treatment senders
  into one multi-sender insight; never the same sender in two insights.

**DSH**
- `internal/handler/api.go`: `UpdateNotification` handler; `UPDATE … WHERE id=? AND dismissed_at IS
  NULL`.
- `cmd/dsh/main.go`: registered `PATCH /api/v1/notifications/{id}`.

## Verification

- **Unit (both repos green):**
  - `internal/notify/identity_test.go`: AC cluster collapses to one key (#1107/#1119/#1137), #1096
    `{team-soc,ac}` stays separate; order/case invariance; `ClassifyInsights` routes
    update/post/skip and picks the lowest active id on collision; the symmetric-derivation case (a
    `from:`-less insight is not identity-matched, falls back to the floor).
  - `internal/handler/notification_update_test.go` (DSH): active update succeeds and changes the
    row; **dismissed update returns 404 and the row is unchanged** (the guard); validation (empty
    message, bad priority → 400).
- **Empirical gemini proof of `BuildInsightDedup`** (the LLM judgment is the part that previously
  failed, so it was tested live): dismissed AC ignore_pattern + three candidates →
  - pure reword of the dismissed AC insight → **dropped**;
  - genuinely-new AC maintenance-window behavior (same sender, new category) → **kept, prefixed
    `Update:`**;
  - unrelated GitHub insight → **kept unchanged**.
  Output was valid insight JSON (passes `ParseAndValidateInsights`).
- **Build/lint:** `go build ./...` both repos; `go vet` clean for the new files; `bash -n
  run-task.sh` OK.

## Advisor catches folded in
1. **Asymmetric key derivation** — candidate keyed on `affected_senders`, stored side on the Link's
   `from:` tokens. Fixed: both derive through `identityFromMessageLink`; identity requires ≥1 sender,
   `from:`-less insights fall back to the structural floor (added a discriminating test).
2. **Deploy/restart** — see below; recorded honestly rather than claimed deployed.

## Not yet run / deployment
- **`run_learn` bash glue is unrun in production.** `bash -n` checked syntax and the gemini prompt
  was proven in isolation, but the `[`-detection branch feeding `insight-dedup` output back into
  `gml insights` has not been exercised by a real cycle.
- **Daemon restart required for Part B.** Image rebuilds are auto-picked-up next cycle, but
  `run-task.sh` changes need the **watch-knowledge daemon restarted** — until then the live pipeline
  still runs `history → gemini → insights` with no `insight-dedup` stage (2b inert). Part A (the
  deterministic poster) ships in the gml image and takes effect next cycle once rebuilt; the DSH
  PATCH endpoint needs the DSH image rebuilt + restarted.

## Known limitations (also GML-068)
- `from:`-less insights get structural dedup only (identity needs a sender).
- Multi-sender regroup instability: `learn` grouping `{a,b,c}` one cycle vs `{a,b}` the next → different
  K → won't dedup. Mitigated by the grouping discipline; not eliminated.
- In-place update strips a prior `Update:` prefix on the next refresh (cosmetic).
