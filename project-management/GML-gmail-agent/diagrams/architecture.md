# GML Architecture

## Mode 1: Rule Engine

```mermaid
graph LR
    subgraph Container["Docker Container (gml)"]
        binary["gml binary (Go)"]
        rules["rules.yaml"]
        gws["gws CLI (Google Workspace CLI)"]
    end

    subgraph Auth["Credentials (injected at runtime)"]
        creds["credentials.json (from 1Password via run.sh)"]
    end

    subgraph Gmail["Google Workspace"]
        inbox["Gmail Inbox\n(user@example.com)"]
    end

    creds -->|"GOOGLE_WORKSPACE_CLI_CREDENTIALS_FILE"| gws
    rules -->|"loaded at startup"| binary
    binary -->|"subprocess calls"| gws
    gws -->|"OAuth2 REST"| inbox
    binary -->|"gml stats"| output["Stats Output\n(table / JSON)"]
    binary -->|"gml run [--dry-run]"| log["Archive Log\n(actions taken)"]
    binary -->|"ensure GML/archived label"| gws
    binary -->|"archive + label\n(atomic modify)"| gws
```

## Mode 2: AI Analysis Pipeline

```mermaid
graph TD
    subgraph Host["HOST"]
        subgraph RunSh["run.sh analyze / watch"]
            cred_cache["1Password creds\n(cached in memory\nfor watch mode)"]
        end

        subgraph Step1["Step 1: Container — Fetch & Sanitize"]
            fetch["gml fetch --days N"]
            gmail_fetch["gws CLI → Gmail API\n(6 box queries)"]
            sanitize["HTML→text\nDatamarking (U+E000)\nInvisible char removal\nBase64 stripping\nInjection flagging"]
            dsh_get["GET DSH notifications\n(previous analysis)"]
            prompt_build["Build per-concern\nLLM prompt"]
        end

        subgraph Step2["Step 2: Host — LLM CLI"]
            llm_choice{{"Gemini (default)\nor Claude"}}
            gemini["npx @google/gemini-cli\n-p '' < prompt"]
            claude["claude -p\n--model claude-opus-4-6\n< prompt"]
        end

        subgraph Step3["Step 3: Container — Validate & Notify"]
            notify["gml notify"]
            strip["stripCodeFence()\n(Gemini compatibility)"]
            validate["JSON schema validation\n(ConcernAnalysis[])"]
            gmail_url["GmailSearchURL()\nper concern"]
            dsh_post["POST DSH notifications\n[Box N — Name] concern"]
        end
    end

    subgraph External["External Services"]
        gmail["Gmail API"]
        dsh["DSH Dashboard"]
    end

    cred_cache -->|"stdin pipe"| fetch
    fetch --> gmail_fetch
    gmail_fetch --> gmail
    gmail --> sanitize
    fetch --> dsh_get
    dsh_get --> dsh
    sanitize --> prompt_build
    dsh_get --> prompt_build
    prompt_build -->|"temp file"| llm_choice
    llm_choice -->|"--model gemini"| gemini
    llm_choice -->|"--model claude"| claude
    gemini -->|"temp file"| notify
    claude -->|"temp file"| notify
    notify --> strip
    strip --> validate
    validate -->|"valid"| gmail_url
    gmail_url --> dsh_post
    dsh_post --> dsh

    style Step1 fill:#a8d8ea,stroke:#3a7ca5
    style Step2 fill:#f9d77e,stroke:#d4a017
    style Step3 fill:#c3e6cb,stroke:#28a745
```

### Data Flow

| Step | Where | Input | Output |
|------|-------|-------|--------|
| Fetch | Container | Gmail creds (stdin), DSH JWT | Complete LLM prompt (temp file) |
| Analyze | Host | Prompt (temp file) | Per-concern JSON (temp file) |
| Notify | Container | LLM JSON (stdin), DSH JWT | Per-concern notifications posted to DSH |

### Prompt Injection Defense (5 layers)

```
Email content (untrusted)
  │
  ├─ Layer 1: Datamarking ──── U+E000 between every word (drops attack <3%)
  ├─ Layer 2: Prompt structure ── XML delimiters, "RAW DATA" framing
  ├─ Layer 3: Input sanitization ── HTML strip, base64 remove, invisible chars
  ├─ Layer 4: Output validation ── Strict JSON schema, enum types
  └─ Layer 5: Architecture ──── No tool access, advisory only, fresh process
```

### Watch Daemon

```
./run.sh watch [--model gemini|claude]
  │
  ├─ Fetch 1Password credentials (once, cached in GML_CACHED_CREDS)
  │
  └─ Loop every N minutes (analysis.schedule_minutes, default 360):
       ├─ run_analyze() — full 3-step pipeline
       ├─ Log success/failure
       └─ sleep Nm
```

## Full Vision (all 3 modes)

```mermaid
graph TD
    subgraph Mode1["Mode 1: Rule Engine ✅"]
        rules["rules.yaml\n(config-driven)"]
        engine["Rule Engine\n(Go)"]
    end

    subgraph Mode2["Mode 2: AI Analysis ✅"]
        llm["Gemini / Claude CLI\n(analysis)"]
        analysis["Per-concern insights\nGmail search links\nDSH notifications"]
    end

    subgraph Mode3["Mode 3: Plan-and-Approve ✅"]
        llm2["LLM\n(proposes rules)"]
        reconcile["Reconcile gate\nstructural floor +\nLLM fold (1 rule/sender)"]
        dsh["DSH Dashboard\n(human approval)"]
        applyd["apply-rules (deterministic)\nstep 4/4 of knowledge cycle\n+ same-sender guard"]
        approved["rules.yaml\n(hot-reloaded by rules daemon)"]
    end

    gws["gws CLI"] -->|"JSON"| engine
    gws -->|"JSON"| llm
    gws -->|"JSON"| llm2

    engine -->|"archive + GML/archived label\n(atomic modify)"| gmail["Gmail API"]
    rules --> engine

    llm --> analysis
    llm2 -->|"proposed rule"| reconcile
    reconcile -->|"new or folded (supersedes)"| dsh
    dsh -->|"approved"| applyd
    applyd -->|"withholds OR-union footgun"| approved
    approved --> rules
```

**Mode 3 knowledge cycle (per interval):** `learn → distill → propose (folds, 1 rule/sender) →
apply-rules (deterministic, same-sender guard)`. The merge/conflict LLM is retired from the cycle
(manual `apply-rules --model` diagnostic only); conflict *prevention* now lives in the reconcile
gate, with a deterministic guard as the backstop. See ASSUMPTIONS GML-063/064/065.

**Insight provenance (back-tracking, iteration 020).** Each artifact records `source_insights`
(DSH notification #IDs), threaded deterministically — the distill prompt makes
`pattern.gmail_search == insight.Link`, so the join attributes insight→pattern with no LLM, then
field-copy carries it pattern→proposal→rule; todos get a `(insight #N)` back-link from LLM
attribution.

```
insight #ID ──(dismiss+comment)──▶ distill ──[Link≡gmail_search join]──▶ knowledge.source_insights
     │                                                                          │ Generate (copy)
     │                                                                          ▼
     └─(LLM-attributed)──▶ todo "(insight #N)"                         plan.source_insights
                                                                                │ apply (copy)
                                                                                ▼
                                                                       rule "# insights #N"
```
Dedup off this: `distill-gather` skips insights already in a pattern's `source_insights`;
`structuralDedup` skips candidates whose source-insight set a live plan already covers. Forward-
only. See ASSUMPTIONS GML-066.

**Insight dedup (iteration 021).** The `learn` LLM re-derives the same insight each cycle with a
different `gmail_search` string, which the structural key can't collapse. Insights are deduped by an
**identity key** = the query's `from:`-tokens + category (NOT the volatile full query). The learn
path now mirrors the analyze path's dedup stage, and the shared poster classifies deterministically:

```
learn ─▶ gemini ─▶ insight-dedup (LLM, vs DISMISSED) ─▶ insights = ClassifyInsights (vs ACTIVE)
                   │  reworded dup → drop                │  identity match (active) → PATCH update
                   │  genuinely new → keep "Update:"     │  exact-query repost   → skip (floor)
                   ▼                                      ▼  else                  → post new
              (2b re-surface)                       (2a update in place)
```
Identity is derived symmetrically — both the stored row and the candidate parse `from:`-tokens from
their notification — so a candidate's key is the key it will have once stored; a `from:`-less insight
falls back to the structural floor. `PATCH /api/v1/notifications/{id}` is guarded `dismissed_at IS
NULL`, so an update never resurrects a dismissed insight. See ASSUMPTIONS GML-068.
