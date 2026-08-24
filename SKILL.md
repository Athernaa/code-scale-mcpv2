---
name: code-scale-mcp
description: "Use this skill whenever you need to explore, understand, or navigate a codebase — especially when the user asks how something works, where something is defined, what calls a function, or needs to trace logic across multiple files. This skill indexes codebases and lets you retrieve individual functions, classes, and methods instead of reading whole files, reducing context according to measured response and baseline sizes. Use it for: understanding architecture or code flow, investigating bugs across files, onboarding to unfamiliar repos, finding all usages of a symbol, tracing call chains, exploring large codebases (50+ files), or any task where you would otherwise need to read 3+ files or grep broadly. Supports Python, TypeScript, JavaScript, Go, Rust, Java, and 7 more languages. DO NOT use when: reading a single small file, making a targeted edit to a known location, reading config/README files, or when the file is already in context."
---

# code-scale-mcp

High-performance codebase indexer and symbol retriever. Index any repo or folder once, then retrieve exactly the symbols you need — functions, classes, methods, constants — with byte-level precision.

## Why Use code-scale Instead of Read/Grep

You have built-in tools like Read and Grep that work great for small, targeted operations. But they become expensive and noisy when you need to understand code across many files. code-scale fills that gap:

- **Read** shows you an entire file to get one function. That's ~2,000 tokens when you only need ~50. `get_symbol` fetches just the function.
- **Grep** finds text matches but doesn't understand code structure. `search_symbols` understands functions, classes, methods, and their relationships.
- **Reading 5 files** to trace a feature can be substantially larger than fetching the relevant symbols; measure actual response sizes for the repository and query.

**The rule of thumb**: if you'd need to read 3+ files or grep across many files to answer a question, index the codebase first and use code-scale tools. You'll get better results with far fewer tokens.

**Still use Read/Grep when:**
- You need the entire file (configs, READMEs, small scripts under 50 lines)
- You're about to edit a file and need line numbers for the Edit tool
- You're looking at a single specific file you already know about

## Quick Start

### Index a GitHub repo
```
index_repo({ "url": "https://github.com/owner/repo" })
```
For private repos, set the `GITHUB_TOKEN` environment variable.

### Index a local folder
```
index_folder({ "path": "/path/to/project" })
```

### Then explore and retrieve
```
get_repo_outline({ "repo": "owner/repo" })
search_symbols({ "repo": "owner/repo", "query": "authenticate" })
get_symbol({ "repo": "owner/repo", "symbol_id": "src/auth.py::authenticate#function" })
```

## Common Workflows

### "How does feature X work?"
1. `search_symbols` for the feature name to find entry points
2. `get_symbol` to read each relevant function/class
3. `search_text` to find where those symbols are called from
4. `get_symbols` to batch-fetch all the callers

### "Find and fix a bug"
1. `search_symbols` or `search_text` to locate the relevant code
2. `get_symbol` with `context_lines: 5` to see surrounding code
3. Once you've identified the fix, use Read to get the full file with line numbers, then Edit

### "Help me understand this codebase"
1. `get_repo_outline` for the high-level structure (directories, languages, symbol distribution)
2. `get_file_tree` to see the full file hierarchy
3. `get_file_outline` on key files to see their symbols
4. `get_symbol` to drill into specific implementations

### "What calls this function?" / "Where is this used?"
1. `search_symbols` to obtain the exact `symbol_id`
2. `trace_relationships` with that `symbol_id`, `direction: "incoming"`, and `relationship_kinds: ["calls", "references"]`
3. `get_symbol` only for the returned callers or references that need source context

### "What does this function call?"
1. `search_symbols` to obtain the exact `symbol_id`
2. `trace_relationships` with `direction: "outgoing"` and `relationship_kinds: ["calls"]`
3. Retrieve only the relevant callees with `get_symbol`

### "What could this change affect?"
1. `search_symbols` to obtain the exact `symbol_id`
2. `analyze_impact` for bounded incoming `calls` and `references`
3. Inspect only the affected symbols/files needed for the change

The relationship graph is conservative: ambiguous, dynamic, or unknown
receiver calls are intentionally left unresolved. Use `search_text` only when
the graph has no deterministic edge or when searching non-symbol text.

## Symbol IDs

Symbols are identified by: `{file_path}::{qualified_name}#{kind}`

Examples:
- `src/auth.py::authenticate#function`
- `src/models.py::UserService.login#method`
- `src/config.py::MAX_RETRIES#constant`

Symbol IDs from `search_symbols`, `get_file_outline`, and `get_repo_outline` can be passed directly to `get_symbol` — no need to construct them manually.

## Tools Reference

### Indexing
- **index_repo**: Index a GitHub repo. Args: `url` (required — GitHub URL or owner/repo), `use_ai_summaries` (optional)
- **index_folder**: Index a local folder. Args: `path` (required), `use_ai_summaries`, `extra_ignore_patterns`, `follow_symlinks` (optional)

### Exploration
- **list_repos**: List all indexed repos with symbol/file counts and languages. No args.
- **get_file_tree**: Get file tree. Args: `repo` (required), `path_prefix` (optional filter)
- **get_file_outline**: Symbol outline for a file. Args: `repo`, `file_path` (required), `flat` (optional — set true for flat list with depth integers instead of nested tree)
- **get_repo_outline**: High-level repo overview — directory file counts, language breakdown, symbol kind distribution. Args: `repo` (required)

### Retrieval
- **get_symbol**: Get one symbol's source. Args: `repo`, `symbol_id` (required), `context_lines` (optional — include N surrounding lines), `verify` (optional — re-hash to detect source drift since indexing), `max_length` (optional — smart 60/40 head/tail truncation for large symbols)
- **get_symbols**: Batch get up to 100 symbols. Args: `repo`, `symbol_ids` (required array). More efficient than multiple `get_symbol` calls.

### Search
- **search_symbols**: 3-layer search: FTS5 BM25 → substring → fuzzy Levenshtein. Args: `repo`, `query` (required), `kind` (optional: function/class/method/constant/type), `language`, `file_pattern`, `max_results` (optional, default 10, max 200). Results include `match_tier` (fts5/substring/fuzzy).
- **search_text**: Full-text search with optional snippet context. Args: `repo`, `query` (required), `file_pattern`, `max_results` (optional, default 20, max 200), `context_lines` (optional, 0=single lines, 1-10=merged snippet windows)
- **search_semantics**: Search compact FiveM semantic entities. Use `analyzer: "fivem"` for events, callbacks, exports, commands, and resource metadata. Results include `symbol_id` when one is associated.
- **trace_relationships**: Traverse FiveM or generic graph edges using either `entity_id` or a parser `symbol_id`. Use `direction: "incoming"` for callers/dependents, `"outgoing"` for callees, and filter with `relationship_kinds` such as `calls`, `references`, or `imports`.
- **analyze_impact**: Return bounded incoming generic dependents for a parser `symbol_id`; use before changing shared code.

### Batch
- **batch_execute**: Execute up to 10 operations in one call. Args: `operations` (array of `{tool, args}`). Supports: get_symbol, get_symbols, search_symbols, search_text, get_file_outline, get_file_tree, get_repo_outline. Operations run concurrently.

### Management
- **invalidate_cache**: Delete a repo's index. Args: `repo` (required)
- **watch_folder**: Start auto-reindex on file changes. Args: `path` (required)
- **unwatch_folder**: Stop watching. Args: `path` (required)
- **list_watches**: List active watches. No args.

## Supported Languages

Python, JavaScript, TypeScript, Go, Rust, Java, PHP, C, C++, Ruby, Kotlin, Swift, Lua

## Tips

- Use `search_symbols` with `kind: "function"` to filter by symbol type — fuzzy matching catches typos automatically
- Use `batch_execute` to combine search + retrieval into a single MCP call — reduces round-trips
- Use `get_symbols` for batch retrieval — one call for many symbols
- Use `search_text` with `context_lines: 3` for contextual snippet windows instead of isolated lines
- Use `max_length` on `get_symbol` to truncate very large symbols (preserves head and tail)
- Set `use_ai_summaries: true` when indexing for richer one-line summaries (requires ANTHROPIC_API_KEY or GEMINI_API_KEY)
- Watch active development folders with `watch_folder` to keep the index fresh
- Use `context_lines` on `get_symbol` when you need to see code around a function (e.g., imports, nearby constants)
- Use `verify: true` on `get_symbol` if you suspect the source file has changed since indexing
