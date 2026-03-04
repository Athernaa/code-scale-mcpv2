package tools

import (
	"context"

	"github.com/syphon1c/code-scale-mcp/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
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

		maxResults := args.MaxResults
		if maxResults <= 0 {
			maxResults = 10
		}

		symbols, scores, err := deps.Store.SearchSymbols(repoID, args.Query, args.Kind, args.Language, args.FilePattern, maxResults)
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}

		var results []map[string]any
		for i, sym := range symbols {
			results = append(results, map[string]any{
				"id":        sym.ID,
				"kind":      sym.Kind,
				"name":      sym.Name,
				"file":      sym.File,
				"line":      sym.Line,
				"signature": sym.Signature,
				"summary":   sym.Summary,
				"score":     scores[i],
			})
		}

		saved, total := deps.addSavings(int64(len(symbols)*500), int64(len(results)*50))

		result := map[string]any{
			"repo":         args.Repo,
			"query":        args.Query,
			"result_count": len(results),
			"results":      results,
			"_meta": Meta{
				TimingMs:    t.elapsedMs(),
				Repo:        args.Repo,
				Truncated:   len(results) >= maxResults,
				TokensSaved: saved,
				TotalSaved:  total,
				CostAvoided: storage.CostAvoided(saved),
				TotalCost:   storage.CostAvoided(total),
			},
		}
		r, _ := toTextResult(result)
		return r, nil, nil
	}
}
