# Iteration 018 — Dedup hardening: query normalizer, semantic propose gate, durable insight dedup

**Started:** 2026-06-04
**Phase:** Implementation

## Goal

Stop two classes of duplicates Tomas hit in real use:

1. **Duplicate DSH plans** — the propose dedup compared filter strings byte-for-byte, so LLM quoting/ordering variants (`-"Critical"` vs `-Critical`) produced duplicate plans. The socradar plan was the trigger example.
2. **Repeated insights** — "I get the same insights again and again." Dismissing an insight removed it from the only structural dedup guard, and the guard was truncated to 20 items by a silent limit bug.

Approach agreed with Tomas: layered, cheapest-first — a deterministic Gmail-query normalizer (floor), the insight-guard fix, and an LLM semantic gate (net) for duplicates structure can't catch.

## What was built

### Part A — `CanonicalQuery` (deterministic normalizer)
`internal/propose/propose.go`. Token-level Gmail-query normalizer: lowercases operator names, strips quotes around single-word tokens, canonicalizes relative date units (`1w` → `7d`), collapses whitespace, dedupes and sorts terms. **Key-only** — never rewrites a stored/applied filter. `CanonicalFilter` (quote-strip) retained for the stored-filter path.

### Part B — Insight repeat fix + DSH limit clamp
- `cmd/gml/main.go` `cmdInsights`: dedup now fetches **dismissed + active** notifications (`GetDismissedNotifications`), so a dismissal durably suppresses re-posting.
- `internal/notify/insights.go` `InsightDedupKey`: decodes the URL-encoded search link, strips volatile `newer_than`/`older_than` windows, then canonicalizes — collapses the LLM's run-to-run query drift.
- `DSH internal/handler/api.go`: clamp notifications `limit` to a 200 max instead of silently falling back to 20 when out of range.

### Part B′ — plan dedup uses `CanonicalQuery`
`cmd/gml/main.go` planKey dedup keys on `CanonicalQuery(filter)`.

### Part C — semantic dedup gate at propose
Split into `propose-gather` → LLM → `propose-apply` (mirrors `merge-plans`):
- `prompt/propose_dedup.go` `BuildProposeDedup` — renders surviving candidates + existing plans, asks the LLM to drop semantic duplicates.
- `propose.ParseProposals` — validates the kept-proposals array (array-aware fence stripping).
- `structuralDedup` helper shared by `cmdPropose`/gather/apply; `formatExistingPlans` for prompt context.
- `run-task.sh` `run_propose()` wired into the `propose` command and `watch-knowledge` step 3. `--no-llm`/`--json` keep the structural-only path.

## Run log

```
# Part A/B unit tests
$ go test ./internal/propose/ -run 'TestCanonical|TestParseProposals'   PASS
$ go test ./internal/notify/ -run TestInsightDedupKey                    PASS
$ go test ./...   (GML)   all packages ok
$ go test ./...   (DSH)   ok — incl. new TestNotification_LimitClampedNotDropped (limit=200 → 25 rows, not 20)

# End-to-end (live DSH, rebuilt image)
$ ./run-task.sh propose --json          # offline: 13 proposals, 11 skipped — quote-strip visible (-"LRINFOSEC-" → -LRINFOSEC-)
$ docker compose run gml propose-gather # read-only: all 13 (incl. socradar) matched by structural dedup → 0 candidates, empty prompt
$ ./run-task.sh propose                 # gather empty → LLM + apply correctly skipped, "nothing to propose"
```

The socradar duplicate that prompted this work is now caught by the structural floor; the semantic gate was correctly *not* invoked (no wasted Gemini call) because nothing survived.

## Bugs found & fixed (self-correct loop)

- **`stripCodeFence` corrupts JSON arrays.** The merge-flow fence stripper trims to the first `{`, which chops the leading `[` of a proposals array. Added array-aware `stripArrayFence` for `ParseProposals`. (Caught by `TestParseProposals`.)
- **`CanonicalQuery` on the insight link was a near no-op** — links are URL-encoded, so the normalizer saw one opaque token. Real `tmp_*.json` samples showed the LLM adds/drops `newer_than:7d` and changes subject phrasing for the same insight. Fixed by decoding + stripping time-window operators in `InsightDedupKey`. (Caught during advisor review, before shipping.)
- **DSH `limit=200` silently became 20** — the `n <= 100` guard fell through to the default, truncating GML's dedup window. Fixed by clamping.

## Known residual

Subject-phrase drift for the same sender (e.g. `security@example.com` "Spike in security events" vs "…for example.com") is semantic, not cosmetic — structural dedup deliberately keeps these separate, so suppression still relies on the `learn` LLM. Next lever if it persists: feed dismissed insights harder into the learn prompt.

## Decisions

### CanonicalQuery is key-only, never rewrites stored filters
**Date:** 2026-06-04 12:00
**Phase:** Implementation
**Decided by:** Tomas (approach), team (impl)
**Decision:** The normalizer is used solely to build dedup comparison keys. The stored/applied filter and search link are left as-is.
**Alternatives considered:** Rewrite stored filters into canonical form (cleaner-looking rules.yaml).
**Reasoning:** An over-normalization bug must at worst cause a missed/false dedup match — never alter what gets applied to the mailbox. Rewriting stored filters would put that bug in the blast radius of real archiving.
**Revisit if:** We gain a fully-tested grammar parser and want canonical rules.yaml output.

### Semantic propose gate defaults to KEEP-on-doubt
**Date:** 2026-06-04 12:00
**Phase:** Implementation
**Decided by:** team (flagged to Tomas for confirmation)
**Decision:** The LLM dedup gate keeps a candidate when unsure, dropping only clear semantic duplicates.
**Alternatives considered:** Remove-on-doubt (as the notification dedup does, and as the approved plan originally stated).
**Reasoning:** Unlike notifications (terminal), every proposed plan still passes human review in DSH before it's applied. A false drop silently loses a legitimate rule; a false keep is just a plan Tomas can reject. This deviates from the approved plan's wording — flagged for Tomas.
**Revisit if:** Tomas prefers fewer plans in the review queue over never silently dropping one.

### Dismissed insights suppress re-posting durably
**Date:** 2026-06-04 12:00
**Phase:** Implementation
**Decided by:** Tomas
**Decision:** A dismissed insight's (canonical) link suppresses re-posting indefinitely; the dedup guard includes dismissed notifications.
**Alternatives considered:** TTL-based resurfacing; magnitude-aware resurfacing.
**Reasoning:** Matches "I've seen this, stop." Simplest correct behavior. Bounded to the most-recent ~200 dismissed items.
**Revisit if:** A genuinely changed situation that shares a link needs to resurface.
