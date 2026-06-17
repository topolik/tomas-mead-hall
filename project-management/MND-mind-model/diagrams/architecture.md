# MND — Architecture

## Pipeline

```mermaid
graph TD
    subgraph sources [Session history - read-only mounts]
        C["~/.claude/projects/**/*.jsonl"]
        G["~/.gemini/tmp/*/chats/session-*.json"]
    end

    subgraph container [Docker: mnd Go binary]
        E["extract<br/>user turns + assistant context<br/>skip tool results / stdout / reminders"]
        R["redact<br/>token shapes, PEM, passwords"]
        BP["build distill prompts<br/>(batched moments)"]
        V["validate + merge<br/>resilient per-item, identity-keyed dedup"]
        PP["build profile prompt"]
        AP["build ask prompt<br/>profiles whole + BM25 top-k evidence"]
        CL["classify<br/>(question → category)"]
        EV["eval<br/>(sample → judge → fidelity report)"]
    end

    subgraph host ["Host: run-task.sh (LLM via LLP or direct CLI)"]
        L1["LLM: distill"]
        L2["LLM: profile"]
        L3["LLM: ask"]
        L4["LLM: classify"]
        L5["LLM: eval judge"]
    end

    subgraph artifacts [Artifacts]
        M["data/moments.jsonl<br/>(gitignored)"]
        I["data/insights.yaml"]
        P["data/profiles/*.md"]
        A["answer + evidence citations"]
    end

    C --> E
    G --> E
    E --> R --> M
    M --> BP --> L1 --> V --> I
    I --> PP --> L2 --> P
    P --> AP
    I --> AP
    Q["herdr agent question"] --> AP --> L3 --> A
    P --> AP
    I --> AP
    Q --> EMB["embed question<br/>(Ollama GPU)"]
    EMB --> GATE["evidence gate<br/>(cosine top-k → category + similarity)"]
    EMBS["data/embeddings.json"] --> GATE
    GATE -->|"auto: safe category + high similarity"| DELIVER["deliver direction"]
    GATE -->|"escalate: judgment / sparse / mixed"| DSH["DSH escalation<br/>(Tomas reviews)"]
    A --> DELIVER
    A --> DSH
    I --> EV --> L5 --> REP["fidelity report<br/>(per-category, calibration)"]
```

## Evidence gate (iteration 11)

```mermaid
graph LR
    Q["agent question"] --> ASK["mnd ask<br/>(BM25 + LLM)"]
    Q --> EMB["embed<br/>(Ollama GPU)"]
    EMB --> COS["cosine top-k<br/>(pre-embedded insights)"]
    COS --> GATE{"evidence gate"}
    GATE -->|"dominant ∈ auto-set<br/>mean_sim ≥ 0.60<br/>dominance ≥ 50%"| AUTO["auto-answer<br/>(91% fidelity)"]
    GATE -->|"judgment-dominant<br/>sparse / mixed"| ESC["escalate to Tomas"]
    ASK --> AUTO
    ASK --> ESC
```

Routing keys on **what the brain actually found** (embedding retrieval evidence), not a standalone LLM classifier or self-reported confidence. The embedding call is local (Ollama + nomic-embed-text on GTX 1080 GPU) — no LLM call for routing. `MND_ROUTE_AUTO` tunes the auto set; `MND_ROUTE=off` disables.

## Data flow contract

| Stage | In | Out | LLM |
|---|---|---|---|
| extract | session files (ro) | `data/moments.jsonl` — `{source, project, session, ts, context, text}` | no |
| distill | moments.jsonl | `data/insights.yaml` — `{id, category, statement, confidence, evidence[]}` | yes, batched |
| profile | insights.yaml | `brain/profiles/{decision-making,technical-preferences,direction-style}.md` | yes |
| ask | question + data/ | direction + citations (text or JSON) | yes |
| embed-batch | insights.yaml | `data/embeddings.json` (768-dim vectors per insight) | no (Ollama local GPU) |
| embed-query | question text | 768-dim vector | no (Ollama local GPU) |
| evidence gate | query vector + embeddings + insights | gate decision (auto/escalate) + evidence metadata | no |
| eval | data/ + sampled moments | fidelity report (per-category %, calibration, disagreement list) | yes, N+1 calls |

## Categories (distill output → evidence gate input)

| Category | Fidelity (21 cases) | Embedding dominant% | Route default |
|---|---|---|---|
| `correction_pattern` | 93% | 58–83% when dominant | ✅ auto |
| `direction_pattern` | 100% | 50–92% when dominant | ✅ auto |
| `tech_preference` | — (0 gold in set) | 50–92% when dominant | ✅ auto |
| `decision_heuristic` | 72% | 50–83% when dominant | ❌ escalate |

Routing signal comes from embedding retrieval (dominant category of top-k insights), not an LLM classifier. Tech re-included: adding it raised fidelity 87%→91% with +19% coverage.
