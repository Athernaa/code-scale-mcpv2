package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ListReposArgs struct{}

func ListReposHandler(deps *Deps) func(context.Context, *mcp.CallToolRequest, ListReposArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args ListReposArgs) (*mcp.CallToolResult, any, error) {
		t := newTimer()

		repos, err := deps.Store.ListRepos()
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}

		result := map[string]any{
			"count": len(repos),
			"repos": repos,
			"_meta": Meta{
				TimingMs: t.elapsedMs(),
			},
		}
		r, _ := toTextResult(result)
		return r, nil, nil
	}
}
