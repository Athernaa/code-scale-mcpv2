package server

import (
	"github.com/Athernaa/code-scale-mcpv2/internal/ratelimit"
	"github.com/Athernaa/code-scale-mcpv2/internal/storage"
	"github.com/Athernaa/code-scale-mcpv2/internal/tools"
	"github.com/Athernaa/code-scale-mcpv2/internal/watcher"
	"github.com/modelcontextprotocol/go-sdk/mcp"
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
		Store:    store,
		Tracker:  storage.NewTokenTracker(store.DB()),
		Watcher:  watchMgr,
		Throttle: ratelimit.NewThrottler(),
	}

	// Register all tools.
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
		Description: "Full-text search across all indexed file contents. Use context_lines for snippet windows around matches.",
	}, tools.SearchTextHandler(deps))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_semantics",
		Description: "Search indexed semantic entities such as FiveM events, callbacks, exports, commands, and resource metadata; optionally scope by resource.",
	}, tools.SearchSemanticsHandler(deps))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_workspace_overview",
		Description: "Get a compact overview of a detected FiveM workspace and its resources.",
	}, tools.WorkspaceOverviewHandler(deps))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "trace_relationships",
		Description: "Trace compact semantic or generic code relationships using entity_id or symbol_id.",
	}, tools.TraceRelationshipsHandler(deps))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "analyze_impact",
		Description: "Find bounded incoming generic code dependents for a symbol.",
	}, tools.AnalyzeImpactHandler(deps))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "plan_context",
		Description: "Plan bounded, evidence-backed context candidates for a task using indexed repository facts.",
	}, tools.PlanContextHandler(deps))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "assemble_context",
		Description: "Assemble a deterministic source-backed context package within an exact serialized token budget.",
	}, tools.AssembleContextHandler(deps))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "batch_execute",
		Description: "Execute multiple operations in a single call. Supports: get_symbol, get_symbols, search_symbols, search_text, get_file_outline, get_file_tree, get_repo_outline. Max 10 operations.",
	}, tools.BatchExecuteHandler(deps))

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
