package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/Athernaa/code-scale-mcpv2/internal/ratelimit"
	"github.com/Athernaa/code-scale-mcpv2/internal/storage"
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

		repoID, err := deps.Store.GetRepoID(args.Repo)
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}

		maxResults := clampResults(args.MaxResults, 10, 200)

		// Progressive throttling
		action := deps.Throttle.Check("search_symbols")
		maxResults, warning := ratelimit.ApplyLimit(action, maxResults)
		if action == ratelimit.ActionBlocked {
			r, _ := errorResult(warning)
			return r, nil, nil
		}

		scored, err := deps.Store.SearchSymbolsWithTier(repoID, args.Query, args.Kind, args.Language, args.FilePattern, maxResults)
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}

		var results []map[string]any
		for _, s := range scored {
			results = append(results, map[string]any{
				"id":         s.Symbol.ID,
				"kind":       s.Symbol.Kind,
				"name":       s.Symbol.Name,
				"file":       s.Symbol.File,
				"line":       s.Symbol.Line,
				"signature":  s.Symbol.Signature,
				"summary":    s.Symbol.Summary,
				"score":      s.Score,
				"match_tier": string(s.Tier),
			})
		}

		saved, total := deps.addSavings(int64(len(scored)*500), int64(len(results)*50))

		result := map[string]any{
			"repo":         args.Repo,
			"query":        args.Query,
			"result_count": len(results),
			"results":      results,
		}
		if warning != "" {
			result["warning"] = warning
		}
		result["_meta"] = Meta{
			TimingMs:    t.elapsedMs(),
			Repo:        args.Repo,
			Truncated:   len(results) >= maxResults,
			TokensSaved: saved,
			TotalSaved:  total,
			CostAvoided: storage.CostAvoided(saved),
			TotalCost:   storage.CostAvoided(total),
		}
		r, _ := toTextResult(result)
		return r, nil, nil
	}
}
