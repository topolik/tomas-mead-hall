# MOP — Iteration 002: A/B Experiment Design

- **Phase:** 0 — Ideation (experiment design)
- **Start:** 2026-05-29
- **End:** 2026-05-29

---

## Experiment: Personas vs. Principles

**Question:** Does the multi-persona model (reading PERSONA.md, SKILLS.md, standing orders, role-based push-back) produce better outcomes than a principles-only approach?

### Arms

| | Arm A — Personas | Arm B — Principles only |
|--|-----------------|------------------------|
| **Project** | DSH: filter and sort notifications | DSH: notification comment field |
| **Method** | Read Security, QA, PM persona files + standing orders. Each domain reviews the plan in their voice. Push-back encouraged. | One conversation, no persona files. Apply principles directly: define done, flag risks, write test requirements. |
| **Artifacts** | Same for both: iteration files, checklist, test requirements |

**Assignment rationale:** Comment field has more security surface (user input → DB → display). Assigning it to Arm B (no personas) tests the harder case — can principles alone catch what Security standing orders would flag?

### Scorecard (filled after each review)

| Metric | Arm A (filter/sort) | Arm B (comment) |
|--------|-------------------|-----------------|
| **Planning issues flagged** | 9 (4 SEC, 3 QA, 2 PM) | 5 (XSS, SQLi, CSRF, input size, authz) |
| **False alarms in planning** | 0 | 0 |
| **Review feedback from Tomas** | 4 rounds, 6 items | 2 rounds, 2 items |
| **...that planning should have caught** | 3 (multi-select, negative filters, message search) | 2 (text-vs-input display, click target size) |
| **Bugs found during implementation** | 0 | 0 |
| **Security-related bugs** | 0 | 0 |
| **Test requirements → real bugs** | 0/8 (tests confirmed correctness, no bugs caught) | 0/5 (tests confirmed correctness) |
| **Planning overhead** | Medium (24 ACs, 9 concerns, 3 decisions) | Low (12 ACs, 5 concerns, 3 decisions) |
| **Tomas satisfaction (1-5)** | 3 (good but notable UI behavior gaps) | 4 (good, minor UX gaps, no functional bugs) |

### Protocol

1. Run Arm A first (filter/sort with personas), full cycle through review
2. Score Arm A
3. Run Arm B (comment field with principles only), full cycle through review
4. Score Arm B
5. Compare, decide on MO revision

### What counts as "persona work"

Arm A must:
- Read `team/security/PERSONA.md` + `SKILLS.md` before security review
- Read `team/qa/PERSONA.md` + `SKILLS.md` before writing test requirements
- Read `team/pm/PERSONA.md` + `SKILLS.md` before creating the plan
- Each domain explicitly states concerns in their voice (not just "also consider security")
- Push-back must be genuine — flag real issues, not rubber-stamp

Arm B must NOT:
- Read any persona files
- Role-play domain voices
- Reference standing orders

Arm B MUST still:
- Write test requirements
- Consider security implications
- Create a plan with checklist
- Follow the same lifecycle phases

---

## Comparison & Analysis

### Confounding factor: scope asymmetry

Tomas flagged this: Arm A (filter/sort) was substantially more complex — multiple filter dimensions, checkboxes, NOT toggles, multi-value parsing, JavaScript auto-submit, debounce. Arm B (comment field) was one textarea, one endpoint, one piece of JS. The raw numbers can't be compared at face value.

### What the data shows

**Security coverage was equivalent.** Both arms caught the same categories: XSS, SQL injection, CSRF, input validation. Arm A's persona-driven security review produced 4 concerns; Arm B's principles-only approach produced the same types with 5 concerns. The Security persona file didn't surface anything that "parameterized SQL, html/template escaping, CSRF on POST" wouldn't catch from first principles.

**Planning missed the same thing both times: UX.** Arm A missed 3 UX items (multi-select, negative, message search — features Tomas wanted that planning didn't anticipate). Arm B missed 2 UX items (text-vs-input display, click target size). In both cases, the gap was in anticipating how Tomas would actually use the feature, not in technical or security concerns.

**Persona overhead was real but didn't pay for itself.** Arm A had 24 ACs and 9 concerns (medium overhead). Arm B had 12 ACs and 5 concerns (low overhead). The extra planning in Arm A didn't prevent the UX feedback — it caught more technical edge cases that didn't end up being bugs anyway.

**Neither arm caught real bugs via tests.** All tests in both arms confirmed correctness rather than catching defects. This is partly because the implementations were straightforward — no complex edge cases emerged.

### Conclusion

For this project (single-developer, single-user personal tool), **principles-only is sufficient**. The persona model adds overhead without proportional benefit:

1. Security concerns are adequately covered by applying standard web security principles (parameterized SQL, template escaping, CSRF, input validation). The Security persona file didn't add novel insight beyond what a careful engineer already knows.
2. The real gap in both arms was UX anticipation — and persona role-play didn't help with that. Only Tomas's actual review caught UX issues.
3. QA test requirements were comparable in quality between both arms.
4. The lower planning overhead in Arm B (low vs medium) is a net win for velocity without a quality tradeoff.

### Recommendation

**Simplify the MO:** Drop the requirement to read persona files and role-play domain voices during planning. Instead:
- Keep the lifecycle phases (ideation → planning → implementation → review)
- Keep the requirement to write test requirements, flag security risks, and create checklists
- Apply principles directly rather than through persona lenses
- Keep persona SKILLS.md as a reference for what the team has learned (useful context), but don't mandate reading them during every phase
- Keep standing orders as a checklist to verify against, not a voice to role-play

This preserves the value (structured process, security awareness, test-first) while reducing the ceremony.
