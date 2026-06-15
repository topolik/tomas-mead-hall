# LLP — Architecture (Ideation sketch)

Exploratory; refined in Phase 1 planning.

## Request flow

```mermaid
graph TD
    subgraph clients [Clients]
        GML[GML Mode 2]
        FUT[Future agents]
    end

    GML -->|"POST /v1/chat/completions<br/>Authorization: Bearer agent-key"| AUTH
    FUT -->|"/v1/chat/completions"| AUTH

    subgraph proxy [LLP Gateway — Go, OpenAI-compatible façade]
        AUTH[Auth<br/>per-agent bearer key] --> QUEUE[Queue + rate limiter<br/>per-impl concurrency cap + token bucket]
        QUEUE --> ROUTER[Router / failover chain]
        REG[(Model registry<br/>logical name → impl + upstream id + base URL/command<br/>+ ordered failover chain)]
        USAGE[(SQLite<br/>usage + cost per request/agent)]
        ROUTER -->|reads| REG
        ROUTER -->|writes| USAGE
    end

    ROUTER -->|exec — default| GEM[Gemini CLI<br/>CliProvider]
    ROUTER -->|exec — 2nd| CLA[Claude CLI<br/>CliProvider]
    ROUTER -->|HTTP — 3rd| OLL[OpenLLM / OpenAI-compatible<br/>HttpProvider → Ollama / remote / OpenRouter]

    AUTHHOST[host CLI auth<br/>Cloud-project / subscription] -.->|impls #1/#2 reuse| ROUTER
    KEY[op → env<br/>only for impl #3 → remote] -.->|bootstrap| proxy
```

## Failover sequence

```mermaid
sequenceDiagram
    participant C as Client (GML)
    participant P as LLP Router
    participant G as Gemini CLI
    participant A as Claude CLI

    C->>P: POST /v1/chat/completions (model: gml-analyze)
    P->>G: exec gemini-cli (default, primary in chain)
    G-->>P: rate-limited / non-zero exit (retryable)
    Note over P: terminal error (bad request) would stop here;<br/>rate-limit / exit / timeout → fail over
    P->>A: exec claude -p (next in chain)
    A-->>P: stdout: completion (+ token usage)
    P->>P: record usage/cost (impl_used = claude-cli)
    P-->>C: 200 OpenAI-shaped response
```
