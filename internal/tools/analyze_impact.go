package tools

import (
	"context"
	"sort"

	"github.com/Athernaa/code-scale-mcpv2/internal/semantic"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type AnalyzeImpactArgs struct {
	Repo              string   `json:"repo" jsonschema:"Repository name"`
	SymbolID          string   `json:"symbol_id" jsonschema:"Parser symbol ID"`
	Depth             int      `json:"depth,omitempty" jsonschema:"Maximum incoming dependency depth, max 3"`
	MaxResults        int      `json:"max_results,omitempty" jsonschema:"Maximum dependent edges, max 200"`
	RelationshipKinds []string `json:"relationship_kinds,omitempty" jsonschema:"Dependency relationship filters, defaults to calls and references"`
}

type impactItem struct {
	SymbolID     string `json:"symbol_id,omitempty"`
	EntityID     string `json:"entity_id"`
	Relationship string `json:"relationship"`
	File         string `json:"file"`
	Line         int    `json:"line"`
	Depth        int    `json:"depth"`
}

func AnalyzeImpactHandler(deps *Deps) func(context.Context, *mcp.CallToolRequest, AnalyzeImpactArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args AnalyzeImpactArgs) (*mcp.CallToolResult, any, error) {
		if args.SymbolID == "" {
			r, _ := errorResult("symbol_id is required")
			return r, nil, nil
		}
		repoID, err := deps.Store.GetRepoID(args.Repo)
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}
		root, err := deps.Store.GetSemanticEntityBySymbolID(repoID, semantic.AnalyzerGenericGraph, args.SymbolID)
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}
		kinds := args.RelationshipKinds
		if len(kinds) == 0 {
			kinds = []string{"calls", "references"}
		}
		edges, truncated, err := deps.Store.TraceSemanticWithOptions(repoID, root.ID, semantic.AnalyzerGenericGraph, "incoming", kinds, args.Depth, args.MaxResults)
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}
		direct := make([]impactItem, 0)
		transitive := make([]impactItem, 0)
		files := make(map[string]struct{})
		for _, edge := range edges {
			if edge.From.ID == "" {
				continue
			}
			item := impactItem{SymbolID: edge.From.SymbolID, EntityID: edge.From.ID, Relationship: edge.Kind, File: edge.From.File, Line: edge.From.Line, Depth: edge.Depth}
			files[edge.From.File] = struct{}{}
			if edge.Depth == 1 {
				direct = append(direct, item)
			} else {
				transitive = append(transitive, item)
			}
		}
		affectedFiles := make([]string, 0, len(files))
		for file := range files {
			affectedFiles = append(affectedFiles, file)
		}
		sort.Strings(affectedFiles)
		result := map[string]any{
			"symbol_id":             args.SymbolID,
			"direct_dependents":     direct,
			"transitive_dependents": transitive,
			"affected_files":        affectedFiles,
			"counts":                map[string]int{"direct": len(direct), "transitive": len(transitive), "files": len(affectedFiles)},
			"truncated":             truncated,
		}
		r, _ := toTextResult(result)
		return r, nil, nil
	}
}
