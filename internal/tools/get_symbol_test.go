package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Athernaa/code-scale-mcpv2/internal/parser"
	"github.com/Athernaa/code-scale-mcpv2/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGetSymbolVerifiesBeforeTruncation(t *testing.T) {
	store, err := storage.NewIndexStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	content := []byte("func target() {\n\treturn veryLongValue\n}\n")
	source := string(content)
	symBytes := []byte("func target() {\n\treturn veryLongValue\n}")
	sym := parser.Symbol{
		ID: "main.go::target#function", File: "main.go", Name: "target", QualifiedName: "target",
		Kind: parser.KindFunction, Language: "go", Signature: "func target()", Line: 1, EndLine: 3,
		ByteOffset: 0, ByteLength: int64(len(symBytes)), ContentHash: parser.ComputeContentHash(symBytes),
	}
	if err := store.ReplaceRepoIndex("local", "verify", "local", "", map[string]string{"main.go": "hash"}, map[string]string{"main.go": "go"}, []parser.Symbol{sym}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveContentFile("local", "verify", "main.go", content); err != nil {
		t.Fatal(err)
	}
	deps := &Deps{Store: store}
	value, err := execGetSymbol(deps, GetSymbolArgs{Repo: "local/verify", SymbolID: sym.ID, Verify: true, MaxLength: 12})
	if err != nil {
		t.Fatal(err)
	}
	result := value.(map[string]any)
	if result["content_verified"] != true {
		t.Fatalf("complete source should verify even when response is truncated: %#v", result)
	}
	if source == result["source"] {
		t.Fatal("expected truncated source")
	}
}

func TestToTextResultUsesCompactValidJSON(t *testing.T) {
	result, err := toTextResult(map[string]any{"ok": true, "items": []string{"a", "b"}})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if strings.ContainsAny(text, "\r\n\t") || strings.Contains(text, ": ") {
		t.Fatalf("expected compact JSON, got %q", text)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(text), &decoded); err != nil || decoded["ok"] != true {
		t.Fatalf("expected valid compact JSON, got %q", text)
	}
}
