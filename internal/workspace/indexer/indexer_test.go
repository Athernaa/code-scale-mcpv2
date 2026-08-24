package indexer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Athernaa/code-scale-mcpv2/internal/parser"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic/generic"
	"github.com/Athernaa/code-scale-mcpv2/internal/storage"
)

func TestWorkspaceCrossResourceFactsAndIsolation(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"server.cfg":                             "ensure core_a\nensure app_a\n",
		"resources/[core]/core_a/fxmanifest.lua": "fx_version 'cerulean'\nserver_script 'server.lua'\nserver_export 'GetValue'\n",
		"resources/[core]/core_a/server.lua":     "local function validate() end\nlocal function run() validate() end\nRegisterNetEvent('workspace:test')\nAddEventHandler('workspace:test', function() end)\nexports('GetValue', function() return 1 end)\nlib.callback.register('workspace:getValue', function() end)\n",
		"resources/[app]/app_a/fxmanifest.lua":   "fx_version 'cerulean'\nclient_script 'client.lua'\n",
		"resources/[app]/app_a/client.lua":       "TriggerServerEvent('workspace:test')\nexports.core_a:GetValue()\nlib.callback.await('workspace:getValue', false)\n",
	}
	contents := map[string][]byte{}
	langs := map[string]string{}
	symbols := map[string][]parser.Symbol{}
	hashes := map[string]string{}
	for path, text := range files {
		full := filepath.Join(root, filepath.FromSlash(path))
		if filepath.Ext(path) == ".cfg" {
			if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(full, []byte(text), 0600); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		data := []byte(text)
		contents[path] = data
		langs[path] = "lua"
		syms, err := parser.ParseFile(data, path, "lua")
		if err != nil {
			t.Fatal(err)
		}
		symbols[path] = syms
		hashes[path] = "hash"
	}
	store, err := storage.NewIndexStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	all := []parser.Symbol{}
	for _, ss := range symbols {
		all = append(all, ss...)
	}
	fl := map[string]string{}
	for p := range hashes {
		fl[p] = "lua"
	}
	if err := store.ReplaceRepoIndex("local", "workspace-test", "local", "", hashes, fl, all, root); err != nil {
		t.Fatal(err)
	}
	id, err := store.GetRepoID("local/workspace-test")
	if err != nil {
		t.Fatal(err)
	}
	got, err := Index(context.Background(), store, id, "local/workspace-test", root, contents, langs, symbols)
	if err != nil {
		t.Fatal(err)
	}
	if got.Discovery.Mode != "fivem_workspace" || len(got.Discovery.Resources) != 2 {
		t.Fatalf("bad mode/result: %#v", got)
	}
	five, err := store.GetSemanticEntitiesForAnalyzer(id, semantic.AnalyzerFiveM)
	if err != nil {
		t.Fatal(err)
	}
	if len(five) == 0 {
		t.Fatal("no per-resource FiveM facts")
	}
	ws, err := store.GetSemanticEntitiesForAnalyzer(id, semantic.AnalyzerFiveMWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	if len(ws) == 0 {
		t.Fatal("no workspace facts")
	}
	rels, err := store.GetSemanticRelationshipsForAnalyzer(id, semantic.AnalyzerFiveMWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	hasEvent, hasExport, hasCallback := false, false, false
	for _, r := range rels {
		switch r.Kind {
		case "cross_resource_event":
			hasEvent = true
		case "cross_resource_export":
			hasExport = true
		case "cross_resource_callback":
			hasCallback = true
		}
	}
	if !hasEvent || !hasExport || !hasCallback {
		t.Fatalf("missing cross-resource relations event=%v export=%v callback=%v rels=%#v entities=%#v", hasEvent, hasExport, hasCallback, rels, five)
	}
	var triggerID string
	for _, entity := range five {
		if entity.Kind == "event_trigger" {
			triggerID = entity.ID
			break
		}
	}
	if triggerID == "" {
		t.Fatal("workspace trigger entity missing")
	}
	trace, _, err := store.TraceSemanticWithOptions(id, triggerID, semantic.AnalyzerFiveMWorkspace, "outgoing", []string{"cross_resource_event"}, 1, 10)
	if err != nil || len(trace) != 1 || trace[0].To == nil || trace[0].To.Kind != "event_handler" {
		t.Fatalf("workspace trace did not cross analyzer endpoints: %#v err=%v", trace, err)
	}
	if _, err := store.GetWorkspace(id); err != nil {
		t.Fatal(err)
	}
	genericResult, err := generic.NewAnalyzer().AnalyzeRepository(context.Background(), semantic.RepositoryInput{Repo: "local/workspace-test", Files: contents, Languages: langs, Symbols: symbols})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceSemanticIndexForAnalyzer(id, semantic.AnalyzerGenericGraph, genericResult); err != nil {
		t.Fatal(err)
	}
	if got, err := store.GetSemanticEntitiesForAnalyzer(id, semantic.AnalyzerGenericGraph); err != nil || len(got) == 0 {
		t.Fatalf("generic graph did not coexist: count=%d err=%v", len(got), err)
	}
	if err := store.ReplaceSemanticIndexForAnalyzer(id, semantic.AnalyzerFiveMWorkspace, semantic.Result{}); err != nil {
		t.Fatal(err)
	}
	if got, err := store.GetSemanticEntitiesForAnalyzer(id, semantic.AnalyzerGenericGraph); err != nil || len(got) == 0 {
		t.Fatalf("workspace clear damaged generic graph: count=%d err=%v", len(got), err)
	}
}
