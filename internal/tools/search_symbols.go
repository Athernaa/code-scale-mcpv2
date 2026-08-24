package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/Athernaa/code-scale-mcpv2/internal/ratelimit"
)

type SearchSymbolsArgs struct {
	Repo        string `json:"repo" jsonschema:"Repository name"`
	Query       string `json:"query" jsonschema:"Search query"`
	Kind        string `json:"kind,omitempty" jsonschema:"Filter by symbol kind (function, class, method, constant, type)"`
	Language    string `json:"language,omitempty" jsonschema:"Filter by language"`
	FilePattern string `json:"file_pattern,omitempty" jsonschema:"Glob pattern for file paths"`
	MaxResults  int    `json:"max_results,omitempty" jsonschema:"Maximum results (default 10)"`
}

func SearchSymbolsHandler(deps *Deps) func(context.Context, *mcp.CallToolRequest, SearchSymbolsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args SearchSymbolsArgs) (*mcp.CallToolResult, any, error) {
		t := newTimer()

		maxResults := clampResults(args.MaxResults, 10, 200)

		// Progressive throttling
		action := deps.Throttle.Check("search_symbols")
		maxResults, warning := ratelimit.ApplyLimit(action, maxResults)
		if action == ratelimit.ActionBlocked {
			r, _ := errorResult(warning)
			return r, nil, nil
		}

		args.MaxResults = maxResults
		value, err := execSearchSymbols(deps, args)
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}

		result := value.(map[string]any)
		results, _ := result["results"].([]map[string]any)
		result["repo"] = args.Repo
		result["query"] = args.Query
		if warning != "" {
			result["warning"] = warning
		}
		result["_meta"] = deps.meta(t, args.Repo, len(results) >= maxResults, 0, 0)
		r, _ := toTextResult(result)
		return r, nil, nil
	}
}
