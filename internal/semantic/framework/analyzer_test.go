package framework

import (
	"context"
	"testing"

	"github.com/Athernaa/code-scale-mcpv2/internal/parser"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic/fivem"
)

func TestCustomProviderAndObjectFlow(t *testing.T) {
	files := map[string][]byte{
		"fxmanifest.lua": []byte("fx_version 'cerulean'\nserver_script 'provider.lua'\n"),
		"provider.lua":   []byte("exports('GetContext', function(source) end)\nexports('FetchEntity', function(source) end)\nexports('SetFlag', function(source) end)\n"),
		"jobs.lua":       []byte("local Context = exports.banana:GetContext(source)\nContext:SetFlag('working', true)\n"),
		"admin.lua":      []byte("local Context = exports.banana:GetContext(source)\nContext:SetFlag('admin', true)\n"),
	}
	result := analyzeFixture(t, "banana", files, []semantic.Entity{
		manifest("banana"),
		exportDefinition("banana", "provider.lua", "GetContext", 1),
		exportDefinition("banana", "provider.lua", "FetchEntity", 2),
		exportDefinition("banana", "provider.lua", "SetFlag", 3),
	})
	var providers, calls, candidates int
	for _, entity := range result.Entities {
		switch entity.Kind {
		case KindAPIProvider:
			providers++
		case KindAPICall:
			calls++
			if entity.Name == "GetContext" && entity.Metadata["provider_verified"] != true {
				t.Fatal("literal custom export call was not verified")
			}
		case KindCandidate:
			candidates++
		}
	}
	if providers != 3 || calls < 4 || candidates != 1 {
		t.Fatalf("unexpected custom framework facts: providers=%d calls=%d candidates=%d result=%#v", providers, calls, candidates, result)
	}
	if len(result.Relationships) < 2 {
		t.Fatalf("expected provider relationships, got %#v", result.Relationships)
	}
}

func TestObjectFlowShadowingReassignmentAndBranchAmbiguity(t *testing.T) {
	files := map[string][]byte{"fxmanifest.lua": []byte("fx_version 'cerulean'\nserver_script 'main.lua'\n"), "main.lua": []byte(`local Core = exports.banana:GetContext(source)
function A()
    Core:SetFlag('a', true)
end
function B(Core)
    Core:SetFlag('b', true)
end
Core = otherObject
Core:SetFlag('c', true)
local Maybe
if condition then
    Maybe = exports.banana:GetContext(source)
else
    Maybe = exports.other:GetContext(source)
end
Maybe:SetFlag('d', true)
`)}
	result := analyzeFixture(t, "banana", files, []semantic.Entity{manifest("banana"), exportDefinition("banana", "main.lua", "GetContext", 1)})
	resolved := 0
	for _, entity := range result.Entities {
		if entity.Kind == KindAPICall && entity.Name == "SetFlag" && entity.Metadata["target_resource"] == "banana" {
			resolved++
		}
	}
	if resolved != 1 {
		t.Fatalf("shadowed, reassigned, or ambiguous object calls were resolved: %d facts %#v", resolved, result.Entities)
	}
}

func TestKnownAdapterRequiresLocalEvidenceAndPreservesCustomAPIs(t *testing.T) {
	files := map[string][]byte{"fxmanifest.lua": []byte("fx_version 'cerulean'\nserver_script 'inventory.lua'\n"), "inventory.lua": []byte("exports('AddItem', function() end)\nexports('RemoveItem', function() end)\nexports('CustomTransfer', function() end)\nexports('ExperimentalContainer', function() end)\n"), "consumer.lua": []byte("exports.ox_inventory:AddItem(source, 'water', 2)\nexports.ox_inventory:CustomTransfer(source)\n")}
	entities := []semantic.Entity{manifest("ox_inventory"), exportDefinition("ox_inventory", "inventory.lua", "AddItem", 1), exportDefinition("ox_inventory", "inventory.lua", "RemoveItem", 2), exportDefinition("ox_inventory", "inventory.lua", "CustomTransfer", 3), exportDefinition("ox_inventory", "inventory.lua", "ExperimentalContainer", 4)}
	result := analyzeFixture(t, "ox_inventory", files, entities)
	providers := map[string]bool{}
	operations := map[string]bool{}
	for _, entity := range result.Entities {
		if entity.Kind == KindAPIProvider {
			providers[entity.Name] = true
		}
		if entity.Kind == KindOperation {
			operations[entity.Name] = true
		}
	}
	if !providers["CustomTransfer"] || !providers["ExperimentalContainer"] || !operations["inventory_add_item"] {
		t.Fatalf("local custom APIs or known operation were lost: providers=%#v operations=%#v", providers, operations)
	}
}

func TestDynamicAndFalsePositiveCallsRemainUnresolved(t *testing.T) {
	files := map[string][]byte{"fxmanifest.lua": []byte("fx_version 'cerulean'\nserver_script 'main.lua'\n"), "main.lua": []byte(`local lib = require('local_lib')
lib.notify('not ox')
function test(QBCore)
    QBCore.Functions.GetPlayer(source)
end
local resource = getResourceName()
exports[resource]:AddItem(source, 'water', 2)
`)}
	result := analyzeFixture(t, "app", files, []semantic.Entity{manifest("app")})
	for _, entity := range result.Entities {
		if entity.Kind == KindOperation || entity.Framework == "ox_lib" || entity.Framework == "qbcore" {
			t.Fatalf("false-positive framework fact: %#v", entity)
		}
	}
}

func TestDuplicateProviderNamesRemainAmbiguous(t *testing.T) {
	files := map[string][]byte{
		"a.lua":      []byte("exports('GetContext', function() end)\nexports('SetFlag', function() end)\nexports('Fetch', function() end)\n"),
		"b.lua":      []byte("exports('GetContext', function() end)\nexports('SetFlag', function() end)\nexports('Fetch', function() end)\n"),
		"caller.lua": []byte("exports.banana:GetContext(source)\n"),
	}
	result := analyzeFixture(t, "caller", files, []semantic.Entity{
		manifestWithPath("banana", "a.lua", "a"),
		exportDefinitionWithResource("banana", "a.lua", "GetContext", 1, "a"),
		exportDefinitionWithResource("banana", "a.lua", "SetFlag", 2, "a"),
		exportDefinitionWithResource("banana", "a.lua", "Fetch", 3, "a"),
		exportDefinitionWithResource("banana", "b.lua", "GetContext", 1, "b"),
		exportDefinitionWithResource("banana", "b.lua", "SetFlag", 2, "b"),
		exportDefinitionWithResource("banana", "b.lua", "Fetch", 3, "b"),
		manifestWithPath("caller", "caller.lua", "caller"),
	})
	for _, entity := range result.Entities {
		if entity.Kind == KindAPICall && entity.File == "caller.lua" && (entity.Metadata["provider_verified"] == true || entity.Metadata["operation"] != nil) {
			t.Fatalf("duplicate provider was resolved: %#v", entity)
		}
	}
	if len(result.Relationships) != 0 {
		t.Fatalf("duplicate providers produced relationships: %#v", result.Relationships)
	}
}

func TestKnownObjectLineageAndShadowing(t *testing.T) {
	files := map[string][]byte{"main.lua": []byte(`local QBCore = exports['qb-core']:GetCoreObject()
local Player = QBCore.Functions.GetPlayer(source)
Player.Functions.AddMoney('cash', 500)
function fake(QBCore)
    QBCore.Functions.GetPlayer(source)
end
`)}
	result := analyzeFixture(t, "app", files, []semantic.Entity{
		manifestWithPath("qb-core", "core.lua", "core"),
		exportDefinitionWithResource("qb-core", "core.lua", "GetCoreObject", 1, "core"),
		exportDefinitionWithResource("qb-core", "core.lua", "GetPlayer", 2, "core"),
	})
	var money, fake bool
	for _, entity := range result.Entities {
		if entity.Kind == KindOperation && entity.Name == "player_money_add" {
			money = true
		}
		if entity.Kind == KindAPICall && entity.Name == "GetPlayer" && entity.Line > 3 {
			fake = true
		}
	}
	if !money || fake {
		t.Fatalf("QBCore lineage or shadowing incorrect: money=%v fake=%v facts=%#v", money, fake, result.Entities)
	}
	byName := map[string]semantic.Entity{}
	for _, entity := range result.Entities {
		if entity.Kind == KindAPICall {
			byName[entity.Name+":"+entity.Metadata["mechanism"].(string)] = entity
		}
	}
	if byName["GetPlayer:object_method"].Metadata["origin_factory_id"] != byName["GetCoreObject:export"].ID {
		t.Fatalf("GetPlayer did not retain exact core factory identity: %#v", byName)
	}
	if byName["AddMoney:object_method"].Metadata["origin_factory_id"] != byName["GetPlayer:object_method"].ID {
		t.Fatalf("AddMoney did not retain exact player factory identity: %#v", byName)
	}
}

func TestOxLibRequiresManifestEvidence(t *testing.T) {
	without := analyzeFixture(t, "app", map[string][]byte{"main.lua": []byte("lib.notify({description='x'})\n")}, []semantic.Entity{manifestWithPath("app", "main.lua", "app")})
	for _, entity := range without.Entities {
		if entity.Framework == "ox_lib" || entity.Kind == KindOperation {
			t.Fatalf("local lib was falsely classified: %#v", entity)
		}
	}
	with := analyzeFixture(t, "app", map[string][]byte{"main.lua": []byte("lib.notify({description='x'})\n")}, []semantic.Entity{manifestWithPath("app", "main.lua", "app", map[string]any{"dependencies": []string{"ox_lib"}})})
	found := false
	for _, entity := range with.Entities {
		if entity.Kind == KindOperation && entity.Name == "notification" && entity.Framework == "ox_lib" {
			found = true
		}
	}
	if !found {
		t.Fatalf("verified ox_lib evidence did not enrich notification: %#v", with.Entities)
	}
}

func TestRebuildFactsRefreshesCrossResourceProviderEdges(t *testing.T) {
	provider := semantic.Entity{Analyzer: semantic.AnalyzerFramework, Repo: "r", File: "core.lua", Kind: KindAPIProvider, Name: "GetContext", Framework: FrameworkCustom, Metadata: map[string]any{"provider_resource": "core", "provider_resource_path": "core"}}
	call := semantic.Entity{Analyzer: semantic.AnalyzerFramework, Repo: "r", File: "app.lua", Kind: KindAPICall, Name: "GetContext", Framework: FrameworkCustom, Metadata: map[string]any{"source_resource": "app", "source_resource_path": "app", "target_resource": "core", "api": "GetContext", "mechanism": "export", "provider_verified": false}}
	result := RebuildFacts("r", []semantic.Entity{provider, call})
	if len(result.Relationships) != 1 {
		t.Fatalf("persisted facts did not resolve provider: %#v", result)
	}
	provider.Metadata["provider_resource"] = "other"
	result = RebuildFacts("r", []semantic.Entity{provider, call})
	if len(result.Relationships) != 0 {
		t.Fatalf("provider path mutation should not create unrelated edge: %#v", result.Relationships)
	}
}

func TestStandaloneExportCallKeepsSourceOwner(t *testing.T) {
	files := map[string][]byte{
		"fxmanifest.lua": []byte("fx_version 'cerulean'\nclient_script 'client.lua'\n"),
		"client.lua":     []byte("exports.ox_inventory:AddItem(source, 'water', 1)\n"),
	}
	languages := map[string]string{"fxmanifest.lua": "lua", "client.lua": "lua"}
	symbols := map[string][]parser.Symbol{}
	for path, content := range files {
		parsed, err := parser.ParseFile(content, path, "lua")
		if err != nil {
			t.Fatal(err)
		}
		symbols[path] = parsed
	}
	fiveM, err := fivem.NewAnalyzer().AnalyzeRepository(context.Background(), semantic.RepositoryInput{Repo: "local/app", Resource: "app", Files: files, Languages: languages, Symbols: symbols})
	if err != nil {
		t.Fatal(err)
	}
	result, err := NewAnalyzer().AnalyzeRepository(context.Background(), semantic.RepositoryInput{Repo: "local/app", Resource: "app", Files: files, Languages: languages, Symbols: symbols, SemanticEntities: fiveM.Entities})
	if err != nil {
		t.Fatal(err)
	}
	for _, entity := range result.Entities {
		if entity.Kind == KindAPICall && entity.Name == "AddItem" && entity.Metadata["source_resource"] != "app" {
			t.Fatalf("standalone export call owner was overwritten by target: %#v", entity)
		}
	}
}

func TestKnownAdapterOperationMappings(t *testing.T) {
	cases := []struct {
		name, target, api, operation string
	}{
		{"qbx_core_object", "qbx_core", "GetCoreObject", "framework_object_acquire"},
		{"qbx", "qbx_core", "GetPlayer", "player_lookup"},
		{"qbcore_object", "qb-core", "GetCoreObject", "framework_object_acquire"},
		{"qbcore", "qb-core", "GetPlayer", "player_lookup"},
		{"esx_object", "es_extended", "getSharedObject", "framework_object_acquire"},
		{"esx", "es_extended", "GetPlayerFromId", "player_lookup"},
		{"ox_lib_register", "ox_lib", "registerContext", "context_menu_register"},
		{"ox_lib_show", "ox_lib", "showContext", "context_menu_show"},
		{"ox_target", "ox_target", "addGlobalEntity", "interaction_register"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string][]byte{"fxmanifest.lua": []byte("fx_version 'cerulean'\nserver_script 'main.lua'\n"), "main.lua": []byte("exports." + tc.target + ":" + tc.api + "(source)\n")}
			result := analyzeFixture(t, "app", files, []semantic.Entity{manifest("app")})
			for _, entity := range result.Entities {
				if entity.Kind == KindOperation && entity.Name == tc.operation && entity.Framework != FrameworkUnknown {
					return
				}
			}
			t.Fatalf("adapter %s did not normalize %s: %#v", tc.name, tc.api, result.Entities)
		})
	}
}

func TestRebuildFactsGatesKnownOperationsByLocalResourceRegistry(t *testing.T) {
	provider := semantic.Entity{Analyzer: semantic.AnalyzerFramework, Repo: "r", File: "inventory/api.lua", Kind: KindAPIProvider, Name: "CustomInventoryAPI", Framework: FrameworkCustom, Metadata: map[string]any{"provider_resource": "ox_inventory", "provider_resource_path": "inventory"}}
	call := semantic.Entity{Analyzer: semantic.AnalyzerFramework, Repo: "r", File: "app.lua", Kind: KindAPICall, Name: "AddItem", Framework: "ox_inventory", Metadata: map[string]any{"source_resource": "app", "source_resource_path": "app", "target_resource": "ox_inventory", "api": "AddItem", "mechanism": "export", "operation": "inventory_add_item"}}
	result := RebuildFacts("r", []semantic.Entity{provider, call}, []semantic.ResourceIdentity{{Name: "app", Path: "app"}, {Name: "ox_inventory", Path: "inventory"}})
	for _, entity := range result.Entities {
		if entity.Kind == KindAPICall && entity.Name == "AddItem" {
			if entity.Metadata["provider_status"] != ProviderStatusLocalMissing || entity.Metadata["operation"] != nil || entity.Metadata["provider_verified"] == true {
				t.Fatalf("missing local API remained authoritative: %#v", entity)
			}
		}
		if entity.Kind == KindOperation {
			t.Fatalf("missing local API produced operation: %#v", entity)
		}
	}
	if len(result.Relationships) != 0 {
		t.Fatalf("missing local API produced relationship: %#v", result.Relationships)
	}
}

func TestRebuildFactsDistinguishesExternalAndDuplicateProviders(t *testing.T) {
	externalCall := semantic.Entity{Analyzer: semantic.AnalyzerFramework, Repo: "r", File: "app.lua", Kind: KindAPICall, Name: "AddItem", Framework: "ox_inventory", Metadata: map[string]any{"source_resource": "app", "target_resource": "ox_inventory", "api": "AddItem", "mechanism": "export", "operation": "inventory_add_item"}}
	external := RebuildFacts("r", []semantic.Entity{externalCall}, []semantic.ResourceIdentity{{Name: "app", Path: "app"}})
	if hasFrameworkCallRelationship(external.Relationships) || !hasFactWithStatus(external.Entities, ProviderStatusExternal) {
		t.Fatalf("external provider was not retained as unverified: %#v", external)
	}

	providerA := semantic.Entity{Analyzer: semantic.AnalyzerFramework, Repo: "r", File: "a.lua", Kind: KindAPIProvider, Name: "GetItem", Metadata: map[string]any{"provider_resource": "inventory", "provider_resource_path": "resources/[a]/inventory"}}
	providerB := semantic.Entity{Analyzer: semantic.AnalyzerFramework, Repo: "r", File: "b.lua", Kind: KindAPIProvider, Name: "OtherAPI", Metadata: map[string]any{"provider_resource": "inventory", "provider_resource_path": "resources/[b]/inventory"}}
	duplicateCall := semantic.Entity{Analyzer: semantic.AnalyzerFramework, Repo: "r", File: "caller.lua", Kind: KindAPICall, Name: "GetItem", Metadata: map[string]any{"source_resource": "caller", "target_resource": "inventory", "api": "GetItem", "mechanism": "export"}}
	duplicate := RebuildFacts("r", []semantic.Entity{providerA, providerB, duplicateCall}, []semantic.ResourceIdentity{{Name: "inventory", Path: "resources/[a]/inventory"}, {Name: "inventory", Path: "resources/[b]/inventory"}, {Name: "caller", Path: "resources/[app]/caller"}})
	for _, entity := range duplicate.Entities {
		if entity.Kind == KindAPICall && (entity.Metadata["provider_verified"] == true || entity.Metadata["provider_ambiguous"] != true) {
			t.Fatalf("duplicate runtime name was treated as unique: %#v", entity)
		}
	}
	if len(duplicate.Relationships) != 0 {
		t.Fatalf("duplicate runtime name produced relationship: %#v", duplicate.Relationships)
	}
}

func TestAnonymousLuaScopesPreserveShadowingAndCapture(t *testing.T) {
	files := map[string][]byte{
		"fxmanifest.lua": []byte("fx_version 'cerulean'\nserver_script 'main.lua'\n"),
		"main.lua":       []byte("local Core = exports.banana:GetContext(source)\nRegisterNetEvent('shadow', function(Core)\n    Core:SetFlag('shadow', true)\nend)\nRegisterNetEvent('capture', function()\n    Core:SetFlag('captured', true)\nend)\n"),
	}
	result := analyzeFixture(t, "banana", files, []semantic.Entity{manifest("banana"), exportDefinition("banana", "main.lua", "GetContext", 1)})
	shadowed, captured := 0, 0
	for _, entity := range result.Entities {
		if entity.Kind != KindAPICall || entity.Name != "SetFlag" {
			continue
		}
		if entity.Metadata["target_resource"] == "banana" {
			for _, relationship := range result.Relationships {
				if relationship.FromEntityID != entity.ID || relationship.Kind != RelationshipObjectCall {
					continue
				}
				if entity.Line == 3 {
					shadowed++
				}
				if entity.Line == 6 {
					captured++
				}
			}
		}
	}
	if shadowed != 0 || captured != 1 {
		t.Fatalf("anonymous scope shadow/capture incorrect: shadowed=%d captured=%d facts=%#v relationships=%#v", shadowed, captured, result.Entities, result.Relationships)
	}
}

func TestFrameworkSideComesFromManifestFileEvidence(t *testing.T) {
	files := map[string][]byte{"fxmanifest.lua": []byte("fx_version 'cerulean'\nclient_script 'client.lua'\nserver_script 'server.lua'\nshared_script 'shared.lua'\n"), "client.lua": []byte("exports.unknown:ClientAPI()\n"), "server.lua": []byte("exports.unknown:ServerAPI()\n"), "shared.lua": []byte("exports.unknown:SharedAPI()\n")}
	fact := manifest("app")
	fact.Metadata["file_sides"] = map[string]string{"client.lua": "client", "server.lua": "server", "shared.lua": "shared"}
	result := analyzeFixture(t, "app", files, []semantic.Entity{fact})
	sides := map[string]string{}
	for _, entity := range result.Entities {
		if entity.Kind == KindAPICall {
			sides[entity.File] = entity.Side
		}
	}
	if sides["client.lua"] != "client" || sides["server.lua"] != "server" || sides["shared.lua"] != "shared" {
		t.Fatalf("manifest sides were not preserved: %#v", sides)
	}
}

func TestSameLineFrameworkOperationsUseBackingCallIdentity(t *testing.T) {
	files := map[string][]byte{"fxmanifest.lua": []byte("fx_version 'cerulean'\nclient_script 'client.lua'\n"), "client.lua": []byte("exports.ox_inventory:AddItem(source, 'water', 1); exports.ox_inventory:AddItem(source, 'water', 2)\n")}
	result := analyzeFixture(t, "app", files, []semantic.Entity{manifest("app")})
	calls := map[string]bool{}
	operations := map[string]bool{}
	for _, entity := range result.Entities {
		if entity.Kind == KindAPICall && entity.Name == "AddItem" {
			calls[entity.ID] = true
		}
		if entity.Kind == KindOperation && entity.Name == "inventory_add_item" {
			operations[entity.ID] = true
		}
	}
	if len(calls) != 2 || len(operations) != 2 {
		t.Fatalf("same-line calls or operations collided: calls=%d operations=%d facts=%#v", len(calls), len(operations), result.Entities)
	}
}

func hasFactWithStatus(entities []semantic.Entity, status string) bool {
	for _, entity := range entities {
		if entity.Metadata["provider_status"] == status {
			return true
		}
	}
	return false
}

func hasFrameworkCallRelationship(relationships []semantic.Relationship) bool {
	for _, relationship := range relationships {
		if relationship.Kind == RelationshipFrameworkCalls {
			return true
		}
	}
	return false
}

func analyzeFixture(t *testing.T, resource string, files map[string][]byte, facts []semantic.Entity) semantic.Result {
	t.Helper()
	languages := map[string]string{}
	symbols := map[string][]parser.Symbol{}
	for path, content := range files {
		language := parser.DetectLanguage(path)
		languages[path] = language
		parsed, err := parser.ParseFile(content, path, language)
		if err != nil {
			t.Fatal(err)
		}
		symbols[path] = parsed
	}
	returnResult, err := NewAnalyzer().AnalyzeRepository(context.Background(), semantic.RepositoryInput{Repo: "local/" + resource, Resource: resource, Files: files, Languages: languages, Symbols: symbols, SemanticEntities: facts})
	if err != nil {
		t.Fatal(err)
	}
	return returnResult
}

func manifest(resource string) semantic.Entity {
	return semantic.Entity{Analyzer: semantic.AnalyzerFiveM, Repo: "local/" + resource, File: "fxmanifest.lua", Kind: fivem.KindManifestResource, Name: resource, Metadata: map[string]any{"resource": resource, "client_scripts": []string{}, "server_scripts": []string{"main.lua"}, "shared_scripts": []string{}}}
}

func manifestWithPath(resource, file, path string, metadata ...map[string]any) semantic.Entity {
	value := map[string]any{"resource": resource, "file_sides": map[string]string{file: "server"}}
	for _, extra := range metadata {
		for key, item := range extra {
			value[key] = item
		}
	}
	value["source_resource"] = resource
	value["source_resource_path"] = path
	return semantic.Entity{Analyzer: semantic.AnalyzerFiveM, Repo: "local/" + resource, File: path + "/fxmanifest.lua", Kind: fivem.KindManifestResource, Name: resource, Metadata: value}
}

func exportDefinition(resource, file, name string, line int) semantic.Entity {
	return semantic.Entity{Analyzer: semantic.AnalyzerFiveM, Repo: "local/" + resource, File: file, Kind: fivem.KindExportDefinition, Name: name, Line: line, Side: "server", Metadata: map[string]any{"resource": resource}}
}

func exportDefinitionWithResource(resource, file, name string, line int, path string) semantic.Entity {
	entity := exportDefinition(resource, file, name, line)
	entity.Metadata["source_resource"] = resource
	entity.Metadata["source_resource_path"] = path
	entity.Metadata["source_resource_id"] = semantic.StableID("workspace_resource", entity.Repo, path)
	return entity
}
