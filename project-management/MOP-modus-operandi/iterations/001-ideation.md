# MOP — Iteration 001: Ideation

- **Phase:** 0 — Ideation
- **Phase lead:** Analyst
- **Start:** 2026-05-29
- **End:** (pending)

---

## Case Study: What Actually Happened

### DSH-dashboard (1 cycle: 4 iterations)
- Ideation → Planning → Implementation → Review: clean single cycle
- Tomas reviewed once, gave feedback, shipped
- Post-review: persona skills updated, ASSUMPTIONS.md updated

### GML-gmail-agent (2 cycles: 9 iterations)

**Cycle 1 (Mode 1): iterations 001-005**
- 001 ideation, 002 planning, 003 implementation — standard flow
- 004 planning-pivot: architecture changed mid-stream (shell scripts → Go daemon + stdin credential injection). Pivot protocol was followed (tag, updated criteria).
- 005 implementation-pivot: re-implemented, Tomas reviewed, shipped

**Cycle 2 (Mode 2): iterations 006-009**
- 006 ideation, 007 planning: thorough — 13 QA tests defined, security review, cost estimates, architecture diagram
- 008 implementation: built it, multiple real-world bugs found and fixed
- 009 review: Tomas tested live, gave 8 pieces of feedback, all addressed in-session

---

## What the MO Prescribes vs. What Happened

### 1. Personas — prescribed multi-role collaboration

**MO says:** Each phase is driven by specific personas (Analyst + Psychologist for ideation, PM + QA for planning, Developer + QA for implementation, PM + Coach for review). Personas have standing orders. "Personas are experts, not yes-men."

**What happened:** A single Claude conversation did everything. No persona was "pulled in" — the work was done and then SKILLS.md files were backfilled as if personas had participated. Nobody read the Security persona's standing orders before the security review. Nobody read QA's standing orders before writing tests.

**Assessment:** The persona system as written assumes multiple independent agents with persistent memory across sessions. In a single-conversation model, personas are fictional characters being role-played by one actor. The SKILLS.md files have value as a knowledge base (they capture what the team can do), but the persona *interaction* model (pulling in experts, standing orders, push-back) isn't happening.

### 2. Phase boundaries — prescribed commit-per-phase

**MO says:** Commit after every lifecycle phase. Intermediate commits during long phases are encouraged.

**What happened:** Mostly followed. Each phase got its own commit. Implementation phases had multiple intermediate commits (good). The discipline of "commit at boundaries" worked well.

**Assessment:** ✅ Works as designed. Keep.

### 3. Iteration files — prescribed structured artifacts per phase

**MO says:** Each phase produces a numbered iteration file with decisions at the bottom.

**What happened:** Followed. The files were useful for context across sessions (the conversation summary referenced iteration 007-planning extensively). Decision log was valuable for understanding "why" later.

**Assessment:** ✅ Works. The iteration files are the most useful MO artifact. Keep.

### 4. ASSUMPTIONS.md — prescribed non-obvious decision rationale

**MO says:** Non-obvious design decisions with ID, rationale, and affected area.

**What happened:** Maintained across both projects. GML has 21 decisions (GML-005 through GML-043). Actively referenced when making related decisions (e.g., GML-042 supersedes GML-031).

**Assessment:** ✅ Works. Keep.

### 5. Planning phase — prescribed TDD test requirements first

**MO says:** "QA writes test requirements FIRST (TDD)" and "Security reviews the plan."

**What happened:** GML 007-planning defined 13 test cases before implementation. Not all were implemented exactly as written (the schema changed from per-box to per-concern during implementation), but having the test *intent* upfront was useful. Security review section was written but was essentially the developer reviewing their own plan.

**Assessment:** Mixed. Test requirements upfront: useful directionally, but they change during implementation. Having them is better than not. The "QA persona writes them" fiction adds nothing over "write test requirements in the planning doc."

### 6. Review phase — prescribed PM + Coach present to Tomas

**MO says:** "Present the solution to Tomas in a way he can run it himself."

**What happened:** Tomas ran it himself and gave live feedback. No formal "presentation" — just "here's how to run it" followed by real testing. The feedback loop was fast (8 issues found and fixed in one session).

**Assessment:** ✅ The principle works. The "PM + Coach present" framing is unnecessary — what matters is that Tomas can run it and give feedback. Keep the principle, drop the persona assignment.

### 7. Persona skill updates — prescribed after each human-reviewed iteration

**MO says:** "Personas update their SKILLS.md with new capabilities gained."

**What happened:** Done after GML Mode 2 review. The SKILLS.md content is genuine (real capabilities were gained). But the update was done by the same conversation that did the work — not by separate persona agents reviewing their own growth.

**Assessment:** The SKILLS.md files are useful as a capability inventory. The question is: does anyone read them at the start of a new project to inform decisions? Not yet. They could be valuable if Claude reads them at conversation start to understand "what I already know how to do."

### 8. Diagrams — prescribed Mermaid in project-management/diagrams/

**MO says:** "Developer + Analyst produce an architecture diagram (Mermaid) — committed to diagrams/"

**What happened:** Done for GML. The diagrams were updated during implementation and review. Useful for understanding the pipeline at a glance.

**Assessment:** ✅ Works when the project has architecture worth diagramming. Overkill for simple projects. Keep as optional.

### 9. Time tracking — prescribed timestamps on all artifacts

**MO says:** Iteration files include start/end dates, decisions include timestamps.

**What happened:** Dates were recorded but times were mostly omitted. The timestamps helped track how long phases took (GML Mode 2: ideation through review in ~2 days).

**Assessment:** ✅ Dates are sufficient. Times are overkill. Simplify to dates only.

### 10. No AI-vs-AI acceptance testing

**MO says:** "The team must not play both developer and customer/tester simultaneously."

**What happened:** Respected. Real Gmail data, real LLM calls, real DSH. Tomas reviewed live. No simulated acceptance.

**Assessment:** ✅ Critical principle. Keep.

---

## Proposed Changes

### P1. Simplify personas to a skills registry

**Change:** Drop the multi-persona interaction model (pulling in experts, standing orders enforcement, push-back between personas). Keep SKILLS.md files as a **capability registry** — read at conversation start to understand what the team already knows how to do. Personas remain as organizational buckets for skills, not as characters with voices.

**What to remove from MO:**
- "Each persona has a unique role, personality, voice" → replace with "each domain has a skills file"
- "Personas are experts, not yes-men" and "push back" language → irrelevant without multi-agent
- "The initial roster... is defined in `team/<persona>/PERSONA.md`" → keep SKILLS.md, make PERSONA.md optional
- Standing orders section → merge critical rules (no secrets, tests first) into project-level conventions or CLAUDE.md

**What to keep:**
- SKILLS.md files — updated after reviews, read at project start
- The concept of domains (security, QA, performance) as lenses to apply during work

### P2. Simplify phase roles to principles

**Change:** Replace "typically involves: PM + QA" with the *principle* each phase must satisfy:
- Ideation: understand the problem, decide if it's code
- Planning: define what done looks like (test requirements), flag security/performance risks
- Implementation: build it, run it, self-correct
- Review: Tomas runs it, gives feedback

The "who" doesn't matter in a single-agent model. The "what must happen" does.

### P3. Make planning phase proportional

**Change:** Planning is mandatory but scope-proportional. A one-day feature needs a checklist and test intent. A multi-week platform needs full QA tests, security review, architecture diagram.

Add: "If the implementation scope is under 2 days, planning can be a checklist in the iteration file. Full test requirements and security review for larger scopes."

### P4. Timestamps: dates only

**Change:** Drop "YYYY-MM-DD HH:MM when time precision matters." Just use dates everywhere.

### P5. Acknowledge the single-agent reality

**Change:** Add a section acknowledging that the current execution model is one Claude conversation per session. The MO is designed to work with this model *and* scale to multi-agent when that becomes practical.

Add: "The lifecycle phases and artifacts are designed for a single-agent workflow. When multi-agent execution becomes available (persistent persona agents across sessions), the same phases apply but with actual inter-agent collaboration replacing the current single-agent approach."

### P6. SKILLS.md: add "read at project start" instruction

**Change:** Add to the lifecycle: "Before Phase 0, read relevant SKILLS.md files to understand existing capabilities. This avoids re-researching solved problems."

### P7. Drop Coach and Psychologist personas

**Change:** These haven't been used in any project. Remove from the roster. They can be re-recruited if a project needs them (the MO already supports adding new personas).

---

## Questions for Tomas

1. **P1 (simplify personas)**: Do you want to keep the multi-persona model as an aspiration for when multi-agent is available? Or cut it now and re-add later?
2. **P7 (drop Coach/Psychologist)**: Any objection? They can always be re-created.
3. **Anything missing?** Any friction you felt during GML/DSH that isn't covered above?
