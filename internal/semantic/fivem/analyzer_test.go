package fivem

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Athernaa/code-scale-mcpv2/internal/parser"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic"
)

func loadBasicResource(t *testing.T) (semantic.Result, map[string][]byte) {
	t.Helper()
	root := filepath.Join("..", "..", "..", "testdata", "fivem", "basic_resource")
	files := map[string][]byte{}
	languages := map[string]string{}
	symbols := map[string][]parser.Symbol{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[rel] = content
		languages[rel] = parser.DetectLanguage(rel)
		parsed, err := parser.ParseFile(content, rel, languages[rel])
		if err != nil {
			return err
		}
		symbols[rel] = parsed
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := NewAnalyzer().AnalyzeRepository(context.Background(), semantic.RepositoryInput{
		Repo:      "local/basic-resource",
		Resource:  "basic_resource",
		Files:     files,
		Languages: languages,
		Symbols:   symbols,
	})
	if err != nil {
		t.Fatal(err)
	}
	return result, files
}

func findEntities(result semantic.Result, kind, name string) []semantic.Entity {
	var matches []semantic.Entity
	for _, entity := range result.Entities {
		if entity.Kind == kind && (name == "" || entity.Name == name) {
			matches = append(matches, entity)
		}
	}
	return matches
}

func hasRelationship(result semantic.Result, kind, fromKind, toKind, name string) bool {
	entities := make(map[string]semantic.Entity)
	for _, entity := range result.Entities {
		entities[entity.ID] = entity
	}
	for _, relationship := range result.Relationships {
		from, fromOK := entities[relationship.FromEntityID]
		to, toOK := entities[relationship.ToEntityID]
		if relationship.Kind == kind && fromOK && toOK && from.Kind == fromKind && to.Kind == toKind && relationship.Name == name {
			return true
		}
	}
	return false
}

func TestAnalyzeBasicFiveMResource(t *testing.T) {
	result, _ := loadBasicResource(t)

	resources := findEntities(result, KindManifestResource, "basic_resource")
	if len(resources) != 1 {
		t.Fatalf("expected manifest resource entity, got %#v", resources)
	}
	dependencies := findEntities(result, KindManifestDependency, "")
	if len(dependencies) != 3 {
		t.Fatalf("expected three dependencies, got %#v", dependencies)
	}
	for _, dependency := range []string{"ox_lib", "qbx_core", "ox_inventory"} {
		if len(findEntities(result, KindManifestDependency, dependency)) != 1 {
			t.Fatalf("missing dependency %q", dependency)
		}
	}
	oxLib := findEntities(result, KindManifestDependency, "ox_lib")[0]
	sources, _ := oxLib.Metadata["sources"].([]string)
	if len(sources) != 2 || sources[0] != "external_script_reference" || sources[1] != "explicit_dependency" {
		t.Fatalf("dependency provenance was not preserved: %#v", oxLib.Metadata)
	}

	for _, want := range []struct {
		path string
		side string
	}{
		{"client/main.lua", "client"},
		{"server/main.lua", "server"},
		{"shared/config.lua", "shared"},
	} {
		found := false
		for _, entity := range result.Entities {
			if entity.File == want.path && entity.Side == want.side {
				found = true
				break
			}
		}
		if !found {
			manifest := findEntities(result, KindManifestResource, "basic_resource")[0]
			found = ClassifyPathFromEntity(manifest, want.path) == want.side
		}
		if !found {
			t.Fatalf("expected %s classification for %s", want.side, want.path)
		}
	}

	for _, want := range []struct {
		kind string
		name string
	}{
		{KindEventRegistration, "avenlo:createCharacter"},
		{KindEventHandler, "avenlo:createCharacter"},
		{KindEventTrigger, "avenlo:localEvent"},
		{KindEventTrigger, "avenlo:createCharacter"},
		{KindEventTrigger, "avenlo:characterCreated"},
		{KindNUICallback, "createCharacter"},
		{KindCommandRegistration, "revive"},
		{KindExportDefinition, "getCharacter"},
		{KindExportDefinition, "getCharacterById"},
		{KindExportCall, "GetPlayer"},
		{KindCallbackRegistration, "avenlo:getCharacter"},
		{KindCallbackCall, "avenlo:getCharacter"},
	} {
		if len(findEntities(result, want.kind, want.name)) == 0 {
			t.Fatalf("missing semantic entity kind=%s name=%s", want.kind, want.name)
		}
	}

	exportCalls := findEntities(result, KindExportCall, "GetPlayer")
	if len(exportCalls) != 2 || exportCalls[0].Metadata["resource"] != "qbx_core" || exportCalls[1].Metadata["resource"] != "qbx_core" {
		t.Fatalf("expected both qbx_core export call forms, got %#v", exportCalls)
	}

	dynamic := []semantic.Entity{}
	for _, entity := range result.Entities {
		if entity.Kind == KindEventTrigger && entity.Dynamic {
			dynamic = append(dynamic, entity)
		}
	}
	if len(dynamic) != 1 {
		t.Fatalf("expected one dynamic event trigger, got %#v", dynamic)
	}
	for _, relationship := range result.Relationships {
		if relationship.FromEntityID == dynamic[0].ID {
			t.Fatalf("dynamic event fabricated a relationship: %#v", relationship)
		}
	}

	if !hasRelationship(result, RelationshipTriggers, KindEventTrigger, KindEventHandler, "avenlo:createCharacter") {
		t.Fatal("exact client trigger was not linked to the server handler")
	}
	if hasRelationship(result, RelationshipTriggers, KindEventTrigger, KindEventHandler, "avenlo:unrelated") {
		t.Fatal("unrelated event was incorrectly linked")
	}
	if !hasRelationship(result, RelationshipCalls, KindCallbackCall, KindCallbackRegistration, "avenlo:getCharacter") {
		t.Fatal("callback call was not linked to registration")
	}

	for _, entity := range result.Entities {
		if entity.Name == "avenlo:fakeComment" || entity.Name == "avenlo:fakeString" {
			t.Fatalf("textual example was incorrectly extracted: %#v", entity)
		}
	}
}

func TestManifestSideClassificationDoesNotGuessUnknownFiles(t *testing.T) {
	result, files := loadBasicResource(t)
	manifest := findEntities(result, KindManifestResource, "basic_resource")[0]
	if got := ClassifyPathFromEntity(manifest, "unlisted.lua"); got != "unknown" {
		t.Fatalf("expected unknown side for unlisted file, got %q", got)
	}
	if got := ClassifyPathFromEntity(manifest, "@ox_lib/init.lua"); got != "unknown" {
		t.Fatalf("external path was treated as local: %q", got)
	}
	if _, ok := files["fxmanifest.lua"]; !ok {
		t.Fatal("fixture manifest was not loaded")
	}
}

func TestAnalyzeInlineNetEventHandler(t *testing.T) {
	result, err := NewAnalyzer().AnalyzeFile(context.Background(), semantic.FileInput{
		Repo: "local/inline", File: "server/main.lua", Language: "lua", Side: "server",
		Content: []byte("RegisterNetEvent('avenlo:inline', function(data) end)\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(findEntities(result, KindEventRegistration, "avenlo:inline")) != 1 || len(findEntities(result, KindEventHandler, "avenlo:inline")) != 1 {
		t.Fatalf("inline registration handler was not extracted: %#v", result.Entities)
	}
}

func analyzeLuaForTest(t *testing.T, file, side, source string) []semantic.Entity {
	t.Helper()
	result, err := NewAnalyzer().AnalyzeFile(context.Background(), semantic.FileInput{
		Repo: "local/test", File: file, Language: "lua", Side: side, Content: []byte(source),
	})
	if err != nil {
		t.Fatal(err)
	}
	return result.Entities
}

func TestNetworkTriggersRequireNetworkRegistration(t *testing.T) {
	entities := append(
		analyzeLuaForTest(t, "client.lua", "client", `TriggerServerEvent("test:event")`),
		analyzeLuaForTest(t, "server.lua", "server", `AddEventHandler("test:event", function() end)`)...,
	)
	if relationships := ResolveRelationships(entities); hasRelationship(semantic.Result{Entities: entities, Relationships: relationships}, RelationshipTriggers, KindEventTrigger, KindEventHandler, "test:event") {
		t.Fatal("network trigger resolved without RegisterNetEvent")
	}

	entities = append(entities, analyzeLuaForTest(t, "server.lua", "server", `RegisterNetEvent("test:event")`)...)
	if relationships := ResolveRelationships(entities); !hasRelationship(semantic.Result{Entities: entities, Relationships: relationships}, RelationshipTriggers, KindEventTrigger, KindEventHandler, "test:event") {
		t.Fatal("network trigger did not resolve after matching RegisterNetEvent")
	}

	clientEntities := append(
		analyzeLuaForTest(t, "server.lua", "server", `TriggerClientEvent("test:client", 1)`),
		analyzeLuaForTest(t, "client.lua", "client", `AddEventHandler("test:client", function() end)`)...,
	)
	if relationships := ResolveRelationships(clientEntities); hasRelationship(semantic.Result{Entities: clientEntities, Relationships: relationships}, RelationshipTriggers, KindEventTrigger, KindEventHandler, "test:client") {
		t.Fatal("client network trigger resolved without RegisterNetEvent")
	}
}

func TestEventRegistrationAndHandlerSidesDoNotCrossLink(t *testing.T) {
	client := analyzeLuaForTest(t, "client.lua", "client", `RegisterNetEvent("same:event")
AddEventHandler("same:event", function() end)`)
	server := analyzeLuaForTest(t, "server.lua", "server", `RegisterNetEvent("same:event")
AddEventHandler("same:event", function() end)`)
	entities := append(client, server...)
	relationships := ResolveRelationships(entities)
	byID := make(map[string]semantic.Entity, len(entities))
	for _, entity := range entities {
		byID[entity.ID] = entity
	}
	for _, relationship := range relationships {
		if relationship.Kind != RelationshipRegisters {
			continue
		}
		if byID[relationship.FromEntityID].Side != byID[relationship.ToEntityID].Side {
			t.Fatalf("handler crossed registration side: %#v", relationship)
		}
	}
}

func TestCallbacksSupportOfficialLibCallbackFormsAndDirections(t *testing.T) {
	entities := append(
		analyzeLuaForTest(t, "client.lua", "client", `lib.callback("client:request", false, function() end)`),
		analyzeLuaForTest(t, "server.lua", "server", `lib.callback.register("client:request", function() end)`)...,
	)
	entities = append(entities,
		analyzeLuaForTest(t, "server.lua", "server", `lib.callback("server:request", 1, function() end)`)...,
	)
	entities = append(entities,
		analyzeLuaForTest(t, "client.lua", "client", `lib.callback.register("server:request", function() end)`)...,
	)
	relationships := ResolveRelationships(entities)
	if !hasRelationship(semantic.Result{Entities: entities, Relationships: relationships}, RelationshipCalls, KindCallbackCall, KindCallbackRegistration, "client:request") || !hasRelationship(semantic.Result{Entities: entities, Relationships: relationships}, RelationshipCalls, KindCallbackCall, KindCallbackRegistration, "server:request") {
		t.Fatalf("official lib.callback directions did not resolve: %#v", relationships)
	}

	dynamic := append(
		analyzeLuaForTest(t, "client.lua", "client", `local name = eventName
lib.callback(name, false, function() end)`),
		analyzeLuaForTest(t, "server.lua", "server", `lib.callback.register("eventName", function() end)`)...,
	)
	for _, relationship := range ResolveRelationships(dynamic) {
		if relationship.Kind == RelationshipCalls {
			t.Fatalf("dynamic callback fabricated relationship: %#v", relationship)
		}
	}
}

func TestLatentNetworkEventsUseRegistrationRules(t *testing.T) {
	entities := append(
		analyzeLuaForTest(t, "client.lua", "client", `TriggerLatentServerEvent("latent:server", 1000)`),
		analyzeLuaForTest(t, "server.lua", "server", `RegisterNetEvent("latent:server")
AddEventHandler("latent:server", function() end)`)...,
	)
	entities = append(entities,
		analyzeLuaForTest(t, "server.lua", "server", `TriggerLatentClientEvent("latent:client", 1, 1000)`)...,
	)
	entities = append(entities, analyzeLuaForTest(t, "client.lua", "client", `RegisterNetEvent("latent:client")
AddEventHandler("latent:client", function() end)`)...)
	relationships := ResolveRelationships(entities)
	if !hasRelationship(semantic.Result{Entities: entities, Relationships: relationships}, RelationshipTriggers, KindEventTrigger, KindEventHandler, "latent:server") || !hasRelationship(semantic.Result{Entities: entities, Relationships: relationships}, RelationshipTriggers, KindEventTrigger, KindEventHandler, "latent:client") {
		t.Fatalf("latent network events did not resolve: %#v", relationships)
	}
}

func TestManifestExportsAndExternalScriptDependencies(t *testing.T) {
	files := map[string][]byte{
		"fxmanifest.lua": []byte(`fx_version 'cerulean'
game 'gta5'
shared_script '@ox_lib/init.lua'
server_script '@oxmysql/lib/MySQL.lua'
export 'clientOne'
exports { 'clientOne', 'clientTwo', 'clientTwo' }
server_export 'serverOne'
server_exports { 'serverOne', 'serverTwo' }`),
	}
	result, err := NewAnalyzer().AnalyzeRepository(context.Background(), semantic.RepositoryInput{
		Repo: "local/resource-hash", Resource: "resource", Files: files, Languages: map[string]string{"fxmanifest.lua": "lua"}, Symbols: map[string][]parser.Symbol{},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, dependency := range []string{"ox_lib", "oxmysql"} {
		if len(findEntities(result, KindManifestDependency, dependency)) != 1 {
			t.Fatalf("external script dependency %q missing: %#v", dependency, result.Entities)
		}
	}
	if len(findEntities(result, KindManifestDependency, "@ox_lib/init.lua")) != 0 {
		t.Fatal("external script path was stored as a dependency name")
	}
	for _, want := range []struct {
		name string
		side string
	}{
		{"clientOne", "client"}, {"clientTwo", "client"}, {"serverOne", "server"}, {"serverTwo", "server"},
	} {
		matches := findEntities(result, KindExportDefinition, want.name)
		if len(matches) != 1 || matches[0].Side != want.side || matches[0].Metadata["operation"] != "manifest_export" || matches[0].Metadata["resource"] != "resource" {
			t.Fatalf("manifest export was not normalized: want=%#v got=%#v", want, matches)
		}
	}
}
