package server

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/syphon1c/code-scale-mcp/internal/storage"
	"github.com/syphon1c/code-scale-mcp/internal/tools"
	"github.com/syphon1c/code-scale-mcp/internal/watcher"
)

// Version is set at build time via ldflags.
var Version = "dev"

// NewCodeScaleServer creates a new MCP server with all tools registered.
// Returns the server and watcher manager (caller should defer manager.Close()).
func NewCodeScaleServer(store *storage.IndexStore) (*mcp.Server, *watcher.Manager) {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "code-scale-mcp",
		Version: Version,
	}, nil)

	watchMgr := watcher.NewManager(store)

	deps := &tools.Deps{
		Store:   store,
		Tracker: storage.NewTokenTracker(store.DB()),
		Watcher: watchMgr,
	}

	// Register all 11 tools
	mcp.AddTool(server, &mcp.Tool{
		Name:        "index_repo",
		Description: "Index a GitHub repository's source code for symbol-level retrieval.",
	}, tools.IndexRepoHandler(deps))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "index_folder",
		Description: "Index a local folder's source code for symbol-level retrieval.",
	}, tools.IndexFolderHandler(deps))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_repos",
		Description: "List all indexed repositories.",
	}, tools.ListReposHandler(deps))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_file_tree",
		Description: "Get the file structure of an indexed repository.",
	}, tools.GetFileTreeHandler(deps))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_file_outline",
		Description: "Get a hierarchical symbol outline for a specific file. Use flat=true for a flat list with depth.",
	}, tools.GetFileOutlineHandler(deps))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_symbol",
		Description: "Retrieve the full source code of a specific symbol by ID.",
	}, tools.GetSymbolHandler(deps))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_symbols",
		Description: "Batch retrieve full source code for multiple symbols.",
	}, tools.GetSymbolsHandler(deps))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_symbols",
		Description: "Search for symbols by name, signature, or description with weighted scoring.",
	}, tools.SearchSymbolsHandler(deps))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_text",
		Description: "Full-text search across all indexed file contents.",
	}, tools.SearchTextHandler(deps))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_repo_outline",
		Description: "Get a high-level overview of an indexed repository.",
	}, tools.GetRepoOutlineHandler(deps))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "invalidate_cache",
		Description: "Delete the index and cached files for a repository.",
	}, tools.InvalidateCacheHandler(deps))

	// Watch tools
	mcp.AddTool(server, &mcp.Tool{
		Name:        "watch_folder",
		Description: "Start watching a local folder for changes and auto-reindex modified files.",
	}, tools.WatchFolderHandler(deps))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "unwatch_folder",
		Description: "Stop watching a folder for changes.",
	}, tools.UnwatchFolderHandler(deps))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_watches",
		Description: "List all active folder watches.",
	}, tools.ListWatchesHandler(deps))

	return server, watchMgr
}
