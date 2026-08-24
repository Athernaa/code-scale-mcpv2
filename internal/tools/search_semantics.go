package tools

import (
	"context"

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
	Framework string `json:"framework,omitempty"`
	Side      string `json:"side,omitempty"`
	File      string `json:"file"`
	Line      int    `json:"line"`
	EndLine   int    `json:"end_line,omitempty"`
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
		entities, err := deps.Store.SearchSemantic(repoID, args.Query, args.Kind, args.Side, args.MaxResults)
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}
		results := make([]semanticSearchResult, 0, len(entities))
		for _, entity := range entities {
			results = append(results, semanticSearchResult{
				ID: entity.ID, Kind: entity.Kind, Name: entity.Name, Framework: entity.Framework,
				Side: entity.Side, File: entity.File, Line: entity.Line, EndLine: entity.EndLine,
				Dynamic: entity.Dynamic,
			})
		}
		result := map[string]any{
			"repo":      args.Repo,
			"results":   results,
			"truncated": args.MaxResults > 0 && len(results) >= minSemanticLimit(args.MaxResults),
			"_meta":     deps.meta(t, args.Repo, args.MaxResults > 0 && len(results) >= minSemanticLimit(args.MaxResults), 0, 0),
		}
		r, _ := toTextResult(result)
		return r, nil, nil
	}
}

func minSemanticLimit(value int) int {
	if value <= 0 {
		return 20
	}
	if value > 200 {
		return 200
	}
	return value
}
