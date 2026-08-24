package tools

import (
	"context"

	"github.com/Athernaa/code-scale-mcpv2/internal/ratelimit"
	"github.com/Athernaa/code-scale-mcpv2/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type SearchTextArgs struct {
	Repo         string `json:"repo" jsonschema:"Repository name"`
	Query        string `json:"query" jsonschema:"Text to search for"`
	FilePattern  string `json:"file_pattern,omitempty" jsonschema:"Glob pattern for file paths"`
	MaxResults   int    `json:"max_results,omitempty" jsonschema:"Maximum results (default 20)"`
	ContextLines int    `json:"context_lines,omitempty" jsonschema:"Lines of context around matches (0=single lines, 3=default snippets, max 10)"`
}

func SearchTextHandler(deps *Deps) func(context.Context, *mcp.CallToolRequest, SearchTextArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args SearchTextArgs) (*mcp.CallToolResult, any, error) {
		t := newTimer()

		maxResults := clampResults(args.MaxResults, 20, 200)

		// Progressive throttling
		action := deps.Throttle.Check("search_text")
		maxResults, warning := ratelimit.ApplyLimit(action, maxResults)
		if action == ratelimit.ActionBlocked {
			r, _ := errorResult(warning)
			return r, nil, nil
		}

		args.MaxResults = maxResults
		value, err := execSearchText(deps, args)
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}

		result := value.(map[string]any)
		results, _ := result["results"].([]storage.TextSearchResult)
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
