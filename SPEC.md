# Technical Specification

## Overview

**code-scale-mcp** pre-indexes repository source code using tree-sitter AST parsing, extracting a structured catalog of every symbol (function, class, method, constant, type). Each symbol stores its **signature + one-line summary**, with full source retrievable on demand via O(1) byte-offset seeking.

Written in Go for single-binary distribution, true parallelism via goroutines, and millisecond startup. SQLite with FTS5 replaces JSON file storage — enabling 100K+ symbols, structured queries, and full-text search.

### Token Savings

| Scenario                        | Raw dump        | code-scale    | Savings   |
| ------------------------------- | --------------- | ------------- | --------- |
| Explore 500-file repo structure | ~200,000 tokens | ~2,000 tokens | **99%**   |
| Find a specific function        | ~40,000 tokens  | ~200 tokens   | **99.5%** |
| Read one function body          | ~40,000 tokens  | ~500 tokens   | **98.7%** |
| Understand module API           | ~15,000 tokens  | ~800 tokens   | **94.7%** |

---

## MCP Tools (14)

### Indexing Tools

#### `index_repo` — Index a GitHub repository

```json
{
  "url": "owner/repo",
  "use_ai_summaries": true
}
```

Fetches source via `git/trees?recursive=1` (single API call), filters through the security pipeline, parses with tree-sitter using 20 concurrent workers, summarizes, and saves the index plus raw files to SQLite.

#### `index_folder` — Index a local folder

```json
{
  "path": "/path/to/project",
  "extra_ignore_patterns": ["*.generated.*"],
  "follow_symlinks": false,
  "use_ai_summaries": false
}
```

Walks the local directory with full security controls: path traversal prevention, symlink escape protection, secret detection, binary filtering, and `.gitignore` respect. Files are parsed concurrently with 20 goroutine workers.

#### `invalidate_cache` — Delete index for a repository

```json
{
  "repo": "owner/repo"
}
```

Deletes all stored index data (SQLite rows and cached content files) for the repository.

---

### Discovery Tools

#### `list_repos` — List indexed repositories

No input required. Returns all indexed repositories with symbol counts, file counts, languages, and index timestamps.

#### `get_file_tree` — Get file structure

```json
{
  "repo": "owner/repo",
  "path_prefix": "src/"
}
```

Returns a nested directory tree with per-file language and symbol count annotations.

#### `get_file_outline` — Get symbols in a file

```json
{
  "repo": "owner/repo",
  "file_path": "src/main.py",
  "flat": false
}
```

Returns a hierarchical symbol tree (classes contain methods) with signatures and summaries. Source code is not included; use `get_symbol` for that. Set `flat: true` to get a flat list with depth integers instead of nested tree — simpler for linear processing and more token-efficient.

#### `get_repo_outline` — High-level repository overview

```json
{
  "repo": "owner/repo"
}
```

Returns directory file counts, language breakdown, and symbol kind distribution. Lighter than `get_file_tree`.

---

### Retrieval Tools

#### `get_symbol` — Get full source of a symbol

```json
{
  "repo": "owner/repo",
  "symbol_id": "src/main.py::MyClass.login#method",
  "verify": true,
  "context_lines": 3
}
```

Retrieves source via byte-offset seeking (O(1)). Optional `verify` re-hashes the source and compares it to the stored `content_hash`. Optional `context_lines` includes surrounding lines.

#### `get_symbols` — Batch retrieve multiple symbols

```json
{
  "repo": "owner/repo",
  "symbol_ids": ["id1", "id2", "id3"]
}
```

Returns a list of symbols plus an error list for any IDs not found.

---

### Search Tools

#### `search_symbols` — Search across all symbols

```json
{
  "repo": "owner/repo",
  "query": "authenticate",
  "kind": "function",
  "language": "python",
  "file_pattern": "src/**/*.py",
  "max_results": 10
}
```

Uses SQLite FTS5 full-text search with weighted scoring across name, signature, summary, keywords, and docstring. All filters are optional.

#### `search_text` — Full-text search across file contents

```json
{
  "repo": "owner/repo",
  "query": "TODO",
  "file_pattern": "*.py",
  "max_results": 20
}
```

Case-insensitive search across indexed file contents. Returns matching lines with file, line number, and surrounding context. Use when symbol search misses (string literals, comments, config values).

---

### Watch Tools

#### `watch_folder` — Start watching a folder for changes

```json
{
  "path": "/path/to/project"
}
```

Starts an fsnotify-based file watcher on the folder. Modified, created, or deleted source files are automatically re-parsed and the index is updated. Changes are debounced (500ms) to batch rapid edits.

#### `unwatch_folder` — Stop watching a folder

```json
{
  "path": "/path/to/project"
}
```

Stops the file watcher for the specified folder.

#### `list_watches` — List active folder watches

No input required. Returns all currently watched folder paths with start times.

---

## Data Models

### Symbol

```go
type Symbol struct {
    ID            string   // "{file_path}::{qualified_name}#{kind}"
    File          string   // Relative file path
    Name          string   // Symbol name
    QualifiedName string   // Dot-separated with parent context
    Kind          string   // function | class | method | constant | type
    Language      string   // python | javascript | typescript | go | rust | java | php | c | cpp | ruby | kotlin | swift | lua
    Signature     string   // Full signature line(s)
    ContentHash   string   // SHA-256 of source bytes (drift detection)
    Docstring     string   // Extracted documentation
    Summary       string   // One-line summary (AI or fallback)
    Decorators    []string // Decorators/attributes
    Keywords      []string // Search keywords
    Parent        string   // Parent symbol ID (methods → class)
    Line          int      // Start line (1-indexed)
    EndLine       int      // End line (1-indexed)
    ByteOffset    int64    // Start byte in raw file
    ByteLength    int64    // Byte length of source
}
```

### SQLite Schema

```sql
-- Repository metadata
CREATE TABLE repos (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    owner TEXT, name TEXT, repo TEXT UNIQUE,
    indexed_at TEXT, git_head TEXT, source_type TEXT
);

-- Source files with content hashes for incremental indexing
CREATE TABLE files (
    id INTEGER PRIMARY KEY, repo_id INTEGER REFERENCES repos(id) ON DELETE CASCADE,
    path TEXT, language TEXT, content_hash TEXT,
    UNIQUE(repo_id, path)
);

-- Extracted symbols
CREATE TABLE symbols (
    id INTEGER PRIMARY KEY, repo_id INTEGER REFERENCES repos(id) ON DELETE CASCADE,
    file_id INTEGER REFERENCES files(id) ON DELETE CASCADE,
    symbol_id TEXT, file_path TEXT, name TEXT, qualified_name TEXT,
    kind TEXT, language TEXT, signature TEXT, docstring TEXT, summary TEXT,
    decorators TEXT, keywords TEXT, parent_id TEXT,
    line INTEGER, end_line INTEGER, byte_offset INTEGER, byte_length INTEGER,
    content_hash TEXT, UNIQUE(repo_id, symbol_id)
);

-- FTS5 for full-text search across symbol metadata
CREATE VIRTUAL TABLE symbols_fts USING fts5(
    name, qualified_name, signature, summary, docstring,
    content='symbols', content_rowid='id'
);

-- Token savings tracking
CREATE TABLE token_savings (
    id INTEGER PRIMARY KEY, total_tokens_saved INTEGER, anon_id TEXT
);
```

Indexes on `name`, `kind`, `language`, `file_path`, `repo_id` for common query patterns.

Raw source files stored on filesystem at `~/.code-index/{owner}-{name}/{file_path}` for O(1) byte-offset retrieval.

---

## File Discovery

### GitHub Repositories

Single API call:
`GET /repos/{owner}/{repo}/git/trees/HEAD?recursive=1`

Concurrent file fetching with a 20-goroutine worker pool.

### Local Folders

Recursive directory walk with `filepath.WalkDir` and full security pipeline. Concurrent parsing with 20-goroutine worker pool.

### Filtering Pipeline (Both Paths)

1. **Extension filter** — must match one of 13 supported languages
2. **Skip patterns** — `node_modules/`, `vendor/`, `.git/`, `build/`, `dist/`, lock files, minified files, etc.
3. **`.gitignore`** — respected via gitignore pattern matching
4. **Secret detection** — `.env`, `*.pem`, `*.key`, `*.p12`, credentials files excluded (36 patterns)
5. **Binary detection** — extension-based (75+ extensions) + null-byte content sniffing in first 8KB
6. **Size limit** — 500 KB per file (configurable)
7. **File count limit** — 10,000 files max, prioritized: `src/` → `lib/` → `pkg/` → `cmd/` → `internal/` → remainder
8. **Encoding safety** — `utf8.Valid` check, replacement on invalid

---

## Concurrency Model

Go's goroutines provide true parallelism (no GIL):

- **GitHub file fetching**: 20-goroutine worker pool with semaphore
- **File parsing**: Parallel tree-sitter parsing across files (each goroutine gets its own parser)
- **AI summarization**: Concurrent batch API calls
- **Local folder walking**: Parallel file reads with `filepath.WalkDir`
- **File watching**: Background goroutine per watched folder, debounced at 500ms

---

## Response Envelope

All tools return a `_meta` object with timing, context, and token savings:

```json
{
  "_meta": {
    "timing_ms": 42,
    "repo": "owner/repo",
    "symbol_count": 387,
    "file_count": 45,
    "truncated": false,
    "tokens_saved": 2450,
    "total_tokens_saved": 184320,
    "cost_avoided": {
      "claude_opus": 0.0613,
      "gpt5_latest": 0.0245
    },
    "total_cost_avoided": {
      "claude_opus": 4.608,
      "gpt5_latest": 1.8432
    }
  }
}
```

- **`tokens_saved`**: Tokens saved by this specific call (raw bytes vs response bytes, divided by 4)
- **`total_tokens_saved`**: Cumulative tokens saved across all tool calls, persisted in SQLite
- **`cost_avoided`**: Cost saved this call at model pricing ($25/1M for Opus, $10/1M for GPT-5)
- **`total_cost_avoided`**: Cumulative cost savings

---

## Error Handling

All errors return:

```json
{
  "error": "Human-readable message",
  "_meta": { "timing_ms": 1 }
}
```

| Scenario                          | Behavior                                              |
| --------------------------------- | ----------------------------------------------------- |
| Repository not found (GitHub 404) | Error with message                                    |
| Rate limited (GitHub 403)         | Error with reset time; suggest setting `GITHUB_TOKEN` |
| File fetch fails                  | File skipped; indexing continues                      |
| Parse fails (single file)         | File skipped; indexing continues                      |
| No source files found             | Error message returned                                |
| Symbol ID not found               | Error in response                                     |
| Repository not indexed            | Error suggesting indexing first                       |
| AI summarization fails            | Falls back to docstring or signature                  |
| Content hash mismatch             | `content_verified: false` in response                 |

---

## Security (9 Layers)

1. **Path traversal prevention** — `filepath.Rel` + prefix check
2. **Symlink escape detection** — `filepath.EvalSymlinks`
3. **Secret file exclusion** — 36 glob patterns
4. **Binary detection** — 75+ extensions + null-byte sniff in first 8KB
5. **File size limits** — 500KB default, configurable
6. **`.gitignore` respect** — pattern matching for file filtering
7. **Skip patterns** — node_modules, vendor, .git, build, dist, lock files
8. **Encoding safety** — `utf8.Valid` check, replacement on invalid
9. **Safe repo component validation** — regex: `[A-Za-z0-9._-]+`

---

## Environment Variables

| Variable            | Purpose                                                              | Required |
| ------------------- | -------------------------------------------------------------------- | -------- |
| `GITHUB_TOKEN`      | GitHub API authentication (higher limits, private repos)             | No       |
| `ANTHROPIC_API_KEY` | AI summarization via Claude Haiku (takes priority if both keys set)  | No       |
| `GOOGLE_API_KEY`    | AI summarization via Gemini Flash (used if Anthropic key not set)    | No       |
| `CODE_INDEX_PATH`   | Custom storage path (default: `~/.code-index/`)                     | No       |

---

## Supported Languages (13)

| Language   | Extensions                          | Symbol Types                                           |
|------------|-------------------------------------|--------------------------------------------------------|
| Python     | `.py`                               | functions, classes, methods, constants, decorators     |
| JavaScript | `.js`, `.jsx`                       | functions, classes, methods, constants                 |
| TypeScript | `.ts`, `.tsx`                       | functions, classes, methods, interfaces, enums, types  |
| Go         | `.go`                               | functions, methods, types, constants                   |
| Rust       | `.rs`                               | functions, structs, enums, traits, impls, types        |
| Java       | `.java`                             | methods, constructors, classes, interfaces, enums      |
| PHP        | `.php`                              | functions, classes, methods, interfaces, traits, enums |
| C          | `.c`, `.h`                          | functions, structs, enums, typedefs                    |
| C++        | `.cpp`, `.cc`, `.cxx`, `.hpp`, `.hh`| functions, classes, structs, enums, namespaces         |
| Ruby       | `.rb`                               | methods, classes, modules                              |
| Kotlin     | `.kt`, `.kts`                       | functions, classes, objects, interfaces                |
| Swift      | `.swift`                            | functions, classes, structs, protocols, enums           |
| Lua        | `.lua`                              | functions                                              |
