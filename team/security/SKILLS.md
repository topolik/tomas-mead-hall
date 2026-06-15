# Security — Skills

Distilled capabilities gained from project work.

---

## LLM Prompt Injection Defense

- Design and implement 5-layer defense against indirect prompt injection in LLM pipelines processing untrusted input (emails, user content)
- Layer 1 — Datamarking: insert U+E000 (PUA) between words via `strings.Fields`/`strings.Join`; drops attack success from ~50% to <3% (Microsoft Research empirical results)
- Layer 2 — Prompt structure: XML delimiters (`<email_content>`) with explicit "RAW DATA" framing to establish data/instruction boundary
- Layer 3 — Input sanitization: HTML→text conversion, base64 block stripping, invisible character removal (zero-width, direction overrides, BOM, soft hyphen, word joiner), injection phrase flagging
- Layer 4 — Output validation: strict JSON schema enforcement with enum types, reject freeform text
- Layer 5 — Architectural constraint: LLM has no tool access (`claude -p`), output is advisory only, fresh process per run

## Container Security

- Run containers as non-root system user with minimal home directory (required by some CLIs for cache)
- Credential injection via stdin pipe — never env vars, never disk, never process args
- Short-lived access tokens passed to subprocesses; refresh tokens stay in Go heap

## Injection Test Suites

- Write comprehensive injection test batteries: 6 prompt injection patterns, 7 invisible character categories, multi-vector attacks (datamarker spoofing, unicode homoglyphs)
- XSS prevention testing: verify `html/template` auto-escaping in notification messages, links, and project codes
- SQL injection testing: parameterized queries verified against crafted payloads in message and link fields

## Hardening agentic LLM CLIs used as text backends

- Treat an agentic CLI invoked for "plain completions" as a **tool-capable process until proven otherwise**: gemini-cli's `-e none` disables only extensions/MCP, NOT built-in tools (`write_file`…); headless `-p` mode marks every workspace trusted; and user-level settings (`defaultApprovalMode: auto_edit`) leak into every headless invocation — together a prompt can make a "completion" write files (incident MND-011 / LLP-014, 2026-06-12)
- The guard must be **layered and survive config regeneration**: an explicit CLI flag (`--approval-mode default` = auto-deny all tool calls non-interactively) in the command, *plus* a committed workspace `.gemini/settings.json` with the same policy — a fleet restart regenerated a runtime config from a stale example and silently reopened the gap the flag-only fix had closed
- Verify CLI security behavior **in the installed version's source** (npx cache `~/.npm/_npx/*/node_modules`), not docs: trust gating, settings resolution order, and tool availability all changed across gemini-cli minor versions

_Updated: 2026-06-12_
