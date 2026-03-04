# code-scale-mcp

A high-performance MCP server that indexes codebases via tree-sitter AST parsing and provides token-efficient symbol retrieval. Reduces agent token costs by up to 99% by letting agents retrieve individual symbols instead of whole files.

**Go** — single binary, true parallelism, SQLite storage, 13 languages.

## Features

- **13 languages**: Python, JavaScript, TypeScript, Go, Rust, Java, PHP, C, C++, Ruby, Kotlin, Swift, Lua
- **14 MCP tools**: Index repos/folders, search symbols, retrieve source code, file watching
- **SQLite + FTS5**: Structured queries and full-text search, no file limits
- **Parallel parsing**: True multi-core via goroutines (3-5x faster than Python)
- **Single binary**: No Python/pip dependency chain, millisecond startup
- **Dual transport**: stdio (default) for CLI tools, SSE/HTTP for web clients
- **File watching**: Auto-reindex on file changes via fsnotify
- **9-layer security**: Path traversal prevention, secret detection, binary filtering, gitignore respect
- **3-tier summarization**: Docstring extraction → AI (Claude Haiku / Gemini Flash) → signature fallback
- **Token savings tracking**: Reports tokens saved and cost avoided per request

## Requirements

- Go 1.24+ (for building from source)
- CGo-capable toolchain (tree-sitter requires CGo)
- Optional: `ANTHROPIC_API_KEY` or `GEMINI_API_KEY` for AI-powered summaries
- Optional: `GITHUB_TOKEN` for indexing private GitHub repos

## Installation

### From source

```bash
go install github.com/syphon1c/code-scale-mcp/cmd/code-scale-mcp@latest
```

### Build from repo

```bash
git clone https://github.com/syphon1c/code-scale-mcp
cd code-scale-mcp
make build
```

### Pre-built binaries

Download from [Releases](https://github.com/syphon1c/code-scale-mcp/releases) for your platform.

## Getting Started (3 steps)

**1. Build and place the binary**

```bash
make build
mkdir -p ~/Desktop/mcp_servers/code-scale-mcp
cp bin/code-scale-mcp ~/Desktop/mcp_servers/code-scale-mcp/
```

**2. Add `.mcp.json` to your project root**

```bash
cat > /path/to/your/project/.mcp.json << 'EOF'
{
  "mcpServers": {
    "code-scale": {
      "command": "~/Desktop/mcp_servers/code-scale-mcp/code-scale-mcp",
      "args": []
    }
  }
}
EOF
```

**3. Start a new Claude Code session and index**

```
> Use index_folder to index this project
```

That's it. The MCP server starts automatically when Claude Code detects `.mcp.json`. You'll be prompted to approve it on first use.

> **Note**: You must restart your Claude Code session after adding or modifying `.mcp.json`. The `.mcp.json` is per-project — add it to each project you want to use code-scale with. Alternatively, copy the binary to `/usr/local/bin/` and use just `"command": "code-scale-mcp"`.

## Usage

Once indexed, use symbol-level retrieval instead of reading entire files:

```
get_repo_outline  → high-level structure (dirs, languages, symbol counts)
get_file_outline  → all symbols in a file without reading the whole file
search_symbols    → find functions/classes by name across the codebase
get_symbol        → fetch just one function's source code (~50 tokens vs ~2,000 for the full file)
```

Every response includes `_meta` with `tokens_saved`, `total_tokens_saved`, and `cost_avoided` so you can track the savings.

### Cowork

Upload the `code-scale-mcp_cowork.zip` skill package to Cowork, or add manually:

```
.claude/skills/code-scale-mcp/SKILL.md
```

### SSE/HTTP mode

```bash
code-scale-mcp --transport=sse --port=8080
```

### CLI flags

| Flag | Default | Description |
|------|---------|-------------|
| `--transport` | `stdio` | Transport type: `stdio` or `sse` |
| `--port` | `8080` | Port for SSE transport |
| `--version` | | Show version and exit |

## MCP Tools

| Tool | Description |
|------|-------------|
| `index_repo` | Index a GitHub repository for symbol retrieval |
| `index_folder` | Index a local folder for symbol retrieval |
| `list_repos` | List all indexed repositories |
| `get_file_tree` | Get file structure of an indexed repo |
| `get_file_outline` | Get hierarchical symbol outline for a file |
| `get_symbol` | Retrieve full source code of a symbol by ID |
| `get_symbols` | Batch retrieve source code for multiple symbols |
| `search_symbols` | Search symbols by name/signature with weighted scoring |
| `search_text` | Full-text search across indexed file contents |
| `get_repo_outline` | High-level overview of an indexed repo |
| `invalidate_cache` | Delete index and cached files for a repo |
| `watch_folder` | Start watching a folder for auto-reindex |
| `unwatch_folder` | Stop watching a folder |
| `list_watches` | List active folder watches |

## Supported Languages

| Language | Extensions | Symbols Extracted |
|----------|-----------|-------------------|
| Python | `.py` | functions, classes, methods, constants, decorators |
| JavaScript | `.js`, `.jsx` | functions, classes, methods, constants |
| TypeScript | `.ts`, `.tsx` | functions, classes, methods, interfaces, enums, types |
| Go | `.go` | functions, methods, types, constants |
| Rust | `.rs` | functions, structs, enums, traits, impls, types |
| Java | `.java` | methods, constructors, classes, interfaces, enums |
| PHP | `.php` | functions, classes, methods, interfaces, traits, enums |
| C | `.c`, `.h` | functions, structs, enums, typedefs |
| C++ | `.cpp`, `.cc`, `.cxx`, `.hpp`, `.hh` | functions, classes, structs, enums, namespaces |
| Ruby | `.rb` | methods, classes, modules |
| Kotlin | `.kt`, `.kts` | functions, classes, objects, interfaces |
| Swift | `.swift` | functions, classes, structs, protocols, enums |
| Lua | `.lua` | functions |

## Architecture

```
code-scale-mcp/
├── cmd/code-scale-mcp/main.go     # Entry point, CLI flags, transport selection
├── internal/
│   ├── parser/                     # Tree-sitter AST parsing (13 languages)
│   ├── security/                   # 9-layer security filtering
│   ├── storage/                    # SQLite + FTS5 index store
│   ├── summarizer/                 # 3-tier symbol summarization
│   ├── github/                     # GitHub API client
│   ├── watcher/                    # fsnotify file watcher
│   ├── server/                     # MCP server setup + tool registration
│   └── tools/                      # 14 MCP tool handlers
└── testdata/                       # Test fixtures for all 13 languages
```

## Development

```bash
make build     # Build binary
make fmt       # Run fmt
make test      # Run tests
make lint      # Run linter
make clean     # Clean build artifacts
```

## License

MIT
