package planner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Athernaa/code-scale-mcpv2/internal/parser"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic/fivem"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic/framework"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic/generic"
	"github.com/Athernaa/code-scale-mcpv2/internal/storage"
	"github.com/Athernaa/code-scale-mcpv2/internal/workspace"
	workspaceindex "github.com/Athernaa/code-scale-mcpv2/internal/workspace/indexer"
)

func TestPlannerGenericFixUsesExactSymbolAndDirectNeighbors(t *testing.T) {
	store := plannerStore(t, "local", "generic")
	defer store.Close()
	repo := "local/generic"
	save := plannerSymbol("main.go", "SaveUser", 1)
	write := plannerSymbol("main.go", "writeDB", 5)
	caller := plannerSymbol("main.go", "HandleRequest", 10)
	if err := replacePlannerIndex(store, "local", "generic", repo, []parser.Symbol{save, write, caller}); err != nil {
		t.Fatal(err)
	}
	repoID, err := store.GetRepoID(repo)
	if err != nil {
		t.Fatal(err)
	}
	saveEntity := plannerCodeSymbol(repo, save)
	writeEntity := plannerCodeSymbol(repo, write)
	callerEntity := plannerCodeSymbol(repo, caller)
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerGenericGraph, semantic.Result{
		Entities: []semantic.Entity{saveEntity, writeEntity, callerEntity},
		Relationships: []semantic.Relationship{
			plannerRelationship(repo, callerEntity, saveEntity, generic.RelationshipCalls),
			plannerRelationship(repo, saveEntity, writeEntity, generic.RelationshipCalls),
		},
	}); err != nil {
		t.Fatal(err)
	}
	result, err := New(store).Plan(context.Background(), Request{Repo: repo, Task: "fix SaveUser"})
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskClass != "localized_change" || len(result.Primary) != 1 || result.Primary[0].SymbolID != save.ID {
		t.Fatalf("unexpected primary plan: %#v", result)
	}
	if !containsCandidate(result.Supporting, write.ID, "direct_callee") || !containsCandidate(result.Supporting, caller.ID, "direct_caller") {
		t.Fatalf("direct caller/callee evidence missing: %#v", result)
	}
	for _, candidate := range append(append([]Candidate{}, result.Primary...), result.Supporting...) {
		if candidate.File != "main.go" || candidate.SymbolID == "" {
			t.Fatalf("candidate bridge is not source-backed: %#v", candidate)
		}
	}
}

func TestPlannerUsesCurrentIndexAfterSymbolDeletion(t *testing.T) {
	store := plannerStore(t, "local", "lifecycle")
	defer store.Close()
	repo := "local/lifecycle"
	symbol := plannerSymbol("main.go", "LoadCharacter", 1)
	if err := replacePlannerIndex(store, "local", "lifecycle", repo, []parser.Symbol{symbol}); err != nil {
		t.Fatal(err)
	}
	repoID, err := store.GetRepoID(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerGenericGraph, semantic.Result{Entities: []semantic.Entity{plannerCodeSymbol(repo, symbol)}}); err != nil {
		t.Fatal(err)
	}
	before, err := New(store).Plan(context.Background(), Request{Repo: repo, Task: "find LoadCharacter"})
	if err != nil || len(before.Primary) != 1 {
		t.Fatalf("initial planner candidate missing: %#v %v", before, err)
	}
	if err := store.ReplaceRepoIndex("local", "lifecycle", "local", "", map[string]string{}, map[string]string{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerGenericGraph, semantic.Result{}); err != nil {
		t.Fatal(err)
	}
	after, err := New(store).Plan(context.Background(), Request{Repo: repo, Task: "find LoadCharacter"})
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Primary) != 0 || len(after.Supporting) != 0 || len(after.Peripheral) != 0 {
		t.Fatalf("planner returned deleted candidate: %#v", after)
	}
}

func TestPlannerFindIsMinimalAndAmbiguityIsPreserved(t *testing.T) {
	store := plannerStore(t, "local", "ambiguous")
	defer store.Close()
	repo := "local/ambiguous"
	first := plannerSymbol("a.lua", "init", 1)
	second := plannerSymbol("b.lua", "init", 1)
	if err := replacePlannerIndex(store, "local", "ambiguous", repo, []parser.Symbol{first, second}); err != nil {
		t.Fatal(err)
	}
	repoID, err := store.GetRepoID(repo)
	if err != nil {
		t.Fatal(err)
	}
	entities := []semantic.Entity{plannerCodeSymbol(repo, first), plannerCodeSymbol(repo, second)}
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerGenericGraph, semantic.Result{Entities: entities}); err != nil {
		t.Fatal(err)
	}
	result, err := New(store).Plan(context.Background(), Request{Repo: repo, Task: "find init"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Primary) != 2 || len(result.Supporting) != 0 || len(result.Ambiguities) == 0 {
		t.Fatalf("find task over-expanded or lost ambiguity: %#v", result)
	}
	if len(result.Ambiguities[0].AnchorIDs) != 2 {
		t.Fatalf("source ambiguity lost anchor identity: %#v", result.Ambiguities[0])
	}
	focused, err := New(store).Plan(context.Background(), Request{Repo: repo, Task: "find init", FocusSymbolID: first.ID})
	if err != nil || focused.FocusedAnchor == "" || len(focused.Ambiguities) == 0 || len(focused.Ambiguities[0].AnchorIDs) != 2 {
		t.Fatalf("focused ambiguity metadata was not preserved: %#v %v", focused, err)
	}
	fileScoped, err := New(store).Plan(context.Background(), Request{Repo: repo, Task: "find init in a.lua"})
	if err != nil {
		t.Fatal(err)
	}
	if len(fileScoped.Primary) != 1 || fileScoped.Primary[0].File != "a.lua" {
		t.Fatalf("file-scoped exact match failed: %#v", fileScoped)
	}
}

func TestPlannerFrameworkAuthorityAndWorkspaceHealth(t *testing.T) {
	store := plannerStore(t, "local", "workspace")
	defer store.Close()
	repo := "local/workspace"
	run := plannerSymbol("resources/[app]/app/server.lua", "run", 1)
	providerSymbol := plannerSymbol("resources/[core]/core/server.lua", "AddItem", 1)
	if err := replacePlannerIndex(store, "local", "workspace", repo, []parser.Symbol{run, providerSymbol}); err != nil {
		t.Fatal(err)
	}
	repoID, err := store.GetRepoID(repo)
	if err != nil {
		t.Fatal(err)
	}
	call := semantic.Entity{ID: "framework-call", Analyzer: semantic.AnalyzerFramework, Repo: repo, File: run.File, SymbolID: run.ID, Kind: framework.KindAPICall, Name: "AddItem", Framework: "ox_inventory", Side: "server", Line: 1, Metadata: map[string]any{"source_resource": "app", "target_resource": "ox_inventory", "provider_status": framework.ProviderStatusLocalVerified, "provider_verified": true}}
	provider := semantic.Entity{ID: "framework-provider", Analyzer: semantic.AnalyzerFramework, Repo: repo, File: providerSymbol.File, SymbolID: providerSymbol.ID, Kind: framework.KindAPIProvider, Name: "AddItem", Framework: "ox_inventory", Side: "server", Line: 1, Metadata: map[string]any{"source_resource": "ox_inventory", "provider_status": framework.ProviderStatusLocalVerified, "provider_verified": true}}
	operation := semantic.Entity{ID: "framework-operation", Analyzer: semantic.AnalyzerFramework, Repo: repo, File: run.File, SymbolID: run.ID, Kind: framework.KindOperation, Name: "inventory_add_item", Framework: "ox_inventory", Side: "server", Line: 1, Metadata: map[string]any{"backing_call_id": call.ID, "provider_status": framework.ProviderStatusLocalVerified, "provider_verified": true}}
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerFramework, semantic.Result{Entities: []semantic.Entity{call, provider, operation}, Relationships: []semantic.Relationship{
		plannerRelationship(repo, operation, call, framework.RelationshipDerivedFrom),
		plannerRelationship(repo, call, provider, framework.RelationshipFrameworkCalls),
	}}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceWorkspaceState(repoID, "workspace", "fivem_workspace", nil, nil, storage.WorkspaceCompleteness{FilesDiscoveredTotal: 2, FilesIndexed: 2, Incomplete: true}); err != nil {
		t.Fatal(err)
	}
	result, err := New(store).Plan(context.Background(), Request{Repo: repo, Task: "inventory_add_item"})
	if err != nil {
		t.Fatal(err)
	}
	if result.IndexState != "incomplete" || !result.IndexIncomplete || len(result.Primary) == 0 {
		t.Fatalf("workspace health or framework seed missing: %#v", result)
	}
	providerFound := false
	for _, candidate := range append(append([]Candidate{}, result.Primary...), result.Supporting...) {
		if candidate.SymbolID == providerSymbol.ID {
			providerFound = true
			if candidate.Authority != framework.ProviderStatusLocalVerified {
				t.Fatalf("verified provider authority was lost: %#v", candidate)
			}
		}
	}
	if !providerFound {
		t.Fatalf("verified provider was not expanded: %#v", result)
	}
}

func TestTraceProviderAuthorityRequiresStaticStructuralProof(t *testing.T) {
	call := semantic.Entity{ID: "call", Kind: framework.KindAPICall, Metadata: map[string]any{"provider_status": framework.ProviderStatusLocalVerified, "provider_verified": true, "provider_entity_id": "provider"}}
	provider := semantic.Entity{ID: "provider", Kind: framework.KindAPIProvider}
	edge := semantic.TraceEdge{Relationship: semantic.Relationship{Kind: framework.RelationshipFrameworkCalls}, From: call, To: &provider}
	if got := traceProviderAuthority(provider, edge); got != framework.ProviderStatusLocalVerified {
		t.Fatalf("static framework provider proof was not propagated: %q", got)
	}
	edge.Dynamic = true
	if got := traceProviderAuthority(provider, edge); got != "" {
		t.Fatalf("dynamic framework provider was verified: %q", got)
	}
	edge.Dynamic = false
	edge.From.Metadata["provider_entity_id"] = "other"
	if got := traceProviderAuthority(provider, edge); got != "" {
		t.Fatalf("mismatched provider identity was verified: %q", got)
	}
	exportCall := semantic.Entity{ID: "export-call", Kind: fivem.KindExportCall, Metadata: map[string]any{"provider_status": framework.ProviderStatusLocalVerified, "provider_verified": true, "provider_entity_id": "export-provider"}}
	exportProvider := semantic.Entity{ID: "export-provider", Kind: fivem.KindExportDefinition}
	exportEdge := semantic.TraceEdge{Relationship: semantic.Relationship{Kind: "cross_resource_export"}, From: exportCall, To: &exportProvider}
	if got := traceProviderAuthority(exportProvider, exportEdge); got != framework.ProviderStatusLocalVerified {
		t.Fatalf("unique static export was not verified: %q", got)
	}
	duplicate := exportProvider
	duplicate.ID = "export-provider-2"
	duplicateEdge := exportEdge
	duplicateEdge.To = &duplicate
	duplicateEdge.From.Metadata = map[string]any{"provider_status": framework.ProviderStatusLocalAmbiguous, "provider_verified": false}
	if got := traceProviderAuthority(exportProvider, duplicateEdge); got != "" {
		t.Fatalf("duplicate static export was verified: %q", got)
	}
}

func TestWorkspaceExportAuthorityIsDirectionInvariant(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		t.Run(fmt.Sprintf("duplicate=%v", duplicate), func(t *testing.T) {
			store, repoID, repo, d := buildExportAuthorityWorkspace(t, duplicate)
			defer store.Close()
			entities, err := store.GetSemanticEntitiesForAnalyzer(repoID, semantic.AnalyzerFiveM)
			if err != nil {
				t.Fatal(err)
			}
			var call semantic.Entity
			providers := make([]semantic.Entity, 0, 2)
			for _, entity := range entities {
				if entity.Kind == fivem.KindExportCall && entity.Name == "AddItem" {
					call = entity
				}
				if entity.Kind == fivem.KindExportDefinition && entity.Name == "AddItem" {
					providers = append(providers, entity)
				}
			}
			if call.ID == "" || len(providers) != 1+boolIntForTest(duplicate) {
				t.Fatalf("unexpected persisted export endpoints: call=%#v providers=%#v", call, providers)
			}
			status, _ := call.Metadata["provider_status"].(string)
			if duplicate && status != framework.ProviderStatusLocalAmbiguous {
				t.Fatalf("duplicate provider status=%q: %#v", status, call)
			}
			if !duplicate && status != framework.ProviderStatusLocalVerified {
				t.Fatalf("unique provider status=%q: %#v", status, call)
			}
			for _, provider := range providers {
				outgoing, _, err := store.TraceSemanticRankedWithOptions(repoID, call.ID, semantic.AnalyzerFiveMWorkspace, "outgoing", []string{"cross_resource_export"}, 1, 20)
				if err != nil {
					t.Fatal(err)
				}
				incoming, _, err := store.TraceSemanticRankedWithOptions(repoID, provider.ID, semantic.AnalyzerFiveMWorkspace, "incoming", []string{"cross_resource_export"}, 1, 20)
				if err != nil {
					t.Fatal(err)
				}
				var incomingEdge *semantic.TraceEdge
				for i := range incoming {
					if incoming[i].To != nil && incoming[i].To.ID == provider.ID {
						incomingEdge = &incoming[i]
					}
				}
				if len(outgoing) == 0 || incomingEdge == nil {
					t.Fatalf("missing persisted direction edge duplicate=%v outgoing=%#v incoming=%#v", duplicate, outgoing, incoming)
				}
				if duplicate {
					if got := traceProviderAuthority(provider, *incomingEdge); got == framework.ProviderStatusLocalVerified {
						t.Fatalf("duplicate provider became verified on incoming traversal: %q", got)
					}
				} else if got := traceProviderAuthority(provider, *incomingEdge); got != framework.ProviderStatusLocalVerified {
					t.Fatalf("unique provider lost verification on incoming traversal: %q", got)
				}
			}
			plan, err := New(store).Plan(context.Background(), Request{Repo: repo, Task: "trace AddItem", IncludeImpact: true, MaxCandidates: 100})
			if err != nil {
				t.Fatal(err)
			}
			_ = d
			for _, candidate := range append(append([]Candidate{}, plan.Primary...), plan.Supporting...) {
				if candidate.Name == "AddItem" && containsCandidateReason(candidate, "export_provider") && duplicate && candidate.Authority == framework.ProviderStatusLocalVerified {
					t.Fatalf("Planner fabricated duplicate export provider verification: %#v", candidate)
				}
			}
		})
	}
}

func buildExportAuthorityWorkspace(t *testing.T, duplicate bool) (*storage.IndexStore, int64, string, workspace.Discovery) {
	t.Helper()
	root := t.TempDir()
	filesText := map[string]string{
		"server.cfg":                         "ensure jobs\nensure inventory\n",
		"resources/jobs/fxmanifest.lua":      "fx_version 'cerulean'\nserver_script 'server.lua'\n",
		"resources/jobs/server.lua":          "exports.inventory:AddItem(source, 'water', 1)\n",
		"resources/inventory/fxmanifest.lua": "fx_version 'cerulean'\nserver_script 'server.lua'\n",
		"resources/inventory/server.lua":     "exports('AddItem', function(source, item, count) end)\n",
	}
	if duplicate {
		filesText["resources/inventory/fxmanifest.lua"] = "fx_version 'cerulean'\nserver_script 'server.lua'\nserver_script 'other.lua'\n"
		filesText["resources/inventory/other.lua"] = "exports('AddItem', function(source, item, count) end)\n"
	}
	contents := map[string][]byte{}
	languages := map[string]string{}
	symbols := map[string][]parser.Symbol{}
	hashes := map[string]string{}
	all := []parser.Symbol{}
	for path, source := range filesText {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
		if path == "server.cfg" {
			continue
		}
		data := []byte(source)
		language := parser.DetectLanguage(path)
		parsed, err := parser.ParseFile(data, path, language)
		if err != nil {
			t.Fatal(err)
		}
		contents[path], languages[path], symbols[path], hashes[path] = data, language, parsed, workspace.ContentHash(data)
		all = append(all, parsed...)
	}
	store, err := storage.NewIndexStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceRepoIndex("local", "export-authority", "local", "", hashes, languages, all, root); err != nil {
		store.Close()
		t.Fatal(err)
	}
	for path, data := range contents {
		if err := store.SaveContentFile("local", "export-authority", path, data); err != nil {
			store.Close()
			t.Fatal(err)
		}
	}
	repo := "local/export-authority"
	repoID, err := store.GetRepoID(repo)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	d, err := workspace.Discover(root)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	if _, err := workspaceindex.Index(context.Background(), store, repoID, repo, root, contents, languages, symbols, d); err != nil {
		store.Close()
		t.Fatal(err)
	}
	return store, repoID, repo, d
}

func boolIntForTest(value bool) int {
	if value {
		return 1
	}
	return 0
}

func containsCandidateReason(candidate Candidate, reason string) bool {
	for _, value := range candidate.ReasonCodes {
		if value == reason {
			return true
		}
	}
	return false
}

func TestPlannerExternalProviderDoesNotFabricateSource(t *testing.T) {
	store := plannerStore(t, "local", "external")
	defer store.Close()
	repo := "local/external"
	run := plannerSymbol("server.lua", "run", 1)
	if err := replacePlannerIndex(store, "local", "external", repo, []parser.Symbol{run}); err != nil {
		t.Fatal(err)
	}
	repoID, err := store.GetRepoID(repo)
	if err != nil {
		t.Fatal(err)
	}
	call := semantic.Entity{ID: "external-call", Analyzer: semantic.AnalyzerFramework, Repo: repo, File: run.File, SymbolID: run.ID, Kind: framework.KindAPICall, Name: "AddItem", Framework: "ox_inventory", Line: 1, Metadata: map[string]any{"source_resource": "app", "target_resource": "ox_inventory", "provider_status": framework.ProviderStatusExternal, "provider_verified": false}}
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerFramework, semantic.Result{Entities: []semantic.Entity{call}}); err != nil {
		t.Fatal(err)
	}
	result, err := New(store).Plan(context.Background(), Request{Repo: repo, Task: "fix AddItem"})
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range append(append([]Candidate{}, result.Primary...), result.Supporting...) {
		if candidate.File != "server.lua" {
			t.Fatalf("external provider fabricated a source candidate: %#v", candidate)
		}
	}
}

func TestPlannerFiveMEventFlowPreservesSideAndResources(t *testing.T) {
	store := plannerStore(t, "local", "events")
	defer store.Close()
	repo := "local/events"
	triggerSymbol := plannerSymbol("resources/[app]/caller/client.lua", "send", 1)
	handlerSymbol := plannerSymbol("resources/[core]/core/server.lua", "handle", 1)
	registrationSymbol := plannerSymbol("resources/[core]/core/server.lua", "register", 1)
	if err := replacePlannerIndex(store, "local", "events", repo, []parser.Symbol{triggerSymbol, handlerSymbol, registrationSymbol}); err != nil {
		t.Fatal(err)
	}
	repoID, err := store.GetRepoID(repo)
	if err != nil {
		t.Fatal(err)
	}
	trigger := semantic.Entity{ID: "event-trigger", Analyzer: semantic.AnalyzerFiveM, Repo: repo, File: triggerSymbol.File, SymbolID: triggerSymbol.ID, Kind: fivem.KindEventTrigger, Name: "avenlo:start", Side: "client", Line: 1, Metadata: map[string]any{"source_resource": "caller", "source_resource_path": "resources/[app]/caller", "operation": "TriggerServerEvent"}}
	handler := semantic.Entity{ID: "event-handler", Analyzer: semantic.AnalyzerFiveM, Repo: repo, File: handlerSymbol.File, SymbolID: handlerSymbol.ID, Kind: fivem.KindEventHandler, Name: "avenlo:start", Side: "server", Line: 1, Metadata: map[string]any{"source_resource": "core", "source_resource_path": "resources/[core]/core"}}
	registration := semantic.Entity{ID: "event-registration", Analyzer: semantic.AnalyzerFiveM, Repo: repo, File: registrationSymbol.File, SymbolID: registrationSymbol.ID, Kind: fivem.KindEventRegistration, Name: "avenlo:start", Side: "server", Line: 1, Metadata: map[string]any{"source_resource": "core", "source_resource_path": "resources/[core]/core"}}
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerFiveM, semantic.Result{Entities: []semantic.Entity{trigger, handler, registration}}); err != nil {
		t.Fatal(err)
	}
	workspaceTrigger := plannerRelationship(repo, trigger, handler, "cross_resource_event")
	workspaceTrigger.Analyzer = semantic.AnalyzerFiveMWorkspace
	workspaceHandler := plannerRelationship(repo, handler, registration, "cross_resource_event")
	workspaceHandler.Analyzer = semantic.AnalyzerFiveMWorkspace
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerFiveMWorkspace, semantic.Result{Relationships: []semantic.Relationship{workspaceTrigger, workspaceHandler}}); err != nil {
		t.Fatal(err)
	}
	result, err := New(store).Plan(context.Background(), Request{Repo: repo, Task: "trace avenlo:start"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Seeds) < 3 || candidateFileMissing(result, triggerSymbol.File) || candidateFileMissing(result, handlerSymbol.File) {
		t.Fatalf("event flow candidates missing: %#v", result)
	}
	for _, candidate := range append(append(append([]Candidate{}, result.Primary...), result.Supporting...), result.Peripheral...) {
		if candidate.Name == "avenlo:start" && candidate.Side == "" {
			t.Fatalf("event side was lost: %#v", candidate)
		}
	}
	if !planHasReason(result, "event_peer") {
		t.Fatalf("event semantic family did not traverse relationships: %#v", result)
	}
}

func TestPlannerRealGenericAnalyzersTreatUsageAsCallerContext(t *testing.T) {
	tests := []struct {
		name, language, file, target string
		source                       string
	}{
		{name: "go", language: "go", file: "main.go", target: "SaveUser", source: "package app\nfunc SaveUser() {}\nfunc HandlerA() { SaveUser() }\nfunc HandlerB() { SaveUser() }\n"},
		{name: "typescript", language: "typescript", file: "main.ts", target: "saveUser", source: "export function saveUser() {}\nfunction handlerA() { saveUser() }\nfunction handlerB() { saveUser() }\nfunction handlerRef() { consume(saveUser) }\n"},
		{name: "lua", language: "lua", file: "main.lua", target: "SaveUser", source: "function SaveUser() end\nfunction HandlerA() SaveUser() end\nfunction HandlerB() SaveUser() end\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := plannerStore(t, "local", "real-"+test.name)
			defer store.Close()
			repo := "local/real-" + test.name
			symbols := persistGenericSources(t, store, repo, map[string]string{test.file: test.source}, map[string]string{test.file: test.language})
			result, err := New(store).Plan(context.Background(), Request{Repo: repo, Task: "fix " + test.target})
			if err != nil {
				t.Fatal(err)
			}
			if result.TaskClass != "localized_change" || len(result.Ambiguities) != 0 || len(result.Seeds) != 1 {
				t.Fatalf("usage sites inflated declaration ambiguity: %#v", result)
			}
			for _, caller := range []string{"HandlerA", "HandlerB", "handlerA", "handlerB"} {
				if symbol := symbolNamed(symbols, caller); symbol != nil && !planContainsSymbolReason(result, symbol.ID, "direct_caller") {
					t.Fatalf("caller %s was not normalized to source_symbol_id: %#v", caller, result)
				}
			}
			if referenceOwner := symbolNamed(symbols, "handlerRef"); referenceOwner != nil && !planContainsSymbolReason(result, referenceOwner.ID, "direct_reference") {
				t.Fatalf("reference site inflated ambiguity or lost its source owner: %#v", result)
			}
		})
	}
}

func TestPlannerHighVolumeUsageTruncationDoesNotCreateDeclarationAmbiguity(t *testing.T) {
	store := plannerStore(t, "local", "high-volume-usage")
	defer store.Close()
	repo := "local/high-volume-usage"
	var source strings.Builder
	source.WriteString("package app\nfunc SaveUser() {}\n")
	for i := 0; i < 90; i++ {
		source.WriteString(fmt.Sprintf("func Handler%03d() { SaveUser() }\n", i))
	}
	persistGenericSources(t, store, repo, map[string]string{"main.go": source.String()}, map[string]string{"main.go": "go"})
	result, err := New(store).Plan(context.Background(), Request{Repo: repo, Task: "fix SaveUser", Debug: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskClass != "localized_change" || len(result.Ambiguities) != 0 || !planContainsName(result, "SaveUser") || result.Debug == nil || result.Debug.SemanticMatchesConsidered == 0 {
		t.Fatalf("usage truncation became declaration ambiguity: %#v", result)
	}
	if candidateCount(result) <= 1 || !result.Truncated {
		t.Fatalf("bounded caller context was not preserved truthfully: %#v", result)
	}
}

func TestPlannerHighVolumeReferenceTruncationDoesNotCreateDeclarationAmbiguity(t *testing.T) {
	store := plannerStore(t, "local", "high-volume-reference")
	defer store.Close()
	repo := "local/high-volume-reference"
	var source strings.Builder
	source.WriteString("package app\nfunc SaveUser() {}\nfunc consume(value any) {}\n")
	for i := 0; i < 90; i++ {
		source.WriteString(fmt.Sprintf("func Reference%03d() { consume(SaveUser) }\n", i))
	}
	persistGenericSources(t, store, repo, map[string]string{"main.go": source.String()}, map[string]string{"main.go": "go"})
	result, err := New(store).Plan(context.Background(), Request{Repo: repo, Task: "fix SaveUser", Debug: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskClass != "localized_change" || len(result.Ambiguities) != 0 || !planContainsName(result, "SaveUser") {
		t.Fatalf("reference truncation became declaration ambiguity: %#v", result)
	}
}

func TestPlannerTrueDeclarationTruncationRemainsAmbiguous(t *testing.T) {
	store := plannerStore(t, "local", "declaration-truncation")
	defer store.Close()
	repo := "local/declaration-truncation"
	symbols := make([]parser.Symbol, 0, DefaultMaxExactAnchors+5)
	for i := 0; i < DefaultMaxExactAnchors+5; i++ {
		symbols = append(symbols, plannerSymbol(fmt.Sprintf("file-%03d.go", i), "init", 1))
	}
	if err := replacePlannerIndex(store, "local", "declaration-truncation", repo, symbols); err != nil {
		t.Fatal(err)
	}
	result, err := New(store).Plan(context.Background(), Request{Repo: repo, Task: "find init"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Ambiguities) == 0 || !result.Ambiguities[0].Truncated || !result.Truncated {
		t.Fatalf("true declaration truncation was hidden: %#v", result)
	}
}

func TestPlannerSemanticDeclarationTruncationIsRoleAware(t *testing.T) {
	store := plannerStore(t, "local", "semantic-declaration-truncation")
	defer store.Close()
	repo := "local/semantic-declaration-truncation"
	files := make(map[string]string, DefaultMaxExactAnchors+5)
	languages := make(map[string]string, DefaultMaxExactAnchors+5)
	entities := make([]semantic.Entity, 0, DefaultMaxExactAnchors+5)
	for i := 0; i < DefaultMaxExactAnchors+5; i++ {
		file := fmt.Sprintf("file-%03d.go", i)
		files[file] = "hash-" + file
		languages[file] = "go"
		entities = append(entities, semantic.Entity{ID: fmt.Sprintf("code-symbol-%03d", i), Analyzer: semantic.AnalyzerGenericGraph, Repo: repo, File: file, Kind: generic.KindCodeSymbol, Name: "init", Line: 1, EndLine: 1})
	}
	if err := store.ReplaceRepoIndex("local", "semantic-declaration-truncation", "local", "", files, languages, nil); err != nil {
		t.Fatal(err)
	}
	repoID, err := store.GetRepoID(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerGenericGraph, semantic.Result{Entities: entities}); err != nil {
		t.Fatal(err)
	}
	result, err := New(store).Plan(context.Background(), Request{Repo: repo, Task: "find init"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Ambiguities) == 0 || !result.Ambiguities[0].Truncated || !result.Truncated {
		t.Fatalf("semantic declaration truncation was not exposed truthfully: %#v", result)
	}
}

func TestPlannerRealFindAndFixDirectionForGoAndTypeScript(t *testing.T) {
	tests := []struct {
		name, language, file, target string
	}{
		{name: "go", language: "go", file: "main.go", target: "SaveUser"},
		{name: "typescript", language: "typescript", file: "main.ts", target: "saveUser"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := plannerStore(t, "local", "direction-"+test.name)
			defer store.Close()
			repo := "local/direction-" + test.name
			var source strings.Builder
			if test.language == "go" {
				source.WriteString("package app\nfunc SaveUser() {}\n")
			} else {
				source.WriteString("export function saveUser() {}\n")
			}
			for i := 0; i < 20; i++ {
				if test.language == "go" {
					source.WriteString(fmt.Sprintf("func Handler%02d() { SaveUser() }\n", i))
				} else {
					source.WriteString(fmt.Sprintf("function handler%02d() { saveUser() }\n", i))
				}
			}
			symbols := persistGenericSources(t, store, repo, map[string]string{test.file: source.String()}, map[string]string{test.file: test.language})
			find, err := New(store).Plan(context.Background(), Request{Repo: repo, Task: "find " + test.target})
			if err != nil {
				t.Fatal(err)
			}
			if find.TaskClass != "exact_symbol" || candidateCount(find) != 1 || len(find.Ambiguities) != 0 {
				t.Fatalf("find task expanded usage context: %#v", find)
			}
			fix, err := New(store).Plan(context.Background(), Request{Repo: repo, Task: "fix " + test.target})
			if err != nil {
				t.Fatal(err)
			}
			if fix.TaskClass != "localized_change" || candidateCount(fix) <= 1 || len(fix.Ambiguities) != 0 {
				t.Fatalf("fix task did not expand safe callers: %#v", fix)
			}
			if symbol := symbolNamed(symbols, test.target); symbol == nil || !planContainsSymbolReason(fix, symbol.ID, "direct_caller") {
				t.Fatalf("direct caller evidence missing: %#v", fix)
			}
		})
	}
}

func TestPlannerWeakAndStrongExactIntent(t *testing.T) {
	store := plannerStore(t, "local", "intent-strength")
	defer store.Close()
	repo := "local/intent-strength"
	save := plannerSymbol("save.go", "save", 1)
	if err := replacePlannerIndex(store, "local", "intent-strength", repo, []parser.Symbol{save}); err != nil {
		t.Fatal(err)
	}
	weak, err := New(store).Plan(context.Background(), Request{Repo: repo, Task: "save is broken"})
	if err != nil {
		t.Fatal(err)
	}
	if weak.TaskClass == "exact_symbol" && weak.TaskConfidence == "high" {
		t.Fatalf("plain prose received strong exact intent: %#v", weak)
	}
	quoted, err := New(store).Plan(context.Background(), Request{Repo: repo, Task: "fix `save`"})
	if err != nil {
		t.Fatal(err)
	}
	if quoted.TaskClass != "localized_change" || len(quoted.Seeds) != 1 || quoted.Seeds[0].Match != "exact_symbol_match" {
		t.Fatalf("quoted lowercase identifier lost strong evidence: %#v", quoted)
	}
	bare, err := New(store).Plan(context.Background(), Request{Repo: repo, Task: "SaveUser"})
	if err != nil {
		t.Fatal(err)
	}
	if bare.TaskClass != "broad_unknown" || bare.TaskConfidence != "low" {
		t.Fatalf("nonexistent strong identifier was misclassified: %#v", bare)
	}
	fix, err := New(store).Plan(context.Background(), Request{Repo: repo, Task: "fix save"})
	if err != nil {
		t.Fatal(err)
	}
	if fix.TaskClass != "localized_change" || fix.TaskConfidence != "medium" {
		t.Fatalf("change intent was downgraded unnecessarily: %#v", fix)
	}
}

func TestPlannerFiveMNUICallbackAndCommandRoles(t *testing.T) {
	store := plannerStore(t, "local", "fivem-flow-roles")
	defer store.Close()
	repo := "local/fivem-flow-roles"
	manifest := "fx_version 'cerulean'\nclient_script 'client.lua'\n"
	source := "RegisterNUICallback('purchaseItem', function(data, cb) cb({}) end)\nRegisterCommand('admincar', function() end, false)\n"
	files := map[string]string{"fxmanifest.lua": manifest, "client.lua": source}
	languages := map[string]string{"fxmanifest.lua": "lua", "client.lua": "lua"}
	contents := make(map[string][]byte, len(files))
	symbolsByFile := make(map[string][]parser.Symbol, len(files))
	var symbols []parser.Symbol
	hashes := make(map[string]string, len(files))
	for file, content := range files {
		contents[file] = []byte(content)
		hashes[file] = "hash-" + file
		parsed, parseErr := parser.ParseFile(contents[file], file, "lua")
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		symbolsByFile[file] = parsed
		symbols = append(symbols, parsed...)
	}
	if err := store.ReplaceRepoIndex("local", "fivem-flow-roles", "local", "", hashes, languages, symbols); err != nil {
		t.Fatal(err)
	}
	result, err := fivem.NewAnalyzer().AnalyzeRepository(context.Background(), semantic.RepositoryInput{Repo: repo, Resource: "flow_roles", Files: contents, Languages: languages, Symbols: symbolsByFile})
	if err != nil {
		t.Fatal(err)
	}
	repoID, err := store.GetRepoID(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerFiveM, result); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ task, name, kind string }{{"trace purchaseItem", "purchaseItem", fivem.KindNUICallback}, {"find admincar", "admincar", fivem.KindCommandRegistration}} {
		plan, planErr := New(store).Plan(context.Background(), Request{Repo: repo, Task: test.task})
		if planErr != nil || candidateCount(plan) == 0 || !planContainsSemanticKind(plan, test.name, test.kind) {
			t.Fatalf("%s role did not produce a source candidate: %#v %v", test.kind, plan, planErr)
		}
	}
}

func TestPlannerFiveMSemanticFamiliesAreMechanismSpecific(t *testing.T) {
	store := plannerStore(t, "local", "fivem-family-mechanisms")
	defer store.Close()
	repo := "local/fivem-family-mechanisms"
	first := plannerSymbol("first.lua", "first", 1)
	second := plannerSymbol("second.lua", "second", 1)
	third := plannerSymbol("third.lua", "third", 1)
	if err := replacePlannerIndex(store, "local", "fivem-family-mechanisms", repo, []parser.Symbol{first, second, third}); err != nil {
		t.Fatal(err)
	}
	repoID, err := store.GetRepoID(repo)
	if err != nil {
		t.Fatal(err)
	}
	entities := []semantic.Entity{
		{ID: "event-get-player", Analyzer: semantic.AnalyzerFiveM, Repo: repo, File: first.File, SymbolID: first.ID, Kind: fivem.KindEventHandler, Name: "GetPlayer", Side: "server", Line: 1},
		{ID: "export-get-player", Analyzer: semantic.AnalyzerFiveM, Repo: repo, File: second.File, SymbolID: second.ID, Kind: fivem.KindExportDefinition, Name: "GetPlayer", Side: "server", Line: 1},
		{ID: "callback-get-player", Analyzer: semantic.AnalyzerFiveM, Repo: repo, File: third.File, SymbolID: third.ID, Kind: fivem.KindCallbackRegistration, Name: "GetPlayer", Side: "server", Line: 1},
	}
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerFiveM, semantic.Result{Entities: entities}); err != nil {
		t.Fatal(err)
	}
	if semanticFamilyKey(entities[0], "GetPlayer") == semanticFamilyKey(entities[1], "GetPlayer") || semanticFamilyKey(entities[0], "GetPlayer") == semanticFamilyKey(entities[2], "GetPlayer") || semanticFamilyKey(entities[1], "GetPlayer") == semanticFamilyKey(entities[2], "GetPlayer") {
		t.Fatal("different FiveM mechanisms were merged into one semantic family")
	}
	plan, err := New(store).Plan(context.Background(), Request{Repo: repo, Task: "trace GetPlayer"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Ambiguities) != 0 || candidateCount(plan) != 3 {
		t.Fatalf("legitimate flow-family multiplicity was treated as declaration ambiguity: %#v", plan)
	}
}

func TestPlannerExactSemanticQueryIgnoresSubstringCollisions(t *testing.T) {
	store := plannerStore(t, "local", "semantic-exact")
	defer store.Close()
	repo := "local/semantic-exact"
	target := plannerSymbol("z.lua", "handle", 1)
	if err := replacePlannerIndex(store, "local", "semantic-exact", repo, []parser.Symbol{target}); err != nil {
		t.Fatal(err)
	}
	repoID, err := store.GetRepoID(repo)
	if err != nil {
		t.Fatal(err)
	}
	entities := make([]semantic.Entity, 0, 82)
	for i := 0; i < 80; i++ {
		entities = append(entities, semantic.Entity{ID: fmt.Sprintf("substring-%03d", i), Analyzer: semantic.AnalyzerFiveM, Repo: repo, File: fmt.Sprintf("a-%03d.lua", i), Kind: fivem.KindEventHandler, Name: fmt.Sprintf("GetPlayerDebug%03d", i), Line: 1})
	}
	exact := semantic.Entity{ID: "exact-get-player", Analyzer: semantic.AnalyzerFiveM, Repo: repo, File: target.File, SymbolID: target.ID, Kind: fivem.KindEventHandler, Name: "GetPlayer", Line: 1}
	operation := semantic.Entity{ID: "exact-operation", Analyzer: semantic.AnalyzerFramework, Repo: repo, File: target.File, SymbolID: target.ID, Kind: framework.KindOperation, Name: "operation", Line: 1, Metadata: map[string]any{"operation": "inventory_add_item"}}
	entities = append(entities, exact)
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerFiveM, semantic.Result{Entities: entities}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerFramework, semantic.Result{Entities: []semantic.Entity{operation}}); err != nil {
		t.Fatal(err)
	}
	for _, task := range []string{"trace GetPlayer", "inventory_add_item"} {
		result, planErr := New(store).Plan(context.Background(), Request{Repo: repo, Task: task})
		if planErr != nil || candidateCount(result) != 1 || result.Primary[0].SymbolID != target.ID {
			t.Fatalf("exact semantic lookup failed for %q: %#v %v", task, result, planErr)
		}
	}
}

func TestPlannerCallbackAndExportFamiliesExpand(t *testing.T) {
	tests := []struct {
		name, semanticName, relationship, reason string
		fromKind, toKind                         string
	}{
		{name: "callback", semanticName: "avenlo:get", fromKind: fivem.KindCallbackCall, toKind: fivem.KindCallbackRegistration, relationship: "cross_resource_callback", reason: "callback_peer"},
		{name: "export", semanticName: "GetPlayer", fromKind: fivem.KindExportCall, toKind: fivem.KindExportDefinition, relationship: "cross_resource_export", reason: "export_provider"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := plannerStore(t, "local", "family-"+test.name)
			defer store.Close()
			repo := "local/family-" + test.name
			caller := plannerSymbol("caller.lua", "caller", 1)
			provider := plannerSymbol("provider.lua", "provider", 1)
			if err := replacePlannerIndex(store, "local", "family-"+test.name, repo, []parser.Symbol{caller, provider}); err != nil {
				t.Fatal(err)
			}
			repoID, err := store.GetRepoID(repo)
			if err != nil {
				t.Fatal(err)
			}
			from := semantic.Entity{ID: test.name + "-from", Analyzer: semantic.AnalyzerFiveM, Repo: repo, File: caller.File, SymbolID: caller.ID, Kind: test.fromKind, Name: test.semanticName, Line: 1}
			to := semantic.Entity{ID: test.name + "-to", Analyzer: semantic.AnalyzerFiveM, Repo: repo, File: provider.File, SymbolID: provider.ID, Kind: test.toKind, Name: test.semanticName, Line: 1}
			if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerFiveM, semantic.Result{Entities: []semantic.Entity{from, to}}); err != nil {
				t.Fatal(err)
			}
			rel := plannerRelationship(repo, from, to, test.relationship)
			rel.Analyzer = semantic.AnalyzerFiveMWorkspace
			if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerFiveMWorkspace, semantic.Result{Relationships: []semantic.Relationship{rel}}); err != nil {
				t.Fatal(err)
			}
			result, planErr := New(store).Plan(context.Background(), Request{Repo: repo, Task: "trace " + test.semanticName})
			if planErr != nil || len(result.Ambiguities) != 0 || !planHasReason(result, test.reason) {
				t.Fatalf("semantic family was blocked: %#v %v", result, planErr)
			}
		})
	}
}

func TestPlannerQueryBudgetsPrioritizeHighSignalAndReserveFallback(t *testing.T) {
	store := plannerStore(t, "local", "query-priority")
	defer store.Close()
	repo := "local/query-priority"
	target := plannerSymbol("target.go", "zTarget", 1)
	if err := replacePlannerIndex(store, "local", "query-priority", repo, []parser.Symbol{target}); err != nil {
		t.Fatal(err)
	}
	terms := make([]string, 0, 28)
	for i := 0; i < 27; i++ {
		terms = append(terms, "word"+string(rune('a'+i/26))+string(rune('a'+i%26)))
	}
	terms = append(terms, "zTarget")
	result, err := New(store).Plan(context.Background(), Request{Repo: repo, Task: "review " + strings.Join(terms, " ") + " architecture", Debug: true})
	if err != nil {
		t.Fatal(err)
	}
	if !planContainsSymbolReason(result, target.ID, "exact_symbol_match") {
		t.Fatalf("weak terms consumed the exact query budget before high signal: %#v", result)
	}
	if result.Debug == nil || result.Debug.FallbackQueries == 0 || result.Debug.ExactQueries > DefaultMaxExactQueries || result.Debug.SemanticQueries > DefaultMaxSemanticQueries || result.Debug.FallbackQueries > DefaultMaxFallbackQueries {
		t.Fatalf("query budgets were not independently bounded/reserved: %#v", result.Debug)
	}
}

func TestSemanticSeedRolesAndContextSymbolBridge(t *testing.T) {
	usage := semantic.Entity{Analyzer: semantic.AnalyzerGenericGraph, Kind: generic.KindReferenceSite, Metadata: map[string]any{"source_symbol_id": "caller-id"}}
	if semanticSeedRole(usage) != roleUsage || contextSymbolID(usage) != "caller-id" {
		t.Fatalf("generic usage role/context bridge failed: %#v", usage)
	}
	if semanticSeedRole(semantic.Entity{Analyzer: semantic.AnalyzerGenericGraph, Kind: generic.KindCodeSymbol}) != roleDeclaration {
		t.Fatal("generic code_symbol must remain a declaration")
	}
	if semanticSeedRole(semantic.Entity{Analyzer: semantic.AnalyzerFramework, Kind: framework.KindOperation}) != roleOperation {
		t.Fatal("framework operations must be occurrence semantics")
	}
}

func TestPlannerBoundsAndRepositoryIsolation(t *testing.T) {
	store := plannerStore(t, "local", "many")
	defer store.Close()
	for _, repoName := range []string{"many", "other"} {
		var symbols []parser.Symbol
		for i := 0; i < 130; i++ {
			symbols = append(symbols, plannerSymbol(fmt.Sprintf("%s-%03d.go", repoName, i), "Save", 1))
		}
		if err := replacePlannerIndex(store, "local", repoName, "local/"+repoName, symbols); err != nil {
			t.Fatal(err)
		}
		repoID, err := store.GetRepoID("local/" + repoName)
		if err != nil {
			t.Fatal(err)
		}
		entities := make([]semantic.Entity, 0, len(symbols))
		for _, symbol := range symbols {
			entities = append(entities, plannerCodeSymbol("local/"+repoName, symbol))
		}
		if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerGenericGraph, semantic.Result{Entities: entities}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := New(store).Plan(context.Background(), Request{Repo: "local/many", Task: "find Save", MaxCandidates: 7})
	if err != nil {
		t.Fatal(err)
	}
	if candidateCount(first) != 7 || !first.Truncated {
		t.Fatalf("planner bounds not truthful: %#v", first)
	}
	second, err := New(store).Plan(context.Background(), Request{Repo: "local/other", Task: "find Save", MaxCandidates: 7})
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range append(append([]Candidate{}, second.Primary...), second.Supporting...) {
		if !strings.HasPrefix(candidate.File, "other-") {
			t.Fatalf("cross-repository candidate leaked: %#v", candidate)
		}
	}
}

func TestPlannerPreservesEvidenceAcrossAnalyzersAndAuthority(t *testing.T) {
	store := plannerStore(t, "local", "evidence")
	defer store.Close()
	repo := "local/evidence"
	symbol := plannerSymbol("server.lua", "run", 1)
	if err := replacePlannerIndex(store, "local", "evidence", repo, []parser.Symbol{symbol}); err != nil {
		t.Fatal(err)
	}
	repoID, err := store.GetRepoID(repo)
	if err != nil {
		t.Fatal(err)
	}
	base := plannerCodeSymbol(repo, symbol)
	event := semantic.Entity{ID: "event-run", Analyzer: semantic.AnalyzerFiveM, Repo: repo, File: symbol.File, SymbolID: symbol.ID, Kind: fivem.KindEventHandler, Name: "run", Side: "server", Line: 1, Metadata: map[string]any{"source_resource": "app"}}
	verified := semantic.Entity{ID: "verified-run", Analyzer: semantic.AnalyzerFramework, Repo: repo, File: symbol.File, SymbolID: symbol.ID, Kind: framework.KindOperation, Name: "run", Framework: "qbx", Side: "server", Line: 1, Metadata: map[string]any{"operation": "run", "provider_status": framework.ProviderStatusLocalVerified, "provider_verified": true}}
	external := semantic.Entity{ID: "external-run", Analyzer: semantic.AnalyzerFramework, Repo: repo, File: symbol.File, SymbolID: symbol.ID, Kind: framework.KindAPICall, Name: "run", Framework: "ox_inventory", Side: "server", Line: 1, Metadata: map[string]any{"provider_status": framework.ProviderStatusExternal, "provider_verified": false}}
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerGenericGraph, semantic.Result{Entities: []semantic.Entity{base}}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerFiveM, semantic.Result{Entities: []semantic.Entity{event}}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerFramework, semantic.Result{Entities: []semantic.Entity{verified, external}}); err != nil {
		t.Fatal(err)
	}
	result, err := New(store).Plan(context.Background(), Request{Repo: repo, Task: "run"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Seeds) != 1 || candidateCount(result) != 1 {
		t.Fatalf("same source anchor was not normalized: %#v", result)
	}
	candidate := result.Primary[0]
	if candidate.SymbolID != symbol.ID || candidate.Authority != "mixed" || len(candidate.Authorities) != 2 {
		t.Fatalf("mixed authority or symbol bridge was lost: %#v", candidate)
	}
	if len(candidate.Frameworks) != 2 || !containsString(candidate.ReasonCodes, "weak_exact_match") || !containsString(candidate.ReasonCodes, "framework_operation_match") {
		t.Fatalf("distinct analyzer evidence was lost: %#v", candidate)
	}
}

func TestPlannerUniqueAnchorClassificationAndBroadMarker(t *testing.T) {
	store := plannerStore(t, "local", "classification")
	defer store.Close()
	repo := "local/classification"
	load := plannerSymbol("player.go", "LoadCharacter", 1)
	dependency := plannerSymbol("player.go", "readInventory", 8)
	if err := replacePlannerIndex(store, "local", "classification", repo, []parser.Symbol{load, dependency}); err != nil {
		t.Fatal(err)
	}
	repoID, err := store.GetRepoID(repo)
	if err != nil {
		t.Fatal(err)
	}
	loadEntity := plannerCodeSymbol(repo, load)
	loadSemantic := semantic.Entity{ID: "load-event", Analyzer: semantic.AnalyzerFiveM, Repo: repo, File: load.File, SymbolID: load.ID, Kind: fivem.KindEventHandler, Name: "LoadCharacter", Side: "server", Line: 1}
	dependencyEntity := plannerCodeSymbol(repo, dependency)
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerGenericGraph, semantic.Result{Entities: []semantic.Entity{loadEntity, dependencyEntity}, Relationships: []semantic.Relationship{plannerRelationship(repo, loadEntity, dependencyEntity, generic.RelationshipCalls)}}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerFiveM, semantic.Result{Entities: []semantic.Entity{loadSemantic}}); err != nil {
		t.Fatal(err)
	}
	narrow, err := New(store).Plan(context.Background(), Request{Repo: repo, Task: "LoadCharacter"})
	if err != nil {
		t.Fatal(err)
	}
	if narrow.TaskClass != "exact_symbol" || narrow.TaskConfidence != "high" || len(narrow.Seeds) != 1 {
		t.Fatalf("duplicate analyzer rows changed exact classification: %#v", narrow)
	}
	broad, err := New(store).Plan(context.Background(), Request{Repo: repo, Task: "review LoadCharacter architecture"})
	if err != nil {
		t.Fatal(err)
	}
	if broad.TaskClass != "broad_unknown" || len(broad.Supporting) == 0 {
		t.Fatalf("broad marker was narrowed or lost representative context: %#v", broad)
	}
}

func TestPlannerAmbiguousFixUsesGlobalBudgets(t *testing.T) {
	store := plannerStore(t, "local", "ambiguous-scale")
	defer store.Close()
	repo := "local/ambiguous-scale"
	symbols := make([]parser.Symbol, 0, 150)
	entities := make([]semantic.Entity, 0, 150)
	for i := 0; i < 150; i++ {
		symbol := plannerSymbol(fmt.Sprintf("file-%03d.go", i), "init", 1)
		symbols = append(symbols, symbol)
		entities = append(entities, plannerCodeSymbol(repo, symbol))
	}
	if err := replacePlannerIndex(store, "local", "ambiguous-scale", repo, symbols); err != nil {
		t.Fatal(err)
	}
	repoID, err := store.GetRepoID(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerGenericGraph, semantic.Result{Entities: entities}); err != nil {
		t.Fatal(err)
	}
	result, err := New(store).Plan(context.Background(), Request{Repo: repo, Task: "fix init", Debug: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskClass != "broad_unknown" || result.TaskConfidence != "low" || len(result.Seeds) > DefaultMaxSeeds || result.Debug == nil || result.Debug.TraceQueries != 0 || !result.Truncated || len(result.Ambiguities) == 0 {
		t.Fatalf("ambiguous fix exceeded bounded expansion: %#v", result)
	}
}

func TestPlannerBroadFallbackEntryPointsAreWeakAndBounded(t *testing.T) {
	store := plannerStore(t, "local", "broad")
	defer store.Close()
	repo := "local/broad"
	symbols := []parser.Symbol{plannerSymbol("character.go", "LoadCharacter", 1), plannerSymbol("repository.go", "CharacterRepository", 1)}
	if err := replacePlannerIndex(store, "local", "broad", repo, symbols); err != nil {
		t.Fatal(err)
	}
	result, err := New(store).Plan(context.Background(), Request{Repo: repo, Task: "review character persistence architecture", Debug: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskClass != "broad_unknown" || candidateCount(result) == 0 {
		t.Fatalf("broad entry points were not returned: %#v", result)
	}
	for _, candidate := range append(append(append([]Candidate{}, result.Primary...), result.Supporting...), result.Peripheral...) {
		if containsString(candidate.ReasonCodes, "lexical_fallback") && candidate.Tier == "primary" {
			t.Fatalf("fallback was treated as an exact primary: %#v", candidate)
		}
	}
}

func TestPlannerIncludeImpactAddsIncomingEvidence(t *testing.T) {
	store := plannerStore(t, "local", "impact")
	defer store.Close()
	repo := "local/impact"
	a := plannerSymbol("a.go", "CallA", 1)
	b := plannerSymbol("b.go", "HandleB", 1)
	c := plannerSymbol("c.go", "CallC", 1)
	if err := replacePlannerIndex(store, "local", "impact", repo, []parser.Symbol{a, b, c}); err != nil {
		t.Fatal(err)
	}
	repoID, err := store.GetRepoID(repo)
	if err != nil {
		t.Fatal(err)
	}
	aEntity, bEntity, cEntity := plannerCodeSymbol(repo, a), plannerCodeSymbol(repo, b), plannerCodeSymbol(repo, c)
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerGenericGraph, semantic.Result{Entities: []semantic.Entity{aEntity, bEntity, cEntity}, Relationships: []semantic.Relationship{plannerRelationship(repo, aEntity, bEntity, generic.RelationshipCalls), plannerRelationship(repo, bEntity, cEntity, generic.RelationshipCalls)}}); err != nil {
		t.Fatal(err)
	}
	without, err := New(store).Plan(context.Background(), Request{Repo: repo, Task: "what does HandleB call"})
	if err != nil {
		t.Fatal(err)
	}
	if candidateFileMissing(without, c.File) || !candidateFileMissing(without, a.File) {
		t.Fatalf("outgoing-only plan included the wrong direction: %#v", without)
	}
	with, err := New(store).Plan(context.Background(), Request{Repo: repo, Task: "what does HandleB call", IncludeImpact: true})
	if err != nil {
		t.Fatal(err)
	}
	if candidateFileMissing(with, a.File) || !containsCandidate(with.Supporting, a.ID, "impact_direct") {
		t.Fatalf("incoming impact was not included: %#v", with)
	}
}

func TestPlannerEmptyIndexAndCancellationAreTruthful(t *testing.T) {
	store := plannerStore(t, "local", "empty")
	defer store.Close()
	if err := replacePlannerIndex(store, "local", "empty", "local/empty", nil); err != nil {
		t.Fatal(err)
	}
	result, err := New(store).Plan(context.Background(), Request{Repo: "local/empty", Task: "review architecture"})
	if err != nil {
		t.Fatal(err)
	}
	if result.IndexState != "unknown" || !containsString(result.Diagnostics, "empty_index") || candidateCount(result) != 0 {
		t.Fatalf("empty index was reported as healthy/populated: %#v", result)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = New(store).Plan(canceled, Request{Repo: "local/empty", Task: "review architecture"})
	if err != context.Canceled {
		t.Fatalf("cancellation was swallowed: %v", err)
	}
}

func TestPlannerFocusFileUsesBoundedIndexedExistence(t *testing.T) {
	store := plannerStore(t, "local", "large-files")
	defer store.Close()
	repo := "local/large-files"
	target := plannerSymbol("src/target.go", "Target", 1)
	files := make(map[string]string, 10001)
	languages := make(map[string]string, 10001)
	for i := 0; i < 10000; i++ {
		path := fmt.Sprintf("generated/%05d.go", i)
		files[path] = "hash-" + path
		languages[path] = "go"
	}
	files[target.File] = "hash-target"
	languages[target.File] = "go"
	if err := store.ReplaceRepoIndex("local", "large-files", "local", "", files, languages, []parser.Symbol{target}); err != nil {
		t.Fatal(err)
	}
	result, err := New(store).Plan(context.Background(), Request{Repo: repo, Task: "find Target", FocusFile: target.File})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Primary) != 1 || result.Primary[0].File != target.File {
		t.Fatalf("bounded focus-file lookup failed on large index: %#v", result)
	}
}

func TestRankPolicyEvidenceSaturatesAndPreservesAuthority(t *testing.T) {
	policy := NewRankPolicy()
	intent := TaskIntent{TaskClass: "localized_change", TraceDirection: "both"}

	strong := newAccumulator("symbol:strong", "repo")
	strong.symbol = &parser.Symbol{ID: "strong", File: "target.go", Name: "Target"}
	strong.file = "target.go"
	strong.distance = 0
	budget := newPlannerBudget()
	addEvidence(strong, Evidence{Kind: "symbol", SourceID: "strong", NoteCode: "exact_symbol_match", Role: string(roleDeclaration)}, budget)
	strongScore := policy.ScoreAccumulator(strong, intent, "", "").Total

	weak := newAccumulator("symbol:weak", "repo")
	weak.symbol = &parser.Symbol{ID: "weak", File: "helper.go", Name: "Helper"}
	weak.file = "helper.go"
	weak.distance = 1
	for i := 0; i < 100; i++ {
		addEvidence(weak, Evidence{Kind: "semantic", SourceID: fmt.Sprintf("reference-%03d", i), RelationshipID: fmt.Sprintf("edge-%03d", i), Relationship: generic.RelationshipReferences, Depth: 1, NoteCode: "direct_reference", Role: string(roleUsage)}, budget)
	}
	weakScore := policy.ScoreAccumulator(weak, intent, "", "").Total
	if strongScore <= weakScore {
		t.Fatalf("weak evidence spam outranked the exact target: strong=%d weak=%d", strongScore, weakScore)
	}

	oneReference := newAccumulator("symbol:one-reference", "repo")
	oneReference.symbol = weak.symbol
	oneReference.file = weak.file
	oneReference.distance = 1
	addEvidence(oneReference, Evidence{Kind: "semantic", SourceID: "reference-one", RelationshipID: "edge-one", Relationship: generic.RelationshipReferences, Depth: 1, NoteCode: "direct_reference", Role: string(roleUsage)}, newPlannerBudget())
	oneScore := policy.ScoreAccumulator(oneReference, intent, "", "").Total
	if weakScore-oneScore > 1000 {
		t.Fatalf("repeated identical evidence did not saturate: one=%d many=%d", oneScore, weakScore)
	}

	local := newAccumulator("entity:local", "repo")
	local.symbol = strong.symbol
	local.file = strong.file
	local.distance = 1
	addEvidence(local, Evidence{Kind: "semantic", SourceID: "local-call", RelationshipID: "local-edge", Relationship: framework.RelationshipFrameworkCalls, Depth: 1, Authority: framework.ProviderStatusLocalVerified, NoteCode: "framework_provider"}, newPlannerBudget())
	external := newAccumulator("entity:external", "repo")
	external.symbol = strong.symbol
	external.file = strong.file
	external.distance = 1
	addEvidence(external, Evidence{Kind: "semantic", SourceID: "external-call", RelationshipID: "external-edge", Relationship: framework.RelationshipFrameworkCalls, Depth: 1, Authority: framework.ProviderStatusExternal, NoteCode: "framework_provider"}, newPlannerBudget())
	if policy.ScoreAccumulator(local, intent, "", "").Total <= policy.ScoreAccumulator(external, intent, "", "").Total {
		t.Fatal("local verified evidence did not outrank equivalent external evidence")
	}
}

func TestRankPolicyDirectionDistanceAndFocus(t *testing.T) {
	policy := NewRankPolicy()
	base := func(id, reason string, depth int) *candidateAccumulator {
		a := newAccumulator("symbol:"+id, "repo")
		a.symbol = &parser.Symbol{ID: id, File: id + ".go", Name: id}
		a.file = id + ".go"
		a.distance = depth
		addEvidence(a, Evidence{Kind: "semantic", SourceID: id, RelationshipID: "edge-" + id, Relationship: generic.RelationshipCalls, Depth: depth, NoteCode: reason}, newPlannerBudget())
		return a
	}
	incoming := TaskIntent{TaskClass: "relationship_trace", TraceDirection: "incoming"}
	outgoing := TaskIntent{TaskClass: "relationship_trace", TraceDirection: "outgoing"}
	caller := base("caller", "direct_caller", 1)
	callee := base("callee", "direct_callee", 1)
	if policy.ScoreAccumulator(caller, incoming, "", "").Total <= policy.ScoreAccumulator(callee, incoming, "", "").Total {
		t.Fatal("incoming trace did not prefer caller")
	}
	if policy.ScoreAccumulator(callee, outgoing, "", "").Total <= policy.ScoreAccumulator(caller, outgoing, "", "").Total {
		t.Fatal("outgoing trace did not prefer callee")
	}
	twoHop := base("two-hop", "direct_callee", 2)
	if policy.ScoreAccumulator(callee, outgoing, "", "").Total <= policy.ScoreAccumulator(twoHop, outgoing, "", "").Total {
		t.Fatal("distance-2 evidence outranked direct evidence")
	}
	focused := base("focused", "direct_reference", 1)
	addEvidence(focused, Evidence{Kind: "focus", SourceID: "focused", NoteCode: "explicit_focus"}, newPlannerBudget())
	if policy.ScoreAccumulator(focused, outgoing, "", "").Total <= policy.ScoreAccumulator(callee, outgoing, "", "").Total {
		t.Fatal("explicit focus did not dominate inferred relationship context")
	}
}

func TestPlannerRanksHighValueEdgeBeforeLateReferences(t *testing.T) {
	store := plannerStore(t, "local", "ranked-edges")
	defer store.Close()
	repo := "local/ranked-edges"
	target := plannerSymbol("target.go", "Target", 1)
	helper := plannerSymbol("helper.go", "RelevantHelper", 1)
	symbols := []parser.Symbol{target, helper}
	entities := []semantic.Entity{plannerCodeSymbol(repo, target), plannerCodeSymbol(repo, helper)}
	relationships := []semantic.Relationship{{ID: "zzzz-high-value-call", Analyzer: semantic.AnalyzerGenericGraph, Repo: repo, FromEntityID: entities[0].ID, ToEntityID: entities[1].ID, Kind: generic.RelationshipCalls, Name: helper.Name, File: target.File, Line: target.Line}}
	for i := 0; i < 120; i++ {
		ref := plannerSymbol(fmt.Sprintf("references/%03d.go", i), fmt.Sprintf("Reference%03d", i), 1)
		symbols = append(symbols, ref)
		refEntity := plannerCodeSymbol(repo, ref)
		entities = append(entities, refEntity)
		relationships = append(relationships, semantic.Relationship{ID: fmt.Sprintf("aaaa-reference-%03d", i), Analyzer: semantic.AnalyzerGenericGraph, Repo: repo, FromEntityID: entities[0].ID, ToEntityID: refEntity.ID, Kind: generic.RelationshipReferences, Name: ref.Name, File: target.File, Line: target.Line})
	}
	if err := replacePlannerIndex(store, "local", "ranked-edges", repo, symbols); err != nil {
		t.Fatal(err)
	}
	repoID, err := store.GetRepoID(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerGenericGraph, semantic.Result{Entities: entities, Relationships: relationships}); err != nil {
		t.Fatal(err)
	}
	plan, err := New(store).Plan(context.Background(), Request{Repo: repo, Task: "fix Target", Debug: true})
	if err != nil {
		t.Fatal(err)
	}
	if !planContainsSymbolReason(plan, helper.ID, "direct_callee") {
		t.Fatalf("late high-value call was hidden by references: %#v", plan)
	}
}

func TestPlannerRankingDebugIsBoundedAndDoesNotChangeResults(t *testing.T) {
	store := plannerStore(t, "local", "rank-debug")
	defer store.Close()
	repo := "local/rank-debug"
	target := plannerSymbol("target.go", "Target", 1)
	caller := plannerSymbol("caller.go", "Caller", 1)
	if err := replacePlannerIndex(store, "local", "rank-debug", repo, []parser.Symbol{target, caller}); err != nil {
		t.Fatal(err)
	}
	repoID, err := store.GetRepoID(repo)
	if err != nil {
		t.Fatal(err)
	}
	targetEntity, callerEntity := plannerCodeSymbol(repo, target), plannerCodeSymbol(repo, caller)
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerGenericGraph, semantic.Result{Entities: []semantic.Entity{targetEntity, callerEntity}, Relationships: []semantic.Relationship{plannerRelationship(repo, callerEntity, targetEntity, generic.RelationshipCalls)}}); err != nil {
		t.Fatal(err)
	}
	normal, err := New(store).Plan(context.Background(), Request{Repo: repo, Task: "fix Target", MaxCandidates: 1})
	if err != nil {
		t.Fatal(err)
	}
	debug, err := New(store).Plan(context.Background(), Request{Repo: repo, Task: "fix Target", MaxCandidates: 1, Debug: true})
	if err != nil {
		t.Fatal(err)
	}
	if debug.Debug == nil || debug.Debug.RankingPolicy != rankPolicyVersion || len(debug.Debug.RankedCandidates) != 1 {
		t.Fatalf("ranking debug was missing or unbounded: %#v", debug.Debug)
	}
	if strings.Join(candidateIDs(normal), "\x00") != strings.Join(candidateIDs(debug), "\x00") || normal.Primary[0].Score != debug.Primary[0].Score {
		t.Fatalf("debug changed ranking output: normal=%#v debug=%#v", normal, debug)
	}
}

func TestBroadDiversityOnlyReordersWeakCandidates(t *testing.T) {
	candidates := make([]Candidate, 0, 12)
	strong := Candidate{ID: "strong", File: "one.go", Score: 9000, Tier: "supporting", ReasonCodes: []string{"direct_callee"}}
	candidates = append(candidates, strong)
	for i := 0; i < 11; i++ {
		file := "same.go"
		if i >= 4 {
			file = fmt.Sprintf("other-%02d.go", i)
		}
		candidates = append(candidates, Candidate{ID: fmt.Sprintf("weak-%02d", i), File: file, Score: 500, Tier: "peripheral", ReasonCodes: []string{"lexical_fallback"}})
	}
	ordered := diversityOrder(candidates, 6)
	if ordered[0].ID != strong.ID {
		t.Fatalf("diversity demoted strong evidence: %#v", ordered)
	}
	files := map[string]bool{}
	for _, candidate := range ordered {
		files[candidate.File] = true
	}
	if len(files) < 3 {
		t.Fatalf("broad weak candidates were not diversified: %#v", ordered)
	}
}

func TestBroadDiversityTruncationIsReported(t *testing.T) {
	acc := make(map[string]*candidateAccumulator)
	files := make(map[string]bool)
	work := newPlannerBudget()
	for i := 0; i < 12; i++ {
		file := fmt.Sprintf("entry-%02d.go", i)
		item := newAccumulator("file:"+file, "local/diversity-truncation")
		item.file, item.name, item.kind = file, fmt.Sprintf("Entry%02d", i), "file"
		addEvidence(item, Evidence{Kind: "fallback", SourceID: file, Strength: 100, NoteCode: "lexical_fallback"}, work)
		acc[item.key] = item
		files[file] = true
	}
	plan := finalize(Plan{}, acc, 5, "", "", files, TaskIntent{TaskClass: "broad_unknown", BroadIntent: true})
	if !plan.Truncated || candidateCount(plan) != 5 {
		t.Fatalf("diversity truncation was not reported: %#v", plan)
	}
}

func TestDiversityTreatsMixedStrongAndWeakEvidenceAsStrong(t *testing.T) {
	candidate := Candidate{ID: "mixed", ReasonCodes: []string{"direct_reference", "exact_symbol_match"}}
	if candidateWeakForDiversity(candidate) {
		t.Fatalf("mixed strong and weak evidence was classified as weak: %#v", candidate)
	}
}

func plannerStore(t *testing.T, owner, name string) *storage.IndexStore {
	t.Helper()
	store, err := storage.NewIndexStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func persistGenericSources(t *testing.T, store *storage.IndexStore, repo string, files map[string]string, languages map[string]string) []parser.Symbol {
	t.Helper()
	contents := make(map[string][]byte, len(files))
	symbolsByFile := make(map[string][]parser.Symbol, len(files))
	var symbols []parser.Symbol
	hashes := make(map[string]string, len(files))
	for file, source := range files {
		contents[file] = []byte(source)
		hashes[file] = "hash-" + file
		parsed, err := parser.ParseFile(contents[file], file, languages[file])
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		symbolsByFile[file] = parsed
		symbols = append(symbols, parsed...)
	}
	parts := strings.SplitN(repo, "/", 2)
	if err := store.ReplaceRepoIndex(parts[0], parts[1], "local", "", hashes, languages, symbols); err != nil {
		t.Fatal(err)
	}
	result, err := generic.NewAnalyzer().AnalyzeRepository(context.Background(), semantic.RepositoryInput{Repo: repo, Files: contents, Languages: languages, Symbols: symbolsByFile})
	if err != nil {
		t.Fatal(err)
	}
	repoID, err := store.GetRepoID(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerGenericGraph, result); err != nil {
		t.Fatal(err)
	}
	return symbols
}

func symbolNamed(symbols []parser.Symbol, name string) *parser.Symbol {
	for i := range symbols {
		if symbols[i].Name == name {
			return &symbols[i]
		}
	}
	return nil
}

func planContainsSymbolReason(plan Plan, symbolID, reason string) bool {
	for _, candidate := range append(append(append([]Candidate{}, plan.Primary...), plan.Supporting...), plan.Peripheral...) {
		if candidate.SymbolID == symbolID && containsString(candidate.ReasonCodes, reason) {
			return true
		}
	}
	return false
}

func planHasReason(plan Plan, reason string) bool {
	for _, candidate := range append(append(append([]Candidate{}, plan.Primary...), plan.Supporting...), plan.Peripheral...) {
		if containsString(candidate.ReasonCodes, reason) {
			return true
		}
	}
	return false
}

func planContainsName(plan Plan, name string) bool {
	for _, candidate := range append(append(append([]Candidate{}, plan.Primary...), plan.Supporting...), plan.Peripheral...) {
		if candidate.Name == name {
			return true
		}
	}
	return false
}

func planContainsSemanticKind(plan Plan, name, kind string) bool {
	for _, candidate := range append(append(append([]Candidate{}, plan.Primary...), plan.Supporting...), plan.Peripheral...) {
		if candidate.Name == name && candidate.Kind == kind {
			return true
		}
	}
	return false
}

func replacePlannerIndex(store *storage.IndexStore, owner, name, repo string, symbols []parser.Symbol) error {
	files := map[string]string{}
	languages := map[string]string{}
	for _, symbol := range symbols {
		files[symbol.File] = "hash-" + symbol.File
		languages[symbol.File] = symbol.Language
	}
	if err := store.ReplaceRepoIndex(owner, name, "local", "", files, languages, symbols); err != nil {
		return err
	}
	_, err := store.GetRepoID(repo)
	return err
}

func plannerSymbol(file, name string, line int) parser.Symbol {
	return parser.Symbol{ID: parser.MakeSymbolID(file, name, parser.KindFunction), File: file, Name: name, QualifiedName: name, Kind: parser.KindFunction, Language: "go", Line: line, EndLine: line + 2}
}

func plannerCodeSymbol(repo string, symbol parser.Symbol) semantic.Entity {
	return semantic.Entity{ID: semantic.StableID("generic_symbol", repo, symbol.ID), Analyzer: semantic.AnalyzerGenericGraph, Repo: repo, File: symbol.File, SymbolID: symbol.ID, Kind: generic.KindCodeSymbol, Name: symbol.Name, Line: symbol.Line, EndLine: symbol.EndLine, Side: "unknown"}
}

func plannerRelationship(repo string, from, to semantic.Entity, kind string) semantic.Relationship {
	return semantic.Relationship{ID: semantic.StableID("planner_relationship", repo, from.ID, to.ID, kind), Analyzer: from.Analyzer, Repo: repo, FromEntityID: from.ID, ToEntityID: to.ID, Kind: kind, Name: to.Name, File: from.File, Line: from.Line}
}

func containsCandidate(candidates []Candidate, symbolID, reason string) bool {
	for _, candidate := range candidates {
		if candidate.SymbolID == symbolID && containsString(candidate.ReasonCodes, reason) {
			return true
		}
	}
	return false
}
func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
func candidateCount(plan Plan) int {
	return len(plan.Primary) + len(plan.Supporting) + len(plan.Peripheral)
}

func candidateIDs(plan Plan) []string {
	all := append(append(append([]Candidate{}, plan.Primary...), plan.Supporting...), plan.Peripheral...)
	ids := make([]string, 0, len(all))
	for _, candidate := range all {
		ids = append(ids, candidate.ID)
	}
	return ids
}

func candidateFileMissing(plan Plan, file string) bool {
	for _, candidate := range append(append(append([]Candidate{}, plan.Primary...), plan.Supporting...), plan.Peripheral...) {
		if candidate.File == file {
			return false
		}
	}
	return true
}
