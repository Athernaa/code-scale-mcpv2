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

func symbolEntityKind(result semantic.Result, file, name, kind string) (semantic.Entity, bool) {
	for _, entity := range result.Entities {
		if entity.Kind == KindCodeSymbol && entity.File == file && entity.Name == name && entity.Metadata["symbol_kind"] == kind {
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
		"x/store/store.go": `package store
func Save() {}`,
		"main.go": `package main
import (
  "fmt"
  svc "example.com/project/internal/store"
  external "example.com/projectx/store"
)
func Execute() { svc.Save(); fmt.Println("x") }
func shadowPackage() { svc := something(); svc.Save() }
type Worker struct{}
func (svc Worker) Run() { svc.Save() }
func externalCall() { external.Save() }`,
	}, map[string]string{"service/service.go": "go", "internal/store/store.go": "go", "x/store/store.go": "go", "main.go": "go"}, "example.com/project")
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
		t.Fatal("aliased local Go package call did not resolve")
	}
	for _, relationship := range result.Relationships {
		if relationship.FromEntityID == execute.ID && relationship.ToEntityID == save.ID {
			t.Fatal("Go call linked to unrelated same-name function")
		}
	}
	shadowPackage, ok := symbolEntity(result, "main.go", "shadowPackage")
	if !ok {
		t.Fatal("shadowPackage symbol missing")
	}
	if relationshipConnects(result, shadowPackage, storeSave, RelationshipCalls) {
		t.Fatal("shadowed Go package binding incorrectly resolved")
	}
	workerRun, ok := symbolEntity(result, "main.go", "Run")
	if !ok {
		t.Fatal("Worker.Run symbol missing")
	}
	if relationshipConnects(result, workerRun, storeSave, RelationshipCalls) {
		t.Fatal("Go method receiver incorrectly resolved to imported package")
	}
	externalCall, ok := symbolEntity(result, "main.go", "externalCall")
	if !ok {
		t.Fatal("externalCall symbol missing")
	}
	externalSave, ok := symbolEntity(result, "x/store/store.go", "Save")
	if ok && relationshipConnects(result, externalCall, externalSave, RelationshipCalls) {
		t.Fatal("module-path prefix collision incorrectly resolved as local")
	}
}

func TestJavaScriptLexicalBindingsShadowImportsAcrossCallsReferencesAndJSX(t *testing.T) {
	result := analyzeTestRepository(t, map[string]string{
		"save.ts":  `export function save() {}`,
		"Card.tsx": `export function Card() { return <div /> }`,
		"main.tsx": `import { save } from "./save"
import { Card } from "./Card"
function parameter(save) { save(); register(save) }
function outer(save) { function inner() { save() } }
function constBinding() { const save = factory(); save(); register(save) }
function letBinding() { let save; save(); register(save) }
function varBinding() { var save = factory(); save() }
const arrowBinding = (save) => save()
function catchBinding() { try {} catch (save) { save() } }
function destructuring({ save }) { save() }
function jsxParameter(Card) { return <Card /> }
function jsxLocal() { const Card = LocalCard; return <Card /> }
function valid() { save(); register(save); return <Card /> }`,
	}, map[string]string{"save.ts": "typescript", "Card.tsx": "tsx", "main.tsx": "tsx"}, "")
	save, ok := symbolEntity(result, "save.ts", "save")
	if !ok {
		t.Fatal("save symbol missing")
	}
	card, ok := symbolEntity(result, "Card.tsx", "Card")
	if !ok {
		t.Fatal("Card symbol missing")
	}
	for _, name := range []string{"parameter", "outer", "constBinding", "letBinding", "varBinding", "arrowBinding", "catchBinding", "destructuring", "jsxParameter", "jsxLocal"} {
		symbol, ok := symbolEntity(result, "main.tsx", name)
		if !ok {
			t.Fatalf("%s symbol missing", name)
		}
		if relationshipConnects(result, symbol, save, RelationshipCalls) || relationshipConnects(result, symbol, save, RelationshipReferences) {
			t.Fatalf("%s incorrectly resolved a shadowed imported save", name)
		}
		if relationshipConnects(result, symbol, card, RelationshipReferences) {
			t.Fatalf("%s incorrectly resolved a shadowed imported Card", name)
		}
	}
	valid, ok := symbolEntity(result, "main.tsx", "valid")
	if !ok {
		t.Fatal("valid symbol missing")
	}
	if !relationshipConnects(result, valid, save, RelationshipCalls) {
		t.Fatal("unshadowed imported call did not resolve")
	}
	if !relationshipConnects(result, valid, save, RelationshipReferences) {
		t.Fatal("unshadowed imported reference did not resolve")
	}
	if !relationshipConnects(result, valid, card, RelationshipReferences) {
		t.Fatal("unshadowed JSX reference did not resolve")
	}
}

func TestJavaScriptAmbiguousModuleAndUnknownReceiverRemainUnresolved(t *testing.T) {
	result := analyzeTestRepository(t, map[string]string{
		"foo.ts": `export function save() {}`,
		"foo.js": `export function save() {}`,
		"main.ts": `import { save } from "./foo"
function run(service) { save(); service.save() }`,
	}, map[string]string{"foo.ts": "typescript", "foo.js": "javascript", "main.ts": "typescript"}, "")
	run, ok := symbolEntity(result, "main.ts", "run")
	if !ok {
		t.Fatal("run symbol missing")
	}
	for _, relationship := range result.Relationships {
		if relationship.FromEntityID == run.ID && relationship.Kind == RelationshipCalls {
			t.Fatalf("ambiguous module or unknown receiver produced a call: %#v", relationship)
		}
	}
	for _, relationship := range relationshipsOf(result, RelationshipImports) {
		if relationship.File == "main.ts" {
			t.Fatalf("ambiguous import produced a resolved module edge: %#v", relationship)
		}
	}
}

func TestJavaScriptSingleArrowParametersAndMethodParametersShadowImports(t *testing.T) {
	result := analyzeTestRepository(t, map[string]string{
		"save.ts": `export function save() {}`,
		"main.ts": `import { save } from "./save"
const arrowCall = save => save()
const arrowReference = save => register(save)
class Service {
  execute(save) { save(); register(save) }
  valid() { save(); register(save) }
}`,
	}, map[string]string{"save.ts": "typescript", "main.ts": "typescript"}, "")
	save, ok := symbolEntity(result, "save.ts", "save")
	if !ok {
		t.Fatal("save symbol missing")
	}
	for _, name := range []string{"arrowCall", "arrowReference", "execute"} {
		symbol, ok := symbolEntity(result, "main.ts", name)
		if !ok {
			t.Fatalf("%s symbol missing", name)
		}
		if relationshipConnects(result, symbol, save, RelationshipCalls) || relationshipConnects(result, symbol, save, RelationshipReferences) {
			t.Fatalf("%s incorrectly resolved a shadowed imported save", name)
		}
	}
	valid, ok := symbolEntity(result, "main.ts", "valid")
	if !ok {
		t.Fatal("valid method symbol missing")
	}
	if !relationshipConnects(result, valid, save, RelationshipCalls) || !relationshipConnects(result, valid, save, RelationshipReferences) {
		t.Fatal("unshadowed imported symbol in method did not resolve")
	}
}

func TestJavaScriptVarUsesFunctionScope(t *testing.T) {
	result := analyzeTestRepository(t, map[string]string{
		"save.ts": `export function save() {}`,
		"main.ts": `import { save } from "./save"
function run() {
  if (condition) { var save = factory() }
  save()
}`,
	}, map[string]string{"save.ts": "typescript", "main.ts": "typescript"}, "")
	run, ok := symbolEntity(result, "main.ts", "run")
	if !ok {
		t.Fatal("run symbol missing")
	}
	save, ok := symbolEntity(result, "save.ts", "save")
	if !ok {
		t.Fatal("save symbol missing")
	}
	if relationshipConnects(result, run, save, RelationshipCalls) {
		t.Fatal("function-scoped var failed to shadow imported save")
	}
}

func TestJavaScriptReferencesRequireLexicalTargetKinds(t *testing.T) {
	result := analyzeTestRepository(t, map[string]string{
		"main.tsx": `class Service { save() {} }
class Foo { Card() {} }
function save() {}
function Page() { registerHandler(save); registerHandler(Service); return <Card /> }`,
	}, map[string]string{"main.tsx": "tsx"}, "")
	page, ok := symbolEntity(result, "main.tsx", "Page")
	if !ok {
		t.Fatal("Page symbol missing")
	}
	serviceMethod, ok := symbolEntityKind(result, "main.tsx", "save", "method")
	if ok && relationshipConnects(result, page, serviceMethod, RelationshipReferences) {
		t.Fatal("bare reference incorrectly resolved to a class method")
	}
	fooMethod, ok := symbolEntityKind(result, "main.tsx", "Card", "method")
	if ok && relationshipConnects(result, page, fooMethod, RelationshipReferences) {
		t.Fatal("JSX reference incorrectly resolved to an unrelated class method")
	}
	functionSave, ok := symbolEntityKind(result, "main.tsx", "save", "function")
	if !ok || !relationshipConnects(result, page, functionSave, RelationshipReferences) {
		t.Fatal("valid lexical function reference did not resolve")
	}
	serviceClass, ok := symbolEntityKind(result, "main.tsx", "Service", "class")
	if !ok || !relationshipConnects(result, page, serviceClass, RelationshipReferences) {
		t.Fatal("valid lexical class reference did not resolve")
	}
}

func TestLuaLexicalShadowingAndAmbiguousRequireRemainUnresolved(t *testing.T) {
	result := analyzeTestRepository(t, map[string]string{
		"foo.lua":      `return { save = function() end }`,
		"foo/init.lua": `return { save = function() end }`,
		"main.lua": `local function save() end
local function parameter(save) save() end
local function localBinding() local save = callbacks[name]; save() end
local module = require("foo")
local function valid() save() end`,
	}, map[string]string{"foo.lua": "lua", "foo/init.lua": "lua", "main.lua": "lua"}, "")
	for _, name := range []string{"parameter", "localBinding"} {
		symbol, ok := symbolEntity(result, "main.lua", name)
		if !ok {
			t.Fatalf("%s symbol missing", name)
		}
		for _, relationship := range result.Relationships {
			if relationship.FromEntityID == symbol.ID && (relationship.Kind == RelationshipCalls || relationship.Kind == RelationshipImports) {
				t.Fatalf("shadowed Lua binding produced a relationship: %#v", relationship)
			}
		}
	}
	for _, relationship := range relationshipsOf(result, RelationshipImports) {
		if relationship.File == "main.lua" {
			t.Fatalf("ambiguous Lua require produced a resolved edge: %#v", relationship)
		}
	}
	valid, ok := symbolEntity(result, "main.lua", "valid")
	if !ok {
		t.Fatal("valid symbol missing")
	}
	save, ok := symbolEntity(result, "main.lua", "save")
	if !ok || !relationshipConnects(result, valid, save, RelationshipCalls) {
		t.Fatal("unshadowed same-file Lua call did not resolve")
	}
}

func TestGoLexicalBindingsAndMethodKindSafety(t *testing.T) {
	result := analyzeTestRepository(t, map[string]string{
		"main.go": `package main
type Service struct{}
func (Service) Save() {}
func Save() {}
func parameter(Save func()) { Save() }
func local() { Save := func() {}; Save() }
func method() { Save() }
func unknown(service Service) { service.Save() }
func receiver(st Service) { st.Save() }`,
	}, map[string]string{"main.go": "go"}, "")
	packageSave, ok := symbolEntityKind(result, "main.go", "Save", "function")
	if !ok {
		t.Fatal("package Save symbol missing")
	}
	for _, name := range []string{"parameter", "local"} {
		symbol, ok := symbolEntity(result, "main.go", name)
		if !ok {
			t.Fatalf("%s symbol missing", name)
		}
		if relationshipConnects(result, symbol, packageSave, RelationshipCalls) {
			t.Fatalf("%s incorrectly resolved a shadowed package function", name)
		}
	}
	method, ok := symbolEntity(result, "main.go", "method")
	if !ok {
		t.Fatal("method symbol missing")
	}
	if !relationshipConnects(result, method, packageSave, RelationshipCalls) {
		t.Fatal("unshadowed package function did not resolve")
	}
	unknown, ok := symbolEntity(result, "main.go", "unknown")
	if !ok {
		t.Fatal("unknown symbol missing")
	}
	for _, relationship := range result.Relationships {
		if relationship.FromEntityID == unknown.ID && relationship.Kind == RelationshipCalls {
			t.Fatalf("receiver call resolved without a safe package binding: %#v", relationship)
		}
	}
}
