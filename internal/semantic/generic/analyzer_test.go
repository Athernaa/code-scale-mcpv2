package generic

import (
	"context"
	"testing"

	"github.com/Athernaa/code-scale-mcpv2/internal/parser"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic"
)

func analyzeTestRepository(t *testing.T, files map[string]string, languages map[string]string, modulePath string) semantic.Result {
	t.Helper()
	symbols := make(map[string][]parser.Symbol, len(files))
	bytes := make(map[string][]byte, len(files))
	for file, source := range files {
		bytes[file] = []byte(source)
		language := languages[file]
		if language == "" {
			continue
		}
		parsed, err := parser.ParseFile(bytes[file], file, language)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		symbols[file] = parsed
	}
	result, err := NewAnalyzer().AnalyzeRepository(context.Background(), semantic.RepositoryInput{
		Repo: "test/repo", ModulePath: modulePath, Files: bytes, Languages: languages, Symbols: symbols,
	})
	if err != nil {
		t.Fatalf("analyze repository: %v", err)
	}
	return result
}

func relationshipsOf(result semantic.Result, kind string) []semantic.Relationship {
	var relationships []semantic.Relationship
	for _, relationship := range result.Relationships {
		if relationship.Kind == kind {
			relationships = append(relationships, relationship)
		}
	}
	return relationships
}

func entityByID(result semantic.Result, id string) (semantic.Entity, bool) {
	for _, entity := range result.Entities {
		if entity.ID == id {
			return entity, true
		}
	}
	return semantic.Entity{}, false
}

func symbolEntity(result semantic.Result, file, name string) (semantic.Entity, bool) {
	for _, entity := range result.Entities {
		if entity.Kind == KindCodeSymbol && entity.File == file && entity.Name == name {
			return entity, true
		}
	}
	return semantic.Entity{}, false
}

func relationshipConnects(result semantic.Result, from, to semantic.Entity, kind string) bool {
	for _, relationship := range result.Relationships {
		if relationship.Kind == kind && relationship.FromEntityID == from.ID && relationship.ToEntityID == to.ID {
			return true
		}
	}
	return false
}

func TestJavaScriptImportsAliasesShadowingAndAmbiguity(t *testing.T) {
	result := analyzeTestRepository(t, map[string]string{
		"a.ts": `export function save() {}`,
		"b.ts": `export function save() {}`,
		"run.ts": `import { save as persist } from "./a"
function run() { persist() }
function shadow(save) { save() }
function ambiguous() { save() }`,
	}, map[string]string{"a.ts": "typescript", "b.ts": "typescript", "run.ts": "typescript"}, "")
	run, ok := symbolEntity(result, "run.ts", "run")
	if !ok {
		t.Fatal("run symbol missing")
	}
	aSave, ok := symbolEntity(result, "a.ts", "save")
	if !ok {
		t.Fatal("a.save symbol missing")
	}
	if !relationshipConnects(result, run, aSave, RelationshipCalls) {
		t.Fatalf("aliased import call did not resolve: %#v", relationshipsOf(result, RelationshipCalls))
	}
	shadow, ok := symbolEntity(result, "run.ts", "shadow")
	if !ok {
		t.Fatal("shadow symbol missing")
	}
	if relationshipConnects(result, shadow, aSave, RelationshipCalls) {
		t.Fatal("shadowed parameter incorrectly resolved to imported symbol")
	}
	ambiguous, ok := symbolEntity(result, "run.ts", "ambiguous")
	if !ok {
		t.Fatal("ambiguous symbol missing")
	}
	for _, relationship := range result.Relationships {
		if relationship.FromEntityID == ambiguous.ID && relationship.Kind == RelationshipCalls {
			t.Fatalf("ambiguous bare name produced a call edge: %#v", relationship)
		}
	}
}

func TestJavaScriptNamespaceAndClassCalls(t *testing.T) {
	result := analyzeTestRepository(t, map[string]string{
		"service.ts": `export function save() {}`,
		"main.ts": `import * as service from "./service"
class Service {
  save() {}
  execute() { this.save() }
}
function run(service) { service.save() }
function use() { service.save() }`,
	}, map[string]string{"service.ts": "typescript", "main.ts": "typescript"}, "")
	serviceSave, ok := symbolEntity(result, "service.ts", "save")
	if !ok {
		t.Fatal("service.save symbol missing")
	}
	use, ok := symbolEntity(result, "main.ts", "use")
	if !ok {
		t.Fatal("use symbol missing")
	}
	if !relationshipConnects(result, use, serviceSave, RelationshipCalls) {
		t.Fatal("namespace import call did not resolve")
	}
	run, ok := symbolEntity(result, "main.ts", "run")
	if !ok {
		t.Fatal("run symbol missing")
	}
	if relationshipConnects(result, run, serviceSave, RelationshipCalls) {
		t.Fatal("unknown object method incorrectly resolved")
	}
	method, ok := symbolEntity(result, "main.ts", "save")
	if !ok {
		t.Fatal("class method missing")
	}
	execute, ok := symbolEntity(result, "main.ts", "execute")
	if !ok {
		t.Fatal("execute symbol missing")
	}
	if !relationshipConnects(result, execute, method, RelationshipCalls) {
		t.Fatal("this.save did not resolve to the unique class method")
	}
}

func TestTSXComponentIsReferenceNotCall(t *testing.T) {
	result := analyzeTestRepository(t, map[string]string{
		"Card.tsx": `export function CharacterCard() { return <div /> }`,
		"Page.tsx": `import { CharacterCard } from "./Card"
function Page() { return <CharacterCard /> }`,
	}, map[string]string{"Card.tsx": "tsx", "Page.tsx": "tsx"}, "")
	page, ok := symbolEntity(result, "Page.tsx", "Page")
	if !ok {
		t.Fatal("Page symbol missing")
	}
	card, ok := symbolEntity(result, "Card.tsx", "CharacterCard")
	if !ok {
		t.Fatal("CharacterCard symbol missing")
	}
	if !relationshipConnects(result, page, card, RelationshipReferences) {
		t.Fatal("JSX component reference did not resolve")
	}
	if relationshipConnects(result, page, card, RelationshipCalls) {
		t.Fatal("JSX component was incorrectly represented as a call")
	}
}

func TestLuaSameFileOnlyAndDynamicCallsRemainConservative(t *testing.T) {
	result := analyzeTestRepository(t, map[string]string{
		"a.lua": `local function validate() end
local function create() validate() end
local fn = callbacks[name]
fn()`,
		"b.lua": `function Save() end`,
		"c.lua": `Save()`,
	}, map[string]string{"a.lua": "lua", "b.lua": "lua", "c.lua": "lua"}, "")
	create, ok := symbolEntity(result, "a.lua", "create")
	if !ok {
		t.Fatal("create symbol missing")
	}
	validate, ok := symbolEntity(result, "a.lua", "validate")
	if !ok {
		t.Fatal("validate symbol missing")
	}
	if !relationshipConnects(result, create, validate, RelationshipCalls) {
		t.Fatal("same-file Lua call did not resolve")
	}
	for _, relationship := range result.Relationships {
		from, fromOK := entityByID(result, relationship.FromEntityID)
		to, toOK := entityByID(result, relationship.ToEntityID)
		if fromOK && toOK && from.File == "c.lua" && to.File == "b.lua" {
			t.Fatalf("cross-file Lua global was incorrectly resolved: %#v", relationship)
		}
	}
}

func TestGoPackageAndExternalResolution(t *testing.T) {
	result := analyzeTestRepository(t, map[string]string{
		"go.mod": `module example.com/project`,
		"service/service.go": `package service
func Save() {}
func Run() { Save() }`,
		"internal/store/store.go": `package store
func Save() {}`,
		"main.go": `package main
import (
  "fmt"
  "example.com/project/internal/store"
)
func Execute() { store.Save(); fmt.Println("x") }`,
	}, map[string]string{"service/service.go": "go", "internal/store/store.go": "go", "main.go": "go"}, "example.com/project")
	run, ok := symbolEntity(result, "service/service.go", "Run")
	if !ok {
		t.Fatal("Go Run symbol missing")
	}
	save, ok := symbolEntity(result, "service/service.go", "Save")
	if !ok {
		t.Fatal("Go Save symbol missing")
	}
	if !relationshipConnects(result, run, save, RelationshipCalls) {
		t.Fatal("same-package Go call did not resolve")
	}
	execute, ok := symbolEntity(result, "main.go", "Execute")
	if !ok {
		t.Fatal("Go Execute symbol missing")
	}
	storeSave, ok := symbolEntity(result, "internal/store/store.go", "Save")
	if !ok {
		t.Fatal("store.Save symbol missing")
	}
	if !relationshipConnects(result, execute, storeSave, RelationshipCalls) {
		t.Fatal("local Go package call did not resolve")
	}
	for _, relationship := range result.Relationships {
		if relationship.FromEntityID == execute.ID && relationship.ToEntityID == save.ID {
			t.Fatal("Go call linked to unrelated same-name function")
		}
	}
}
