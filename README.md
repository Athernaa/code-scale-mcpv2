# code-scale-mcp

Local-first code intelligence and agent context infrastructure for coding
workflows.

code-scale indexes source repositories, extracts AST symbols, builds indexed
semantic relationships, and turns a natural-language task into bounded,
source-backed context. It is retrieval and context infrastructure for coding
agents—not an autonomous coding system.

Its code-intelligence workflow was an important inspiration; see
[jcodemunch-mcp](https://github.com/jgravelle/jcodemunch-mcp).

## What it does

code-scale combines several layers of repository understanding:

1. Tree-sitter parses source into a SQLite symbol index.
2. Generic and domain analyzers add structural and semantic facts.
3. Relationship graphs connect calls, references, imports, events, callbacks,
   exports, providers, and other indexed evidence where statically resolvable.
4. The Planner classifies a task and ranks relevant candidates.
5. ContextAssembler progressively loads source-backed sections under an exact
   serialized token ceiling.
6. Sufficiency evaluates whether the returned evidence is enough to stop.

~~~
source repository
        ↓
tree-sitter symbol index
        ↓
generic + domain semantic analyzers
        ↓
SQLite relationship graph
        ↓
Planner / relevance ranking
        ↓
ContextAssembler
        ↓
sufficiency / early stop
        ↓
bounded source-backed context package
~~~

## Features

- AST symbol indexing for 13 languages
- SQLite and FTS5 storage
- Generic calls, references, imports, and module relationships
- Semantic search and bounded relationship tracing
- Incoming impact analysis for indexed symbols
- FiveM resource and multi-resource workspace intelligence
- Framework and provider authority analysis
- Execution-side-aware provider resolution
- Task-specific context planning and progressive retrieval
- Exact serialized token-budget enforcement
- Evidence-based sufficiency and early stopping
- Incremental file watching and workspace refresh
- Deterministic offline benchmark infrastructure
- Path/content safety filtering and gitignore-aware indexing
- Per-tool throttling and bounded source truncation
- stdio and SSE/HTTP transports
- Batch execution for supported read operations

Relationship resolution is based on indexed structural evidence. It is not
intended to provide perfect static resolution for every language or dynamic
runtime pattern.

## Context engine

assemble_context is the primary high-level operation for implementation-oriented
agent workflows. Conceptually, it performs:

~~~
task classification
    → Planner
    → relevance ranking
    → progressive retrieval
    → source assembly
    → sufficiency evaluation
    → serialized token-budget enforcement
~~~

max_context_tokens is a ceiling, not a target. A request for 32,000 tokens does
not cause code-scale to fill 32,000 tokens when sufficient evidence is
available earlier. The final serialized package remains within the requested
budget.

plan_context exposes the evidence-backed candidate plan without performing full
source assembly. Use it when an agent needs to inspect ranking, candidate roles,
ambiguity, or index health before retrieving source.

### Sufficiency

Context packages report one of these states:

- sufficient: indexed evidence and source sections meet the policy for the task;
- needs_more_context: additional ranked stages may still provide required
  evidence;
- blocked: index, source, authority, or required evidence prevents a
  trustworthy sufficient result;
- indeterminate: the task or anchor is too broad or weakly identified for a
  confident stop.

The policy is intentionally conservative. A small source package is not enough
by itself. Provider ambiguity, missing or partial source, incomplete index
state, unresolved high-signal hints, degraded resources, or missing required
relationships can prevent sufficient.

## Generic relationship intelligence

Beyond symbol text, the generic analyzer records structural relationships when
they can be resolved from indexed code, including:

- calls and incoming callers/callees;
- references;
- imports and local module relationships;
- file and symbol ownership;
- bounded incoming dependents used by impact analysis.

Resolution remains conservative around dynamic dispatch, ambiguous modules,
shadowed bindings, and unsupported runtime behavior.

## FiveM Intelligence

FiveM workspaces receive additional semantic analysis for resources and their
workspace configuration. Supported facts include:

- resource manifests, identities, dependencies, and start configuration;
- client, server, and shared execution sides;
- events, handlers, triggers, and registrations;
- callbacks and callback registrations;
- exports and export calls;
- commands;
- NUI callbacks and related flows;
- cross-resource event, callback, and export relationships;
- framework operations and object/facade chains;
- multi-resource workspace relationships.

Execution-side compatibility matters. A client caller cannot treat a
server-only provider as a verified local implementation, and a server caller
cannot treat a client-only provider as verified. Shared providers can satisfy
compatible sides where the indexed evidence supports it.

## Provider authority

Provider evidence is positive-proof based. The indexed model distinguishes
states such as:

- local_verified: a unique, structurally compatible local provider is
  supported by persisted evidence;
- local_ambiguous: multiple compatible local providers remain;
- local_api_missing: no local provider was found;
- external_unverified: the target is external or otherwise not locally verified.

Exports and framework operations are not automatically trusted. Dynamic
targets, duplicate resource identities, duplicate providers, missing APIs,
external providers, and wrong-side providers must not be fabricated as
local_verified.

## Watching and incremental refresh

watch_folder uses the local watcher to refresh changed files and their derived
state. Depending on the workspace, refreshes update:

- parser symbols and cached source;
- generic semantic facts and relationships;
- FiveM resource facts;
- framework/provider facts;
- workspace-derived relationships and provider authority.

Configuration changes and resource refreshes are handled as indexed state
updates. Watching is not zero-cost real-time indexing; it performs bounded
analysis and storage work as changes arrive.

## MCP tools

The server currently registers 21 tools:

| Tool | Purpose |
| --- | --- |
| index_repo | Index a GitHub repository's source code. |
| index_folder | Index a local folder. |
| list_repos | List indexed repositories. |
| get_file_tree | Inspect repository file structure. |
| get_file_outline | Get a hierarchical or flat symbol outline for a file. |
| get_symbol | Retrieve one symbol's source by ID. |
| get_symbols | Retrieve multiple symbols in one operation. |
| search_symbols | Search indexed symbols by name, signature, or description. |
| search_text | Full-text search with optional context windows. |
| search_semantics | Search indexed semantic entities such as events, callbacks, exports, commands, and resource facts. |
| get_workspace_overview | Inspect a detected FiveM workspace and its resources. |
| trace_relationships | Traverse generic, FiveM, workspace, or framework relationships. |
| analyze_impact | Find bounded incoming generic dependents for a symbol. |
| plan_context | Produce an evidence-backed task context plan. |
| assemble_context | Produce a source-backed package within an exact serialized token budget. |
| batch_execute | Execute supported read operations together, up to 10 operations. |
| get_repo_outline | Get a high-level indexed repository overview. |
| invalidate_cache | Delete an indexed repository and cached content. |
| watch_folder | Start watching a local folder for changes. |
| unwatch_folder | Stop watching a folder. |
| list_watches | List active folder watches. |

## Recommended agent workflows

### Implementation work

~~~
ensure index/watch freshness
    → assemble_context(task)
    → inspect source-backed sections and sufficiency
    → implement the change
    → trace_relationships / analyze_impact when needed
    → run the project's native tests and build
~~~

### Exploration and navigation

Use the lowest-cost operation that answers the question:

~~~
get_repo_outline
    → get_file_tree / get_file_outline
    → search_symbols / search_semantics / search_text
    → trace_relationships
    → get_symbol / get_symbols
~~~

For task-oriented retrieval, prefer plan_context when you need the plan and
assemble_context when you need the bounded source package.

## Supported languages

Tree-sitter symbol parsing currently covers:

| Language | Extensions |
| --- | --- |
| Python | .py |
| JavaScript | .js, .jsx |
| TypeScript | .ts, .tsx |
| Go | .go |
| Rust | .rs |
| Java | .java |
| PHP | .php |
| C | .c, .h |
| C++ | .cpp, .cc, .cxx, .hpp, .hh |
| Ruby | .rb |
| Kotlin | .kt, .kts |
| Swift | .swift |
| Lua | .lua |

Symbol parsing support and semantic/domain relationship support are separate
capabilities; semantic parity is not implied across all 13 languages.

## Requirements

- Go 1.24 or newer for building from source
- A CGo-capable toolchain for tree-sitter and SQLite dependencies
- Optional ANTHROPIC_API_KEY or GEMINI_API_KEY for AI-assisted summaries
- Optional GITHUB_TOKEN for private GitHub repositories and higher API limits

## Configuration

| Variable | Default | Description |
| --- | --- | --- |
| GITHUB_TOKEN | unset | GitHub token for private repositories and higher rate limits. |
| ANTHROPIC_API_KEY | unset | Anthropic key for optional summaries. |
| GEMINI_API_KEY | unset | Google Gemini key for optional summaries. |
| CODE_INDEX_PATH | ~/.code-index | Directory for the SQLite index and cached content. |
| CODE_SCALE_ALLOWED_ROOTS | unset | Allowed indexing/watch roots; use ; as the separator on Windows. |
| CODE_SCALE_AUTH_TOKEN | unset | Bearer token required by SSE requests when configured. |

When CODE_SCALE_AUTH_TOKEN is unset, SSE starts unauthenticated and logs a
warning. Use authentication for exposed HTTP/SSE deployments.

## Agent integration resources

- [PROMPTS.md](PROMPTS.md) contains example prompts for exercising the MCP
  tools.
- [SKILL.md](SKILL.md) contains optional agent guidance for choosing the
  indexing, exploration, planning, and retrieval workflow.

## Installation

### From source

~~~
go install github.com/Athernaa/code-scale-mcpv2/cmd/code-scale-mcp@latest
~~~

### Build from the repository

~~~
git clone https://github.com/Athernaa/code-scale-mcpv2
cd code-scale-mcpv2
make build
~~~

The Makefile also provides make test, make fmt, make lint, and make clean.

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

Start a session, then index a local project with index_folder. Enable
watch_folder when you want changes to refresh the index automatically.

### SSE/HTTP mode

~~~
# Unauthenticated; the server logs a warning
code-scale-mcp --transport=sse --port=8080

# Authenticated
CODE_SCALE_AUTH_TOKEN=your-secret-token code-scale-mcp --transport=sse --port=8080
~~~

CLI flags:

| Flag | Default | Description |
| --- | --- | --- |
| --transport | stdio | stdio or sse. |
| --port | 8080 | SSE listen port. |
| --version | — | Print the build version. |

## Phase 7 Validation Status

Phase 7 status:

**7.0 CLOSED · 7.1 CLOSED · 7.2 CLOSED · 7.3 CLOSED · 7.4 CLOSED — CONDITIONAL PASS**

The condition is the provisional token-efficiency target, not a known
correctness or safety failure in the validated benchmark paths.

The current official benchmark contains 34 tasks, 6 modes, 7 budgets, 2
repeats, and 2,856 results. Supported deterministic tasks passed 26 / 26.
Hard validation results are all zero for false sufficiency, provider
fabrication, cross-repository leakage, serialized-budget violations,
baseline-accounting violations, determinism failures, and incremental/full
mismatches.

The current report measured:

- 93.6% median reduction against the broad-context baseline on the realistic
  fixture tier;
- -13.7% median reduction on the small-fixture tier;
- approximately 1.0% combined Phase-7 median broad-baseline reduction;
- approximately 0.5% supported maximum-budget broad-baseline reduction;
- 13,544 tokens, 14 source reads, and 586 retrieval rounds avoided by early
  stopping.

These are benchmark measurements, not universal savings claims. Results vary
with repository scale, task scope, required evidence, context budget, and
metadata overhead. Small repositories can show negative savings because the
serialized context package has fixed structural metadata relative to a tiny
source payload.

See the [benchmark methodology](benchmarks/README.md),
[latest Markdown report](benchmarks/reports/latest.md), and
[latest JSON report](benchmarks/reports/latest.json).

## Benchmarking

The deterministic benchmark CLI is separate from the MCP runtime:

~~~
go run ./cmd/code-scale-bench run \
  --budgets 512,1024,2048,4000,8000,16000,32000 \
  --repeat 2
~~~

Available modes:

- manual
- panoramic
- scoped_panoramic
- primitive
- phase7
- phase7_no_early_stop

phase7_no_early_stop is benchmark-only. It compares the same Planner and
ranking policy while continuing eligible packing stages; it is not a
production MCP mode.

The benchmark runs offline against temporary fixture indexes. It does not
require an LLM API, network access, Repomix, embeddings, or telemetry.

## Repository layout

~~~
cmd/
├── code-scale-mcp/          # MCP server entry point and transports
└── code-scale-bench/        # Offline deterministic benchmark CLI

internal/
├── parser/                  # Tree-sitter parsing and symbols
├── storage/                 # SQLite/FTS5 index and cached source
├── semantic/
│   ├── generic/             # Generic structural relationships
│   ├── fivem/               # FiveM resource/Lua semantics
│   └── framework/           # Framework/provider intelligence
├── planner/                 # Task classification and candidate ranking
├── contextpack/             # Source-backed token-budgeted packages
├── sufficiency/             # Evidence coverage and stop policy
├── repository/              # Repository/index lifecycle
├── watcher/                 # Incremental local refresh
├── tools/                   # MCP tool handlers
├── server/                  # MCP registration and dependencies
├── benchmark/               # Benchmark corpus runner and scoring
├── security/                # Path/content safety checks
├── search/                  # Search helpers
├── snippet/                 # Search context windows
└── ratelimit/               # Per-tool throttling

benchmarks/
├── corpus.json              # Manually reviewed benchmark tasks
├── fixtures/                # Generic, FiveM, and adversarial fixtures
└── reports/                 # Reference JSON and Markdown results
~~~

## Development

~~~
make build
make fmt
make test
make lint
~~~

For the full Phase 7.4 validation workflow, use the commands and constraints
in [benchmarks/README.md](benchmarks/README.md).

## License

MIT
