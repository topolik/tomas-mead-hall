# 002 — Planning

- **Start:** 2026-06-12
- **End:** 2026-06-12
- **Phase:** Planning

## Test requirements (written FIRST — this is "done")

### extract (pure Go, fixtures modeled on real records)
- **T1** Claude parser: given a fixture JSONL with a real-shaped human turn (`type:user`, `message.content` string), emits one moment with `{source:claude, project, session, ts, text}`.
- **T2** Claude parser skips: tool_result user records (203/295 in the surveyed real session!), `isSidechain:true`, `userType != external`, `<command-name>`/`<local-command-stdout>` turns, and strips `<system-reminder>` blocks from kept turns.
- **T3** Claude parser captures context: the last assistant `text` before the user turn, truncated to budget.
- **T4** Claude parser handles `message.content` as array of text blocks (joins text parts).
- **T5** Gemini parser: fixture `session-*.json` → moments from `type:user` entries (content string AND `[{text}]` array forms), with preceding `type:gemini` content as context.
- **T6** Gemini fallback: project with `logs.json` but no `chats/` still yields moments (no context).
- **T7** Noise floor: moments shorter than a minimum signal length with no decision verbs (e.g., bare "continue", "/stats", "ok") are dropped.
- **T8** Dedup: identical (session, ts, text) moments emitted once even if files overlap.

### redact
- **T9** Redacts: `ghp_…`, `gho_…`, `sk-…`, `AKIA…`, JWT triplets, `op://…` paths, `password=…`/`token: …` assignments, PEM blocks → `[REDACTED:<kind>]`. Plain prose, emails, URLs survive untouched.
- **T10** Redaction applies to both `text` and `context` fields before write.

### distill
- **T11** Batcher: N moments → ⌈N/B⌉ batches with stable IDs; batch prompt contains schema + datamarked moments.
- **T12** Response validation is resilient per-item (GML lesson): a batch response with one malformed insight keeps the valid ones, logs the bad one, never sinks the batch.
- **T13** Insights merge: same identity key (category + normalized statement) across batches/runs → single insight, evidence appended, confidence bumped.
- **T14** Prose-wrapped JSON (Gemini habit: "Here's the analysis: …") is unwrapped (GML `validate.go` lesson).

### profile
- **T15** Profile prompt includes all insights grouped by category; output written only if non-empty for each of the three profiles.

### ask
- **T16** BM25: query "which language for a new agent service" ranks a `tech_preference` Go insight above unrelated insights (fixture corpus).
- **T17** Ask prompt contains: all profiles whole, top-k retrieved evidence, the question; `--json` output parses to `{answer, confidence, citations[]}`.

### E2E (real data — "run it or it doesn't count")
- **T18** `extract` over real `~/.claude/projects` + `~/.gemini/tmp` completes, reports per-source counts, output spot-checked for tool-result leakage and unredacted tokens (grep for token prefixes must find none).
- **T19** `distill --limit` on a real batch via gemini-cli produces ≥1 valid insight with evidence pointing at a real session.
- **T20** `profile` produces the three Markdown profiles; Tomas can read and recognize himself in them (review-gate criterion).
- **T21** `ask "Should the new agent service use Postgres or a flat file, and why?"` (real question, real brain) returns a direction with ≥1 citation.

## Implementation checklist

- [ ] Scaffold `projects/MND-mind-model/` — go.mod, cmd/mnd, Dockerfile, run-task.sh, .gitignore (`data/`)
- [ ] `internal/claude` — JSONL session parser (T1–T4)
- [ ] `internal/gemini` — chats/logs parser (T5–T6)
- [ ] `internal/extract` — moment model, noise filter, dedup, context budget (T7–T8)
- [ ] `internal/redact` — secret patterns (T9–T10)
- [ ] `internal/distill` — batcher, prompt builder, datamarking, response validation, merge (T11–T14)
- [ ] `internal/brain` — insights.yaml read/write, profile prompt, profile writer (T15)
- [ ] `internal/ask` — BM25, ask prompt, JSON output (T16–T17)
- [ ] `run-task.sh` — extract | distill | profile | ask | pipeline, host-side gemini-cli with prompt files (GML pattern), `--model claude` fallback
- [ ] E2E run on real data (T18–T21), bugs fixed, run log in 003-implementation.md
- [ ] README.md, SKILLS.md update, ASSUMPTIONS.md final pass

## Safety checklist review (MO §7)

- **Secrets:** no secrets in code; session mounts read-only; tool results skipped at parse time (MND-001); regex redaction before disk/LLM (MND-002); raw `data/` gitignored (MND-008); LLM auth stays host-side (MND-003).
- **SQL/HTML/CSRF/auth:** N/A — no DB, no web UI, no endpoints in iteration 1.
- **Input validation:** LLM responses schema-validated per-item; session files treated as untrusted input (size caps, datamarking of moment content in prompts — sessions can quote external/hostile text, same threat model as GML email bodies).
- **Quality:** TDD per above; nothing ships without T18–T21 on real data.

## Performance notes (flagged early)

- 204 MB Claude JSONL: stream line-by-line (`bufio.Scanner` with grown buffer — single lines can be MBs); never slurp files.
- `~/.gemini/tmp` is 9.4 GB but almost all non-chat artifacts; only read `chats/session-*.json` + `logs.json` (~MBs).
- Distillation cost is the bottleneck: ~180 s/call ceiling (GML timeout). First real run uses `--limit` to cap batches; full corpus run is a background job later if needed.
- BM25 in-memory over hundreds of insights: negligible.

## Decisions

### Stream-parse sessions; cap context budget per moment
**Date:** 2026-06-12
**Phase:** Planning
**Decided by:** Team
**Decision:** Line-streaming JSONL parser; assistant context truncated to ~700 chars per moment; moment text capped at ~4000 chars.
**Reasoning:** Keeps memory flat over 204 MB corpus; context is for orientation, not completeness; distill prompts must stay well under gemini-cli limits.
**Revisit if:** Distill quality suffers from truncated context.

### Reuse GML's untrusted-content defenses for moment text
**Date:** 2026-06-12
**Phase:** Planning
**Decided by:** Team
**Decision:** Datamark moment content inside distill prompts; instruct the LLM that moments are data, never instructions.
**Reasoning:** Session text can embed hostile content (pasted emails, web text). Same threat model GML already solved for email bodies.
**Revisit if:** Datamarking measurably hurts distill quality.
