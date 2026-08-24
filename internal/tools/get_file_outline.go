package tools

import (
	"context"

	"github.com/Athernaa/code-scale-mcpv2/internal/parser"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetFileOutlineArgs struct {
	Repo       string `json:"repo" jsonschema:"Repository name"`
	FilePath   string `json:"file_path" jsonschema:"Path to file within the repository"`
	Flat       bool   `json:"flat,omitempty" jsonschema:"Return flat list with depth instead of nested tree"`
	MaxSymbols int    `json:"max_symbols,omitempty" jsonschema:"Maximum symbols to return (default 200)"`
}

type OutlineSymbol struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Kind      string          `json:"kind"`
	Signature string          `json:"signature,omitempty"`
	Line      int             `json:"line"`
	Depth     int             `json:"depth,omitempty"`
	Children  []OutlineSymbol `json:"children,omitempty"`
}

func compactOutline(sym parser.Symbol, depth int) OutlineSymbol {
	return OutlineSymbol{ID: sym.ID, Name: sym.Name, Kind: sym.Kind, Signature: sym.Signature, Line: sym.Line, Depth: depth}
}

func compactOutlineTree(node parser.SymbolNode, depth int) OutlineSymbol {
	result := compactOutline(node.Symbol, depth)
	for _, child := range node.Children {
		result.Children = append(result.Children, compactOutlineTree(child, depth+1))
	}
	return result
}

func GetFileOutlineHandler(deps *Deps) func(context.Context, *mcp.CallToolRequest, GetFileOutlineArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetFileOutlineArgs) (*mcp.CallToolResult, any, error) {
		t := newTimer()

		value, err := execGetFileOutline(deps, args)
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}
		result := value.(map[string]any)
		result["_meta"] = Meta{TimingMs: t.elapsedMs(), SymbolCount: len(result["symbols"].([]OutlineSymbol)), Truncated: result["truncated"] == true}
		r, _ := toTextResult(result)
		return r, nil, nil
	}
}
