package tools

import (
	"context"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetSymbolArgs struct {
	Repo         string `json:"repo" jsonschema:"Repository name"`
	SymbolID     string `json:"symbol_id" jsonschema:"Symbol ID (e.g. src/main.py::authenticate#function)"`
	Verify       bool   `json:"verify,omitempty" jsonschema:"Verify content hash for drift detection"`
	ContextLines int    `json:"context_lines,omitempty" jsonschema:"Lines of context before/after symbol"`
	MaxLength    int    `json:"max_length,omitempty" jsonschema:"Max bytes for source (0=unlimited). Applies 60/40 head/tail truncation."`
}

func GetSymbolHandler(deps *Deps) func(context.Context, *mcp.CallToolRequest, GetSymbolArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetSymbolArgs) (*mcp.CallToolResult, any, error) {
		t := newTimer()
		value, err := execGetSymbol(deps, args)
		if err != nil { r, _ := errorResult(err.Error()); return r, nil, nil }
		result := value.(map[string]any)
		baselineBytes, responseBytes := int64(0), int64(0)
		if strings.EqualFold(strings.TrimSpace(os.Getenv("CODE_SCALE_TELEMETRY")), "full") {
			if repoID, repoErr := deps.Store.GetRepoID(args.Repo); repoErr == nil {
				if sym, symErr := deps.Store.GetSymbolByID(repoID, args.SymbolID); symErr == nil {
					if fileContent, fileErr := deps.Store.GetFileContent(repoID, sym.File); fileErr == nil { baselineBytes = int64(len(fileContent)) }
				}
			}
		}
		if source, ok := result["source"].(string); ok { responseBytes += int64(len(source)) }
		result["_meta"] = deps.meta(t, args.Repo, result["truncated"] == true, baselineBytes, responseBytes)

		r, _ := toTextResult(result)
		return r, nil, nil
	}
}

// GetSymbolsArgs is for batch symbol retrieval.
type GetSymbolsArgs struct {
	Repo          string `json:"repo" jsonschema:"Repository name"`
	SymbolIDs     []string `json:"symbol_ids" jsonschema:"List of symbol IDs to retrieve"`
	MaxTotalBytes int      `json:"max_total_bytes,omitempty" jsonschema:"Maximum combined source bytes (default 1048576)"`
}

func GetSymbolsHandler(deps *Deps) func(context.Context, *mcp.CallToolRequest, GetSymbolsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetSymbolsArgs) (*mcp.CallToolResult, any, error) {
		t := newTimer()

		value, err := execGetSymbols(deps, args)
		if err != nil { r, _ := errorResult(err.Error()); return r, nil, nil }
		result := value.(map[string]any)
		meta := deps.meta(t, args.Repo, result["truncated"] == true, 0, 0)
		meta.SymbolCount = len(result["symbols"].([]map[string]any))
		result["_meta"] = meta
		r, _ := toTextResult(result)
		return r, nil, nil
	}
}
