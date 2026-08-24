package tools

import (
	"context"
	"path/filepath"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetRepoOutlineArgs struct {
	Repo           string `json:"repo" jsonschema:"Repository name (owner/repo or local name)"`
	MaxDepth       int    `json:"max_depth,omitempty" jsonschema:"Maximum directory depth (default 20)"`
	MaxDirectories int    `json:"max_directories,omitempty" jsonschema:"Maximum directories to return (default 500)"`
}

func GetRepoOutlineHandler(deps *Deps) func(context.Context, *mcp.CallToolRequest, GetRepoOutlineArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetRepoOutlineArgs) (*mcp.CallToolResult, any, error) {
		t := newTimer()

		repoID, err := deps.Store.GetRepoID(args.Repo)
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}

		info, dirs, kinds, err := deps.Store.GetRepoOutline(repoID)
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}

		maxDepth := args.MaxDepth
		if maxDepth <= 0 {
			maxDepth = 20
		}
		if maxDepth > 100 {
			maxDepth = 100
		}
		maxDirectories := args.MaxDirectories
		if maxDirectories <= 0 {
			maxDirectories = 500
		}
		if maxDirectories > 10000 {
			maxDirectories = 10000
		}
		keys := make([]string, 0, len(dirs))
		for key := range dirs {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		filteredDirs := make(map[string]int)
		truncated := false
		for _, key := range keys {
			depth := len(strings.Split(filepath.ToSlash(strings.Trim(key, "/")), "/"))
			if strings.Trim(key, "/") == "" {
				depth = 0
			}
			if depth > maxDepth || len(filteredDirs) >= maxDirectories {
				truncated = true
				continue
			}
			filteredDirs[key] = dirs[key]
		}

		result := map[string]any{
			"repo":         info.Repo,
			"indexed_at":   info.IndexedAt,
			"file_count":   info.FileCount,
			"symbol_count": info.SymbolCount,
			"languages":    info.Languages,
			"directories":  filteredDirs,
			"symbol_kinds": kinds,
			"truncated":    truncated,
			"_meta": Meta{
				TimingMs:  t.elapsedMs(),
				Truncated: truncated,
			},
		}
		r, _ := toTextResult(result)
		return r, nil, nil
	}
}
