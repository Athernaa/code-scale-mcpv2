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
