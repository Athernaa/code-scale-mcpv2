# User Guide

## Installation

### Build from source

```bash
git clone https://github.com/Athernaa/code-scale-mcpv2
cd code-scale-mcpv2
make build
```

### Place the binary

Copy the binary to a consistent location so all your projects can reference it:

```bash
mkdir -p ~/Desktop/mcp_servers/code-scale-mcp
cp bin/code-scale-mcp ~/Desktop/mcp_servers/code-scale-mcp/
```

Alternatively, install to your PATH:

```bash
cp bin/code-scale-mcp /usr/local/bin/
```

---

## Configuration

### Claude Code

Add a `.mcp.json` file to the root of any project you want to use code-scale with:

```json
{
  "mcpServers": {
    "code-scale": {
      "command": "~/Desktop/mcp_servers/code-scale-mcp/code-scale-mcp",
      "args": [],
      "env": {
        "GITHUB_TOKEN": "ghp_xxxxxxxx",
        "ANTHROPIC_API_KEY": "sk-ant-xxxxxxxx"
      }
    }
  }
}
```

Both environment variables are optional:

* `GITHUB_TOKEN` enables private repositories and higher GitHub API rate limits.
* `ANTHROPIC_API_KEY` enables AI-generated summaries via Claude Haiku.
* `GOOGLE_API_KEY` enables AI-generated summaries via Gemini Flash (used if `ANTHROPIC_API_KEY` is not set).
* If neither key is set, summaries fall back to docstrings or signatures.

The `.mcp.json` file is per-project. Claude Code automatically detects it when you start a session in that directory. **You must restart your Claude Code session after adding or modifying `.mcp.json`.**

If you installed the binary to `/usr/local/bin/`, you can simplify the command:

```json
{
  "mcpServers": {
    "code-scale": {
      "command": "code-scale-mcp",
      "args": []
    }
  }
}
```

### SSE/HTTP Mode

For web-based MCP clients, run in SSE mode:

```bash
code-scale-mcp --transport=sse --port=8080
```

---

## Quick Start

Once `.mcp.json` is in your project and you've started a new Claude Code session:

**Step 1: Index your project**

```
> Use index_folder to index this project
```

**Step 2: Explore the structure**

```
> Show me the repo outline
> Show me the file tree
```

**Step 3: Find and retrieve symbols**

```
> Search for the authenticate function
> Get the source code of that function
```

That's it. Every tool call now fetches just the symbols you need instead of entire files, saving tokens automatically.

---

## Workflows

### Explore a New Repository

```
index_repo:       { "url": "fastapi/fastapi" }
get_repo_outline: { "repo": "fastapi/fastapi" }
get_file_tree:    { "repo": "fastapi/fastapi", "path_prefix": "fastapi" }
get_file_outline: { "repo": "fastapi/fastapi", "file_path": "fastapi/main.py" }
```

### Explore a Local Project

```
index_folder:     { "path": "/home/user/myproject" }
get_repo_outline: { "repo": "local/myproject-<12-char-path-hash>" }
search_symbols:   { "repo": "local/myproject-<12-char-path-hash>", "query": "main" }
```

### Find and Read a Function

```
search_symbols: { "repo": "owner/repo", "query": "authenticate", "kind": "function" }
get_symbol:     { "repo": "owner/repo", "symbol_id": "src/auth.py::authenticate#function" }
```

### Understand a Class

```
get_file_outline: { "repo": "owner/repo", "file_path": "src/auth.py" }
get_symbols: {
  "repo": "owner/repo",
  "symbol_ids": [
    "src/auth.py::AuthHandler.login#method",
    "src/auth.py::AuthHandler.logout#method"
  ]
}
```

### Verify Source Hasn't Changed

```
get_symbol: {
  "repo": "owner/repo",
  "symbol_id": "src/main.py::process#function",
  "verify": true
}
```

The response `content_verified` will be `true` if the source matches the stored hash and `false` if it has drifted since indexing.

### Search for Non-Symbol Content

```
search_text: { "repo": "owner/repo", "query": "TODO", "file_pattern": "*.py" }
```

Use `search_text` for string literals, comments, configuration values, or anything that is not a symbol name.

For contextual snippets instead of isolated lines, add `context_lines`:

```
search_text: { "repo": "owner/repo", "query": "TODO", "context_lines": 3 }
```

This returns merged snippet windows with ±3 lines of context around each match, reducing token usage while providing meaningful context.

### Batch Multiple Operations

```
batch_execute: {
  "operations": [
    { "tool": "search_symbols", "args": { "repo": "owner/repo", "query": "auth" } },
    { "tool": "get_symbol", "args": { "repo": "owner/repo", "symbol_id": "src/auth.py::login#function" } },
    { "tool": "get_file_outline", "args": { "repo": "owner/repo", "file_path": "src/auth.py" } }
  ]
}
```

Execute up to 10 operations in a single MCP call. All operations run concurrently. Supports: `get_symbol`, `get_symbols`, `search_symbols`, `search_text`, `get_file_outline`, `get_file_tree`, `get_repo_outline`.

### Watch a Folder for Changes

```
watch_folder:  { "path": "/home/user/myproject" }
list_watches:  {}
unwatch_folder: { "path": "/home/user/myproject" }
```

When watching, modified files are automatically re-parsed and the index is updated. Changes are debounced (500ms) to batch rapid edits.

### Force Re-index

```
invalidate_cache: { "repo": "owner/repo" }
index_repo:       { "url": "owner/repo" }
```

---

## Tool Reference

| Tool               | Purpose                       | Key Parameters                                                     |
| ------------------ | ----------------------------- | ------------------------------------------------------------------ |
| `index_repo`       | Index GitHub repository       | `url`, `use_ai_summaries`                                          |
| `index_folder`     | Index local folder            | `path`, `extra_ignore_patterns`, `follow_symlinks`, `use_ai_summaries` |
| `list_repos`       | List all indexed repositories | —                                                                  |
| `get_file_tree`    | Browse file structure         | `repo`, `path_prefix`                                              |
| `get_file_outline` | Symbols in a file             | `repo`, `file_path`, `flat`                                        |
| `get_symbol`       | Full source of one symbol     | `repo`, `symbol_id`, `verify`, `context_lines`, `max_length`      |
| `get_symbols`      | Batch retrieve symbols        | `repo`, `symbol_ids`                                               |
| `search_symbols`   | Search symbols (FTS5+fuzzy)   | `repo`, `query`, `kind`, `language`, `file_pattern`, `max_results` |
| `search_text`      | Full-text search with snippets| `repo`, `query`, `file_pattern`, `max_results`, `context_lines`    |
| `batch_execute`    | Multi-operation batch call    | `operations` (array of `{tool, args}`, max 10)                     |
| `get_repo_outline` | High-level overview           | `repo`                                                             |
| `invalidate_cache` | Delete cached index           | `repo`                                                             |
| `watch_folder`     | Watch folder for changes      | `path`                                                             |
| `unwatch_folder`   | Stop watching folder          | `path`                                                             |
| `list_watches`     | List active watches           | —                                                                  |

---

## Symbol IDs

Symbol IDs follow the format:

```
{file_path}::{qualified_name}#{kind}
```

Examples:

```
src/main.py::UserService#class
src/main.py::UserService.login#method
src/utils.py::authenticate#function
config.py::MAX_RETRIES#constant
internal/server/server.go::NewCodeScaleServer#function
cmd/main.go::main#function
```

IDs are returned by `get_file_outline`, `search_symbols`, and `search_text`. Pass them to `get_symbol` or `get_symbols` to retrieve source code.

---

## Token Savings

Responses use compact `_meta` telemetry by default. Set `CODE_SCALE_TELEMETRY=full` to include measured response and baseline byte counts where a defensible baseline exists; set it to `off` to omit telemetry.

```json
{
  "_meta": {
    "timing_ms": 12,
    "response_bytes": 740,
    "baseline_context_bytes": 8200,
    "estimated_context_saved_bytes": 7460,
    "estimated_context_saved_tokens": 1865
  }
}
```

- **`response_bytes`** and **`baseline_context_bytes`**: Measured byte counts when available
- **`estimated_context_saved_bytes`** and **`estimated_context_saved_tokens`**: Explicit estimates based on those measurements; tokens use approximately four bytes per token
- Cumulative tracker fields and model-cost estimates are not emitted by default; pricing is intentionally not embedded in production telemetry

Measure your own workflow when comparing whole-file reads with symbol retrieval; savings depend on file size, symbol size, and the selected response limits.

---

## Troubleshooting

**"Repository not found"**
Check the URL format (`owner/repo` or full GitHub URL). For private repositories, set `GITHUB_TOKEN` in your `.mcp.json` env.

**"No source files found"**
The repository may not contain supported language files, or files may be excluded by skip patterns. code-scale supports 13 languages: Python, JavaScript, TypeScript, Go, Rust, Java, PHP, C, C++, Ruby, Kotlin, Swift, Lua.

**MCP server not loading in Claude Code**
Restart your Claude Code session after adding `.mcp.json`. The server is only loaded at session startup. Check that the binary path in `.mcp.json` is correct and the binary is executable.

**Rate limiting**
Set `GITHUB_TOKEN` in your `.mcp.json` env to increase GitHub API limits (5,000 requests/hour vs 60 unauthenticated).

**AI summaries not working**
Set `ANTHROPIC_API_KEY` (Claude Haiku) or `GOOGLE_API_KEY` (Gemini Flash) in your `.mcp.json` env. Anthropic takes priority if both are set. Without either, summaries fall back to docstrings or signatures.

**Stale index**
Use `invalidate_cache` followed by `index_repo` or `index_folder` to force a clean re-index. Or use `watch_folder` for automatic reindexing on file changes.

**Encoding issues**
Files with invalid UTF-8 are handled safely using replacement characters.

---

## Storage

Indexes are stored in SQLite at `~/.code-index/code-scale.db` (override with `CODE_INDEX_PATH` environment variable).

Raw source files are cached on the filesystem for byte-offset retrieval. Local
cache directories include a canonical path hash; indexes created before this
identity change may need one invalidation and re-index. Source files are not
deleted.

```
~/.code-index/
├── code-scale.db             # SQLite database (repos, files, symbols, FTS5)
├── owner-repo/               # Raw source files (GitHub repos)
│   └── src/main.py
└── local-myproject/          # Raw source files (local folders)
    └── src/main.py
```

---

## Tips

1. **Start with `get_repo_outline`** to quickly understand the repository structure before diving in.
2. **Use `get_file_outline` before reading source** to understand the API surface first — it's much cheaper than reading the whole file.
3. **Narrow searches** using `kind`, `language`, and `file_pattern` filters.
4. **Batch-retrieve** related symbols with `get_symbols` instead of repeated `get_symbol` calls.
5. **Use `batch_execute`** to combine search + retrieval into a single MCP call — reduces round-trips and context overhead.
6. **Use `search_text` with `context_lines`** for contextual snippet windows instead of isolated lines — more useful, fewer tokens.
7. **Use `max_length`** on `get_symbol` to truncate very large symbols (60% head / 40% tail, preserving the end where errors appear).
8. **Fuzzy search works** — `search_symbols` catches typos like "authenticat" → "authenticate" via Levenshtein matching.
9. **Use `verify: true`** on `get_symbol` to detect source drift since indexing.
10. **Use `watch_folder`** on active projects to keep the index up to date automatically.
11. **Use `context_lines`** on `get_symbol` to see surrounding code without reading the full file.
