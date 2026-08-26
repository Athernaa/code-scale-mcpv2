---
type: project-home
project: code-scale-mcpv2
status: active
updated: 2026-08-26
tags:
  - project/code-scale-mcpv2
  - project-knowledge
---

# code-scale-mcpv2

Central project knowledge index. Read this note before substantial implementation work.

## Navigation
- [[Architecture]]
- [[Development]]
- [[Project Memory]]

## Project snapshot
- Repository: `D:\agent plugin\mcp\code-scale-mcpv2`
- Stack: Go 1.24 module, SQLite/FTS5, tree-sitter, MCP stdio/SSE transports.
- Current state: Phase 7.0–7.3 closed; Phase 7.4 closed as CONDITIONAL PASS.
- Architecture: AST symbols → semantic/workspace analyzers → Planner → ContextAssembler → Sufficiency.
- Public entry points: `cmd/code-scale-mcp/`, `cmd/code-scale-bench/`.
- Reference validation: `benchmarks/reports/latest.md`.

## Knowledge rules
- Durable internal project knowledge belongs in this Obsidian vault.
- Update an existing note before creating a new note.
- Do not create a new Markdown file merely to report that a task or feature was completed.
- Create a subsystem note only when the subject is durable, substantial, and no core note can hold it clearly.
