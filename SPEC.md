# Technical Specification

## Scope

code-scale-mcp is a local-first MCP server and agent context engine. It indexes
source repositories, persists symbols and semantic facts, plans task-specific
evidence, and assembles bounded source-backed context.

It is not an autonomous coding agent. Editing, tests, builds, deployment, and
runtime validation remain the responsibility of the calling agent or developer.

## System goals

- Make repository structure and source symbols searchable.
- Preserve repository and analyzer identity in persisted facts.
- Resolve useful structural and domain relationships conservatively.
- Provide FiveM workspace and framework/provider intelligence.
- Assemble task context under an exact serialized token ceiling.
- Stop only when sufficiency evidence supports stopping.
- Refresh local indexes incrementally without retaining stale derived authority.
- Keep indexing and benchmark operation local-first and deterministic by default.

## Non-goals

- Perfect static resolution for dynamic languages or runtime-generated calls.
- Treating text co-occurrence as proof of a symbol or relationship.
- Treating every export, framework operation, or external API as verified local
  authority.
- Simulating an autonomous agent or guaranteeing that a retrieved package is
  sufficient for every open-ended task.
- Replacing native project tests, builds, security checks, or runtime validation.

## Runtime architecture

~~~text
MCP transport
    → internal/server registration and internal/tools handlers
    → storage.IndexStore
    → parser and repository indexers
    → generic, FiveM, framework, and workspace analyzers
    → planner.Plan and RankPolicy
    → contextpack.Assembler
    → sufficiency.Policy
    → serialized source-backed Package
~~~

The MCP server owns dependency construction and registers the current tool
surface. Tool handlers translate MCP arguments into repository, semantic,
planner, context, watcher, and storage operations.

## Repository identity

GitHub repositories use an owner/name identity. Local folders receive a stable
repository identity derived from their canonical path. Repository identity is
used for SQLite rows, cached source paths, semantic facts, and repository
isolation.

A semantic query is always scoped to a repository ID. A symbol or semantic
entity from another repository must not satisfy a query for the current
repository.

## Indexing pipeline

1. The repository or local-folder indexer discovers candidate files.
2. Security and path filters reject disallowed, ignored, secret, binary, invalid,
   or oversized inputs.
3. Tree-sitter parses supported source files into parser.Symbol values.
4. Symbols, file metadata, content hashes, and searchable fields are persisted.
5. The generic analyzer builds language-appropriate structural facts.
6. A FiveM resource analyzer adds resource/Lua facts when the repository is a
   resource or workspace.
7. The framework analyzer derives framework calls, operations, providers, and
   authority.
8. The workspace indexer resolves resource/configuration relationships for
   FiveM workspaces.
9. Analyzer-scoped semantic entities and relationships are persisted.

AI summaries are optional. Without an AI key, summarization falls back to
docstrings or signatures. The implemented optional provider variables are
ANTHROPIC_API_KEY and GOOGLE_API_KEY.

## AST and symbol model

A parser symbol contains an identity, file path, name, qualified name, kind,
language, signature, content hash, optional docstring/summary, parent
relationship, source line range, and byte range.

Symbol IDs have this form:

~~~text
file_path::qualified_name#kind
~~~

The supported parser languages are Python, JavaScript, TypeScript, Go, Rust,
Java, PHP, C, C++, Ruby, Kotlin, Swift, and Lua. Parser coverage does not imply
semantic relationship parity across all languages.

## Semantic entity model

Semantic entities are analyzer-owned facts with fields including:

- stable entity ID;
- repository and analyzer;
- file and optional parser symbol ID;
- semantic kind and name;
- framework classification;
- execution side;
- line range;
- dynamic marker;
- structured metadata.

Current analyzer identities include generic_graph, fivem, framework, and
fivem_workspace. Storage and traversal preserve analyzer ownership.

## Semantic relationship model

Semantic relationships contain:

- stable relationship ID;
- analyzer and repository;
- from and optional to entity IDs;
- relationship kind and name;
- dynamic marker;
- confidence;
- source file and line.

Relationships are traversed by analyzer, direction, relationship kind, depth,
and result bound. An edge is evidence from an analyzer; it is not inferred only
because two returned source sections mention the same name.

## Generic analyzer

The generic analyzer indexes code files, declarations, imports, call sites,
and reference sites. It resolves exact local or module relationships when the
indexed language facts prove the target.

Supported generic relationship kinds include calls, references, and imports.
Ambiguous modules, dynamic dispatch, unknown receivers, and shadowed bindings
remain unresolved rather than being fabricated.

## FiveM analyzer

The FiveM analyzer parses resource manifests and Lua AST facts including:

- manifest resources and dependencies;
- event registrations, handlers, and triggers;
- callback calls and registrations;
- export calls and definitions;
- command registrations;
- NUI callbacks.

It resolves literal relationships conservatively. Dynamic names and dynamic
targets are excluded from target resolution. Execution-side compatibility is
checked for event and callback flows.

## Workspace detection and indexing

A FiveM workspace is discovered from server configuration, resource manifests,
resource paths, and related configuration files. Workspace facts include
resources, config files, start/ensure information, dependencies, duplicate
resource names, completeness, and workspace-derived relationships.

Workspace relationships include cross-resource events, callbacks, exports,
resource starts, and dependencies where identity, literal names, sides, and
resource resolution support the edge.

Configuration parsing describes indexed configuration facts; it does not prove
live runtime state.

## Framework intelligence

Framework analysis enriches local facts with framework API calls, providers,
operations, object/facade lineage, resource ownership, and relationship edges.
Adapters recognize supported framework evidence without replacing deterministic
source/resource discovery.

### Provider authority

Provider status values include:

- local_verified;
- local_ambiguous;
- local_api_missing;
- external_unverified.

local_verified requires positive structural proof of a unique compatible local
provider. A name match, source mention, dynamic target, external reference,
duplicate provider, missing API, or incompatible execution side is not enough.

Provider proof is persisted on affected source-call facts and used by Planner
candidates and relationship traversal.

### Execution-side compatibility

Supported sides are client, server, shared, and unknown. A provider must be
compatible with the caller side:

- client callers may use compatible client/shared providers;
- server callers may use compatible server/shared providers;
- shared callers require evidence compatible with shared execution.

Unknown or incompatible sides cannot produce local_verified authority.

## Planner and RankPolicy

Planner receives a repository, task text, optional focus file/symbol/resource,
candidate limit, and optional impact request. It classifies task intent, finds
anchors, gathers relationship evidence, expands bounded graph support, and
returns ranked candidates.

Candidates carry source location, symbol/entity identity, tier, score,
reason codes, resource/framework/side information, authority, and distance.
The current context tiers are anchor, direct_support, domain_support, and
peripheral.

RankPolicy is the centralized ranking seam for task alignment, relationship
quality, provider evidence, focus, locality, distance, uncertainty, and
candidate tier.

Planner output can be truncated, ambiguous, incomplete, degraded, or
unresolved. Those states remain visible to ContextAssembler and Sufficiency.

## ContextAssembler

ContextAssembler consumes a planner and source store and returns a Package
containing source-backed sections, candidate tiers, retrieval rounds, omissions,
diagnostics, stop reason, sufficiency, and budget accounting.

Important request fields include:

- repo;
- task;
- max_context_tokens;
- tokenizer;
- max_candidates;
- focus_file;
- focus_symbol_id;
- focus_resource;
- include_impact;
- debug.

The debug option exposes bounded counters; it does not change retrieval
semantics.

### Token budget contract

Current context budget constants are:

- minimum: 512 tokens;
- default: 8,000 tokens;
- maximum: 64,000 tokens.

The requested budget is the final serialized package ceiling. The assembler
accounts for package metadata and source sections, performs final stabilization,
and rejects a package that exceeds the requested serialized budget.

Source and outline bounds include:

- maximum source reads: 64;
- aggregate source bytes: 4 MiB;
- per-symbol source bytes: 32 KiB;
- outline symbols per file: 128;
- total outline symbols: 512.

The planner has a hard candidate maximum of 100. Lower request limits can be
used for narrower retrieval.

### Progressive retrieval and early stop

The assembler evaluates stages progressively:

1. anchor;
2. direct_support;
3. domain_support;
4. peripheral.

At each stage it loads eligible source sections, evaluates sufficiency, and may
stop when the policy is satisfied. A larger budget can permit more context, but
does not require consuming the full budget.

## Sufficiency engine

Sufficiency evaluates planner obligations against returned sections and index
health. It considers anchors, critical support, providers, flow peers, cross-
resource coverage, impact evidence, source availability, partial content,
ambiguity, degradation, and truncation.

States are:

- sufficient;
- needs_more_context;
- blocked;
- indeterminate.

Sufficiency is evidence-based and conservative. Missing or partial source,
unresolved high-signal hints, incomplete/truncated index state, degraded
framework resources, unverified providers, or missing required flow evidence
can prevent sufficient.

## Incremental refresh

The watcher handles local file events, debounce, resource/configuration changes,
and repository lifecycle. Depending on repository type, refresh paths update
parser symbols, generic facts, FiveM facts, framework facts, workspace facts,
and derived relationships.

For FiveM resources, RefreshResource reparses the changed resource and rebuilds
workspace-derived facts from persisted compact facts. Configuration refresh
rebuilds workspace state from persisted source facts. Failures clear or degrade
affected derived facts rather than leaving mismatched authoritative state.

## Storage model

The current storage schema version is 10.

Conceptually, SQLite stores:

- repositories and source identity;
- files, languages, content hashes, and cached-source references;
- symbols and FTS5 symbol metadata;
- analyzer-scoped semantic entities;
- analyzer-scoped semantic relationships;
- workspace/resource/configuration metadata;
- persisted watches and token-tracking data where enabled;
- schema version and migrations.

The semantic tables are analyzer-aware. Generic, FiveM, framework, and
workspace facts have separate ownership and can be replaced or refreshed
without silently erasing unrelated analyzer data.

## MCP tool surface

The server registers 21 tools for indexing, exploration, retrieval, semantic
search, relationship tracing, impact analysis, context planning, context
assembly, batching, cache management, and watching. The authoritative list and
user-facing descriptions are maintained in README.md and USER_GUIDE.md.

## Security boundaries

Indexing validates repository identity and local paths, denies restricted system
roots, checks symlinks, respects ignore rules, excludes known secrets and binary
files, applies file-size limits, safely decodes invalid UTF-8, and stores data
under validated repository identities.

SSE/HTTP authentication is opt-in through CODE_SCALE_AUTH_TOKEN. stdio is the
default local transport.

code-scale is local-first, not necessarily network-zero. index_repo uses the
GitHub API. Optional summarization calls Anthropic or Google when explicitly
enabled and configured. Ordinary local indexing, semantic analysis, planning,
and context assembly do not require those services.

## Telemetry

Compact timing/truncation metadata is the default. CODE_SCALE_TELEMETRY=full
enables measured response/baseline metadata and token-tracking fields where a
defensible baseline exists. CODE_SCALE_TELEMETRY=off suppresses the metadata
envelope.

Telemetry is local to the server/index store; no remote telemetry service is
required by the runtime.

## Benchmark and validation boundary

The deterministic benchmark lives in internal/benchmark,
cmd/code-scale-bench, and benchmarks/. It uses temporary offline indexes,
manually authored ground truth, six modes, seven budgets, and repeated runs.

The current Phase 7 status is:

- 7.0 CLOSED;
- 7.1 CLOSED;
- 7.2 CLOSED;
- 7.3 CLOSED;
- 7.4 CLOSED — CONDITIONAL PASS.

The condition is token-efficiency acceptance. The official report records zero
false sufficiency, provider fabrication, repository leakage, serialized budget,
baseline accounting, determinism, and incremental/full mismatch failures. It
does not claim universal token savings.

## Known limitations

- Dynamic dispatch and runtime-generated names may remain unresolved.
- Semantic relationship coverage differs by language and domain.
- Configuration facts do not prove live runtime state.
- Sufficiency can remain blocked or indeterminate for broad tasks, incomplete
  indexes, degraded resources, missing source, ambiguity, or unverified
  authority.
- Benchmark results are fixture- and baseline-specific.
- Optional AI summarization has external-provider network boundaries.

## Change policy

Source implementation, tests, and generated benchmark reports are the
authorities for runtime behavior. Update this specification when durable
contracts change; keep operational examples in USER_GUIDE.md and PROMPTS.md.
