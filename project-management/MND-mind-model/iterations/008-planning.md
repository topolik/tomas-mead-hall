# 008 — Planning (Iteration 3: DSH low-confidence feedback loop)

- **Start:** 2026-06-12
- **End:** 2026-06-12
- **Phase:** Planning

## Goal

Tomas's directive 4: when the orchestrator can't answer (`confidence: low`), get the answer from Tomas through DSH — and learn from it. Same concept as GML insights: his comment on the notification becomes corrective knowledge.

```
orchestrate (confidence: low)
  → DSH notification [action_needed/Q1]: agent's question + brain's best guess
  → Tomas dismisses with a comment (his actual direction)
  → mnd learn: comment → corrective insight (strength: strong, source: feedback)
  → merged into brain → profiles regen → next time the orchestrator knows
```

## Design

- **`internal/dsh`** — trimmed GML client: OAuth client-credentials token, `PostNotification`, `GetDismissedNotifications(project_code=MND, has_comment=true)`. Config from `data/dsh.yaml` (gitignored; committed template `dsh.yaml.example` with empty creds — GML's pattern). Bootstrap: copy creds from the parent workspace's GML rules.yaml; Tomas can issue MND its own client at `/admin/clients` later.
- **Notification format:** `[MND ask <qhash>] <question (clipped)> — proposed: <answer (clipped)> (confidence: low)`. `qhash` = NormHash(question)[:12] — identity for dedup (GML iteration-021 lesson: update active notification on re-ask, don't repost).
- **`mnd feedback-post`** — reads `{question, answer, confidence}` JSON, posts/updates the DSH notification.
- **`mnd learn-gather`** — fetches dismissed-with-comment MND notifications not yet ingested (ingest ledger `brain/feedback-ledger.yaml` holds processed notification IDs), builds an LLM prompt: Tomas's comment is the **authoritative direction**, question+proposed answer are context; output = insight schema.
- **`mnd learn-merge`** — validates (evidence ref `dsh:<notification-id>`, quote = comment; separate validation path from distill's moments map), forces `strength: strong` + `source: feedback`, merges identity-keyed, appends to feedback ledger.
- **`run-task.sh learn`** — gather → LLM → merge; folded into `retrain` so the daemon picks up feedback automatically.
- **MND-013 hardening:** `mnd ask-prompt --tail-file F` — frames AND datamarks the terminal tail (orchestrate.sh stops inlining raw tails). Bonus: the datamark makes any leaked orchestrate question self-marking for the retraining exclusion.
- **Insight provenance:** `source: distill|feedback` field on insights; feedback insights surface first in profile prompts (they're direct corrections).

## Test requirements (first)

- **T29** dsh client against httptest: token caching, post, get-dismissed query params, error surfaces.
- **T30** feedback-post: message contains `[MND ask <qhash>]`, clipped question/answer; same question re-posted → update path, not duplicate.
- **T31** learn-gather prompt: comment marked authoritative; already-ingested notification IDs skipped via feedback ledger.
- **T32** learn-merge: valid item → insight with `dsh:<id>` evidence, strength strong, source feedback; identity-keyed merge into existing brain; ledger appended; malformed item dropped without sinking batch.
- **T33** ask-prompt --tail-file: output framed + datamarked; `exclude.IsPipelineContent(prompt)` == true.
- **T34 (live)** post a real low-confidence escalation to DSH; verify visible in UI; mechanically dismiss with a test comment via API (mechanics only — **acceptance judgment stays with Tomas**, MO §8); `learn` ingests it into the brain; re-`ask` the same question → direction now reflects the feedback.

## Safety checklist (§7)

- Secrets: DSH creds in gitignored `data/dsh.yaml` chmod 600; committed example has empty values (GML pattern). No creds in code or logs.
- Input validation: Tomas's comments pass through the LLM prompt as data (datamarked); learn responses schema-validated per-item.
- Auth: DSH API behind OAuth client credentials (existing mechanism).
- Quality: TDD T29–T33; live T34 before presenting.

## Decisions

### Feedback insights are strong by default and carry provenance
**Date:** 2026-06-12
**Phase:** Planning
**Decided by:** Team
**Decision:** `source: feedback`, `strength: strong`, evidence `dsh:<id>` with Tomas's comment as quote.
**Alternatives considered:** Letting the LLM rate strength (wrong — a direct correction from Tomas is definitionally authoritative); storing feedback in a separate file (fragments the brain; identity-keyed merge already handles collision).
**Reasoning:** The validity probe showed general insights losing to situational rulings; feedback must outrank distilled inference.
**Revisit if:** Comment-derived insights prove too situational and pollute general directions (then add a situational/general split).
