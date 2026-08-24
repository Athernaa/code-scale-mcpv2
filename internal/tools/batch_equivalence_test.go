package tools

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Athernaa/code-scale-mcpv2/internal/parser"
	"github.com/Athernaa/code-scale-mcpv2/internal/ratelimit"
	"github.com/Athernaa/code-scale-mcpv2/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestBatchAndStandaloneOperationsShareResults(t *testing.T) {
	t.Setenv("CODE_SCALE_TELEMETRY", "off")
	store, err := storage.NewIndexStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	content := []byte("func Example() {\n\treturn 1\n}\n")
	sym := parser.Symbol{ID: "main.go::Example#function", File: "main.go", Name: "Example", QualifiedName: "Example", Kind: parser.KindFunction, Language: "go", Signature: "func Example()", Line: 1, EndLine: 3, ByteLength: int64(len(content) - 1), ContentHash: parser.ComputeContentHash(content[:len(content)-1])}
	if err := store.SaveContentFile("local", "equiv", "main.go", content); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceRepoIndex("local", "equiv", "local", "", map[string]string{"main.go": "hash"}, map[string]string{"main.go": "go"}, []parser.Symbol{sym}); err != nil {
		t.Fatal(err)
	}
	deps := &Deps{Store: store, Throttle: ratelimit.NewThrottler()}

	cases := []struct {
		name string
		tool string
		args any
	}{
		{"get_symbol", "get_symbol", GetSymbolArgs{Repo: "local/equiv", SymbolID: sym.ID, Verify: true}},
		{"get_symbols", "get_symbols", GetSymbolsArgs{Repo: "local/equiv", SymbolIDs: []string{sym.ID}}},
		{"search_symbols", "search_symbols", SearchSymbolsArgs{Repo: "local/equiv", Query: "Example"}},
		{"search_text", "search_text", SearchTextArgs{Repo: "local/equiv", Query: "return"}},
		{"get_file_outline", "get_file_outline", GetFileOutlineArgs{Repo: "local/equiv", FilePath: "main.go", Flat: true}},
		{"get_file_tree", "get_file_tree", GetFileTreeArgs{Repo: "local/equiv"}},
		{"get_repo_outline", "get_repo_outline", GetRepoOutlineArgs{Repo: "local/equiv"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			standalone := standaloneResult(t, deps, tc.tool, tc.args)
			batch := extractBatchResult(t, deps, tc.tool, tc.args)
			deleteMeta(standalone)
			if tc.name == "get_symbol" {
				deleteMeta(batch)
			}
			if !reflect.DeepEqual(standalone, batch) {
				t.Fatalf("standalone and batch results differ:\nstandalone=%#v\nbatch=%#v", standalone, batch)
			}
		})
	}
}

func standaloneResult(t *testing.T, deps *Deps, tool string, args any) map[string]any {
	t.Helper()
	var resultText string
	switch tool {
	case "get_symbol":
		r, _, _ := GetSymbolHandler(deps)(context.Background(), nil, args.(GetSymbolArgs))
		resultText = r.Content[0].(*mcp.TextContent).Text
	case "get_symbols":
		r, _, _ := GetSymbolsHandler(deps)(context.Background(), nil, args.(GetSymbolsArgs))
		resultText = r.Content[0].(*mcp.TextContent).Text
	case "search_symbols":
		r, _, _ := SearchSymbolsHandler(deps)(context.Background(), nil, args.(SearchSymbolsArgs))
		resultText = r.Content[0].(*mcp.TextContent).Text
	case "search_text":
		r, _, _ := SearchTextHandler(deps)(context.Background(), nil, args.(SearchTextArgs))
		resultText = r.Content[0].(*mcp.TextContent).Text
	case "get_file_outline":
		r, _, _ := GetFileOutlineHandler(deps)(context.Background(), nil, args.(GetFileOutlineArgs))
		resultText = r.Content[0].(*mcp.TextContent).Text
	case "get_file_tree":
		r, _, _ := GetFileTreeHandler(deps)(context.Background(), nil, args.(GetFileTreeArgs))
		resultText = r.Content[0].(*mcp.TextContent).Text
	case "get_repo_outline":
		r, _, _ := GetRepoOutlineHandler(deps)(context.Background(), nil, args.(GetRepoOutlineArgs))
		resultText = r.Content[0].(*mcp.TextContent).Text
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(resultText), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func extractBatchResult(t *testing.T, deps *Deps, tool string, args any) map[string]any {
	t.Helper()
	argBytes, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	var argMap map[string]any
	if err := json.Unmarshal(argBytes, &argMap); err != nil {
		t.Fatal(err)
	}
	r, _, _ := BatchExecuteHandler(deps)(context.Background(), nil, BatchArgs{Operations: []BatchOp{{Tool: tool, Args: argMap}}})
	var outer map[string]any
	if err := json.Unmarshal([]byte(r.Content[0].(*mcp.TextContent).Text), &outer); err != nil {
		t.Fatal(err)
	}
	results := outer["results"].([]any)
	return results[0].(map[string]any)["result"].(map[string]any)
}

func deleteMeta(result map[string]any) { delete(result, "_meta") }
