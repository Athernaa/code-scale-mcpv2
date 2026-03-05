---
name: code-scale-mcp
description: Index codebases and retrieve symbols with byte-level precision. Reduces token costs by up to 99% by fetching individual functions, classes, and methods instead of whole files. Supports 13 languages.
allowed-tools:
  - mcp__code-scale__index_repo
  - mcp__code-scale__index_folder
  - mcp__code-scale__list_repos
  - mcp__code-scale__get_file_tree
  - mcp__code-scale__get_file_outline
  - mcp__code-scale__get_symbol
  - mcp__code-scale__get_symbols
  - mcp__code-scale__search_symbols
  - mcp__code-scale__search_text
  - mcp__code-scale__get_repo_outline
  - mcp__code-scale__invalidate_cache
  - mcp__code-scale__watch_folder
  - mcp__code-scale__unwatch_folder
  - mcp__code-scale__list_watches
---

# code-scale-mcp

High-performance codebase indexer and symbol retriever. Index any repo or folder once, then retrieve exactly the symbols you need — functions, classes, methods, constants — with byte-level precision.

## Quick Start

### Index a GitHub repo
```
index_repo({ "url": "https://github.com/owner/repo" })
```

### Index a local folder
```
index_folder({ "path": "/path/to/project" })
```

### Search for symbols
```
search_symbols({ "repo": "owner/repo", "query": "authenticate" })
```

### Get a specific symbol's source code
```
get_symbol({ "repo": "owner/repo", "symbol_id": "src/auth.py::authenticate#function" })
```

## Workflow

1. **Index**: Use `index_repo` or `index_folder` to index a codebase
2. **Explore**: Use `get_file_tree` and `get_repo_outline` to understand structure
3. **Search**: Use `search_symbols` or `search_text` to find what you need
4. **Retrieve**: Use `get_symbol` or `get_symbols` to fetch exact source code
5. **Watch**: Use `watch_folder` to auto-reindex on file changes

## Symbol IDs

Symbols are identified by: `{file_path}::{qualified_name}#{kind}`

Examples:
- `src/auth.py::authenticate#function`
- `src/models.py::UserService.login#method`
- `src/config.py::MAX_RETRIES#constant`

## Tools Reference

### Indexing
- **index_repo**: Index a GitHub repo. Args: `url` (required), `use_ai_summaries` (optional)
- **index_folder**: Index a local folder. Args: `path` (required), `use_ai_summaries`, `extra_ignore_patterns`, `follow_symlinks` (optional)

### Exploration
- **list_repos**: List all indexed repos. No args.
- **get_file_tree**: Get file tree. Args: `repo` (required), `path_prefix` (optional filter)
- **get_file_outline**: Symbol outline for a file. Args: `repo`, `file_path` (required)
- **get_repo_outline**: High-level repo overview. Args: `repo` (required)

### Retrieval
- **get_symbol**: Get one symbol's source. Args: `repo`, `symbol_id` (required), `verify`, `context_lines` (optional)
- **get_symbols**: Batch get symbols. Args: `repo`, `symbol_ids` (required)

### Search
- **search_symbols**: Search by name/signature. Args: `repo`, `query` (required), `kind`, `language`, `file_pattern`, `max_results` (optional)
- **search_text**: Full-text search in files. Args: `repo`, `query` (required), `file_pattern`, `max_results` (optional)

### Management
- **invalidate_cache**: Delete a repo's index. Args: `repo` (required)
- **watch_folder**: Start auto-reindex on changes. Args: `path` (required)
- **unwatch_folder**: Stop watching. Args: `path` (required)
- **list_watches**: List active watches. No args.

## Supported Languages

Python, JavaScript, TypeScript, Go, Rust, Java, PHP, C, C++, Ruby, Kotlin, Swift, Lua

## Tips

- Use `search_symbols` with `kind: "function"` to filter by symbol type
- Use `get_symbols` for batch retrieval (more efficient than multiple `get_symbol` calls)
- Set `use_ai_summaries: true` when indexing for richer one-line summaries (requires API key)
- Watch active development folders with `watch_folder` to keep the index fresh
- Symbol IDs from `search_symbols` and `get_file_outline` can be passed directly to `get_symbol`
