# 001 — Ideation

- **Start:** 2026-06-12
- **End:** 2026-06-12
- **Phase:** Ideation

## The idea (Tomas, verbatim intent)

> This project will learn from claude/gemini sessions and distill my brain. I'm working with claude and gemini for some time so there should be lot of important information about how I decide, what's my priority when answering and directing. This is to build an orchestrator agent that will replace me. My goal of this process is to make me redundant so that the orchestrator can act like I would based on the past history and experience and guess the best course of actions. Especially now that I use herdr the orchestrator should be able to answer and navigate agents with proper directions in "my style".

## Brainstorm notes

### Raw material (surveyed 2026-06-12)

| Source | Location | Volume | Format |
|---|---|---|---|
| Claude Code sessions | `~/.claude/projects/*/` | 238 `.jsonl` files, ~204 MB | JSONL: `type:user` turns with `message.content`, `cwd`, `timestamp`, `sessionId`; assistant turns interleaved (text, thinking, tool calls) |
| Gemini CLI sessions | `~/.gemini/tmp/*/chats/` | 1202 `session-*.json` files | JSON: `messages[]` of `{type: user\|gemini, content}` — both sides of conversation |
| Gemini CLI logs | `~/.gemini/tmp/*/logs.json` | 61 files | JSON array of user messages only (fallback for projects without chats/) |

The highest-signal records are **Tomas's own turns**: every place he directed, corrected, rejected, prioritized, or approved. The assistant turn immediately before a user turn gives the context the decision responded to.

### What the brain must capture (Tomas's weighting)

1. **Technical/domain preferences** — tooling choices, architecture defaults, security posture, KISS calls.
2. **How Tomas directs & corrects** — what he rejects, how he re-scopes, when "good enough" vs "do it properly", priority calls (Eisenhower).
3. Voice/communication style — explicitly **deprioritized** ("get the shit done").

### Two-phase brain representation (Tomas's call)

1. **Readable Markdown profiles** — curatable, inspectable, Tomas can correct them directly.
2. **Automatically distilled into RAG** — so the orchestrator retrieves relevant evidence at decision time without manual curation.

### Orchestrator shape for iteration 1

`mnd ask "<question>"` — CLI command (with `--json` for agents). Given a question an agent would otherwise ask Tomas, it retrieves relevant insights, loads the profiles, and produces a Tomas-style direction with evidence citations. herdr agents can shell out to it. HTTP/DSH integration comes later if the answers prove useful.

### Is this code or a brainstorm?

Code. The data exists, the consumer (herdr agents) exists, and the team conventions (GML's prompt-file + host gemini-cli pattern, sanitize/distill modules) give a proven implementation path.

### Rejected ideas

- **Vector-DB RAG in iteration 1** — rejected. Needs embeddings (API key or service), another container, and the corpus after distillation is small (hundreds of insights). Pure-Go BM25 lexical retrieval over `insights.yaml` is good enough and zero-infra. Revisit if retrieval quality disappoints.
- **Orchestrator-first with hand-written rules** — rejected by Tomas. Iteration 1 delivers both halves tested on real session data.
- **Fine-tuning a model on sessions** — not considered seriously; against KISS, no infra for it, and a profile+RAG approach is correctable by editing Markdown.
- **Mining herdr logs in iteration 1** — deferred. Claude + Gemini first (Tomas's call); herdr orchestration history is a natural iteration 2 source.

## Requirements

1. **Extract** decision moments from real Claude JSONL + Gemini chats JSON: Tomas's turns + truncated preceding assistant context. Skip tool results, command stdout, system reminders (noise + where secrets live).
2. **Redact** secrets before anything leaves the extract stage (token shapes: `ghp_`, `sk-`, `AKIA`, JWTs, `op://`, passwords).
3. **Distill** moments into structured insights via LLM (Gemini default, Claude fallback — GML pattern), each insight carrying category, statement, and evidence references (session, timestamp, quote).
4. **Profile** — merge insights into readable Markdown profiles: decision-making, technical preferences, direction & correction style.
5. **Ask** — `mnd ask` answers a question in Tomas's style: profiles + BM25-retrieved evidence → LLM → direction with citations. `--json` mode for agents.
6. Everything containerized (MO §1); session dirs mounted **read-only**; LLM called host-side via `npx @google/gemini-cli` (GML pattern).
7. Tested against **real live session data** from the first run — no synthetic-only validation.

## Decisions

### Project scope: both halves in iteration 1, on real data
**Date:** 2026-06-12
**Phase:** Ideation
**Decided by:** Tomas
**Decision:** Iteration 1 delivers brain distillation AND the orchestrator, tested on real live sessions.
**Alternatives considered:** Brain-first (orchestrator later); orchestrator-first with hand-written rules.
**Reasoning:** Tomas wants to test the full concept on real data immediately.
**Revisit if:** The end-to-end slice proves too shallow to evaluate either half.

### Brain representation: Markdown profiles auto-distilled into RAG
**Date:** 2026-06-12
**Phase:** Ideation
**Decided by:** Tomas
**Decision:** Two-phase: readable Markdown profiles that are automatically distilled into RAG for orchestrator access.
**Alternatives considered:** Profiles only; RAG only.
**Reasoning:** Profiles are inspectable/correctable; RAG gives the orchestrator the long tail without manual curation.
**Revisit if:** Maintaining both representations drifts them apart.

### Signal priority: technical/domain + direction/correction; voice deprioritized
**Date:** 2026-06-12
**Phase:** Ideation
**Decided by:** Tomas
**Decision:** Optimize extraction for what Tomas decides and how he steers, not how he sounds.
**Reasoning:** "Get the shit done" — the orchestrator must direct correctly; sounding like Tomas is cosmetic.
**Revisit if:** Agents misread directions because phrasing carries meaning.

### Sources: Claude + Gemini (herdr logs deferred)
**Date:** 2026-06-12
**Phase:** Ideation
**Decided by:** Tomas
**Decision:** First distillation pass mines Claude and Gemini session histories.
**Alternatives considered:** Claude only; Claude + Gemini + herdr logs.
**Reasoning:** Both corpora are rich and already on disk; herdr plumbing can wait.
**Revisit if:** Iteration 2 — herdr history is the most on-target data for orchestration style.

### Project code: MND
**Date:** 2026-06-12
**Phase:** Ideation
**Decided by:** Team (Tomas did not object)
**Decision:** `MND-mind-model`, matching the worktree name `tomas-clone-mind-model`.
**Alternatives considered:** TMC-tomas-clone.
**Reasoning:** "Mind model" describes the deliverable; the clone is the long-term goal, not this project.

### Priority: Q2
**Date:** 2026-06-12
**Phase:** Ideation
**Decided by:** Team
**Decision:** Q2 — important, not urgent.
**Reasoning:** Strategic capability (Tomas redundancy) with no external deadline. Consistent with DSH/GML.
