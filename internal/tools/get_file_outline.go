package tools

import (
	"context"

	"github.com/syphon1c/code-scale-mcp/internal/parser"
	"github.com/syphon1c/code-scale-mcp/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetFileOutlineArgs struct {
	Repo     string `json:"repo" jsonschema:"Repository name"`
	FilePath string `json:"file_path" jsonschema:"Path to file within the repository"`
}

func GetFileOutlineHandler(deps *Deps) func(context.Context, *mcp.CallToolRequest, GetFileOutlineArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetFileOutlineArgs) (*mcp.CallToolResult, any, error) {
		t := newTimer()

		repoID, err := deps.Store.GetRepoID(args.Repo)
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}

		symbols, err := deps.Store.GetSymbolsByFile(repoID, args.FilePath)
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}

		tree := parser.BuildSymbolTree(symbols)

		// Detect language from first symbol
		language := ""
		if len(symbols) > 0 {
			language = symbols[0].Language
		}

		saved, total := deps.addSavings(int64(len(symbols)*500), int64(len(symbols)*50))

		result := map[string]any{
			"repo":     args.Repo,
			"file":     args.FilePath,
			"language": language,
			"symbols":  tree,
			"_meta": Meta{
				TimingMs:    t.elapsedMs(),
				SymbolCount: len(symbols),
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
