# AI Team Workspace

A real-world example of a human + AI agent team building production software together. This repository contains the operational framework, four active Go projects, and the full iteration history showing how decisions were made, bugs were found, and features shipped.

## What's here

| Directory | Purpose |
|-----------|---------|
| `MODUS_OPERANDI.md` | How the team operates — lifecycle phases, commit policy, safety checklist, worktree workflow |
| `projects/` | Deliverables — working Go code with tests |
| `project-management/` | Workflow artifacts — iteration docs, decisions, architecture diagrams |
| `team/` | Domain skills accumulated from real project work |
| `templates/` | Standard artifact templates |
| `todo.txt` | Shared idea backlog with Eisenhower priority quadrants |

## Projects

| Code | Name | What it does |
|------|------|-------------|
| **DSH** | Dashboard | Local web UI aggregating projects, notifications, threads. WebAuthn passkey auth, OAuth2.1 for agents, Web Push, SQLite, HTMX. |
| **GML** | Gmail Agent | Automated email triage with plan-and-approve workflow. 3-pipeline architecture (Analyze → Knowledge → Rules), 5-layer prompt injection defense. |
| **LLP** | LLM Proxy | OpenAI-compatible gateway with Gemini/Claude/local GPU failover. Queue per impl, automatic failover, quota-aware cooldown, usage tracking. |
| **MND** | Mind Model | Distills human decision patterns from 1400+ AI sessions into an evidence-backed brain. Blind-replay fidelity eval, continuous retraining, agent orchestration. |

## Getting started

```sh
./setup.sh    # prerequisites, Docker builds, DSH client provisioning, config files
./run.sh      # start all services (DSH → LLP → GML → MND)
./stop.sh     # stop everything
```

```sh
./backup.sh               # encrypted backup of all project state
./backup.sh --dir /path   # all backups into one directory
./restore.sh              # restore latest backups (auto-detects)
./restore.sh --dir /path  # restore from a shared backup directory
```

Setup delegates to per-project `setup.sh` scripts — DSH clients for GML and MND are provisioned automatically.

Then:
1. Read `MODUS_OPERANDI.md` for the operational framework
2. Browse `projects/<CODE>/README.md` for how to run each project
3. Check `project-management/<CODE>/` for the full decision trail
4. See `team/developer/SKILLS.md` for accumulated technical capabilities

## Tech stack

- **Language:** Go
- **Infrastructure:** Docker containers, SQLite, Tailscale
- **LLM providers:** Gemini CLI, Claude CLI (via LLP proxy)
- **Frontend:** HTMX + server-rendered HTML templates
- **Auth:** WebAuthn passkeys, OAuth 2.1, TOTP
