# QA — Skills

Distilled capabilities gained from project work.

---

## LLM Pipeline Testing

- Test LLM output validation: valid/invalid JSON, freeform text rejection, enum enforcement, missing required fields, empty arrays
- Test LLM output normalization: markdown code fence stripping, preamble/trailing text removal for Gemini compatibility
- Verify URL generation safety: `url.QueryEscape` for Gmail search URLs, script injection prevention, javascript: protocol blocking

## Integration Testing

- Write end-to-end integration tests against real APIs (Gmail, DSH) — not mocks
- Test auth flows: OAuth2 client credentials → JWT → API calls, missing/invalid token rejection
- Verify DSH notification rendering: XSS escaping in templates, SQL injection in API inputs, COALESCE for nullable columns

## Security Test Design

- Design test matrices: 30 sanitization tests (injection patterns × character categories × multi-vector), 19 validation tests, 14 DSH integration tests
- Handle special characters in Go test source: hex escapes for BOM (`\xEF\xBB\xBF`), Unicode escapes for PUA characters, avoid literal bytes that cause compiler errors

## Filter & Query Testing

- Test multi-value API filters: comma-separated include, `!`-prefixed exclude, combined filters (type+priority), invalid values ignored
- Test negative filters against NULL columns: `NOT IN` must not silently drop NULL rows
- Test message text search: case-insensitive LIKE, multi-term OR, exclude terms, SQL injection in search params
- Verify filter state round-trips: URL params → server → template → hidden fields → submit → URL params

_Updated: 2026-05-29_
