package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Athernaa/code-scale-mcpv2/internal/semantic"
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
	trigger := semantic.Entity{ID: "tool-trigger", Repo: "local/semantic-tools", File: "client.lua", Kind: "event_trigger", Name: "avenlo:create", Framework: "fivem", Side: "client", Line: 4}
	handler := semantic.Entity{ID: "tool-handler", Repo: "local/semantic-tools", File: "server.lua", Kind: "event_handler", Name: "avenlo:create", Framework: "fivem", Side: "server", Line: 9}
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
}
