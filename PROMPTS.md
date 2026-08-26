# Current code-scale MCP prompts

Ready-to-use prompts for the current 21-tool code-scale MCP server. These
prompts describe user intent; they do not guarantee that every task is
sufficient or that every dynamic runtime relationship is statically resolvable.

## Indexing and repository management

| Tool | Example prompt |
| --- | --- |
| index_repo | "Index the GitHub repository owner/repo so we can inspect its source and relationships." |
| index_folder | "Index this local project with the default safety filters and no AI summaries." |
| list_repos | "List the repositories currently indexed, including their file and symbol counts." |
| invalidate_cache | "Invalidate the cached index for owner/repo; I will re-index it afterward." |

## Repository exploration

| Tool | Example prompt |
| --- | --- |
| get_repo_outline | "Give me a compact overview of this repository's directories, languages, and symbol kinds." |
| get_file_tree | "Show the file tree for this repository, scoped to internal/." |
| get_file_outline | "Show the symbols in internal/server/server.go as a flat outline." |

## Symbol retrieval and text search

| Tool | Example prompt |
| --- | --- |
| search_symbols | "Find functions related to SaveUser in this repository." |
| get_symbol | "Retrieve the source for the exact SaveUser symbol returned by the index." |
| get_symbols | "Retrieve these three related symbols in one operation: SaveUser, ValidateUser, and WriteUser." |
| search_text | "Search comments, string literals, and configuration for inventory:use with three context lines." |

## Semantic and workspace exploration

| Tool | Example prompt |
| --- | --- |
| search_semantics | "Find FiveM callback registrations named inventory:get on the server side, limited to the inventory resource." |
| get_workspace_overview | "Give me the FiveM workspace overview, include resources, completeness, duplicate names, and relationship counts." |

Useful semantic filters include query, kind, side, analyzer, resource,
target_resource, framework, include_internal, and max_results. Workspace
resource details are bounded by max_resources when requested.

## Relationship tracing and impact

| Tool | Example prompt |
| --- | --- |
| trace_relationships | "Find SaveUser and show incoming callers using calls and references relationships." |
| trace_relationships | "Trace outgoing relationships from this FiveM export entity across resources, including provider metadata." |
| analyze_impact | "Analyze the bounded incoming impact of changing SaveUser, using calls and references to depth two." |

Use a parser symbol_id for generic graph operations or an entity_id for
semantic/workspace operations. Incoming, outgoing, and both directions are
supported where the selected analyzer has those relationships.

## Context planning and assembly

| Tool | Example prompt |
| --- | --- |
| plan_context | "Plan the evidence-backed context needed to fix SaveUser persistence, but do not assemble source yet." |
| assemble_context | "Assemble context for fixing SaveUser persistence with an 8000-token serialized ceiling." |
| assemble_context | "Fix character inventory loading with focus on LoadCharacter; include bounded impact evidence and return the source-backed package." |
| assemble_context | "Try the same implementation task with a 512-token ceiling and report whether the result is sufficient or blocked." |

plan_context returns candidates, task classification, ranking evidence, and
index diagnostics. assemble_context progressively retrieves source and returns
a serialized package with budget, rounds, omitted candidates, and sufficiency
state. A token budget is a ceiling; retrieval may stop early.

## Batch operations

| Tool | Example prompt |
| --- | --- |
| batch_execute | "In one batch, search for SaveUser, get its file outline, and retrieve the ValidateUser symbol." |

Batch execution supports up to 10 supported read operations: get_symbol,
get_symbols, search_symbols, search_text, get_file_outline, get_file_tree, and
get_repo_outline.

## Watching

| Tool | Example prompt |
| --- | --- |
| watch_folder | "Watch this local project so changed files and derived semantic facts stay fresh." |
| list_watches | "List the active folder watches and their repository identities." |
| unwatch_folder | "Stop watching this project folder." |

Watching performs incremental parsing and derived refresh work; it is not a
zero-cost real-time index.

## Realistic workflow prompts

### Generic implementation

> Index this repository, then assemble the minimum source-backed context needed
> to fix SaveUser persistence without including unrelated files. Inspect the
> sufficiency state before editing, then use native tests and build commands.

### Relationship trace

> Find SaveUser and show who calls it using indexed relationships. Retrieve only
> the callers that are relevant to the requested change.

### Impact analysis

> Analyze what could be affected if I change SaveUser. Separate direct and
> transitive incoming dependents, then assemble context for the highest-risk
> files.

### FiveM event flow

> Search the FiveM semantics for inventory:use, identify its side and resource,
> then trace the event across resources and retrieve the relevant handlers.

### FiveM export authority

> Find which resource provides AddItem. Report provider status, execution
> sides, target resource, and whether the provider is locally verified. Do not
> treat ambiguous, dynamic, external, missing, or wrong-side providers as
> verified.

### FiveM workspace

> Give me the workspace overview including resources, duplicate resource names,
> config files, file coverage, index completeness, and workspace relationship
> counts.

### Context plan before source

> Plan context for fixing character inventory loading without assembling source
> yet. Show the task class, anchor, ranked support, provider evidence, and any
> ambiguity or incomplete-index diagnostics.

### Context assembly

> Assemble context for fixing character inventory loading with an 8000-token
> ceiling. Preserve source-backed sections, report sufficiency, and stop when
> the evidence policy is satisfied.

### Small-budget behavior

> Assemble context for this narrow task with a 512-token ceiling. If the result
> is blocked or indeterminate, explain which evidence could not fit or could not
> be verified.

## Smoke-test sequence

1. Index a local fixture with index_folder.
2. Confirm it with list_repos and get_repo_outline.
3. Inspect get_file_tree and get_file_outline.
4. Search a known symbol with search_symbols and retrieve it with get_symbol.
5. Search a non-symbol string with search_text.
6. Run search_semantics on a FiveM event, callback, or export when applicable.
7. Trace an incoming or outgoing relationship.
8. Run analyze_impact for a shared symbol.
9. Run plan_context for an implementation task.
10. Run assemble_context with a bounded token ceiling.
11. Start, inspect, and stop a watch with watch_folder, list_watches, and
    unwatch_folder.
12. Use invalidate_cache only when a clean re-index is intentional.
