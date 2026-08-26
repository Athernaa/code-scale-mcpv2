---
type: project-memory
project: code-scale-mcpv2
status: active
updated: 2026-08-25
tags:
  - project/code-scale-mcpv2
  - project-knowledge
---

# Project Memory

This is the compact durable memory of the project. It is **not** a per-task diary.

## Current state
- Workflow Governor / Obsidian project knowledge initialized.

## Active constraints
- Durable internal knowledge is maintained in this vault.
- Avoid loose Markdown task summaries and ad-hoc persistent test/debug scripts.

## Decisions
### Obsidian-first project knowledge
Durable architecture, conventions, important constraints, and handoff context are stored in this vault instead of proliferating Markdown files throughout the repository.

## Durable changes
Add only changes future developers/agents need to know. Prefer concise dated bullets; do not log routine edits.
- 2026-08-26: Phase 7.4 official clean run is recorded by implementation commit `dc229dee2c56081e8f49d0f03ab5c3468c495e63` (tree `e8c212f33f3dcd411708077627f6e678a9e159c8`): 34 tasks, 6 modes, 7 budgets, 2 repeats; supported recall 26/26, false sufficiency/provider fabrication/repo leakage/budget/determinism/incremental mismatches all zero. Final status is CONDITIONAL PASS because the provisional token target was missed; realistic fixtures measured 93.6% median broad reduction while small fixtures measured -13.7%.

## Handoff
Keep this section current only when unfinished work, migration state, or non-obvious follow-up matters.
