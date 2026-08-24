package indexer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Athernaa/code-scale-mcpv2/internal/parser"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic/framework"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic/generic"
	"github.com/Athernaa/code-scale-mcpv2/internal/storage"
	"github.com/Athernaa/code-scale-mcpv2/internal/workspace"
)

func setupWorkspaceRefreshFixture(t *testing.T) (*storage.IndexStore, string, int64, workspace.Discovery, map[string][]byte, map[string]string, map[string][]parser.Symbol) {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"server.cfg":                               "ensure source_a\nensure target_b\nensure unrelated_c\n",
		"resources/[a]/source_a/fxmanifest.lua":    "fx_version 'cerulean'\nclient_script 'client.lua'\n",
		"resources/[a]/source_a/client.lua":        "TriggerServerEvent('refresh:test')\n",
		"resources/[b]/target_b/fxmanifest.lua":    "fx_version 'cerulean'\nserver_script 'server.lua'\n",
		"resources/[b]/target_b/server.lua":        "RegisterNetEvent('refresh:test')\nAddEventHandler('refresh:test', function() end)\n",
		"resources/[c]/unrelated_c/fxmanifest.lua": "fx_version 'cerulean'\nserver_script 'server.lua'\n",
		"resources/[c]/unrelated_c/server.lua":     "RegisterNetEvent('unrelated:event')\n",
	}
	contents := map[string][]byte{}
	languages := map[string]string{}
	symbols := map[string][]parser.Symbol{}
	hashes := map[string]string{}
	var allSymbols []parser.Symbol
	for path, text := range files {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		data := []byte(text)
		if err := os.WriteFile(full, data, 0600); err != nil {
			t.Fatal(err)
		}
		if filepath.Ext(path) == ".cfg" {
			continue
		}
		lang := parser.DetectLanguage(path)
		parsed, err := parser.ParseFile(data, path, lang)
		if err != nil {
			t.Fatal(err)
		}
		contents[path] = data
		languages[path] = lang
		symbols[path] = parsed
		hashes[path] = workspace.ContentHash(data)
		allSymbols = append(allSymbols, parsed...)
	}
	store, err := storage.NewIndexStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceRepoIndex("local", "refresh-workspace", "local", "", hashes, languages, allSymbols, root); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	repoID, err := store.GetRepoID("local/refresh-workspace")
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	d, err := workspace.Discover(root)
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if _, err := Index(context.Background(), store, repoID, "local/refresh-workspace", root, contents, languages, symbols, d); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	return store, root, repoID, d, contents, languages, symbols
}

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
	frameworkFacts, err := store.GetSemanticEntitiesForAnalyzer(id, semantic.AnalyzerFramework)
	if err != nil {
		t.Fatal(err)
	}
	if len(frameworkFacts) == 0 {
		t.Fatal("workspace framework analyzer did not persist API facts")
	}
	for _, fact := range frameworkFacts {
		if fact.Kind == "framework_api_call" && fact.File == "resources/[app]/app_a/client.lua" {
			if fact.Metadata["source_resource"] != "app_a" || fact.Metadata["target_resource"] != "core_a" {
				t.Fatalf("workspace framework owner/target metadata incorrect: %#v", fact)
			}
		}
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

func TestFrameworkAnalysisFailureIsResourceScoped(t *testing.T) {
	store, root, repoID, discovery, contents, languages, symbols := setupWorkspaceRefreshFixture(t)
	defer store.Close()
	original := analyzeFrameworkFn
	defer func() { analyzeFrameworkFn = original }()
	failed := false
	analyzeFrameworkFn = func(ctx context.Context, input semantic.RepositoryInput) (semantic.Result, error) {
		if input.Resource == "target_b" {
			failed = true
			return semantic.Result{}, errors.New("synthetic framework failure")
		}
		return original(ctx, input)
	}
	if _, err := Index(context.Background(), store, repoID, "local/refresh-workspace", root, contents, languages, symbols, discovery); err != nil {
		t.Fatal(err)
	}
	if !failed {
		t.Fatal("framework failure seam was not exercised")
	}
	facts, err := store.GetSemanticEntitiesForAnalyzer(repoID, semantic.AnalyzerFramework)
	if err != nil {
		t.Fatal(err)
	}
	for _, fact := range facts {
		if fact.Metadata["source_resource"] == "target_b" && fact.Kind != framework.KindStatus {
			t.Fatalf("failed resource retained framework fact: %#v", fact)
		}
	}
	if five, _ := store.GetSemanticEntitiesForAnalyzer(repoID, semantic.AnalyzerFiveM); len(five) == 0 {
		t.Fatal("FiveM facts were damaged by framework failure")
	}
	if incomplete, err := store.GetWorkspace(repoID); err != nil || !incomplete.Incomplete {
		t.Fatalf("workspace failure state was not marked incomplete: %#v err=%v", incomplete, err)
	}
}

func TestDuplicateResourcePathsDoNotCrossResolveEventsOrExports(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"server.cfg":                             "ensure inventory\nensure caller\n",
		"resources/[a]/inventory/fxmanifest.lua": "fx_version 'cerulean'\nserver_script 'server.lua'\n",
		"resources/[a]/inventory/server.lua":     "RegisterNetEvent('duplicate:test')\nAddEventHandler('duplicate:test', function() end)\nexports('GetItem', function() end)\n",
		"resources/[b]/inventory/fxmanifest.lua": "fx_version 'cerulean'\nserver_script 'server.lua'\n",
		"resources/[b]/inventory/server.lua":     "AddEventHandler('duplicate:test', function() end)\nexports('GetItem', function() end)\n",
		"resources/[app]/caller/fxmanifest.lua":  "fx_version 'cerulean'\nclient_script 'client.lua'\n",
		"resources/[app]/caller/client.lua":      "TriggerServerEvent('duplicate:test')\nexports.inventory:GetItem()\n",
	}
	contents := map[string][]byte{}
	languages := map[string]string{}
	symbols := map[string][]parser.Symbol{}
	hashes := map[string]string{}
	var allSymbols []parser.Symbol
	for path, text := range files {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		if filepath.Ext(path) == ".cfg" {
			continue
		}
		contents[path] = []byte(text)
		languages[path] = "lua"
		parsed, err := parser.ParseFile([]byte(text), path, "lua")
		if err != nil {
			t.Fatal(err)
		}
		symbols[path] = parsed
		hashes[path] = workspace.ContentHash([]byte(text))
		allSymbols = append(allSymbols, parsed...)
	}
	store, err := storage.NewIndexStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	langs := map[string]string{}
	for path := range hashes {
		langs[path] = languages[path]
	}
	if err := store.ReplaceRepoIndex("local", "duplicate-workspace", "local", "", hashes, langs, allSymbols, root); err != nil {
		t.Fatal(err)
	}
	repoID, err := store.GetRepoID("local/duplicate-workspace")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Index(context.Background(), store, repoID, "local/duplicate-workspace", root, contents, languages, symbols); err != nil {
		t.Fatal(err)
	}
	rels, err := store.GetSemanticRelationshipsForAnalyzer(repoID, semantic.AnalyzerFiveMWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	entities, err := store.GetSemanticEntities(repoID)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]semantic.Entity{}
	for _, entity := range entities {
		byID[entity.ID] = entity
	}
	for _, rel := range rels {
		if rel.Kind != "cross_resource_event" && rel.Kind != "cross_resource_export" {
			continue
		}
		from, fromOK := byID[rel.FromEntityID]
		to, toOK := byID[rel.ToEntityID]
		if !fromOK || !toOK {
			t.Fatalf("relationship endpoint lookup failed: %#v %#v", fromOK, toOK)
		}
		if from.Metadata["source_resource_path"] == "resources/[app]/caller" && to.Metadata["source_resource_path"] == "resources/[b]/inventory" {
			t.Fatalf("duplicate resource produced unsafe cross-resource edge: %#v", rel)
		}
		if rel.Kind == "cross_resource_export" {
			t.Fatalf("duplicate export target should remain unresolved: %#v", rel)
		}
	}
}

func TestRefreshResourceFailureClearsStaleWorkspaceEdgesAndPreservesUnrelatedFacts(t *testing.T) {
	store, root, repoID, discovery, contents, languages, symbols := setupWorkspaceRefreshFixture(t)
	defer func() { _ = store.Close() }()
	initial, err := store.GetSemanticRelationshipsForAnalyzer(repoID, semantic.AnalyzerFiveMWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	if len(initial) == 0 {
		t.Fatal("fixture did not create an initial workspace relationship")
	}
	original := analyzeResourceFn
	analyzeResourceFn = func(ctx context.Context, repo string, resource workspace.Resource, files map[string][]byte, languages map[string]string, symbols map[string][]parser.Symbol) (semantic.Result, error) {
		if resource.Name == "target_b" {
			return semantic.Result{}, errors.New("synthetic analyzer failure")
		}
		return original(ctx, repo, resource, files, languages, symbols)
	}
	defer func() { analyzeResourceFn = original }()
	_, err = RefreshResource(context.Background(), store, repoID, "local/refresh-workspace", root, "resources/[b]/target_b", contents, languages, symbols, discovery)
	if err == nil {
		t.Fatal("expected resource analysis failure")
	}
	remaining, err := store.GetSemanticEntitiesForAnalyzer(repoID, semantic.AnalyzerFiveM)
	if err != nil {
		t.Fatal(err)
	}
	var sourceTrigger, unrelatedFacts bool
	for _, entity := range remaining {
		if entity.Name == "refresh:test" && entity.Kind == "event_trigger" && entity.Metadata["source_resource"] == "source_a" {
			sourceTrigger = true
		}
		if entity.Metadata["source_resource"] == "target_b" {
			t.Fatalf("failed resource retained FiveM facts: %#v", entity)
		}
		if entity.Metadata["source_resource"] == "unrelated_c" && entity.Name == "unrelated:event" {
			unrelatedFacts = true
		}
	}
	if !sourceTrigger || !unrelatedFacts {
		t.Fatal("unrelated source resource facts were removed")
	}
	workspaceRels, err := store.GetSemanticRelationshipsForAnalyzer(repoID, semantic.AnalyzerFiveMWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	for _, relationship := range workspaceRels {
		if relationship.Kind == "cross_resource_event" {
			t.Fatalf("stale workspace relationship survived endpoint removal: %#v", workspaceRels)
		}
	}
	info, err := store.GetWorkspace(repoID)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Incomplete || info.ResourcesWithoutSemantics == 0 {
		t.Fatalf("failed resource was not reflected in workspace completeness: %#v", info)
	}
}

func TestIndexKeepsValidResourcesWhenOneResourceAnalyzerFails(t *testing.T) {
	store, root, repoID, discovery, contents, languages, symbols := setupWorkspaceRefreshFixture(t)
	defer func() { _ = store.Close() }()
	original := analyzeResourceFn
	analyzeResourceFn = func(ctx context.Context, repo string, resource workspace.Resource, files map[string][]byte, languages map[string]string, symbols map[string][]parser.Symbol) (semantic.Result, error) {
		if resource.Name == "target_b" {
			return semantic.Result{}, errors.New("synthetic topology failure")
		}
		return original(ctx, repo, resource, files, languages, symbols)
	}
	defer func() { analyzeResourceFn = original }()
	result, err := Index(context.Background(), store, repoID, "local/refresh-workspace", root, contents, languages, symbols, discovery)
	if err != nil {
		t.Fatalf("partial workspace analysis should be persisted, not returned as a fatal error: %v", err)
	}
	if len(result.FailedResources) != 1 || result.FailedResources[0] != "target_b" {
		t.Fatalf("failed resource diagnostics were not bounded/correct: %#v", result)
	}
	entities, err := store.GetSemanticEntitiesForAnalyzer(repoID, semantic.AnalyzerFiveM)
	if err != nil {
		t.Fatal(err)
	}
	var sourceOK, unrelatedOK bool
	for _, entity := range entities {
		if entity.Metadata["source_resource"] == "target_b" {
			t.Fatalf("failed resource facts survived full rebuild: %#v", entity)
		}
		if entity.Metadata["source_resource"] == "source_a" && entity.Name == "refresh:test" {
			sourceOK = true
		}
		if entity.Metadata["source_resource"] == "unrelated_c" && entity.Name == "unrelated:event" {
			unrelatedOK = true
		}
	}
	if !sourceOK || !unrelatedOK {
		t.Fatalf("valid resource facts were lost during partial rebuild: source=%v unrelated=%v", sourceOK, unrelatedOK)
	}
	relationships, err := store.GetSemanticRelationshipsForAnalyzer(repoID, semantic.AnalyzerFiveMWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	for _, relationship := range relationships {
		if relationship.Kind == "cross_resource_event" {
			t.Fatalf("failed resource left a workspace relationship: %#v", relationship)
		}
	}
	info, err := store.GetWorkspace(repoID)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Incomplete || info.ResourcesWithoutSemantics == 0 {
		t.Fatalf("partial rebuild did not mark workspace incomplete: %#v", info)
	}
}

func TestRefreshResourceFailureRecoversCoverageAndRelationships(t *testing.T) {
	store, root, repoID, discovery, contents, languages, symbols := setupWorkspaceRefreshFixture(t)
	defer func() { _ = store.Close() }()
	original := analyzeResourceFn
	failTarget := true
	analyzeResourceFn = func(ctx context.Context, repo string, resource workspace.Resource, files map[string][]byte, languages map[string]string, symbols map[string][]parser.Symbol) (semantic.Result, error) {
		if failTarget && resource.Name == "target_b" {
			return semantic.Result{}, errors.New("temporary analyzer failure")
		}
		return original(ctx, repo, resource, files, languages, symbols)
	}
	if _, err := RefreshResource(context.Background(), store, repoID, "local/refresh-workspace", root, "resources/[b]/target_b", contents, languages, symbols, discovery); err == nil {
		t.Fatal("expected temporary resource failure")
	}
	failedInfo, err := store.GetWorkspace(repoID)
	if err != nil || !failedInfo.Incomplete {
		t.Fatalf("failed refresh was not marked incomplete: %#v err=%v", failedInfo, err)
	}
	failTarget = false
	if _, err := RefreshResource(context.Background(), store, repoID, "local/refresh-workspace", root, "resources/[b]/target_b", contents, languages, symbols, discovery); err != nil {
		t.Fatal(err)
	}
	analyzeResourceFn = original
	info, err := store.GetWorkspace(repoID)
	if err != nil {
		t.Fatal(err)
	}
	if info.Incomplete || info.ResourcesWithoutSemantics != 0 || info.ResourcesWithSemantics != 3 {
		t.Fatalf("successful resource refresh did not heal coverage: %#v", info)
	}
	relationships, err := store.GetSemanticRelationshipsForAnalyzer(repoID, semantic.AnalyzerFiveMWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	foundEvent := false
	for _, relationship := range relationships {
		if relationship.Kind == "cross_resource_event" && relationship.Name == "refresh:test" {
			foundEvent = true
		}
	}
	if !foundEvent {
		t.Fatalf("relationship was not restored after resource recovery: %#v", relationships)
	}
}

func TestRefreshWorkspaceConfigurationDoesNotAnalyzeResourcesOrResetCompleteness(t *testing.T) {
	store, root, repoID, _, _, _, _ := setupWorkspaceRefreshFixture(t)
	defer func() { _ = store.Close() }()
	if err := store.UpdateWorkspaceCompleteness(repoID, storage.WorkspaceCompleteness{FilesDiscoveredTotal: 100, FilesIndexed: 99, IndexTruncated: true, Incomplete: true, ResourcesWithSemantics: 1}); err != nil {
		t.Fatal(err)
	}
	original := analyzeResourceFn
	analyzeResourceFn = func(context.Context, string, workspace.Resource, map[string][]byte, map[string]string, map[string][]parser.Symbol) (semantic.Result, error) {
		return semantic.Result{}, errors.New("configuration refresh must not analyze source")
	}
	defer func() { analyzeResourceFn = original }()
	if err := os.WriteFile(filepath.Join(root, "server.cfg"), []byte("# ensure source_a\nensure target_b\n"), 0600); err != nil {
		t.Fatal(err)
	}
	d, err := workspace.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RefreshWorkspaceConfiguration(store, repoID, "local/refresh-workspace", root, d); err != nil {
		t.Fatal(err)
	}
	info, err := store.GetWorkspace(repoID)
	if err != nil {
		t.Fatal(err)
	}
	if info.FilesDiscoveredTotal != 100 || info.FilesIndexed != 99 || !info.IndexTruncated || !info.Incomplete {
		t.Fatalf("configuration refresh changed source completeness metadata: %#v", info)
	}
	resources, err := store.GetWorkspaceResources(repoID)
	if err != nil {
		t.Fatal(err)
	}
	for _, resource := range resources {
		if resource.Name == "source_a" && resource.EnabledState != "unknown" {
			t.Fatalf("unexpected config state after commented ensure: %#v", resource)
		}
	}
}

func TestIndexMarksResourceWithoutIndexedManifestAsIncomplete(t *testing.T) {
	root := t.TempDir()
	resourceDir := filepath.Join(root, "resources", "app", "missing_manifest")
	if err := os.MkdirAll(resourceDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "server.cfg"), []byte("ensure missing_manifest\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resourceDir, "fxmanifest.lua"), []byte("fx_version 'cerulean'"), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := storage.NewIndexStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if err := store.ReplaceRepoIndex("local", "missing-manifest", "local", "", nil, nil, nil, root); err != nil {
		t.Fatal(err)
	}
	repoID, err := store.GetRepoID("local/missing-manifest")
	if err != nil {
		t.Fatal(err)
	}
	d, err := workspace.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Index(context.Background(), store, repoID, "local/missing-manifest", root, map[string][]byte{}, map[string]string{}, map[string][]parser.Symbol{}, d)
	if err != nil {
		t.Fatal(err)
	}
	if result.ResourcesWithSemantics != 0 || result.ResourcesWithoutSemantics != 1 {
		t.Fatalf("resource coverage was counted without a manifest entity: %#v", result)
	}
	info, err := store.GetWorkspace(repoID)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Incomplete || info.ResourcesWithoutSemantics != 1 {
		t.Fatalf("missing manifest coverage was not marked incomplete: %#v", info)
	}
}
