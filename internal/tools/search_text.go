package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/syphon1c/code-scale-mcp/internal/ratelimit"
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

		repoID, err := deps.Store.GetRepoID(args.Repo)
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}

		maxResults := clampResults(args.MaxResults, 20, 200)

		// Progressive throttling
		action := deps.Throttle.Check("search_text")
		maxResults, warning := ratelimit.ApplyLimit(action, maxResults)
		if action == ratelimit.ActionBlocked {
			r, _ := errorResult(warning)
			return r, nil, nil
		}

		contextLines := args.ContextLines
		if contextLines < 0 {
			contextLines = 0
		}
		if contextLines > 10 {
			contextLines = 10
		}

		results, err := deps.Store.SearchText(repoID, args.Query, args.FilePattern, maxResults, contextLines)
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}

		result := map[string]any{
			"repo":          args.Repo,
			"query":         args.Query,
			"result_count":  len(results),
			"context_lines": contextLines,
			"results":       results,
		}
		if warning != "" {
			result["warning"] = warning
		}
		result["_meta"] = Meta{
			TimingMs:  t.elapsedMs(),
			Repo:      args.Repo,
			Truncated: len(results) >= maxResults,
		}
		r, _ := toTextResult(result)
		return r, nil, nil
	}
}
