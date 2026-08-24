package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Athernaa/code-scale-mcpv2/internal/parser"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic/fivem"
	"github.com/Athernaa/code-scale-mcpv2/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func semanticToolStore(t *testing.T) (*storage.IndexStore, int64) {
	t.Helper()
	store, err := storage.NewIndexStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceRepoIndex("local", "semantic-tools", "local", "", map[string]string{"client.lua": "x", "server.lua": "x"}, map[string]string{"client.lua": "lua", "server.lua": "lua"}, nil); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	repoID, err := store.GetRepoID("local/semantic-tools")
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	trigger := semantic.Entity{ID: "tool-trigger", Repo: "local/semantic-tools", File: "client.lua", SymbolID: "symbol-trigger", Kind: "event_trigger", Name: "avenlo:create", Framework: "fivem", Side: "client", Line: 4}
	handler := semantic.Entity{ID: "tool-handler", Repo: "local/semantic-tools", File: "server.lua", SymbolID: "symbol-handler", Kind: "event_handler", Name: "avenlo:create", Framework: "fivem", Side: "server", Line: 9}
	entities := []semantic.Entity{trigger, handler}
	for i := 0; i < 10; i++ {
		entities = append(entities, semantic.Entity{ID: "tool-command-" + string(rune('a'+i)), Repo: "local/semantic-tools", File: "server.lua", Kind: "command_registration", Name: "cmd", Framework: "fivem", Side: "server", Line: 20 + i})
	}
	if err := store.ReplaceSemanticIndex(repoID, semantic.Result{Entities: entities, Relationships: []semantic.Relationship{{ID: "tool-link", Repo: "local/semantic-tools", FromEntityID: trigger.ID, ToEntityID: handler.ID, Kind: "triggers", Name: trigger.Name, Confidence: 1, File: trigger.File, Line: trigger.Line}}}); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	return store, repoID
}

func decodeToolJSON(t *testing.T, result *mcp.CallToolResult) map[string]any {
	t.Helper()
	if result == nil || result.IsError {
		t.Fatalf("unexpected tool error: %#v", result)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func TestSemanticToolsFilterTraceAndBoundResults(t *testing.T) {
	store, _ := semanticToolStore(t)
	defer func() { _ = store.Close() }()
	deps := &Deps{Store: store}

	searchResult, _, err := SearchSemanticsHandler(deps)(context.Background(), nil, SearchSemanticsArgs{
		Repo: "local/semantic-tools", Query: "create", Kind: "event_handler", Side: "server", MaxResults: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	search := decodeToolJSON(t, searchResult)
	searchItems, ok := search["results"].([]any)
	if !ok || len(searchItems) != 1 || searchItems[0].(map[string]any)["kind"] != "event_handler" {
		t.Fatalf("semantic search returned unexpected result: %#v", search)
	}
	if searchItems[0].(map[string]any)["symbol_id"] != "symbol-handler" {
		t.Fatalf("semantic search omitted symbol bridge: %#v", search)
	}
	if search["truncated"] != false {
		t.Fatalf("exact-limit semantic search incorrectly reported truncation: %#v", search)
	}

	boundedResult, _, err := SearchSemanticsHandler(deps)(context.Background(), nil, SearchSemanticsArgs{Repo: "local/semantic-tools", Query: "cmd", MaxResults: 3})
	if err != nil {
		t.Fatal(err)
	}
	bounded := decodeToolJSON(t, boundedResult)
	if len(bounded["results"].([]any)) != 3 {
		t.Fatalf("semantic search exceeded max_results: %#v", bounded)
	}

	traceResult, _, err := TraceRelationshipsHandler(deps)(context.Background(), nil, TraceRelationshipsArgs{
		Repo: "local/semantic-tools", EntityID: "tool-trigger", Direction: "outgoing", Depth: 2, MaxResults: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	trace := decodeToolJSON(t, traceResult)
	if len(trace["results"].([]any)) > 1 {
		t.Fatalf("trace exceeded max_results: %#v", trace)
	}
	incomingResult, _, err := TraceRelationshipsHandler(deps)(context.Background(), nil, TraceRelationshipsArgs{
		Repo: "local/semantic-tools", EntityID: "tool-handler", Direction: "incoming", Depth: 2, MaxResults: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	incoming := decodeToolJSON(t, incomingResult)
	if len(incoming["results"].([]any)) != 1 {
		t.Fatalf("incoming trace did not resolve the caller: %#v", incoming)
	}
	invalidResult, _, err := TraceRelationshipsHandler(deps)(context.Background(), nil, TraceRelationshipsArgs{
		Repo: "local/semantic-tools", EntityID: "missing", Direction: "outgoing",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !invalidResult.IsError {
		t.Fatal("trace for an unknown entity should return an MCP error result")
	}
}

func TestSemanticSearchPreservesAnalyzerSymbolBridge(t *testing.T) {
	store, err := storage.NewIndexStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	content := []byte("function setup()\n  TriggerEvent('avenlo:local')\nend\n")
	symbols, err := parser.ParseFile(content, "client.lua", "lua")
	if err != nil {
		t.Fatal(err)
	}
	result, err := fivem.NewAnalyzer().AnalyzeFile(context.Background(), semantic.FileInput{
		Repo: "local/bridge", File: "client.lua", Language: "lua", Side: "client", Content: content, Symbols: symbols,
	})
	if err != nil || len(result.Entities) != 1 || result.Entities[0].SymbolID == "" {
		t.Fatalf("analyzer did not associate a symbol: %#v err=%v", result.Entities, err)
	}
	if err := store.ReplaceRepoIndex("local", "bridge", "local", "", map[string]string{"client.lua": "hash"}, map[string]string{"client.lua": "lua"}, symbols); err != nil {
		t.Fatal(err)
	}
	repoID, err := store.GetRepoID("local/bridge")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceSemanticIndex(repoID, result); err != nil {
		t.Fatal(err)
	}
	response, _, err := SearchSemanticsHandler(&Deps{Store: store})(context.Background(), nil, SearchSemanticsArgs{
		Repo: "local/bridge", Query: "avenlo:local", MaxResults: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	decoded := decodeToolJSON(t, response)
	items := decoded["results"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["symbol_id"] != result.Entities[0].SymbolID {
		t.Fatalf("semantic-to-symbol bridge was lost: %#v", decoded)
	}
}

func TestGenericTraceBySymbolIDAndImpact(t *testing.T) {
	store, err := storage.NewIndexStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if err := store.ReplaceRepoIndex("local", "generic-tools", "local", "", map[string]string{"a.ts": "a", "b.ts": "b", "c.ts": "c"}, map[string]string{"a.ts": "typescript", "b.ts": "typescript", "c.ts": "typescript"}, nil); err != nil {
		t.Fatal(err)
	}
	repoID, err := store.GetRepoID("local/generic-tools")
	if err != nil {
		t.Fatal(err)
	}
	a := semantic.Entity{ID: "generic-a", Analyzer: semantic.AnalyzerGenericGraph, Repo: "local/generic-tools", File: "a.ts", SymbolID: "a.ts::save#function", Kind: "code_symbol", Name: "save", Line: 1}
	b := semantic.Entity{ID: "generic-b", Analyzer: semantic.AnalyzerGenericGraph, Repo: "local/generic-tools", File: "b.ts", SymbolID: "b.ts::run#function", Kind: "code_symbol", Name: "run", Line: 2}
	c := semantic.Entity{ID: "generic-c", Analyzer: semantic.AnalyzerGenericGraph, Repo: "local/generic-tools", File: "c.ts", SymbolID: "c.ts::page#function", Kind: "code_symbol", Name: "page", Line: 3}
	edges := []semantic.Relationship{
		{ID: "generic-call-b-a", Analyzer: semantic.AnalyzerGenericGraph, Repo: a.Repo, FromEntityID: b.ID, ToEntityID: a.ID, Kind: "calls", File: b.File, Line: 2},
		{ID: "generic-ref-c-b", Analyzer: semantic.AnalyzerGenericGraph, Repo: a.Repo, FromEntityID: c.ID, ToEntityID: b.ID, Kind: "references", File: c.File, Line: 3},
	}
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerGenericGraph, semantic.Result{Entities: []semantic.Entity{a, b, c}, Relationships: edges}); err != nil {
		t.Fatal(err)
	}
	deps := &Deps{Store: store}
	traceResult, _, err := TraceRelationshipsHandler(deps)(context.Background(), nil, TraceRelationshipsArgs{
		Repo: "local/generic-tools", SymbolID: a.SymbolID, Direction: "incoming", RelationshipKinds: []string{"calls"}, Depth: 1, MaxResults: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	trace := decodeToolJSON(t, traceResult)
	traceItems := trace["results"].([]any)
	traceFrom := traceItems[0].(map[string]any)["from"].(map[string]any)
	if len(traceItems) != 1 || traceFrom["symbol_id"] == nil {
		t.Fatalf("symbol-id trace did not return compact endpoint bridge: %#v", trace)
	}
	impactResult, _, err := AnalyzeImpactHandler(deps)(context.Background(), nil, AnalyzeImpactArgs{
		Repo: "local/generic-tools", SymbolID: a.SymbolID, Depth: 3, MaxResults: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	impact := decodeToolJSON(t, impactResult)
	counts := impact["counts"].(map[string]any)
	if counts["direct"] != float64(1) || counts["transitive"] != float64(1) || counts["files"] != float64(2) {
		t.Fatalf("impact traversal returned incorrect dependent counts: %#v", impact)
	}
}
