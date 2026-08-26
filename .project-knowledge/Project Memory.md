---
type: project-memory
project: code-scale-mcpv2
status: active
updated: 2026-08-26
tags:
  - project/code-scale-mcpv2
  - project-knowledge
---

# Project Memory

This is the compact durable memory of the project. It is **not** a per-task diary.

## Current state
- Phase 7 is functionally complete. Phase 7.4 is CONDITIONAL PASS because the
  provisional token-efficiency target was missed; validated correctness and
  safety gates remain green.
- The current product is semantic/context infrastructure, not only a symbol
  retriever: generic, FiveM, framework, workspace, Planner, ContextAssembler,
  and Sufficiency layers are active.
- The official reference report is `benchmarks/reports/latest.md` and records
  the clean tested implementation SHA separately from the later report/docs
  commits.

## Active constraints
- Durable internal knowledge is maintained in this vault.
- Avoid loose Markdown task summaries and ad-hoc persistent test/debug scripts.

## Decisions
### Obsidian-first project knowledge
Durable architecture, conventions, important constraints, and handoff context are stored in this vault instead of proliferating Markdown files throughout the repository.

## Durable changes
Add only changes future developers/agents need to know. Prefer concise dated bullets; do not log routine edits.
- 2026-08-26: Phase 7.4 corrected official clean run is recorded by implementation commit `d58a016fb252a7e9d34f0256825a2f43de67722c` (tree `6cf404944ff95fee1b627d9e5680feeb51073794`): 34 tasks, 6 modes, 7 budgets, 2 repeats; supported recall 26/26, panoramic/scoped self-baseline violations, false sufficiency, provider fabrication, repo leakage, budget, determinism, and incremental mismatches all zero. Final status remains CONDITIONAL PASS because the provisional token target was missed; realistic/small/scoped strata remain reported separately.

## Handoff
Keep this section current only when unfinished work, migration state, or non-obvious follow-up matters.
