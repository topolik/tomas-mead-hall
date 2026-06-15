# Analyst — Skills

Distilled capabilities gained from project work.

---

## LLM Prompt Engineering for Email Analysis

- Design per-concern output schemas for LLM email analysis: concern-level granularity maps to "one actionable item" for the user
- Craft LLM prompts that produce structured JSON: schema definitions, field rules, splitting rules, box-specific priority guidance
- Include previous analysis context in prompts to enable change tracking and dedup across runs
- Instruct LLMs to skip unchanged items to prevent duplicate notifications

## Eisenhower Priority Framework for Notifications

- Map email concerns to Q1-Q4 Eisenhower quadrants with clear criteria: urgency (time-sensitive) × importance (affects work/team/security)
- Provide box-specific priority guidance: e.g., security vulns → Q1, routine notifications → Q3, newsletters → Q4

## Prompt Injection Research

- Research and synthesize defense strategies from Microsoft Research (datamarking), Simon Willison (dual LLM), OWASP, Anthropic, and DeepMind (CaMeL)
- Evaluate empirical effectiveness: datamarking alone drops attack success from ~50% to <3%
- Design 5-layer defense-in-depth architecture combining multiple orthogonal defenses

_Updated: 2026-05-29_
