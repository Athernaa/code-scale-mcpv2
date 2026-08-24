package tools

import (
	"context"
	"errors"
	"sort"

	"github.com/Athernaa/code-scale-mcpv2/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type WorkspaceOverviewArgs struct {
	Repo             string `json:"repo"`
	IncludeResources bool   `json:"include_resources,omitempty"`
	MaxResources     int    `json:"max_resources,omitempty"`
}

func WorkspaceOverviewHandler(deps *Deps) func(context.Context, *mcp.CallToolRequest, WorkspaceOverviewArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args WorkspaceOverviewArgs) (*mcp.CallToolResult, any, error) {
		id, err := deps.Store.GetRepoID(args.Repo)
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}
		w, err := deps.Store.GetWorkspace(id)
		if err != nil {
			if errors.Is(err, storageErrNoRows()) {
				r, _ := errorResult("repository has no persisted workspace overview")
				return r, nil, nil
			}
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}
		resources, err := deps.Store.GetWorkspaceResources(id)
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}
		relationships, err := deps.Store.GetSemanticRelationshipsForAnalyzer(id, "fivem_workspace")
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}
		configs, err := deps.Store.GetWorkspaceConfigs(id)
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}
		result := map[string]any{"repo": args.Repo, "mode": w.Kind, "root": w.RootPath, "resource_count": len(resources), "config_files": len(configs), "files_discovered_total": w.FilesDiscoveredTotal, "files_indexed": w.FilesIndexed, "index_truncated": w.IndexTruncated, "index_complete": !w.Incomplete, "incomplete": w.Incomplete, "resources_with_semantics": w.ResourcesWithSemantics, "resources_without_semantics": w.ResourcesWithoutSemantics}
		result["relationship_count"] = len(relationships)
		nameCounts := map[string]int{}
		for _, resource := range resources {
			nameCounts[resource.Name]++
		}
		var duplicates []string
		for name, count := range nameCounts {
			if count > 1 {
				duplicates = append(duplicates, name)
			}
		}
		sort.Strings(duplicates)
		if len(duplicates) > 0 {
			result["duplicate_names"] = duplicates
		}
		enabled, disabled, unknown := 0, 0, 0
		for _, r := range resources {
			switch r.EnabledState {
			case "enabled":
				enabled++
			case "disabled":
				disabled++
			default:
				unknown++
			}
		}
		result["enabled_count"] = enabled
		result["disabled_count"] = disabled
		result["unknown_count"] = unknown
		if args.IncludeResources {
			limit := args.MaxResources
			if limit <= 0 {
				limit = 50
			}
			if limit > 200 {
				limit = 200
			}
			if len(resources) > limit {
				resources = resources[:limit]
				result["truncated"] = true
			}
			result["resources"] = resources
		}
		r, _ := toTextResult(result)
		return r, nil, nil
	}
}

// Kept in a helper to avoid exposing database/sql in the tool package.
func storageErrNoRows() error { return storage.ErrWorkspaceNotFound }
