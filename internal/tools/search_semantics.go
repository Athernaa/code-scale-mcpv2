package tools

import (
	"context"

	"github.com/Athernaa/code-scale-mcpv2/internal/semantic"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type SearchSemanticsArgs struct {
	Repo            string `json:"repo" jsonschema:"Repository name"`
	Query           string `json:"query,omitempty" jsonschema:"Optional case-insensitive semantic name query"`
	Kind            string `json:"kind,omitempty" jsonschema:"Optional semantic kind filter"`
	Side            string `json:"side,omitempty" jsonschema:"Optional side filter: client, server, shared, unknown"`
	Analyzer        string `json:"analyzer,omitempty" jsonschema:"Optional analyzer filter, such as fivem or generic_graph"`
	Resource        string `json:"resource,omitempty" jsonschema:"Optional FiveM resource name filter"`
	TargetResource  string `json:"target_resource,omitempty" jsonschema:"Optional export/callback target resource filter"`
	Framework       string `json:"framework,omitempty" jsonschema:"Optional framework classification filter"`
	IncludeInternal bool   `json:"include_internal,omitempty" jsonschema:"Include generic graph site entities"`
	MaxResults      int    `json:"max_results,omitempty" jsonschema:"Maximum results (default 20, max 200)"`
}

type semanticSearchResult struct {
	ID                 string `json:"id"`
	Analyzer           string `json:"analyzer,omitempty"`
	Kind               string `json:"kind"`
	Name               string `json:"name"`
	Operation          string `json:"operation,omitempty"`
	Resource           string `json:"resource,omitempty"`
	SourceResource     string `json:"source_resource,omitempty"`
	SourceResourcePath string `json:"source_resource_path,omitempty"`
	ResourceID         string `json:"resource_id,omitempty"`
	TargetResource     string `json:"target_resource,omitempty"`
	Framework          string `json:"framework,omitempty"`
	ProviderStatus     string `json:"provider_status,omitempty"`
	ProviderVerified   *bool  `json:"provider_verified,omitempty"`
	Side               string `json:"side,omitempty"`
	File               string `json:"file"`
	Line               int    `json:"line"`
	EndLine            int    `json:"end_line,omitempty"`
	SymbolID           string `json:"symbol_id,omitempty"`
	Dynamic            bool   `json:"dynamic,omitempty"`
}

func SearchSemanticsHandler(deps *Deps) func(context.Context, *mcp.CallToolRequest, SearchSemanticsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args SearchSemanticsArgs) (*mcp.CallToolResult, any, error) {
		t := newTimer()
		repoID, err := deps.Store.GetRepoID(args.Repo)
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}
		entities, truncated, err := deps.Store.SearchSemanticWithResourceTargetFrameworkOptions(repoID, args.Query, args.Kind, args.Side, args.Analyzer, args.Resource, args.TargetResource, args.Framework, args.IncludeInternal, args.MaxResults)
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}
		results := make([]semanticSearchResult, 0, len(entities))
		for _, entity := range entities {
			operation, resource, sourceResource, sourceResourcePath, resourceID, targetResource := semanticMetadata(entity)
			providerStatus, providerVerified := providerAuthority(entity)
			results = append(results, semanticSearchResult{
				ID: entity.ID, Analyzer: entity.Analyzer, Kind: entity.Kind, Name: entity.Name, Framework: entity.Framework,
				Side: entity.Side, File: entity.File, Line: entity.Line, EndLine: entity.EndLine,
				SymbolID: entity.SymbolID, Operation: operation, Resource: resource, SourceResource: sourceResource, SourceResourcePath: sourceResourcePath, ResourceID: resourceID, TargetResource: targetResource, ProviderStatus: providerStatus, ProviderVerified: providerVerified, Dynamic: entity.Dynamic,
			})
		}
		result := map[string]any{
			"repo":      args.Repo,
			"results":   results,
			"truncated": truncated,
			"_meta":     deps.meta(t, args.Repo, truncated, 0, 0),
		}
		r, _ := toTextResult(result)
		return r, nil, nil
	}
}

func providerAuthority(entity semantic.Entity) (string, *bool) {
	if entity.Metadata == nil {
		return "", nil
	}
	status, _ := entity.Metadata["provider_status"].(string)
	value, ok := entity.Metadata["provider_verified"].(bool)
	if !ok {
		return status, nil
	}
	return status, &value
}

func semanticMetadata(entity semantic.Entity) (operation, resource, sourceResource, sourceResourcePath, resourceID, targetResource string) {
	if entity.Metadata == nil {
		return "", "", "", "", "", ""
	}
	operation, _ = entity.Metadata["operation"].(string)
	resource, _ = entity.Metadata["resource"].(string)
	sourceResource, _ = entity.Metadata["source_resource"].(string)
	if resource == "" {
		resource = sourceResource
	}
	sourceResourcePath, _ = entity.Metadata["source_resource_path"].(string)
	if sourceResourcePath == "" {
		sourceResourcePath, _ = entity.Metadata["path"].(string)
	}
	resourceID, _ = entity.Metadata["source_resource_id"].(string)
	if resourceID == "" {
		resourceID, _ = entity.Metadata["resource_id"].(string)
	}
	targetResource, _ = entity.Metadata["target_resource"].(string)
	return operation, resource, sourceResource, sourceResourcePath, resourceID, targetResource
}
