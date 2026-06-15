# Iteration 021 — Planning: insight dedup (identity-keyed update / skip / re-surface)

**Started:** 2026-06-09
**Phase:** Planning

## Problem

The `learn` phase re-derives the *same* behavioral insight every cycle with a *different*
`gmail_search` string, so the structural guard (`InsightDedupKey`, canonical query-string equality)
can't collapse them. Visible symptom: three live "Ignore AC project status" insights for
`ac@example.com` (#1107/#1119/#1137), each a reworded query — `{subject:…}` vs `(… OR …)` vs a
subset. **Verified empirically** — the three produce three different dedup keys. Their timestamps
(09:15 / 09:44 / 10:44, dismissed together 11:35) show each was posted **while the previous was
still active**.

Iterations 018–020 deduped everything *downstream* of insights (rules, provenance); nothing deduped
insight **generation**. This iteration does.

## Desired behavior (confirmed with Tomas)
1. Don't post duplicate insights.
2. On a new observation/clarification of an existing insight:
   - 2a. matching insight **active** → update it in place.
   - 2b. matching insight **dismissed** → open a new clarifying insight (only when genuinely new).
3. Granularity: **multi-sender insights allowed** (one insight for senders sharing the same category
   + treatment, e.g. 5 marketing senders → one `from:{…}` insight). Safe because `propose.Generate`
   emits one `archive_by_sender` rule per knowledge pattern with all senders OR'd — no split, and
   not the 019 OR-union footgun (that is two *rules* for one sender; one rule listing N senders is
   fine).

## Design — identity key drives everything; LLM confined to one sub-case

**Identity key** = `from:`-tokens of the insight's `gmail_search` + category. Two stages split the
responsibility:

- **Deterministic, in the shared poster `cmdInsights`** (`notify.ClassifyInsights`): candidate vs
  **active** insights → K matches → update in place (2a); else post. Structural `InsightDedupKey`
  stays as the cheap floor (exact-query repost → skip).
- **LLM, reusing the dedup stage analyze already has** (`gml insight-dedup` → `BuildInsightDedup`):
  candidate vs **dismissed** insights → reworded duplicate dropped; genuinely-new same-topic kept
  and prefixed `Update:` so it reads as a clarification (2b). The `learn` path now runs this stage,
  mirroring the analyze path.

**Symmetric key derivation (advisor catch).** A stored notification exposes only its Message+Link,
so identity must key on the query's `from:`-tokens, NOT a separate `affected_senders` field. The
candidate derives its key through the *same* parse of its rendered notification
(`identityFromMessageLink(InsightToNotification(c))`), so the key a candidate computes is provably
the key it will have once stored. A `from:`-less insight (category alone is too coarse — it would
merge unrelated same-category topics) yields `ok=false` and falls back to the structural floor only.

**Grouping discipline (prompt).** `learn` must group same-category + same-treatment senders into one
multi-sender insight and never place the same sender in two insights — keeps the identity key stable
and preserves the 019 one-rule-per-sender invariant.

## Why deterministic where it counts
The bug exists *because* the learn LLM was already given prior insights + told to avoid repeats and
still duplicated. So the load-bearing fix (2a) is pure determinism; the LLM is confined to the one
sub-case it's actually needed for (is a dismissed-topic candidate genuinely new?), behind the
deterministic identity gate.

## Rejected alternatives
- **LLM reconcile for the whole decision** (classify every candidate new/update/skip via LLM): the
  exact failure mode that produced the bug. Confine the LLM to the dismissed-rework judgment only.
- **New `insight-gather`/`insight-apply` verb pair** (distill-style): over-engineered for the rare
  2b tail; reuse the existing analyze dedup stage (KISS).
- **Primary-sender-only key**: wrongly merges `{team-soc,ac}` (#1096) into the `{ac}` cluster.
  Sender-SET avoids it.
- **Keying on `affected_senders`**: the stored side can't see that field — asymmetric, silently
  under-dedups. Key on the query's `from:`-tokens both sides.
- **Per-sender insights**: stable key but N cards where one suffices; rejected per Tomas.

## Decisions
- Identity = `gmail_search` `from:`-tokens + category; `from:`-less insights use the structural floor
  only (documented limitation).
- 2a deterministic (no LLM); 2b reuses the analyze dedup stage with a learn-tuned prompt.
- DSH gains `PATCH /api/v1/notifications/{id}`, guarded `dismissed_at IS NULL` so an update can never
  resurrect a dismissed insight.
- Forward-only: the already-dismissed AC dups are left as-is.
- Advisor-reviewed (identity split, symmetric derivation, deploy/restart caveat).
