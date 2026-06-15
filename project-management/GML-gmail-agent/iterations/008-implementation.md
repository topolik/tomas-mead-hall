# GML — Iteration 008: Mode 2 Implementation

- **Phase:** 2 — Implementation
- **Phase lead:** Developer
- **Start:** 2026-05-28
- **End:** 2026-05-28

---

## Scope

Implementation of Mode 2: AI Email Analysis as planned in iteration 007. Three-step pipeline (container fetch → host LLM → container notify) with 5-layer prompt injection defense, per-concern notifications with Gmail search links, Gemini as default model, and scheduled `watch` daemon.

---

## What Was Built

### New Go packages (1,621 lines added across GML)

| Package | Purpose |
|---------|---------|
| `internal/fetch/` | Fetches emails from 6 boxes (5 triage + unboxed remainder), full body, time-window query builder |
| `internal/sanitize/` | HTML→text (`html2text`), datamarking (U+E000), base64 stripping, invisible char removal, injection flagging |
| `internal/prompt/` | Builds complete LLM prompt: system instructions, per-concern output schema, box definitions, previous analysis, datamarked emails |
| `internal/notify/` | LLM JSON validation (`ConcernAnalysis` schema), Gmail search URL generation, DSH OAuth2 client, notification posting |

### New commands

- `gml fetch --days N` — fetches, sanitizes, builds prompt, writes to stdout
- `gml notify` — reads LLM JSON from stdin, validates schema, posts per-concern notifications to DSH

### Shell orchestration (`run.sh`)

- `./run.sh analyze [--days N] [--model gemini|claude]` — three-step pipeline with temp files
- `./run.sh watch [--model gemini|claude]` — scheduled analysis daemon
- Gemini as default model; `--model claude` to switch
- 1Password credential caching for watch mode (`GML_CACHED_CREDS` shell variable)
- Helper functions: `run_analyze()`, `send_creds()`, `read_config()`

### DSH changes (prerequisite)

- `GET /api/v1/notifications` endpoint (JWT-protected, project_code filter, limit param)
- `link` column in notifications table (migration 006)
- `[open]` link button in notification UI
- 14 integration tests (XSS, SQL injection, auth enforcement)

### Config extension

- `analysis:` section in `rules.yaml`: `days`, `max_days`, `schedule_minutes`, `dsh:` (url, client_id, client_secret)
- `EffectiveDays()`, `EffectiveMaxDays()`, `EffectiveScheduleMinutes()` defaults

---

## Deviations from Plan

### Per-concern schema (replaces per-box)

The 007 plan specified one notification per non-empty box (up to 6). During implementation and real-world testing, Tomas requested per-concern notifications instead — one notification per actionable concern (3-15 total), each with:
- Box prefix `[Box N — Name]` for prioritization context
- `gmail_search` field with a clickable Gmail search URL
- `email_ids` for traceability

This is more actionable than per-box summaries. See decision [GML-036].

### Gemini CLI support as default model

The 007 plan specified Claude-only. During implementation, Gemini CLI was added as default model (cheaper, sufficient quality for analysis). Claude available via `--model claude`. See decision [GML-037].

### `stripCodeFence()` hardening

Gemini sometimes returns prose before/after JSON. `stripCodeFence()` was hardened to strip: markdown code fences, preamble text before `[` (JSON array start), trailing text after `]`. Necessary for reliable Gemini output parsing.

### Temp files for large prompts

Initial implementation passed the prompt as a shell argument to `claude -p`. This hit `ARG_MAX` for large inboxes. Fixed by writing to temp files and using stdin redirection.

### 1Password credential caching in watch mode

`op item get` requires user approval each time. For the scheduled watch daemon, credentials are fetched once at startup and cached in `GML_CACHED_CREDS` shell variable (memory only, never disk).

---

## Bugs Found & Fixed

| Bug | Cause | Fix |
|-----|-------|-----|
| `ARG_MAX` exceeded for large prompts | Shell argument passed directly to `claude -p` | Write prompt to temp file, redirect stdin |
| Gmail search URLs had wrong encoding | `url.PathEscape` keeps colons unescaped, uses `%20` | Switched to `url.QueryEscape` (uses `+` and `%3A`) |
| BOM character in test source | Literal UTF-8 BOM (U+FEFF) in Go source file | Changed to hex escape `"\xEF\xBB\xBF"` |
| DatamarkerSpoofing test wrong expectation | U+E000 is not whitespace — `strings.Fields` doesn't split on it | Corrected expected word count |
| DSH integration tests stale | `bootstrap()` signature changed (2→1 params), `buildMux()` lost `totpKey` param | Rewrote test harness with current signatures |
| Box prefix missing from notifications | Notifications showed `[concern] summary` without box context | Changed format to `[Box N — Name] concern — summary` |
| `gml notify` rejected Gemini output | Gemini prepends prose before JSON array | Hardened `stripCodeFence()` to strip preamble/trailing text |

---

## Test Coverage

### `internal/sanitize/sanitize_test.go` — 30 tests
- 6 injection patterns (ignore previous, system prompt override, role play, encoding tricks, markdown injection, delimiter escape)
- 7 invisible character categories (zero-width space, joiner, non-joiner, soft hyphen, direction overrides, BOM, word joiner)
- Base64 stripping, HTML attacks, multi-vector attacks (datamarker spoofing, unicode homoglyphs)

### `internal/notify/validate_test.go` — 17 tests
- Schema validation (valid JSON, freeform text, invalid type, missing fields, empty array)
- Code fence stripping (markdown, preamble text before `[`, trailing text after `]`)
- Gmail URL escaping, XSS prevention in summary/gmail_search fields
- Malicious gmail_search (javascript: protocol, HTML injection)

### `projects/DSH-dashboard/cmd/dsh/integration_test.go` — 14 tests
- XSS in message, link, project_code fields
- SQL injection in message and link fields
- Invalid notification type, empty message
- Auth enforcement (missing/invalid JWT)
- Template escaping verification

---

## Commits

| Hash | Description |
|------|-------------|
| `07ca86e` | Mode 2 implementation — fetch, sanitize, prompt, notify pipeline |
| `e6c0a82` | Fix: temp files for large prompts, strip markdown code fences |
| `248109e` | Per-concern notifications with Gmail links, Gemini default, injection tests |
| `14dc382` | Scheduled analysis daemon via ./run.sh watch |
| `243dc8f` | Cache 1Password credentials in watch mode |

---

## Decisions

### [GML-036] Per-concern notifications replace per-box summaries
**Date:** 2026-05-28
**Phase:** Implementation
**Decided by:** Tomas
**Decision:** LLM outputs per-concern items (3-15 per run), each with `concern`, `box`, `box_name`, `type`, `summary`, `email_ids`, and `gmail_search`. Each becomes one DSH notification formatted as `[Box N — Name] concern — summary` with a clickable Gmail search link.
**Alternatives considered:** Per-box summaries (007 plan — too coarse, not actionable), per-email notifications (too noisy)
**Reasoning:** Tomas found per-box summaries lacked actionability. Per-concern maps to "one thing I can act on" — read, reply, archive. Gmail search link lets Tomas jump directly to the relevant emails.
**Revisit if:** Notification volume becomes noisy (then add priority filtering or daily digest)

### [GML-037] Gemini CLI as default model
**Date:** 2026-05-28
**Phase:** Implementation
**Decided by:** Tomas
**Decision:** Gemini CLI is the default model for `./run.sh analyze` and `./run.sh watch`. Claude available via `--model claude`. Gemini invocation: `GOOGLE_CLOUD_PROJECT=your-gcp-project-id npx @google/gemini-cli -p "" < input 2>/dev/null`.
**Alternatives considered:** Claude-only (007 plan), Gemini-only
**Reasoning:** Gemini is cheaper and quality is sufficient for email analysis. Claude is available for complex cases. Both produce the same JSON schema.
**Revisit if:** Gemini CLI sunsets (announced June 18, 2026) or LLM proxy project ships

### [GML-038] 1Password credential caching in watch daemon
**Date:** 2026-05-28
**Phase:** Implementation
**Decided by:** Tomas
**Decision:** `./run.sh watch` fetches 1Password credentials once at startup and caches them in `GML_CACHED_CREDS` shell variable. All subsequent analysis cycles use the cached value.
**Alternatives considered:** Re-fetch each cycle (requires user approval each time — defeats scheduled daemon purpose)
**Reasoning:** `op item get` prompts for biometric/approval each invocation. A daemon that needs approval every 6 hours isn't autonomous. Shell variable is memory-only, never written to disk, dies with the process.
**Revisit if:** 1Password adds headless service account support without env vars

### [GML-039] Temp files for prompt transfer between pipeline steps
**Date:** 2026-05-28
**Phase:** Implementation
**Decided by:** Developer
**Decision:** `run_analyze()` writes the LLM prompt and response to temp files (`mktemp`) with a `trap` cleanup on exit, instead of passing as shell arguments or through pipes.
**Alternatives considered:** Shell argument to `claude -p` (hit `ARG_MAX`), named pipes (complexity)
**Reasoning:** Large inboxes produce prompts exceeding `ARG_MAX`. Temp files are cleaned up on exit (including signals via trap). LLM response also needs temp file since `gml notify` reads from stdin.
**Revisit if:** Never — temp files with trap cleanup is the standard pattern for this
