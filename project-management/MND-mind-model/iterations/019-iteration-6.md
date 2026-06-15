# 019 — Iteration 6: retrain hygiene + relic closure

- **Start:** 2026-06-14
- **Trigger:** Tomas, "finish it" — complete the carried follow-ups from the iteration-5 review (018) and leave the brain consistent.

## Ideation

Iteration 5 merged the features but left the brain in a knowingly-inconsistent state, plus an operational footgun:
- **Stale profiles**: the 2 retired + 4 scoped insights aren't reflected in the `.md` profiles (ask filters at query time, but the human-readable core lags).
- **Unfolded moments**: newer sessions (incl. today's heavy direction-giving) weren't distilled.
- **The branches relic** (`560f24f9796a`, "work directly on master, no branches") couldn't be retired — nothing contradicted it (MND-025 limit).
- **The daemon footgun**: `watch-retrain` ran from the integration checkout and left uncommitted brain output (root-caused 2026-06-13).

## Planning

1. **Preserve self-exclusion**: append the 4 salvaged orchestrator send-ledger entries from the stopped daemon (MND-015).
2. **Defeat the relic**: encode the branch-per-project workflow as an authoritative `feedback` insight (MND-026) so the contradiction sweep retires the relic by provenance.
3. **Guard the workflow** (MND-028): `retrain` refuses to run on `master` (override env), so brain mutation is always committed + review-gated.
4. **Clean retrain on the feature branch**: extract → distill new moments → learn → contradiction sweep → profile regen — makes the brain consistent and reproduces the daemon's work properly through the full iteration-5 pipeline.
5. Review the diff; merge per MO §10.

No TDD code beyond the guard (a shell conditional) — this iteration is operational, exercising existing tested machinery on real data.

## Implementation

- Salvaged ledger entries appended (4 new, 1 dup skipped).
- Branch-workflow insight `df825545bb3d` added (`tech_preference`, feedback, strong).
- `retrain` master-guard added (MND-028); verified it refuses on master and runs on the feature branch.
- Clean retrain executed on `tomas-clone-mind-model`.

### Retrain results (live)
- **extract**: 1968 moments (306 claude files, 1246 gemini chats); dropped noise=644, dup=1637, **self=43** (self-exclusion incl. the new `[MND orchestrator]` prefix).
- **distill**: 95 new moments → 3 batches → **+30 insights** (832 total).
- **learn**: nothing to learn — no new commented DSH escalations (drill notification 1291 was dismissed without a comment, as intended).
- **contradiction sweep** (830 active): **3 retired, 10 scoped**.
  - ★ **`560f24f9796a` ("work directly on master, no branches") RETIRED**, superseded by the branch-workflow insight `df825545bb3d` — the iteration-4/5 acceptance case, finally closed by provenance (feedback > distill).
  - Also retired: a Pandoc-vs-Podman doc-conversion conflict; another env-var-credentials belief (`dc116ba47fdd`).
  - 10 context-splits scoped (sub-agent delegation, containers-vs-local, AFK-caching-vs-in-memory-secrets, autonomy-vs-clarify, …) — all kept active with sharpened contexts.
  - Final: **827 active, 5 superseded**.
- **profile regen**: gemini emitted invalid JSON (raw newlines inside string values — a recurring gemini-cli quirk; profiles were left intact, not corrupted, since WriteProfiles parses before writing). **Re-ran with claude** (clean JSON) → profiles regenerated from the 827 active insights.

## Verification
- Relic retired (acceptance case closed). New insights folded in. Profiles regenerated and now reflect the superseded/scoped set. `ask` already filtered superseded at query time; profiles are now consistent too.

## Review

**Tomas: "finish it"** — accepted; merge per MO §10. No push (dsh:1282). Brain consistent: 832 insights / 827 active / 5 superseded, profiles regenerated (claude), relic retired.

## Carried to iteration 7
- **Harden `WriteProfiles` against gemini's raw-newline JSON** (bit us this run; the recurring retrain defaults to gemini for profiles, so it'll recur). Options: lenient per-key extraction fallback, or change the profile output contract away from multiline-markdown-in-JSON.
- Loop-until-dry contradiction sweep (deterministic coverage — MND-025 limit 2).
- Semantic insight dedup.
- Authority-boundary insight ("may an agent restart shared infra?").
- A safe **recurring** retrain — **on master**, committing each learning update there (Tomas's 2026-06-14 correction: the brain is production data and lives on master; MND-028 + MO §10 "code vs production data" revised accordingly — the earlier "retrain only in a worktree" framing was backwards).
