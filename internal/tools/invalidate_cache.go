package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type InvalidateCacheArgs struct {
	Repo string `json:"repo" jsonschema:"Repository name (owner/repo or local name)"`
}

func InvalidateCacheHandler(deps *Deps) func(context.Context, *mcp.CallToolRequest, InvalidateCacheArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args InvalidateCacheArgs) (*mcp.CallToolResult, any, error) {
		err := deps.Store.DeleteIndex(args.Repo)
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}

		result := map[string]any{
			"success": true,
			"repo":    args.Repo,
			"message": "Index deleted successfully",
		}
		r, _ := toTextResult(result)
		return r, nil, nil
	}
}
