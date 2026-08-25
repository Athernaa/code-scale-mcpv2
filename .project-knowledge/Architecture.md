---
type: architecture
project: code-scale-mcpv2
status: active
updated: 2026-08-25
tags:
  - project/code-scale-mcpv2
  - project-knowledge
---

# Architecture

> This note is evidence-based. The bootstrap only records facts visible from manifests; the agent should refine it after inspecting entry points, boundaries, data flow, and runtime behavior.

## Detected foundation
- Stack hints: Go
- Manifests: `go.mod`

## System boundaries
- Populate after source inspection. Do not guess architecture from package names alone.

## Data and state flow
- Persisted FiveM/framework facts flow through workspace/provider resolution into Planner evidence and ContextAssembler sufficiency policy.
- Export provider verification is execution-side aware: client callers require client/shared providers, server callers require server/shared providers, and shared callers require shared providers. Unknown or incompatible sides cannot produce local verification.
- Framework `RebuildFacts` and workspace `cross_resource_export` use the shared semantic side-compatibility rule. Workspace resolution persists the complete static export-provider uniqueness proof on the source call before traversal, so Planner authority is invariant across incoming/outgoing trace windows.
- Planner propagates `local_verified` only from a unique static relationship whose upstream provider proof is verified.

## External integrations
- Record durable integrations, ownership, and failure boundaries here. Never copy secrets or credential values.

## Performance-critical paths
- Record measured or structurally important hot paths when they become known.
