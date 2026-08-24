package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/Athernaa/code-scale-mcpv2/internal/parser"
	"github.com/Athernaa/code-scale-mcpv2/internal/storage"
)

type GetFileOutlineArgs struct {
	Repo     string `json:"repo" jsonschema:"Repository name"`
	FilePath string `json:"file_path" jsonschema:"Path to file within the repository"`
	Flat     bool   `json:"flat,omitempty" jsonschema:"Return flat list with depth instead of nested tree"`
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

		// Detect language from first symbol
		language := ""
		if len(symbols) > 0 {
			language = symbols[0].Language
		}

		saved, total := deps.addSavings(int64(len(symbols)*500), int64(len(symbols)*50))

		var syms any
		if args.Flat {
			syms = parser.FlattenSymbols(symbols)
		} else {
			syms = parser.BuildSymbolTree(symbols)
		}

		result := map[string]any{
			"repo":     args.Repo,
			"file":     args.FilePath,
			"language": language,
			"symbols":  syms,
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
