package tools

import (
	"context"
	"os"

	"github.com/Athernaa/code-scale-mcpv2/internal/repository"
	"github.com/Athernaa/code-scale-mcpv2/internal/security"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type WatchFolderArgs struct {
	Path string `json:"path" jsonschema:"Absolute or ~ path to local folder to watch"`
}

func WatchFolderHandler(deps *Deps) func(context.Context, *mcp.CallToolRequest, WatchFolderArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args WatchFolderArgs) (*mcp.CallToolResult, any, error) {
		t := newTimer()

		if deps.Watcher == nil {
			r, _ := errorResult("file watcher not initialized")
			return r, nil, nil
		}

		folderPath, err := expandHomePath(args.Path)
		if err != nil {
			r, _ := errorResult("invalid path")
			return r, nil, nil
		}

		localID, err := repository.Local(folderPath)
		if err != nil {
			r, _ := errorResult("invalid path")
			return r, nil, nil
		}
		absPath := localID.CanonicalPath

		info, err := os.Stat(absPath)
		if err != nil || !info.IsDir() {
			r, _ := errorResult("path is not a valid directory")
			return r, nil, nil
		}

		if !security.IsAllowedRootPath(absPath) {
			r, _ := errorResult("directory is not allowed for watching (system or restricted path)")
			return r, nil, nil
		}

		err = deps.Watcher.Watch(absPath)
		if err != nil {
			r, _ := errorResult("watch failed")
			return r, nil, nil
		}

		result := map[string]any{
			"success": true,
			"path":    absPath,
			"message": "Now watching for changes. Modified files will be automatically reindexed.",
			"_meta": Meta{
				TimingMs: t.elapsedMs(),
			},
		}
		r, _ := toTextResult(result)
		return r, nil, nil
	}
}

type UnwatchFolderArgs struct {
	Path string `json:"path" jsonschema:"Path of the folder to stop watching"`
}

func UnwatchFolderHandler(deps *Deps) func(context.Context, *mcp.CallToolRequest, UnwatchFolderArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args UnwatchFolderArgs) (*mcp.CallToolResult, any, error) {
		t := newTimer()

		if deps.Watcher == nil {
			r, _ := errorResult("file watcher not initialized")
			return r, nil, nil
		}

		folderPath, err := expandHomePath(args.Path)
		if err != nil {
			r, _ := errorResult("invalid path")
			return r, nil, nil
		}

		localID, err := repository.Local(folderPath)
		if err != nil {
			r, _ := errorResult("invalid path")
			return r, nil, nil
		}
		absPath := localID.CanonicalPath

		err = deps.Watcher.Unwatch(absPath)
		if err != nil {
			r, _ := errorResult("unwatch failed")
			return r, nil, nil
		}

		result := map[string]any{
			"success": true,
			"path":    absPath,
			"message": "Stopped watching folder.",
			"_meta": Meta{
				TimingMs: t.elapsedMs(),
			},
		}
		r, _ := toTextResult(result)
		return r, nil, nil
	}
}

type ListWatchesArgs struct{}

func ListWatchesHandler(deps *Deps) func(context.Context, *mcp.CallToolRequest, ListWatchesArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args ListWatchesArgs) (*mcp.CallToolResult, any, error) {
		t := newTimer()

		if deps.Watcher == nil {
			r, _ := errorResult("file watcher not initialized")
			return r, nil, nil
		}

		watches := deps.Watcher.ListWatches()

		result := map[string]any{
			"watches": watches,
			"count":   len(watches),
			"_meta": Meta{
				TimingMs: t.elapsedMs(),
			},
		}
		r, _ := toTextResult(result)
		return r, nil, nil
	}
}
