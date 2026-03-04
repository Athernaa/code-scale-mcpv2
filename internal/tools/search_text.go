package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type SearchTextArgs struct {
	Repo        string `json:"repo" jsonschema:"Repository name"`
	Query       string `json:"query" jsonschema:"Text to search for"`
	FilePattern string `json:"file_pattern,omitempty" jsonschema:"Glob pattern for file paths"`
	MaxResults  int    `json:"max_results,omitempty" jsonschema:"Maximum results (default 20)"`
}

func SearchTextHandler(deps *Deps) func(context.Context, *mcp.CallToolRequest, SearchTextArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args SearchTextArgs) (*mcp.CallToolResult, any, error) {
		t := newTimer()

		repoID, err := deps.Store.GetRepoID(args.Repo)
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}

		maxResults := args.MaxResults
		if maxResults <= 0 {
			maxResults = 20
		}

		results, err := deps.Store.SearchText(repoID, args.Query, args.FilePattern, maxResults)
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}

		result := map[string]any{
			"repo":         args.Repo,
			"query":        args.Query,
			"result_count": len(results),
			"results":      results,
			"_meta": Meta{
				TimingMs:  t.elapsedMs(),
				Repo:      args.Repo,
				Truncated: len(results) >= maxResults,
			},
		}
		r, _ := toTextResult(result)
		return r, nil, nil
	}
}
