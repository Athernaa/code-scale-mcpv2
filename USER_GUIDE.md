# User Guide

code-scale is an MCP server for local-first code intelligence and bounded
context retrieval. It indexes symbols and semantic relationships, then helps
an agent explore or assemble source-backed context for a task.

## Installation

### Build from source

~~~
git clone https://github.com/Athernaa/code-scale-mcpv2
cd code-scale-mcpv2
make build
~~~

The binary is written to bin/code-scale-mcp. You can also install it with:

~~~
go install github.com/Athernaa/code-scale-mcpv2/cmd/code-scale-mcp@latest
~~~

### MCP configuration

Add a project-local .mcp.json:

~~~
{
  "mcpServers": {
    "code-scale": {
      "command": "/path/to/code-scale-mcp",
      "args": []
    }
  }
}
~~~

Start a new MCP client session after changing this file. Then index a local
project with index_folder.

## Configuration

Useful environment variables:

| Variable | Purpose |
| --- | --- |
| GITHUB_TOKEN | Private GitHub repositories and higher GitHub API limits. |
| ANTHROPIC_API_KEY | Optional Anthropic symbol summaries. |
| GOOGLE_API_KEY | Optional Google symbol summaries when Anthropic is not configured. |
| CODE_INDEX_PATH | SQLite index and cached source directory; defaults to ~/.code-index. |
| CODE_SCALE_ALLOWED_ROOTS | Restricts local indexing and watching to configured roots. |
| CODE_SCALE_AUTH_TOKEN | Bearer token for SSE requests. |
| CODE_SCALE_TELEMETRY | compact by default, full for measured metadata, or off. |

AI summaries are optional. Without an AI key, summaries fall back to
docstrings or signatures. Optional summarization sends symbol kind/signature
prompts to the configured provider; code-scale does not require network access
for ordinary local indexing and retrieval.

## Core workflow

### 1. Index and keep the repository fresh

Use index_folder for a local project or index_repo for a GitHub repository.

Then use watch_folder during active development. Watching refreshes changed
symbols and derived facts, but it still performs bounded indexing work.

### 2. Explore before retrieving source

For broad structure:

~~~
get_repo_outline
get_file_tree
get_file_outline
~~~

For targeted discovery:

~~~
search_symbols
search_text
search_semantics
~~~

Use search_text for comments, string literals, configuration, and other text
that is not a symbol identity.

### 3. Retrieve source

Use get_symbol for one symbol and get_symbols for a batch. Symbol IDs have the
form:

~~~
file_path::qualified_name#kind
~~~

Use verify=true with get_symbol when you want content-hash drift detection.
Use context_lines for nearby lines and max_length for bounded head/tail source.

## Implementation workflow

For a non-trivial change:

~~~
ensure index/watch freshness
    → assemble_context(task)
    → inspect source-backed sections and sufficiency
    → implement
    → trace_relationships / analyze_impact when needed
    → run native tests and build
~~~

assemble_context is the primary task-oriented operation. Its
max_context_tokens value is a serialized output ceiling, not a target to fill.
It may stop before the ceiling when required evidence is covered.

Example intent:

> Assemble context for fixing SaveUser persistence with an 8000-token ceiling.
> Include the relevant caller and storage dependency, then report whether the
> evidence is sufficient.

## Planning without source assembly

plan_context returns the task classification, ranked candidates, relationship
evidence, authority, ambiguity, and index diagnostics without assembling full
source sections.

Use it when you want to inspect the plan first:

> Plan context for fixing character inventory loading without reading source
> yet. Focus on LoadCharacter and include bounded impact evidence.

## Sufficiency states

assemble_context reports evidence state such as:

- sufficient: the indexed source and relationships meet the task policy;
- needs_more_context: more ranked stages may provide required evidence;
- blocked: required source, provider, index, or relationship evidence cannot be
  trusted or obtained;
- indeterminate: the task or anchor is too broad or weakly identified.

A blocked or indeterminate result does not mean the task is impossible. It means
the current indexed evidence did not prove that the returned package was enough.
Use the reason codes, missing evidence, index health, and omitted candidates to
decide whether to refresh, focus, trace, or inspect manually.

## Generic relationship workflow

For a known symbol:

~~~
search_symbols → trace_relationships → get_symbols
~~~

Incoming relationships show callers or dependents. Outgoing relationships show
callees or downstream references. The generic analyzer resolves calls,
references, and imports where the indexed language rules can prove them; dynamic
or ambiguous dispatch can remain unresolved.

For change-risk analysis:

~~~
search_symbols → analyze_impact → assemble_context
~~~

analyze_impact accepts either a parser symbol_id or a semantic entity_id. Its
default generic relationship filters are calls and references; depth and result
limits are bounded.

## FiveM workflow

For a FiveM server-data root:

1. Run index_folder on the workspace root.
2. Use get_workspace_overview to inspect resources, config files, coverage,
   completeness, duplicate resource names, and workspace relationship counts.
3. Use search_semantics with resource, side, analyzer, target_resource, or
   framework filters.
4. Use trace_relationships on returned entity IDs for events, callbacks,
   exports, providers, or workspace relationships.
5. Use assemble_context with a resource or symbol focus for implementation work.

FiveM semantics include manifests, resource identity, client/server/shared
sides, events, handlers, callbacks, exports, commands, NUI callbacks,
framework operations, and cross-resource relationships.

Provider authority is conservative:

- local_verified means a unique compatible local provider has positive indexed
  proof;
- local_ambiguous means compatible candidates remain ambiguous;
- local_api_missing means no local provider was found;
- external_unverified means the target is not locally verified.

Client/server/shared compatibility matters. A server-only provider does not
satisfy a client caller, and a client-only provider does not satisfy a server
caller. Dynamic targets and duplicate resource identities remain unresolved.

## Tool reference

| Tool | Required input | Important optional input |
| --- | --- | --- |
| index_repo | url | use_ai_summaries |
| index_folder | path | extra_ignore_patterns, follow_symlinks, use_ai_summaries |
| list_repos | none | none |
| get_file_tree | repo | path_prefix, max_depth, max_entries |
| get_file_outline | repo, file_path | flat |
| get_symbol | repo, symbol_id | verify, context_lines, max_length |
| get_symbols | repo, symbol_ids | max_total_bytes |
| search_symbols | repo, query | kind, language, file_pattern, max_results |
| search_text | repo, query | file_pattern, max_results, context_lines |
| search_semantics | repo | query, kind, side, analyzer, resource, target_resource, framework, include_internal, max_results |
| get_workspace_overview | repo | include_resources, max_resources |
| trace_relationships | repo plus entity_id or symbol_id | analyzer, direction, relationship_kinds, depth, max_results |
| analyze_impact | repo plus symbol_id or entity_id | analyzer, depth, max_results, relationship_kinds |
| plan_context | repo, task | max_candidates, focus_file, focus_symbol_id, focus_resource, include_impact, debug |
| assemble_context | repo, task | max_context_tokens, tokenizer, max_candidates, focus_file, focus_symbol_id, focus_resource, include_impact, debug |
| batch_execute | operations | up to 10 supported read operations |
| get_repo_outline | repo | none |
| invalidate_cache | repo | none |
| watch_folder | path | none |
| unwatch_folder | path | none |
| list_watches | none | none |

trace_relationships and analyze_impact require exactly one of entity_id or
symbol_id. Defaults and maximums are enforced by the server; invalid or
over-limit requests return an error.

## Batch operations

batch_execute runs up to 10 supported read operations concurrently. It supports
get_symbol, get_symbols, search_symbols, search_text, get_file_outline,
get_file_tree, and get_repo_outline.

## SSE/HTTP mode

The default transport is stdio:

~~~
code-scale-mcp --transport=stdio
~~~

For an HTTP/SSE client:

~~~
code-scale-mcp --transport=sse --port=8080
~~~

When CODE_SCALE_AUTH_TOKEN is configured, send Authorization: Bearer
your-secret-token. Without it, SSE is unauthenticated and the server logs a
warning.

## Troubleshooting

### Repository not found

Check that index_repo receives a GitHub URL or owner/repo name. Set GITHUB_TOKEN
for private repositories or API rate limits.

### No source files found

Check supported extensions, ignore rules, secret/binary filtering, allowed
roots, and the requested path. Use get_repo_outline after indexing to inspect
what was accepted.

### Stale results

Use watch_folder during active development. For a deliberate clean rebuild,
use invalidate_cache followed by index_folder or index_repo.

### Insufficient context

Read the sufficiency reason codes. Refresh an incomplete or degraded index,
provide a focus_file/focus_symbol_id/focus_resource, trace the missing
relationship, or request a larger budget. Do not assume that increasing the
budget alone resolves an authority or index-health blocker.

## Related documentation

- PROMPTS.md: ready-to-use exploration and workflow prompts
- SKILL.md: operating guidance for AI agents
- SPEC.md: technical architecture and contracts
- SECURITY.md: path, content, storage, and network controls
- benchmarks/README.md: deterministic benchmark methodology
