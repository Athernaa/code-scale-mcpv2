package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/syphon1c/code-scale-mcp/internal/storage"
)

type GetRepoOutlineArgs struct {
	Repo string `json:"repo" jsonschema:"Repository name (owner/repo or local name)"`
}

func GetRepoOutlineHandler(deps *Deps) func(context.Context, *mcp.CallToolRequest, GetRepoOutlineArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetRepoOutlineArgs) (*mcp.CallToolResult, any, error) {
		t := newTimer()

		repoID, err := deps.Store.GetRepoID(args.Repo)
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}

		info, dirs, kinds, err := deps.Store.GetRepoOutline(repoID)
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}

		saved, total := deps.addSavings(int64(info.SymbolCount*200), 200)

		result := map[string]any{
			"repo":         info.Repo,
			"indexed_at":   info.IndexedAt,
			"file_count":   info.FileCount,
			"symbol_count": info.SymbolCount,
			"languages":    info.Languages,
			"directories":  dirs,
			"symbol_kinds": kinds,
			"_meta": Meta{
				TimingMs:    t.elapsedMs(),
				Repo:        info.Repo,
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
