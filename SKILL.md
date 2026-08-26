---
name: code-scale-mcp
description: "Use code-scale for repository exploration, semantic tracing, impact analysis, and bounded agent context. Prefer assemble_context for non-trivial implementation tasks after ensuring the index is fresh."
---

# code-scale-mcp

Use code-scale as local-first context infrastructure for coding agents. It
indexes source, symbols, semantic entities, relationships, and workspace facts.
It does not replace the agent's reasoning, editing, tests, or build tools.

## Choose the smallest useful workflow

Do not call assemble_context for every trivial lookup.

### Trivial exact lookup

Use:

~~~
search_symbols
→ get_symbol or get_symbols
~~~

Use get_file_outline first when the symbol ID is unknown. Use search_text for
comments, string literals, configuration, and other non-symbol text.

### Targeted exploration

Use:

~~~
get_repo_outline
→ get_file_tree / get_file_outline
→ search_symbols / search_semantics
→ trace_relationships
→ get_symbol
~~~

### Non-trivial implementation task

Use:

~~~
ensure repository/index freshness
→ assemble_context(task)
→ inspect source-backed sections and sufficiency
→ implement
→ trace_relationships or analyze_impact when risk warrants
→ run native tests and build
~~~

assemble_context is the preferred high-level operation when the task spans
multiple files or needs relationship/context reasoning. Its token budget is a
serialized ceiling. It may stop before the ceiling when sufficient evidence is
available.

### Plan without source

Use plan_context when the agent needs to inspect task classification, ranking,
candidate roles, provider authority, ambiguity, or index health before source
assembly.

### Broad structure

Use get_repo_outline and get_file_tree for repository shape. In FiveM workspaces,
use get_workspace_overview before making assumptions about resources, config
coverage, duplicate names, or workspace completeness.

## Sufficiency behavior

Treat the package sufficiency state as evidence:

- sufficient: the returned evidence meets the task policy;
- needs_more_context: eligible retrieval stages may still add required evidence;
- blocked: the current index, source, authority, or required relationship is
  not trustworthy or available;
- indeterminate: the task or anchor is too broad or weakly identified.

Do not blindly continue searching after sufficient. Do not interpret blocked or
indeterminate as proof that the task cannot be solved. Inspect reason codes,
missing evidence, omitted candidates, ambiguity, and index diagnostics. Then
refresh, focus, trace, or use a native source tool as appropriate.

## Relationship routing

For callers and dependents:

~~~
search_symbols → trace_relationships(direction=incoming)
~~~

For callees:

~~~
search_symbols → trace_relationships(direction=outgoing)
~~~

For change impact:

~~~
search_symbols → analyze_impact
~~~

Generic relationship kinds include calls, references, and imports where
statically resolvable. Dynamic dispatch, ambiguous modules, unknown receivers,
and shadowed bindings may remain unresolved.

## FiveM routing

When the repository is a FiveM resource or workspace:

1. Use index_folder on the workspace root.
2. Use get_workspace_overview for resource and completeness facts.
3. Use search_semantics for events, callbacks, exports, commands, NUI
   callbacks, resource metadata, or framework entities.
4. Use trace_relationships with returned entity IDs for cross-resource and
   framework relationships.
5. Use assemble_context for multi-resource implementation tasks.

Pay attention to client, server, and shared execution sides. Provider status is
not a name match:

- local_verified requires positive unique structural proof;
- local_ambiguous means compatible candidates remain;
- local_api_missing means no local implementation was found;
- external_unverified means the target is not locally verified.

Dynamic targets, duplicate resource identities, duplicate providers, missing
APIs, external targets, and wrong-side providers must not be treated as
verified local authority.

## Index freshness and watching

Use watch_folder for active local work. Watching refreshes changed source,
symbols, semantic facts, framework/provider facts, and workspace-derived
relationships as applicable. It is bounded incremental work, not zero-cost
real-time indexing.

When an index is incomplete, truncated, degraded, or stale, preserve that
uncertainty in the agent's reasoning.

## Tool selection summary

- Indexing: index_repo, index_folder
- Repository shape: list_repos, get_repo_outline, get_file_tree,
  get_file_outline, get_workspace_overview
- Source/search: search_symbols, search_text, get_symbol, get_symbols
- Semantics/graph: search_semantics, trace_relationships, analyze_impact
- Context: plan_context, assemble_context
- Batching: batch_execute
- Lifecycle: invalidate_cache, watch_folder, unwatch_folder, list_watches

## Tool arguments

Verify the current tool schema before inventing arguments. Important context
arguments include repo, task, max_context_tokens, max_candidates, focus_file,
focus_symbol_id, focus_resource, include_impact, tokenizer, and debug.

trace_relationships and analyze_impact require exactly one of entity_id or
symbol_id. Relationship direction is incoming, outgoing, or both. Limits are
bounded by the server.

## Boundaries

Use ordinary file tools when reading a single small file, a README, a config, or
a known edit location. Use code-scale when the task would otherwise require
broad repository search, multiple source files, semantic tracing, or bounded
context assembly.

Do not claim perfect dependency resolution, universal token savings, autonomous
coding, or guaranteed sufficiency for every natural-language task.
