# DSH — Iteration 001: Ideation

- **Phase:** 0 — Ideation
- **Phase lead:** Analyst
- **Start:** 2026-05-27
- **End:** 2026-05-27

---

## The Idea

A local web dashboard that Tomas can open from any machine and see what's going on across all projects and agents. Right now tracking lives in `todo.txt` (ideas), markdown files in `project-management/` (projects), and nowhere (agent outputs). That's fine for a few projects, but doesn't scale and creates context-switching overhead when Tomas needs to find out what needs attention.

The core problem: **visibility and surfacing action items**. Not project management tooling for its own sake — Tomas already has git history and markdown files. The dashboard's job is to present the right things at the right time.

---

## What It Does (Scope for MVP)

### Confirmed features
- **Idea backlog** — view and manage `todo.txt`-style ideas, organized by Eisenhower quadrant (Q1–Q4)
- **Project management progress** — per-project status: phase, last updated, any blockers
- **Notifications** — surface items waiting for Tomas's action (e.g., "review ready", "decision needed")
- **Add TODOs** — Tomas can create new backlog items from the UI

### Explicitly deferred
- Agent run outputs (undefined; see DSH-004)
- Mobile UI (desktop-first for now)
- Multi-user (single user: Tomas)

---

## What It's Not

- Not a replacement for git — source of truth stays in the repo
- Not a task management system (no assignees, sprints, estimates)
- Not a monitoring dashboard (no uptime, metrics, infra)

---

## Open Questions (not deciding now — need input or exploration in Phase 1)

### Auth
How does the dashboard authenticate Tomas?
- Option A: No auth — trusted home network / VPN, HTTP only. KISS.
- Option B: Static token in Authorization header or cookie. One secret, easy to set up.
- Option C: OIDC / OAuth2 (e.g., Tailscale auth). More robust but heavy.
- **Lean:** Option A or B for MVP. Option C is a real-world concern if ever exposed past VPN.

### Client discovery
How do laptop and desktop know the dashboard URL?
- Hardcoded hostname (e.g., `http://homeserver:8080`)
- mDNS (`.local` hostname)
- Tailscale hostname (if VPN is Tailscale)
- **Need to know:** does Tomas use Tailscale or a different VPN? What's the home server's hostname?

### File ingestion contract
"Long-term data via files" — which files, what format, where?
- Option A: Projects drop a `.json` sidecar in their repo folder that the dashboard watches/polls
- Option B: A dedicated `dashboard-data/` directory in this repo that projects write to
- Option C: REST API only; files were a nice idea but REST is sufficient
- **Lean:** REST first. Add file ingest only when a concrete use case demands it.

### Database
SQLite is the obvious KISS pick for a Go container:
- No separate DB container
- Data persists on a named volume
- Good enough for one user, dozens of projects
- **Lean:** SQLite unless a reason to deviate emerges in Phase 1.

### Frontend approach
- Option A: Go `html/template` + HTMX — server-rendered, dynamic without SPA complexity, single binary
- Option B: Embedded SPA (React/Svelte) compiled and embedded in Go binary — more JS tooling
- Option C: Serve static files from a separate frontend build
- **Lean:** Option A (HTMX). Matches KISS, keeps it a single binary, minimal JS.

### Bootstrap problem
This dashboard will eventually track itself (DSH project in project list). Worth building with that in mind — the dashboard must be able to ingest its own project data without special-casing.

### Data model sketch (exploratory, not final)
```
Project
  - code (e.g. DSH)
  - name
  - status (Ideation | Planning | Implementation | Review | Done | Parked)
  - priority (Q1–Q4)
  - lead
  - last_updated
  - current_phase_started

Backlog item
  - id
  - text
  - priority (Q1–Q4)
  - added_date
  - status (open | in_progress | done | parked)
  - project_code (optional link)

Notification
  - id
  - project_code
  - message
  - created_at
  - dismissed_at (null = active)
  - type (action_needed | info)
```

---

## Key Constraints Established

- Go + Docker (see DSH-001)
- Home server hosting for MVP (see DSH-002)
- REST API as primary ingest; files secondary (see DSH-003)
- Agent outputs out of scope (see DSH-004)

---

## Candidate Project Name

`DSH-dashboard` (code `DSH`) — straightforward, matches MO example.

---

## Phase Lead Recommendation for Phase 1

- **PM** to write the planning checklist
- **QA** to define test requirements first (TDD)
- **Developer + Analyst** to produce the architecture diagram
- **Security** to review secret handling (API tokens, volume permissions)

---

## Decisions

### [Decision: Project name and code]
**Date:** 2026-05-27 10:00
**Phase:** Ideation
**Decided by:** Tomas + Analyst
**Decision:** `DSH-dashboard`, code `DSH`
**Alternatives considered:** WDB, DAM
**Reasoning:** DSH is used as the canonical example in MODUS_OPERANDI.md — no ambiguity.
**Revisit if:** Another project takes DSH first (unlikely).

### [Decision: Phase 0 lead]
**Date:** 2026-05-27 10:00
**Phase:** Ideation
**Decided by:** Analyst
**Decision:** Analyst leads Phase 0. Developer is overall project lead.
**Alternatives considered:** Developer-led from the start
**Reasoning:** Per MO: Phase 0 is Analyst + Psychologist. Psychologist persona doesn't exist yet — Analyst runs solo for now.
**Revisit if:** Psychologist is bootstrapped; they can contribute retrospectively to the ideation notes.
