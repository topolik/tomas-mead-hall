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
    Q["herdr agent question"] --> CL --> L4
    L4 -->|"category ∈ auto set"| AP --> L3 --> A
    L4 -->|"category ∉ auto set"| DSH["DSH escalation<br/>(Tomas reviews)"]
    I --> EV --> L5 --> REP["fidelity report<br/>(per-category, calibration)"]
```

## Competence gate (iteration 10)

```mermaid
graph LR
    Q["agent question"] --> CL["classify<br/>(cheap LLM)"]
    CL -->|"correction_pattern<br/>direction_pattern"| AUTO["auto-answer<br/>(78% fidelity)"]
    CL -->|"decision_heuristic<br/>tech_preference<br/>other"| ESC["escalate to Tomas<br/>(via DSH)"]
    AUTO --> ASK["mnd ask → direction"]
    ESC --> DSH["DSH notification"]
```

Routing keys on the **predicted question category**, not self-reported confidence (which is non-discriminating). The routing signal is validated externally via the eval judge. `MND_ROUTE_AUTO` tunes the auto set; `MND_ROUTE=off` disables.

## Data flow contract

| Stage | In | Out | LLM |
|---|---|---|---|
| extract | session files (ro) | `data/moments.jsonl` — `{source, project, session, ts, context, text}` | no |
| distill | moments.jsonl | `data/insights.yaml` — `{id, category, statement, confidence, evidence[]}` | yes, batched |
| profile | insights.yaml | `brain/profiles/{decision-making,technical-preferences,direction-style}.md` | yes |
| ask | question + data/ | direction + citations (text or JSON) | yes |
| classify | question text | category label (one of 4 + `other`) | yes, single call |
| eval | data/ + sampled moments | fidelity report (per-category %, calibration, disagreement list) | yes, N+1 calls |

## Categories (distill output → routing input)

| Category | Gold fidelity | Predicted fidelity | Route default |
|---|---|---|---|
| `correction_pattern` | 67% | **86%** | ✅ auto |
| `direction_pattern` | 57% | 64% | ✅ auto |
| `tech_preference` | 80% | 50% | ❌ escalate |
| `decision_heuristic` | 38% | 42% | ❌ escalate |
| `other` | — | 50% | ❌ escalate |

Gold ≠ predicted: the classifier is 49% accurate but the *predicted* buckets it gets right are reliably high-fidelity. Tech is best by gold but unreliable by predicted (over-assigned).
