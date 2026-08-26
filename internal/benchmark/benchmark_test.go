package benchmark

import (
	"context"
	"testing"

	"github.com/Athernaa/code-scale-mcpv2/internal/contextpack"
)

func TestCorpusLoadsIndependentGroundTruthAndFixtureIndexes(t *testing.T) {
	corpus, err := LoadCorpus("../../benchmarks/corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(corpus.Tasks) != 34 || len(corpus.Repositories) != 8 {
		t.Fatalf("unexpected corpus size: tasks=%d repos=%d", len(corpus.Tasks), len(corpus.Repositories))
	}
	index, cleanup, err := BuildFixtureIndex(context.Background(), corpus, "../../benchmarks/fixtures")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	for _, repo := range corpus.Repositories {
		if index.RepoIDs[repo.Name] == 0 || len(index.Files[repo.Name]) == 0 {
			t.Fatalf("fixture repo was not indexed: %s", repo.Name)
		}
	}
}

func TestRelationshipGroundTruthRequiresDistinctEndpoints(t *testing.T) {
	from := retrievedItem{Name: "SaveUser", File: "user/service.go", SymbolID: "user/service.go::SaveUser#function"}
	to := retrievedItem{Name: "WriteUser", File: "storage/repository.go", SymbolID: "storage/repository.go::WriteUser#function"}
	required := GroundTruthItem{Kind: "relationship", From: "SaveUser", To: "WriteUser", FromFile: "user/service.go", ToFile: "storage/repository.go", Relationship: "calls"}
	if requiredFound(required, []retrievedItem{from, to}, nil) {
		t.Fatal("endpoint co-occurrence satisfied a relationship without evidence")
	}
	evidence := []RelationshipEvidence{{ID: "wrong", Kind: "references", FromName: "SaveUser", ToName: "WriteUser", FromFile: from.File, ToFile: to.File}}
	if requiredFound(required, []retrievedItem{from, to}, evidence) {
		t.Fatal("wrong relationship kind satisfied a calls requirement")
	}
	evidence[0].Kind = "calls"
	if !requiredFound(required, []retrievedItem{from, to}, evidence) {
		t.Fatal("exact relationship evidence was not accepted")
	}
	wrongEndpoint := evidence[0]
	wrongEndpoint.ToFile = "other/repository.go"
	if requiredFound(required, []retrievedItem{from, to}, []RelationshipEvidence{wrongEndpoint}) {
		t.Fatal("wrong endpoint file satisfied a relationship")
	}
	callbackRequired := GroundTruthItem{Kind: "relationship", From: "inventory:get", To: "inventory:get", FromFile: "jobs/client.lua", ToFile: "inventory/server.lua", Relationship: "cross_resource_callback"}
	callbackEvidence := RelationshipEvidence{ID: "callback", Kind: "cross_resource_event", FromName: "inventory:get", ToName: "inventory:get", FromFile: "jobs/client.lua", ToFile: "inventory/server.lua"}
	if requiredFound(callbackRequired, []retrievedItem{{Name: "inventory:get", File: "jobs/client.lua"}, {Name: "inventory:get", File: "inventory/server.lua"}}, []RelationshipEvidence{callbackEvidence}) {
		t.Fatal("cross-resource event evidence satisfied a callback requirement")
	}
	callbackEvidence.Kind = "cross_resource_callback"
	if !requiredFound(callbackRequired, []retrievedItem{{Name: "inventory:get", File: "jobs/client.lua"}, {Name: "inventory:get", File: "inventory/server.lua"}}, []RelationshipEvidence{callbackEvidence}) {
		t.Fatal("exact callback relationship evidence was not accepted")
	}
	callerSection := retrievedItem{Name: "openInventory", File: "jobs/client.lua", SymbolID: "jobs/client.lua::openInventory#function"}
	callerEdge := RelationshipEvidence{ID: "callback-with-owner", Kind: "cross_resource_callback", FromName: "inventory:get", ToName: "inventory:get", FromSymbolID: callerSection.SymbolID, FromFile: callerSection.File, ToFile: "inventory/server.lua"}
	if !requiredFound(GroundTruthItem{Kind: "symbol", Name: "inventory:get", File: "jobs/client.lua"}, []retrievedItem{callerSection, {Name: "inventory:get", File: "inventory/server.lua"}}, []RelationshipEvidence{callerEdge}) {
		t.Fatal("actual callback endpoint identity was not recovered from its selected source symbol")
	}
	if _, err := contextpack.NewTokenCounter(contextpack.TokenizerO200K); err != nil {
		t.Fatal(err)
	}
}

func TestStructuredIdentityDoesNotUseSourceSubstring(t *testing.T) {
	items := []retrievedItem{{Name: "caller", File: "caller.lua", SymbolID: "caller.lua::caller#function", Source: "-- mentions AddItem and SaveUser"}}
	if requiredFound(GroundTruthItem{Kind: "symbol", Name: "SaveUser", File: "caller.lua"}, items, nil) {
		t.Fatal("source mention fabricated a symbol identity")
	}
	if requiredFound(GroundTruthItem{Kind: "provider", Name: "AddItem", File: "caller.lua", Authority: "local_verified"}, items, nil) {
		t.Fatal("source mention fabricated a provider identity")
	}
}

func TestEmptyRetrievalPrecisionIsUndefined(t *testing.T) {
	value, defined := precision(Task{}, nil)
	if defined || value != 0 {
		t.Fatalf("empty precision was not undefined: value=%v defined=%v", value, defined)
	}
}

func TestScoreResultPreservesSuppliedContextPayload(t *testing.T) {
	counter, err := contextpack.NewTokenCounter(contextpack.TokenizerO200K)
	if err != nil {
		t.Fatal(err)
	}
	task := Task{ID: "payload", Required: []GroundTruthItem{{Kind: "symbol", Name: "Target", File: "target.go"}}}
	output := modeOutput{
		Items: []retrievedItem{
			{Key: "symbol:target", Name: "Target", File: "target.go", SymbolID: "target.go::Target#function", Source: "scoring-only source"},
			{Key: "provider:target", Name: "Target", File: "provider.go", Kind: "framework_api_provider", Authority: "local_verified", Source: "scoring-only provider metadata"},
		},
		Ranked:      []retrievedItem{{Key: "symbol:target", Name: "Target", File: "target.go", SymbolID: "target.go::Target#function"}},
		ContextText: "actual manual context payload",
	}
	result := scoreResult(task, ModeManual, 1024, 1, output, counter, counter.Count(output.ContextText), counter.Count(output.ContextText))
	if result.ContextTokens != counter.Count(output.ContextText) || result.TokenSaving != 0 {
		t.Fatalf("scoring-only items changed supplied payload accounting: got=%d saving=%v", result.ContextTokens, result.TokenSaving)
	}
}

func TestPanoramicSelfBaselinesUseExactPayloadTokens(t *testing.T) {
	report, err := Run(context.Background(), Config{
		CorpusPath: "../../benchmarks/corpus.json",
		Mode:       "all",
		Budgets:    []int{512},
		Repeat:     1,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range report.Results {
		switch result.Mode {
		case ModePanoramic:
			if result.ContextTokens != result.BaselineContextTokens || result.TokenSaving != 0 {
				t.Fatalf("panoramic self baseline mismatch for %s: context=%d baseline=%d saving=%v", result.TaskID, result.ContextTokens, result.BaselineContextTokens, result.TokenSaving)
			}
		case ModeScopedPanoramic:
			if result.ContextTokens != result.ScopedBaselineContextTokens || result.ScopedTokenSaving != 0 {
				t.Fatalf("scoped panoramic self baseline mismatch for %s: context=%d baseline=%d saving=%v", result.TaskID, result.ContextTokens, result.ScopedBaselineContextTokens, result.ScopedTokenSaving)
			}
		}
	}
	if !report.Validation.PanoramicSelfBaselineZero || !report.Validation.ScopedSelfBaselineZero || report.Aggregate.BaselineAccountingViolations != 0 {
		t.Fatalf("baseline accounting validation failed: validation=%+v aggregate=%+v", report.Validation, report.Aggregate)
	}
}

func TestManualPrimitiveAndPhase7PayloadAccounting(t *testing.T) {
	corpus, err := LoadCorpus("../../benchmarks/corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	index, cleanup, err := BuildFixtureIndex(context.Background(), corpus, "../../benchmarks/fixtures")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	var task Task
	for _, candidate := range corpus.Tasks {
		if candidate.ID == "go_save_fix" {
			task = candidate
			break
		}
	}
	counter, err := contextpack.NewTokenCounter(corpus.DefaultTokenizer)
	if err != nil {
		t.Fatal(err)
	}
	runner := &runner{index: index, corpus: corpus, counter: counter, tokenizer: corpus.DefaultTokenizer}
	broad := runner.panoramic(task)
	scoped := runner.scopedPanoramic(task)
	broadTokens, scopedTokens := counter.Count(broad.ContextText), counter.Count(scoped.ContextText)
	manual := scoreResult(task, ModeManual, 32000, 1, runner.manual(task), counter, broadTokens, scopedTokens)
	if manual.ContextTokens != counter.Count(runner.manual(task).ContextText) {
		t.Fatalf("manual accounting did not use supplied payload: %d", manual.ContextTokens)
	}
	primitiveOutput, err := runner.primitive(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	primitive := scoreResult(task, ModePrimitive, 32000, 1, primitiveOutput, counter, broadTokens, scopedTokens)
	if primitive.ContextTokens != counter.Count(primitiveOutput.ContextText) {
		t.Fatalf("primitive accounting did not use supplied payload: %d", primitive.ContextTokens)
	}
	phaseOutput, err := runner.phase7(context.Background(), task, 2048)
	if err != nil {
		t.Fatal(err)
	}
	phase := scoreResult(task, ModePhase7, 2048, 1, phaseOutput, counter, broadTokens, scopedTokens)
	if phase.ContextTokens != counter.Count(phaseOutput.ContextText) {
		t.Fatalf("Phase-7 accounting changed serialized package tokens: %d", phase.ContextTokens)
	}
}

func TestReportStatusTreatsHardFailuresAsFail(t *testing.T) {
	base := Report{Validation: Validation{ProviderFabricationZero: true, CrossRepoLeakageZero: true, SerializedBudgetZero: true, FalseSufficiencyZero: true, SupportedRecallAtLeast95: true, RuntimeErrorsZero: true, BaselineAccountingZero: true, TokenReductionAtLeast50: true}}
	if Status(base) != "PASS" {
		t.Fatalf("all release gates should pass: %s", Status(base))
	}
	base.Validation.TokenReductionAtLeast50 = false
	if Status(base) != "CONDITIONAL PASS" {
		t.Fatalf("secondary token miss should be conditional: %s", Status(base))
	}
	base.Validation.ProviderFabricationZero = false
	if Status(base) != "FAIL" {
		t.Fatalf("provider fabrication should fail release: %s", Status(base))
	}
}

func TestPhase7KeepsNormalSerializedPackageWithinSmallBudget(t *testing.T) {
	corpus, err := LoadCorpus("../../benchmarks/corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	index, cleanup, err := BuildFixtureIndex(context.Background(), corpus, "../../benchmarks/fixtures")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	var task Task
	for _, candidate := range corpus.Tasks {
		if candidate.ID == "fivem_focused_inventory" {
			task = candidate
			break
		}
	}
	runner := &runner{index: index, corpus: corpus, tokenizer: corpus.DefaultTokenizer}
	output, err := runner.phase7(context.Background(), task, 1024)
	if err != nil {
		t.Fatal(err)
	}
	counter, err := contextpack.NewTokenCounter(corpus.DefaultTokenizer)
	if err != nil {
		t.Fatal(err)
	}
	if got := counter.Count(output.ContextText); got > 1024 {
		t.Fatalf("normal Phase-7 package exceeded serialized budget: %d", got)
	}
}

func TestCallbackGroundTruthHasNoFalseSufficiencyAcrossBudgets(t *testing.T) {
	report, err := Run(context.Background(), Config{
		CorpusPath: "../../benchmarks/corpus.json",
		Mode:       string(ModePhase7),
		TaskID:     "fivem_callback_flow",
		Budgets:    []int{512, 1024, 2048, 4000, 8000, 16000, 32000},
		Repeat:     2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Aggregate.FalseSufficiencyAllBudgets != 0 || report.Aggregate.FalseSufficiencyMaxBudget != 0 {
		t.Fatalf("callback flow still has false sufficiency: all=%d max=%d", report.Aggregate.FalseSufficiencyAllBudgets, report.Aggregate.FalseSufficiencyMaxBudget)
	}
}

func TestFacadeGroundTruthHasNoFalseSufficiencyAcrossBudgets(t *testing.T) {
	report, err := Run(context.Background(), Config{
		CorpusPath: "../../benchmarks/corpus.json",
		Mode:       string(ModePhase7),
		TaskID:     "fivem_facade_chain",
		Budgets:    []int{512, 1024, 2048, 4000, 8000, 16000, 32000},
		Repeat:     2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Aggregate.FalseSufficiencyAllBudgets != 0 || report.Aggregate.FalseSufficiencyMaxBudget != 0 {
		t.Fatalf("facade chain still has false sufficiency: all=%d max=%d", report.Aggregate.FalseSufficiencyAllBudgets, report.Aggregate.FalseSufficiencyMaxBudget)
	}
}

func TestSmallBudgetTraceTasksCannotClaimMissingEvidence(t *testing.T) {
	for _, taskID := range []string{"fivem_known_framework", "fivem_vehicle_impact"} {
		report, err := Run(context.Background(), Config{
			CorpusPath: "../../benchmarks/corpus.json",
			Mode:       string(ModePhase7),
			TaskID:     taskID,
			Budgets:    []int{512, 1024},
			Repeat:     2,
		})
		if err != nil {
			t.Fatal(err)
		}
		if report.Aggregate.FalseSufficiencyAllBudgets != 0 {
			t.Fatalf("%s claimed sufficiency with missing small-budget evidence: %d", taskID, report.Aggregate.FalseSufficiencyAllBudgets)
		}
	}
}
