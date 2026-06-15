# GML — Iteration 010: Mode 3 Ideation — Behavioral Learning & Rule Proposals

- **Phase:** 0 — Ideation
- **Phase lead:** Analyst
- **Start:** 2026-05-29

---

## Goal

Mode 3 teaches the Gmail agent to learn from Tomas's actual behavior — comments on notifications, email reply patterns, read/ignore signals — and propose new rules for the rule engine. The system observes what Tomas does with emails, builds a behavioral knowledge base, and surfaces actionable rule proposals via DSH notifications.

**Core principle:** The agent proposes, Tomas approves. No rule is ever auto-applied. (MO §8: No AI-vs-AI acceptance.)

---

## Two Stages

### Stage A: Behavioral Insights

Extract and surface Tomas's email behavior patterns as DSH notifications. "Here's what I observe about how you handle email." This validates that the learning signals are accurate before trusting them for rule proposals.

### Stage B: Rule Proposals

Use the validated behavior patterns to propose concrete rules. Each proposal appears as a DSH notification. Tomas comments "approved" to activate, or dismisses to reject.

---

## Data Sources for Learning

### 1. DSH Notification History (comments + dismissal patterns)

**What exists:** Notifications table with `comment` (TEXT), `dismissed_at` (DATETIME), `message`, `priority`, `link`, `created_at`.

**What Tomas writes:** Free-text comments before dismissing — reactions, decisions, notes about what he did or plans to do.

**Signals:**
- Comment content → explicit feedback on what mattered and what didn't
- Time-to-dismiss → how quickly Tomas acted on different concern types
- Priority vs. action → did Q1 concerns get faster response than Q3?
- Patterns in comments → recurring phrases like "already handled", "don't care", "need to reply"

**Gap:** Current DSH API only returns active (non-dismissed) notifications (`WHERE dismissed_at IS NULL`). Mode 3 needs access to historical dismissed notifications with their comments.

**Required DSH change:** Add `include_dismissed=true` query parameter to `GET /api/v1/notifications`. Return all matching notifications regardless of dismissed state.

### 2. Gmail Thread Analysis (reply detection)

**Discovery:** `gws` supports `gws gmail users threads get <thread_id>` — returns all messages in a thread. `MessageRef` already carries `ThreadID` from list results.

**Signals:**
- Thread contains message with `SENT` label → Tomas replied
- Thread has only `INBOX`/`UNREAD` labels → received but not replied to
- Thread has `INBOX` without `UNREAD` → read but not replied to (acknowledged/ignored)
- Time between received and reply → response urgency patterns

**New gws operations needed in Go:**
- `GetThread(threadId)` → returns all messages in thread with their labels

### 3. Gmail Label & State Signals

**Already available via existing `gws` operations:**
- Read/Unread state (UNREAD label presence)
- Starred status (STARRED label)
- Custom labels (user-applied categorization)
- Archive state (message no longer has INBOX label)

**Signals:**
- Emails from sender X are always read within 1 hour → high priority
- Emails with label Y are always left unread for 3+ days → low priority / ignore
- Emails from sender Z always get starred → action-required pattern
- Emails matching subject pattern always get archived same day → auto-archive candidate

### 4. Existing Rules (baseline)

Current `rules.yaml` shows what Tomas has already explicitly automated. Mode 3 should:
- Not re-propose rules that already exist
- Identify gaps — senders/patterns that behave like existing rules but aren't covered
- Suggest refinements — "rule X catches 80% of pattern Y; broadening the pattern to Z would catch 95%"

---

## New Rule Types

Mode 3 can propose rules beyond the existing three types:

| Existing | New (Mode 3 can propose) |
|----------|--------------------------|
| `archive_by_age` | `auto_label` — apply a label based on sender/subject pattern |
| `archive_by_sender` | `auto_star` — star emails matching criteria |
| `archive_by_label` | `priority_by_sender` — auto-assign priority tier for analysis |
| | `archive_by_subject` — archive based on subject pattern match |
| | `archive_by_thread_age` — archive read threads older than N days with no reply |

New types require rule engine extension — but only when a proposal using them gets approved. Stage A ships without engine changes.

---

## Architecture

### New Command: `gml learn`

```
./run.sh learn [--stage insights|propose] [--days N]
```

Pipeline:
1. **Fetch historical data** (in container):
   - `gml history --days N` → collects:
     - Dismissed DSH notifications with comments (via new DSH endpoint)
     - Gmail thread states for emails referenced in those notifications
     - Current inbox patterns (sender frequency, label distribution, read/unread ratios)
   - Outputs structured JSON to stdout

2. **LLM analysis** (on host):
   - Stage A: "Given these behavioral signals, what patterns do you see?"
   - Stage B: "Given these patterns, propose concrete rules in YAML format"
   - Same model selection as Mode 2 (Gemini default, `--model claude`)

3. **Post results** (in container):
   - `gml propose` → validates LLM output, posts to DSH as notifications
   - Stage A: Info notifications summarizing behavior patterns
   - Stage B: Action_needed notifications with proposed rules

### Approval Flow

1. Proposed rule appears as DSH notification:
   ```
   🟡 Q2 [Rule Proposal] Archive emails from noreply@jira.example.com older than 7 days (read) — matches 23 emails/month, you've manually archived 18 of them
   ```
2. Tomas reviews, comments "approved" (or "approved with changes: ...")
3. Next `./run.sh learn --stage propose` run:
   - Reads approved proposals from DSH comments
   - Generates updated `rules.yaml` section (or applies via new `gml apply-rule` command)
   - Posts confirmation notification

### Cost Controls

- `--days` flag with default 30, max 90 (configurable)
- Historical notification limit: last 200 dismissed notifications
- Gmail thread lookups limited to emails referenced in notifications (not full inbox scan)
- LLM token budget logged per run

---

## Security Considerations

- **Prompt injection defense (GML-019):** DSH comments and historical notifications fed to LLM get the same 5-layer protection as email content. Comments are user-authored but could contain copy-pasted email text.
- **Reversibility (GML-009):** All proposed rules compose to "archive = remove INBOX label only." New rule types (auto_label, auto_star) are also non-destructive and reversible.
- **No auto-apply:** Proposed rules never execute without explicit "approved" comment from Tomas.
- **Credential handling:** Same stdin-pipe model — `gml history` reads creds from stdin like other commands.

---

## DSH Changes Required

1. **`GET /api/v1/notifications?include_dismissed=true`** — return dismissed notifications with comments and dismissed_at timestamp
2. **Optional:** `GET /api/v1/notifications?project_code=GML&has_comment=true` — filter to only notifications with non-empty comments (optimization for learning)

These are additive — no breaking changes to existing API.

---

## Open Questions

1. **Comment format for approval** — exact string matching ("approved") or more flexible (LLM parses the comment)? Exact string is safer; LLM parsing is more natural.
2. **How to apply approved rules** — append to `rules.yaml` automatically, or output a diff for Tomas to review? Auto-append is convenient; diff is more explicit.
3. **Feedback loop** — should rejected proposals be remembered so the system doesn't re-propose them? (Probably yes — "rejected" comment serves as negative signal.)
4. **Stage A output format** — free-text insights? Structured patterns? Both?

---

## Decisions

### [GML-044] Mode 3 ships in two stages
**Date:** 2026-05-29
**Phase:** Ideation
**Decided by:** Tomas
**Decision:** Stage A (behavioral insights) ships first, Stage B (rule proposals) follows after Stage A is validated.
**Alternatives considered:** All-at-once delivery
**Reasoning:** Validates that the learning signals are accurate before trusting them for automated rule proposals. Lower risk.
**Revisit if:** Stage A signals are obviously perfect and Stage B is trivial to add.

### [GML-045] Mode 3 supports new rule types
**Date:** 2026-05-29
**Phase:** Ideation
**Decided by:** Tomas
**Decision:** Mode 3 can propose existing rule types AND new ones (auto_label, auto_star, priority_by_sender, archive_by_subject, archive_by_thread_age). New types added to engine only when a proposal using them gets approved.
**Alternatives considered:** Existing types only (too limiting), all new types upfront (premature)
**Reasoning:** Learning may reveal patterns that don't fit existing rule types. Building new types on-demand avoids premature engineering.
**Revisit if:** Never — this is the right incremental approach.

### [GML-046] Rule approval via DSH notifications
**Date:** 2026-05-29
**Phase:** Ideation
**Decided by:** Tomas
**Decision:** Proposed rules appear as DSH action_needed notifications. Tomas comments "approved" to activate, dismisses to reject. No dedicated UI.
**Alternatives considered:** Dedicated DSH rules page (too much UI work), rules.yaml manual merge (too manual)
**Reasoning:** Reuses existing notification + comment infrastructure. Tomas already reviews notifications daily. KISS.
**Revisit if:** Approval volume exceeds what's comfortable in the notification flow.

### [GML-047] Learning triggered by explicit command, not watch cycle
**Date:** 2026-05-29
**Phase:** Ideation
**Decided by:** Tomas
**Decision:** `./run.sh learn` is a separate command Tomas invokes when he wants fresh insights/proposals. Not part of the watch daemon cycle.
**Alternatives considered:** Auto-learn every N watch cycles
**Reasoning:** Explicit invocation avoids surprise LLM costs and gives Tomas control over when proposals appear.
**Revisit if:** Tomas finds he always forgets to run it — then consider a weekly schedule.

### [GML-048] Reply detection via gws thread support
**Date:** 2026-05-29
**Phase:** Ideation
**Decided by:** Team
**Decision:** Use `gws gmail users threads get <thread_id>` to detect replies. A thread containing a message with `SENT` label means Tomas replied. This is a stronger behavioral signal than read/unread alone.
**Alternatives considered:** Read/unread only (weaker signal), `in:sent` query matching (harder to correlate)
**Reasoning:** Empirically confirmed `gws` supports thread operations. ThreadID already present in message list results. Direct thread lookup is most reliable.
**Revisit if:** gws thread API proves too slow for batch historical lookups.
