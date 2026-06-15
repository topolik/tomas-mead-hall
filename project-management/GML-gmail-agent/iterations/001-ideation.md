# GML — Iteration 001: Ideation

- **Phase:** 0 — Ideation
- **Phase lead:** Analyst
- **Start:** 2026-05-27
- **End:** 2026-05-27

---

## The Idea

Automate Gmail maintenance via the `gws` Google Workspace CLI — get inbox stats and archive emails according to rules, reducing manual triage overhead. The todo specifically mentions `https://github.com/googleworkspace/cli` as the intended tooling.

---

## What `gws` Provides (Confirmed)

`gws` (Google Workspace CLI, `@googleworkspace/cli`) is a production-grade Rust CLI with:
- **`gws gmail +triage`** — unread inbox summary (sender, subject, date) as structured JSON
- **`gws gmail +watch`** — stream new emails as NDJSON
- **`gws gmail +send/+reply/+forward`** — composing helpers
- **Auto-discovery surface** — wraps all Gmail REST API methods directly (list, modify, trash, etc.)
- **`--page-all`** pagination for bulk queries
- **40+ agent skills included** — pre-built SKILL.md files for use with AI agents
- **Auth options:** interactive OAuth (`gws auth login`), credentials file, pre-obtained access token

---

## Candidate Feature Set

### Confirmed (from todo)
- **Stats** — inbox counts, breakdown by sender/label/age
- **Archive** — move messages out of inbox by rule (age, sender, label, read status)

### Plausible additions
- Scheduled runs (daily/weekly digest) surfaced to DSH
- "Reply needed" detection — emails from specific senders or with questions that haven't been replied to
- Archive preview ("dry run" before actually archiving)

### Explicitly deferred
- Full email composition (that's a separate workflow)
- Calendar or Drive integration (separate future project)

---

## Interpretation Spectrum

The "gmail agent" label could mean different things — ranked from simplest to most complex:

| Option | Description | Effort |
|---|---|---|
| A | Shell script wrapping `gws` commands — stats + archive on cron | Low |
| B | Go program using `gws` as subprocess — stats + rule-based archive, config file | Medium |
| C | Claude API agent that reads inbox via `gws`, decides what to archive, explains reasoning | High |
| D | Claude agent + DSH integration — surfaces decisions in dashboard for Tomas to approve | High+ |

KISS principle favors A or B. The "agent" label and the `gws` repo's own AI agent skills suggest C may be the intent.

---

## Open Questions (Blocking)

These need Tomas's answers before Phase 1 (Planning) can start.

### 1. Which Gmail account?
- `user@example.com` (work Google Workspace) — admins control OAuth app approval
- Personal Gmail — testing mode OAuth has a ~25-scope limit per app
- **Why it matters:** affects OAuth setup path, what `gws auth setup` can do unassisted, and whether Liferay IT approval is needed

### 2. Is `gws` already installed?
- Yes, already authenticated → can immediately use `gws gmail +triage` to test
- Needs install → `npm install -g @googleworkspace/cli` or binary download
- **Why it matters:** determines whether Phase 2 starts with working tooling or has an auth-setup sub-task

### 3. Scope of "agent" — script vs AI reasoning?
- Simple rules ("archive anything older than 30 days from newsletters") → Option A/B
- AI decides what to archive and why → Option C
- **Why it matters:** determines whether this is a 1-day script or a multi-day Claude API project

### 4. What stats matter most?
- Total unread count
- Top senders (by message volume)
- By label / category
- Inbox age distribution (how many emails >30/90/365 days old)
- **Why it matters:** drives the output format and what `gws` queries to run

### 5. What archive rules are wanted?
- Age-based (e.g., archive anything read and >90 days old)
- Sender-based (mailing lists, automated notifications)
- Label-based
- Combination
- **Why it matters:** drives configuration schema

### 6. Standalone or DSH-integrated?
- Standalone script/binary → runs via cron or manually
- Feeds into DSH → agent run outputs visible in dashboard (DSH explicitly deferred this in iteration 1, but this could be the first real use case)
- **Why it matters:** DSH integration is a larger scope change; if wanted, it should be planned now

### 7. Relationship to "Review Google OAuth2 apps Will shared"?
- That todo item likely refers to a list of approved OAuth apps for Liferay accounts
- If work Gmail is the target, GML may need to wait on or align with that review
- **Why it matters:** could be a dependency or completely unrelated

---

## Decisions

### [GML-001] Project code and name
**Date:** 2026-05-27 18:00
**Phase:** Ideation
**Decided by:** Analyst
**Decision:** Code `GML`, name `GML-gmail-agent`
**Alternatives considered:** EML, MAL
**Reasoning:** GML is the most recognizable abbreviation for Gmail
**Revisit if:** Another project claims GML

### [GML-002] `gws` as the tooling foundation
**Date:** 2026-05-27 18:00
**Phase:** Ideation
**Decided by:** Analyst
**Decision:** Use `gws` (googleworkspace/cli) as the primary Gmail interface — matches the todo explicitly
**Alternatives considered:** Gmail API directly (Python/Go), `gam` (Google Apps Manager)
**Reasoning:** Todo specifically cites this repo. It provides structured JSON output, agent skills, and handles auth/pagination — no value in rolling a custom wrapper.
**Revisit if:** `gws` auth is blocked by Liferay Google Workspace policy

### [GML-003] Target Gmail account
**Date:** 2026-05-27 18:30
**Phase:** Ideation
**Decided by:** Tomas
**Decision:** Liferay work Gmail — `user@example.com`
**Alternatives considered:** Personal Gmail, both accounts
**Reasoning:** Work inbox is the primary triage target; personal email not in scope for now.
**Revisit if:** Personal inbox becomes relevant

### [GML-004] Three-mode architecture
**Date:** 2026-05-27 18:30
**Phase:** Ideation
**Decided by:** Tomas
**Decision:** Three operating modes, implemented in separate iterations:
1. **Rule engine** — autonomous, config-driven rules (archive by age/sender/label), no LLM
2. **AI analysis** — Claude reads inbox, generates stats/insights, drafts responses (read-only)
3. **Rule creation** — Claude proposes new rules → Tomas approves via DSH → rule engine executes
**Alternatives considered:** Single AI-only mode, simple script only
**Reasoning:** Rules are predictable and safe to automate; AI adds value for ambiguous cases and response drafting; human approval gate before any new rule runs keeps risk low.
**Revisit if:** Trust in AI analysis grows enough to skip approval gate

### [GML-005] Container deployment
**Date:** 2026-05-27 18:30
**Phase:** Ideation
**Decided by:** Tomas
**Decision:** `gws` installed inside Docker container, credentials injected via bind-mount/env var (headless export flow, same pattern as DSH)
**Alternatives considered:** Install `gws` directly on host
**Reasoning:** KISS + consistency with DSH; container isolates dependencies and makes it portable.
**Revisit if:** Container overhead is a real problem

### [GML-006] Iteration sequence
**Date:** 2026-05-27 18:30
**Phase:** Ideation
**Decided by:** Tomas
**Decision:** Three iterations aligned with modes: iter 1 = rule engine MVP, iter 2 = AI analysis, iter 3 = DSH rule creation approval flow
**Alternatives considered:** Build all three modes together
**Reasoning:** Standalone rule engine is immediately useful and lower risk. DSH integration requires DSH to support agent outputs (not built yet) — deferring keeps scope manageable.
**Revisit if:** DSH agent output support is built earlier than expected
