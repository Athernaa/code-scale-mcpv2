package tools

import (
	"context"

	"github.com/Athernaa/code-scale-mcpv2/internal/semantic"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type SearchSemanticsArgs struct {
	Repo       string `json:"repo" jsonschema:"Repository name"`
	Query      string `json:"query,omitempty" jsonschema:"Optional case-insensitive semantic name query"`
	Kind       string `json:"kind,omitempty" jsonschema:"Optional semantic kind filter"`
	Side       string `json:"side,omitempty" jsonschema:"Optional side filter: client, server, shared, unknown"`
	MaxResults int    `json:"max_results,omitempty" jsonschema:"Maximum results (default 20, max 200)"`
}

type semanticSearchResult struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Operation string `json:"operation,omitempty"`
	Resource  string `json:"resource,omitempty"`
	Framework string `json:"framework,omitempty"`
	Side      string `json:"side,omitempty"`
	File      string `json:"file"`
	Line      int    `json:"line"`
	EndLine   int    `json:"end_line,omitempty"`
	SymbolID  string `json:"symbol_id,omitempty"`
	Dynamic   bool   `json:"dynamic,omitempty"`
}

func SearchSemanticsHandler(deps *Deps) func(context.Context, *mcp.CallToolRequest, SearchSemanticsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args SearchSemanticsArgs) (*mcp.CallToolResult, any, error) {
		t := newTimer()
		repoID, err := deps.Store.GetRepoID(args.Repo)
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}
		entities, truncated, err := deps.Store.SearchSemantic(repoID, args.Query, args.Kind, args.Side, args.MaxResults)
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}
		results := make([]semanticSearchResult, 0, len(entities))
		for _, entity := range entities {
			operation, resource := semanticMetadata(entity)
			results = append(results, semanticSearchResult{
				ID: entity.ID, Kind: entity.Kind, Name: entity.Name, Framework: entity.Framework,
				Side: entity.Side, File: entity.File, Line: entity.Line, EndLine: entity.EndLine,
				SymbolID: entity.SymbolID, Operation: operation, Resource: resource, Dynamic: entity.Dynamic,
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

func semanticMetadata(entity semantic.Entity) (operation, resource string) {
	if entity.Metadata == nil {
		return "", ""
	}
	operation, _ = entity.Metadata["operation"].(string)
	resource, _ = entity.Metadata["resource"].(string)
	return operation, resource
}
