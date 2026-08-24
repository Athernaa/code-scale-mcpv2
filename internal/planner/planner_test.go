package planner

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Athernaa/code-scale-mcpv2/internal/parser"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic/fivem"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic/framework"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic/generic"
	"github.com/Athernaa/code-scale-mcpv2/internal/storage"
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
	if len(candidate.Frameworks) != 2 || !containsString(candidate.ReasonCodes, "exact_symbol_match") || !containsString(candidate.ReasonCodes, "exact_semantic_match") {
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

func plannerStore(t *testing.T, owner, name string) *storage.IndexStore {
	t.Helper()
	store, err := storage.NewIndexStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return store
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

func candidateFileMissing(plan Plan, file string) bool {
	for _, candidate := range append(append(append([]Candidate{}, plan.Primary...), plan.Supporting...), plan.Peripheral...) {
		if candidate.File == file {
			return false
		}
	}
	return true
}
