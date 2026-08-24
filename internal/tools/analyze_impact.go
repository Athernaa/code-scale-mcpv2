package tools

import (
	"context"
	"sort"

	"github.com/Athernaa/code-scale-mcpv2/internal/semantic"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type AnalyzeImpactArgs struct {
	Repo              string   `json:"repo" jsonschema:"Repository name"`
	SymbolID          string   `json:"symbol_id,omitempty" jsonschema:"Parser symbol ID; use this or entity_id"`
	EntityID          string   `json:"entity_id,omitempty" jsonschema:"Semantic entity ID for workspace impact; use this or symbol_id"`
	Analyzer          string   `json:"analyzer,omitempty" jsonschema:"Analyzer, defaults to generic_graph for symbol_id or fivem_workspace for entity_id"`
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
		if (args.SymbolID == "") == (args.EntityID == "") {
			r, _ := errorResult("provide exactly one of symbol_id or entity_id")
			return r, nil, nil
		}
		repoID, err := deps.Store.GetRepoID(args.Repo)
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}
		analyzer := args.Analyzer
		if analyzer == "" {
			if args.EntityID != "" {
				if entity, lookupErr := deps.Store.GetSemanticEntityByID(repoID, args.EntityID); lookupErr == nil {
					analyzer = entity.Analyzer
				} else {
					analyzer = semantic.AnalyzerFiveMWorkspace
				}
			} else {
				analyzer = semantic.AnalyzerGenericGraph
			}
		}
		rootID := args.EntityID
		if args.SymbolID != "" {
			root, lookupErr := deps.Store.GetSemanticEntityBySymbolID(repoID, analyzer, args.SymbolID)
			if lookupErr != nil {
				r, _ := errorResult(lookupErr.Error())
				return r, nil, nil
			}
			rootID = root.ID
		}
		kinds := args.RelationshipKinds
		if len(kinds) == 0 {
			if analyzer == semantic.AnalyzerFramework {
				kinds = []string{"framework_calls", "framework_object_call"}
			} else {
				kinds = []string{"calls", "references"}
			}
		}
		edges, truncated, err := deps.Store.TraceSemanticWithOptions(repoID, rootID, analyzer, "incoming", kinds, args.Depth, args.MaxResults)
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}
		direct := make([]impactItem, 0)
		transitive := make([]impactItem, 0)
		files := make(map[string]struct{})
		resources := make(map[string]struct{})
		for _, edge := range edges {
			if edge.From.ID == "" {
				continue
			}
			item := impactItem{SymbolID: edge.From.SymbolID, EntityID: edge.From.ID, Relationship: edge.Kind, File: edge.From.File, Line: edge.From.Line, Depth: edge.Depth}
			files[edge.From.File] = struct{}{}
			_, resource, sourceResource, _, _, _ := semanticMetadata(edge.From)
			if sourceResource == "" {
				sourceResource = resource
			}
			if sourceResource != "" {
				resources[sourceResource] = struct{}{}
			}
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
		affectedResources := make([]string, 0, len(resources))
		for resource := range resources {
			affectedResources = append(affectedResources, resource)
		}
		sort.Strings(affectedResources)
		result := map[string]any{
			"symbol_id":             args.SymbolID,
			"entity_id":             rootID,
			"analyzer":              analyzer,
			"direct_dependents":     direct,
			"transitive_dependents": transitive,
			"affected_files":        affectedFiles,
			"affected_resources":    affectedResources,
			"counts":                map[string]int{"direct": len(direct), "transitive": len(transitive), "files": len(affectedFiles)},
			"truncated":             truncated,
		}
		r, _ := toTextResult(result)
		return r, nil, nil
	}
}
