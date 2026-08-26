---
type: architecture
project: code-scale-mcpv2
status: active
updated: 2026-08-26
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
- `cmd/code-scale-mcp` owns transport startup, storage construction, stale
  cleanup, watcher restoration, and MCP server startup.
- `internal/server` registers the 21 MCP tools and wires storage, watcher, and
  throttling dependencies into `internal/tools`.
- `internal/tools` owns MCP argument/result translation; domain logic remains
  in parser, repository, storage, semantic, planner, contextpack, sufficiency,
  and watcher packages.
- `internal/benchmark` and `cmd/code-scale-bench` are offline evaluation code,
  not runtime dependencies of the MCP server.

## Data and state flow
- Source files enter through repository/local-folder indexing and are filtered,
  parsed, persisted, and optionally summarized.
- Generic, FiveM, framework, and workspace analyzers own separate semantic
  entity/relationship facts in the analyzer-scoped SQLite model.
- Planner candidates consume indexed facts; ContextAssembler loads bounded
  source sections; Sufficiency evaluates evidence obligations and early stop.
- Persisted FiveM/framework facts flow through workspace/provider resolution into Planner evidence and ContextAssembler sufficiency policy.
- Export provider verification is execution-side aware: client callers require client/shared providers, server callers require server/shared providers, and shared callers require shared providers. Unknown or incompatible sides cannot produce local verification.
- Framework `RebuildFacts` and workspace `cross_resource_export` use the shared semantic side-compatibility rule. Workspace resolution persists the complete static export-provider uniqueness proof on the source call before traversal, so Planner authority is invariant across incoming/outgoing trace windows.
- Incremental workspace rebuilds persist the bounded export-call proof refresh and the replacement workspace entities/relationships through one storage transaction; non-verified states clear all provider identity fields, and a failed derived rebuild marks workspace completeness incomplete when possible.
- Planner propagates `local_verified` only from a unique static relationship whose upstream provider proof is verified.

## External integrations
- GitHub API access is used by `index_repo`.
- Optional symbol summarization uses Anthropic or Google when configured.
- SSE is an explicit HTTP transport and can require `CODE_SCALE_AUTH_TOKEN`.
- No remote telemetry service is required.

## Performance-critical paths
- Planner graph expansion and ranking are bounded by candidate, depth, and
  relationship limits.
- Context assembly is bounded by serialized tokens, source reads, per-symbol
  bytes, and aggregate source bytes.
- Watcher refreshes reanalyze changed resources and rebuild derived facts; they
  are incremental but not free.

## Benchmark boundary
- Phase 7.4 evaluation lives in `internal/benchmark`, `cmd/code-scale-bench`, and `benchmarks/`; it builds temporary offline indexes and is not part of the MCP runtime or a runtime dependency.
- The corrected official Phase 7.4 corpus also enforces exact panoramic and scoped-panoramic self-baseline token invariants; both are zero. It passed supported-task recall, provider fabrication, repository isolation, serialized-budget, determinism, false-sufficiency, and incremental/full authority checks. Token efficiency remains stratified: realistic fixtures reached 93.6% median broad-baseline reduction, small fixtures -13.7%, and the combined supported maximum-budget result 0.5%, below the provisional 50% target.
