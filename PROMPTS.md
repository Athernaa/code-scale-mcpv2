# Test Prompts for code-scale-mcp

Use these prompts to verify that the code-scale-mcp server is working correctly as an MCP tool provider. Each prompt targets a specific tool.

## Indexing & Setup

| Tool | Prompt |
|------|--------|
| `index_repo` | "Index this repository so we can explore it" |
| `index_folder` | "Index just the `internal/` folder of this project" |
| `list_repos` | "What repos do you have indexed right now?" |

## Exploration

| Tool | Prompt |
|------|--------|
| `get_file_tree` | "Show me the file tree for this repo" |
| `get_repo_outline` | "Give me a high-level outline of the codebase — what are the main packages and exported symbols?" |
| `get_file_outline` | "What functions and types are defined in `internal/indexer/indexer.go`?" |

## Symbol Retrieval

| Tool | Prompt |
|------|--------|
| `search_symbols` | "Find all symbols related to 'parse' in this codebase" |
| `get_symbol` | "Show me the implementation of the `IndexRepo` function" |
| `get_symbols` | "Retrieve the `Server` struct and its `Start` method" |

## Text Search

| Tool | Prompt |
|------|--------|
| `search_text` | "Search for all occurrences of 'tree-sitter' in the codebase" |

## Watch / Cache Management

| Tool | Prompt |
|------|--------|
| `watch_folder` | "Watch this repo folder for changes so the index stays up to date" |
| `list_watches` | "What folders are currently being watched?" |
| `unwatch_folder` | "Stop watching this repo" |
| `invalidate_cache` | "Clear the cache for this repo and re-index" |

## Recommended Test Sequence

For a quick end-to-end smoke test, run these prompts in order:

1. "Index this repository so we can explore it"
2. "What repos do you have indexed right now?"
3. "Show me the file tree for this repo"
4. "Give me a high-level outline of the codebase"
5. "Find all symbols related to 'parse' in this codebase"
6. "Show me the implementation of the `IndexRepo` function"
7. "Search for all occurrences of 'tree-sitter' in the codebase"

This covers indexing, listing, tree/outline views, symbol search, symbol retrieval, and text search — hitting the core workflow.
