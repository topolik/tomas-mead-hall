# MND — Architecture

## Pipeline (iteration 1)

```mermaid
graph TD
    subgraph sources [Session history - read-only mounts]
        C["~/.claude/projects/**/*.jsonl<br/>238 sessions, 204 MB"]
        G["~/.gemini/tmp/*/chats/session-*.json<br/>1202 sessions (+ logs.json fallback)"]
    end

    subgraph container [Docker: mnd Go binary]
        E["extract<br/>user turns + assistant context<br/>skip tool results / stdout / reminders"]
        R["redact<br/>token shapes, PEM, passwords"]
        BP["build distill prompts<br/>(batched moments)"]
        V["validate + merge<br/>resilient per-item, identity-keyed dedup"]
        PP["build profile prompt"]
        AP["build ask prompt<br/>profiles whole + BM25 top-k evidence"]
    end

    subgraph host [Host: run-task.sh]
        L1["npx gemini-cli<br/>(distill)"]
        L2["npx gemini-cli<br/>(profile)"]
        L3["npx gemini-cli<br/>(ask)"]
    end

    subgraph artifacts [Artifacts]
        M["data/moments.jsonl<br/>(gitignored)"]
        I["brain/insights.yaml<br/>(committed)"]
        P["brain/profiles/*.md<br/>(committed, human-editable)"]
        A["answer + evidence citations<br/>(stdout / --json)"]
    end

    C --> E
    G --> E
    E --> R --> M
    M --> BP --> L1 --> V --> I
    I --> PP --> L2 --> P
    P --> AP
    I --> AP
    Q["herdr agent question<br/>mnd ask '...'"] --> AP --> L3 --> A
```

## Data flow contract

| Stage | In | Out | LLM |
|---|---|---|---|
| extract | session files (ro) | `data/moments.jsonl` — `{source, project, session, ts, context, text}` | no |
| distill | moments.jsonl | `brain/insights.yaml` — `{id, category, statement, confidence, evidence[]}` | yes, batched |
| profile | insights.yaml | `brain/profiles/{decision-making,technical-preferences,direction-style}.md` | yes |
| ask | question + brain/ | direction + citations (text or JSON) | yes |

## Categories (distill output)

- `tech_preference` — tooling/architecture/security defaults
- `decision_heuristic` — how Tomas weighs options (KISS, good-enough, run-it)
- `direction_pattern` — how he scopes, prioritizes (Eisenhower), delegates
- `correction_pattern` — what he rejects and how he re-steers
