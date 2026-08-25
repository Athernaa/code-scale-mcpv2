package contextpack

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Athernaa/code-scale-mcpv2/internal/parser"
	"github.com/Athernaa/code-scale-mcpv2/internal/planner"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic/framework"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic/generic"
	"github.com/Athernaa/code-scale-mcpv2/internal/storage"
	"github.com/Athernaa/code-scale-mcpv2/internal/sufficiency"
	"github.com/Athernaa/code-scale-mcpv2/internal/workspace"
	workspaceindex "github.com/Athernaa/code-scale-mcpv2/internal/workspace/indexer"
)

type staticPlanner struct {
	plan planner.Plan
	err  error
}

type fixedSufficiencyEvaluator struct {
	decision sufficiency.Decision
}

func (e fixedSufficiencyEvaluator) Evaluate(sufficiency.Input) sufficiency.Decision {
	return e.decision
}

func (p staticPlanner) Plan(ctx context.Context, request planner.Request) (planner.Plan, error) {
	return p.plan, p.err
}

func TestTokenCountersAreExactDeterministicAndUnicodeSafe(t *testing.T) {
	for _, name := range []string{TokenizerO200K, TokenizerCL100K} {
		counter, err := NewTokenCounter(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, text := range []string{"", "func café😀() {}", strings.Repeat("inventory_add_item\n", 5000)} {
			first, second := counter.Count(text), counter.Count(text)
			if first != second || !counter.Exact() || counter.Name() != name {
				t.Fatalf("nondeterministic counter %s: %d %d", name, first, second)
			}
		}
	}
}

func TestAssembleEnforcesFinalSerializedBudgetAndProtectsPrimary(t *testing.T) {
	store, repo, primary, support, weak := assemblyFixture(t)
	defer store.Close()
	repoID, _ := store.GetRepoID(repo)
	if source, err := store.GetSymbolContent(repoID, primary.ID); err != nil || parser.ComputeContentHash([]byte(source)) != primary.ContentHash {
		t.Fatalf("fixture source mismatch: err=%v bytes=%d hash=%s expected=%s", err, len(source), parser.ComputeContentHash([]byte(source)), primary.ContentHash)
	}
	plan := planner.Plan{Repo: repo, TaskClass: "localized_change", TaskConfidence: "high", IndexState: "complete", Primary: []planner.Candidate{candidate(primary, "primary", 9000, "exact_symbol_match")}, Supporting: []planner.Candidate{candidate(support, "supporting", 7000, "direct_callee")}, Peripheral: []planner.Candidate{candidate(weak, "peripheral", 200, "lexical_fallback")}}
	for _, budget := range []int{512, 1000, 2000, 8000, 32000, HardMaxContextTokenBudget} {
		pkg, err := New(staticPlanner{plan: plan}, store).Assemble(context.Background(), Request{Repo: repo, Task: "fix Primary", MaxContextTokens: budget, Tokenizer: TokenizerO200K, MaxCandidates: 100})
		if err != nil {
			t.Fatalf("budget %d: %v", budget, err)
		}
		data, _ := json.Marshal(pkg)
		counter, _ := NewTokenCounter(TokenizerO200K)
		if got := counter.Count(string(data)); got > budget || got != pkg.Budget.UsedTokens {
			t.Fatalf("budget %d used=%d measured=%d", budget, pkg.Budget.UsedTokens, got)
		}
		if len(pkg.Sections) == 0 || pkg.Sections[0].SymbolID != primary.ID {
			t.Fatalf("primary was displaced at budget %d: %#v", budget, pkg)
		}
	}
}

func TestSufficiencyStopsExactFindAfterAnchor(t *testing.T) {
	store, repo, primary, support, weak := assemblyFixture(t)
	defer store.Close()
	plan := planner.Plan{Repo: repo, TaskClass: "exact_symbol", TaskConfidence: "high", IndexState: "complete", Primary: []planner.Candidate{candidate(primary, "primary", 9000, "exact_symbol_match")}, Supporting: []planner.Candidate{candidate(support, "supporting", 7000, "direct_callee")}, Peripheral: []planner.Candidate{candidate(weak, "peripheral", 100, "lexical_fallback")}}
	pkg, err := New(staticPlanner{plan: plan}, store).Assemble(context.Background(), Request{Repo: repo, Task: "find Primary", MaxContextTokens: 2000})
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Sufficiency.Status != sufficiency.StatusSufficient || pkg.StopReason != "sufficiency_satisfied" || pkg.ContextTruncated || pkg.Truncated || len(pkg.Sections) != 1 || len(pkg.Rounds) != 1 {
		t.Fatalf("exact find did not stop after anchor: %+v", pkg)
	}
}

func TestSufficiencyLocalizedChangeContinuesThroughDirectSupport(t *testing.T) {
	store := assemblyStore(t)
	defer store.Close()
	repo := "local/localized-sufficiency"
	primarySource, supportSource := "func Primary() {}\n", "func Support() {}\n"
	primary := indexedSymbol("primary.go", "Primary", primarySource)
	support := indexedSymbol("support.go", "Support", supportSource)
	writeAssemblyRepo(t, store, repo, map[string]string{"primary.go": primarySource, "support.go": supportSource}, []parser.Symbol{primary, support})
	plan := planner.Plan{Repo: repo, TaskClass: "localized_change", TaskConfidence: "high", IndexState: "complete", Primary: []planner.Candidate{candidate(primary, "primary", 9000, "exact_symbol_match")}, Supporting: []planner.Candidate{candidate(support, "supporting", 7000, "direct_callee")}}
	pkg, err := New(staticPlanner{plan: plan}, store).Assemble(context.Background(), Request{Repo: repo, Task: "fix Primary", MaxContextTokens: 2000})
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Sufficiency.Status != sufficiency.StatusSufficient || len(pkg.Sections) != 2 || len(pkg.Rounds) != 2 {
		t.Fatalf("localized change did not retrieve required support: %+v", pkg)
	}
}

func TestPostSerializationGuardRevokesStaleSufficiency(t *testing.T) {
	store, repo, primary, support, _ := assemblyFixture(t)
	defer store.Close()
	plan := planner.Plan{Repo: repo, TaskClass: "localized_change", TaskConfidence: "high", IndexState: "complete", Primary: []planner.Candidate{candidate(primary, "primary", 9000, "exact_symbol_match")}, Supporting: []planner.Candidate{candidate(support, "supporting", 7000, "direct_callee")}}
	pkg, err := New(staticPlanner{plan: plan}, store).Assemble(context.Background(), Request{Repo: repo, Task: "fix Primary", MaxContextTokens: MinContextTokenBudget})
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Budget.UsedTokens > MinContextTokenBudget || pkg.Sufficiency.Status == sufficiency.StatusSufficient {
		t.Fatalf("final serialization retained stale sufficiency or exceeded budget: %+v", pkg)
	}
}

func TestMinimumBudgetCompactsSufficiencyMetadataWithoutChangingTruth(t *testing.T) {
	store := assemblyStore(t)
	defer store.Close()
	repo := "local/minimum-sufficiency"
	primarySource := "func Primary() {}\n"
	primary := indexedSymbol("primary.go", "Primary", primarySource)
	writeAssemblyRepo(t, store, repo, map[string]string{"primary.go": primarySource}, []parser.Symbol{primary})
	missingCandidates := make([]planner.Candidate, 0, sufficiency.MaxMissing+4)
	for i := 0; i < sufficiency.MaxMissing+4; i++ {
		missingCandidates = append(missingCandidates, planner.Candidate{ID: strings.Repeat(fmt.Sprintf("missing-%02d-", i), 20), SymbolID: fmt.Sprintf("missing-%02d::Provider", i), File: fmt.Sprintf("missing-%02d.go", i), Name: "Provider", Kind: "function", Tier: "supporting", Score: 100, ReasonCodes: []string{"framework_provider"}})
	}
	plan := planner.Plan{Repo: repo, TaskClass: "localized_change", TaskConfidence: "high", IndexState: "complete", Primary: []planner.Candidate{candidate(primary, "primary", 9000, "exact_symbol_match")}, Supporting: missingCandidates}
	assembler := New(staticPlanner{plan: plan}, store)
	pkg, err := assembler.Assemble(context.Background(), Request{Repo: repo, Task: "fix Primary", MaxContextTokens: MinContextTokenBudget})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(pkg)
	counter, _ := NewTokenCounter(TokenizerO200K)
	if counter.Count(string(data)) > MinContextTokenBudget || pkg.Sufficiency.Status != sufficiency.StatusBlocked || pkg.Sufficiency.Coverage.CriticalSupportRequired != len(missingCandidates) || len(pkg.Sufficiency.Missing) >= sufficiency.MaxMissing || len(pkg.Sufficiency.ReasonCodes) >= sufficiency.MaxReasonCodes {
		t.Fatalf("minimum-budget sufficiency metadata was not compacted truthfully: tokens=%d package=%+v", counter.Count(string(data)), pkg)
	}
}

func TestMinimumBudgetCompactsManySufficiencyReasonsAndMissing(t *testing.T) {
	store := assemblyStore(t)
	defer store.Close()
	repo := "local/minimum-sufficiency-many"
	source := "func Primary() {}\n"
	primary := indexedSymbol("primary.go", "Primary", source)
	writeAssemblyRepo(t, store, repo, map[string]string{"primary.go": source}, []parser.Symbol{primary})
	reasons := make([]string, 0, sufficiency.MaxReasonCodes)
	for i := 0; i < sufficiency.MaxReasonCodes; i++ {
		reasons = append(reasons, fmt.Sprintf("reason_%02d_%s", i, strings.Repeat("with_detail_", 12)))
	}
	missing := make([]sufficiency.Missing, 0, sufficiency.MaxMissing)
	for i := 0; i < sufficiency.MaxMissing; i++ {
		missing = append(missing, sufficiency.Missing{Kind: "provider", CandidateID: strings.Repeat(fmt.Sprintf("missing-%02d-", i), 20), Reason: "required_provider_missing", Resource: "long-resource", TargetResource: "long-target"})
	}
	decision := sufficiency.Decision{Status: sufficiency.StatusBlocked, EvaluatedAfterStage: "anchor", ReasonCodes: reasons, Coverage: sufficiency.Coverage{AnchorsRequired: 1, AnchorsSatisfied: 1, CriticalSupportRequired: 99, CriticalSupportSatisfied: 1, ProvidersRequired: 88, ProvidersSatisfied: 2}, Missing: missing}
	plan := planner.Plan{Repo: repo, TaskClass: "localized_change", TaskConfidence: "high", IndexState: "complete", Primary: []planner.Candidate{candidate(primary, "primary", 9000, "exact_symbol_match")}}
	assembler := New(staticPlanner{plan: plan}, store)
	assembler.Evaluator = fixedSufficiencyEvaluator{decision: decision}
	pkg, err := assembler.Assemble(context.Background(), Request{Repo: repo, Task: "fix Primary", MaxContextTokens: MinContextTokenBudget})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(pkg)
	counter, _ := NewTokenCounter(TokenizerO200K)
	if counter.Count(string(data)) > MinContextTokenBudget || pkg.Sufficiency.Status != sufficiency.StatusBlocked || pkg.Sufficiency.Coverage.ProvidersRequired != 88 || len(pkg.Sufficiency.Missing) >= sufficiency.MaxMissing || len(pkg.Sufficiency.ReasonCodes) >= sufficiency.MaxReasonCodes {
		t.Fatalf("many sufficiency metadata fields were not compacted truthfully: tokens=%d package=%+v", counter.Count(string(data)), pkg)
	}
}

func TestAssembleOversizedPrimaryUsesDeterministicHeadTail(t *testing.T) {
	store := assemblyStore(t)
	defer store.Close()
	repo := "local/huge"
	source := "func Huge() {\n" + strings.Repeat("\twork()\n", 5000) + "\treturn\n}\n"
	symbol := indexedSymbol("huge.go", "Huge", source)
	if err := store.ReplaceRepoIndex("local", "huge", "local", "", map[string]string{"huge.go": source}, map[string]string{"huge.go": "go"}, []parser.Symbol{symbol}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveContentFile("local", "huge", "huge.go", []byte(source)); err != nil {
		t.Fatal(err)
	}
	plan := planner.Plan{Repo: repo, TaskClass: "exact_symbol", IndexState: "complete", Primary: []planner.Candidate{candidate(symbol, "primary", 9000, "exact_symbol_match")}}
	first, err := New(staticPlanner{plan: plan}, store).Assemble(context.Background(), Request{Repo: repo, Task: "Huge", MaxContextTokens: 1000})
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(staticPlanner{plan: plan}, store).Assemble(context.Background(), Request{Repo: repo, Task: "Huge", MaxContextTokens: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Sections) != 1 || !first.Sections[0].Partial || first.Sections[0].OriginalTokens != 0 || !strings.Contains(first.Sections[0].Source, omissionMarker) {
		t.Fatalf("oversized primary was not represented truthfully: %#v", first)
	}
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	if string(a) != string(b) {
		t.Fatal("assembly was not deterministic")
	}
}

func TestAssembleDeduplicatesSourceAndBudgetIsMonotonic(t *testing.T) {
	store, repo, primary, support, _ := assemblyFixture(t)
	defer store.Close()
	duplicate := candidate(primary, "primary", 8500, "exact_semantic_match")
	plan := planner.Plan{Repo: repo, TaskClass: "localized_change", IndexState: "complete", Primary: []planner.Candidate{candidate(primary, "primary", 9000, "exact_symbol_match"), duplicate}, Supporting: []planner.Candidate{candidate(support, "supporting", 7000, "direct_callee")}}
	small, err := New(staticPlanner{plan: plan}, store).Assemble(context.Background(), Request{Repo: repo, Task: "fix Primary", MaxContextTokens: 1000})
	if err != nil {
		t.Fatal(err)
	}
	large, err := New(staticPlanner{plan: plan}, store).Assemble(context.Background(), Request{Repo: repo, Task: "fix Primary", MaxContextTokens: 4000})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, section := range large.Sections {
		if seen[section.SymbolID] {
			t.Fatalf("duplicate source: %s", section.SymbolID)
		}
		seen[section.SymbolID] = true
	}
	for _, section := range small.Sections {
		if !containsSection(large.Sections, section.SymbolID) {
			t.Fatalf("larger budget lost higher-priority section %s", section.SymbolID)
		}
	}
}

func TestAssembleDebugDoesNotChangePacking(t *testing.T) {
	store, repo, primary, support, _ := assemblyFixture(t)
	defer store.Close()
	plan := planner.Plan{Repo: repo, TaskClass: "localized_change", IndexState: "complete", Primary: []planner.Candidate{candidate(primary, "primary", 9000, "exact_symbol_match")}, Supporting: []planner.Candidate{candidate(support, "supporting", 7000, "direct_callee")}}
	assembler := New(staticPlanner{plan: plan}, store)
	normal, err := assembler.Assemble(context.Background(), Request{Repo: repo, Task: "fix Primary", MaxContextTokens: 1200})
	if err != nil {
		t.Fatal(err)
	}
	debug, err := assembler.Assemble(context.Background(), Request{Repo: repo, Task: "fix Primary", MaxContextTokens: 1200, Debug: true})
	if err != nil {
		t.Fatal(err)
	}
	if debug.Debug == nil || len(normal.Sections) != len(debug.Sections) {
		t.Fatalf("debug changed packing: normal=%#v debug=%#v", normal.Sections, debug.Sections)
	}
	for i := range normal.Sections {
		if normal.Sections[i].CandidateID != debug.Sections[i].CandidateID || normal.Sections[i].Source != debug.Sections[i].Source {
			t.Fatalf("debug changed section %d", i)
		}
	}
	normalSufficiency, _ := json.Marshal(normal.Sufficiency)
	debugSufficiency, _ := json.Marshal(debug.Sufficiency)
	if string(normalSufficiency) != string(debugSufficiency) || normal.StopReason != debug.StopReason {
		t.Fatalf("debug changed sufficiency decision: normal=%+v debug=%+v", normal.Sufficiency, debug.Sufficiency)
	}
	counter, _ := NewTokenCounter(TokenizerO200K)
	for _, pkg := range []Package{normal, debug} {
		data, _ := json.Marshal(pkg)
		if got := counter.Count(string(data)); got > 1200 || got != pkg.Budget.UsedTokens {
			t.Fatalf("debug budget mismatch: measured=%d reported=%d", got, pkg.Budget.UsedTokens)
		}
	}
}

func TestUnusedDirectSupportReserveRollsBackToPrimary(t *testing.T) {
	store, repo, primary, _, _ := assemblyFixture(t)
	defer store.Close()
	missing := planner.Candidate{ID: "missing", SymbolID: "missing.go::Missing#function", File: "missing.go", Name: "Missing", Kind: "function", Tier: "supporting", Score: 7000, ReasonCodes: []string{"direct_callee"}, EstimatedScope: 100}
	base := planner.Plan{Repo: repo, TaskClass: "localized_change", IndexState: "complete", Primary: []planner.Candidate{candidate(primary, "primary", 9000, "exact_symbol_match")}}
	withMissing := base
	withMissing.Supporting = []planner.Candidate{missing}
	without, err := New(staticPlanner{plan: base}, store).Assemble(context.Background(), Request{Repo: repo, Task: "fix Primary", MaxContextTokens: 1600})
	if err != nil {
		t.Fatal(err)
	}
	with, err := New(staticPlanner{plan: withMissing}, store).Assemble(context.Background(), Request{Repo: repo, Task: "fix Primary", MaxContextTokens: 1600})
	if err != nil {
		t.Fatal(err)
	}
	if len(with.Sections) == 0 || len(without.Sections) == 0 || with.Sections[0].TokenCount < without.Sections[0].TokenCount-32 {
		t.Fatalf("unused support reserve was not reclaimed: with=%d without=%d", with.Sections[0].TokenCount, without.Sections[0].TokenCount)
	}
}

func TestUnusedDirectSupportReserveExpandsPrimaryBeforePeripheral(t *testing.T) {
	store, repo, primary, _, weak := assemblyFixture(t)
	defer store.Close()
	missing := planner.Candidate{ID: "missing-critical", SymbolID: "missing.go::Provider#function", File: "missing.go", Name: "Provider", Kind: "function", Tier: "supporting", Score: 9000, ReasonCodes: []string{"framework_provider"}, EstimatedScope: 500}
	base := planner.Plan{Repo: repo, TaskClass: "localized_change", IndexState: "complete", Primary: []planner.Candidate{candidate(primary, "primary", 10000, "exact_symbol_match")}, Supporting: []planner.Candidate{missing}}
	withoutPeripheral, err := New(staticPlanner{plan: base}, store).Assemble(context.Background(), Request{Repo: repo, Task: "fix Primary", MaxContextTokens: 1600})
	if err != nil {
		t.Fatal(err)
	}
	withPeripheralPlan := base
	withPeripheralPlan.Peripheral = []planner.Candidate{candidate(weak, "peripheral", 100, "lexical_fallback")}
	withPeripheral, err := New(staticPlanner{plan: withPeripheralPlan}, store).Assemble(context.Background(), Request{Repo: repo, Task: "fix Primary", MaxContextTokens: 1600})
	if err != nil {
		t.Fatal(err)
	}
	withoutIndex, withIndex := sectionIndex(withoutPeripheral.Sections, primary.ID), sectionIndex(withPeripheral.Sections, primary.ID)
	if withoutIndex < 0 || withIndex < 0 || withoutPeripheral.Sections[withoutIndex].TokenCount != withPeripheral.Sections[withIndex].TokenCount {
		t.Fatalf("peripheral context consumed reclaimable primary reserve: without=%#v with=%#v", withoutPeripheral.Sections, withPeripheral.Sections)
	}
}

func TestCriticalDirectSupportPrecedesTinyReferenceAndImport(t *testing.T) {
	for _, tc := range []struct {
		name, criticalReason, weakReason string
	}{
		{"verified_provider", "framework_provider", "direct_reference"},
		{"direct_callee", "direct_callee", "direct_import"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := assemblyStore(t)
			defer store.Close()
			repo := "local/critical-" + tc.name
			primarySource := "func Target() {}\n"
			criticalSource := "func Critical() {\n" + strings.Repeat("\twork()\n", 700) + "}\n"
			weakSource := "func Tiny() {}\n"
			primary := indexedSymbol("target.go", "Target", primarySource)
			critical := indexedSymbol("critical.go", "Critical", criticalSource)
			weak := indexedSymbol("tiny.go", "Tiny", weakSource)
			writeAssemblyRepo(t, store, repo, map[string]string{primary.File: primarySource, critical.File: criticalSource, weak.File: weakSource}, []parser.Symbol{primary, critical, weak})
			plan := planner.Plan{Repo: repo, TaskClass: "localized_change", IndexState: "complete", Primary: []planner.Candidate{candidate(primary, "primary", 9000, "exact_symbol_match")}, Supporting: []planner.Candidate{candidate(weak, "supporting", 8800, tc.weakReason), candidate(critical, "supporting", 7000, tc.criticalReason)}}
			pkg, err := New(staticPlanner{plan: plan}, store).Assemble(context.Background(), Request{Repo: repo, Task: "fix Target", MaxContextTokens: 1400})
			if err != nil {
				t.Fatal(err)
			}
			criticalIndex, weakIndex := sectionIndex(pkg.Sections, critical.ID), sectionIndex(pkg.Sections, weak.ID)
			if criticalIndex < 0 || (weakIndex >= 0 && criticalIndex > weakIndex) {
				t.Fatalf("critical direct support lost to tiny %s: %#v", tc.weakReason, pkg.Sections)
			}
		})
	}
}

func TestDirectSupportReserveSharesCriticalCapacity(t *testing.T) {
	store := assemblyStore(t)
	defer store.Close()
	repo := "local/support-share"
	sources := map[string]string{
		"target.go": "func Target() {}\n",
		"large.go":  "func Large() {\n" + strings.Repeat("\twork()\n", 1800) + "}\n",
		"two.go":    "func Two() {}\n",
		"three.go":  "func Three() {}\n",
	}
	target := indexedSymbol("target.go", "Target", sources["target.go"])
	large := indexedSymbol("large.go", "Large", sources["large.go"])
	two := indexedSymbol("two.go", "Two", sources["two.go"])
	three := indexedSymbol("three.go", "Three", sources["three.go"])
	writeAssemblyRepo(t, store, repo, sources, []parser.Symbol{target, large, two, three})
	plan := planner.Plan{Repo: repo, TaskClass: "localized_change", IndexState: "complete", Primary: []planner.Candidate{candidate(target, "primary", 9000, "exact_symbol_match")}, Supporting: []planner.Candidate{candidate(large, "supporting", 8500, "direct_callee"), candidate(two, "supporting", 8400, "direct_caller"), candidate(three, "supporting", 8300, "event_peer")}}
	pkg, err := New(staticPlanner{plan: plan}, store).Assemble(context.Background(), Request{Repo: repo, Task: "fix Target", MaxContextTokens: 2200})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{large.ID, two.ID, three.ID} {
		if !containsSection(pkg.Sections, id) {
			t.Fatalf("critical support %s was starved by the oversized sibling: %#v", id, pkg.Sections)
		}
	}
}

func TestHydratedByteLengthControlsDirectSupportUtility(t *testing.T) {
	short := indexedSymbol("short.go", "Short", "func Short() {}\n")
	long := indexedSymbol("min.go", "Minified", "func Minified(){"+strings.Repeat("x", 20000)+"}\n")
	pool := []stagedCandidate{
		{candidate: candidate(long, "supporting", 9000, "direct_callee"), stage: "direct_support", stageRank: 1, supportClass: supportCritical, estimate: 29},
		{candidate: candidate(short, "supporting", 5000, "direct_callee"), stage: "direct_support", stageRank: 1, supportClass: supportCritical, estimate: 29},
	}
	pool = hydrateEstimates(pool, map[string]parser.Symbol{long.ID: long, short.ID: short})
	sortStagedCandidates(pool)
	if pool[0].candidate.SymbolID != short.ID || pool[1].estimate <= pool[0].estimate {
		t.Fatalf("minified source did not use ByteLength estimate: %#v", pool)
	}
}

func TestIncreasingBudgetPreservesCriticalSupportOrder(t *testing.T) {
	store, repo, primary, support, weak := assemblyFixture(t)
	defer store.Close()
	provider := candidate(support, "supporting", 8000, "framework_provider")
	plan := planner.Plan{Repo: repo, TaskClass: "localized_change", IndexState: "complete", Primary: []planner.Candidate{candidate(primary, "primary", 9000, "exact_symbol_match")}, Supporting: []planner.Candidate{candidate(weak, "supporting", 8900, "direct_reference"), provider}}
	assembler := New(staticPlanner{plan: plan}, store)
	small, err := assembler.Assemble(context.Background(), Request{Repo: repo, Task: "fix Primary", MaxContextTokens: 2000})
	if err != nil {
		t.Fatal(err)
	}
	large, err := assembler.Assemble(context.Background(), Request{Repo: repo, Task: "fix Primary", MaxContextTokens: 2400})
	if err != nil {
		t.Fatal(err)
	}
	if sectionIndex(small.Sections, support.ID) < 0 || sectionIndex(large.Sections, support.ID) < 0 || sectionIndex(large.Sections, support.ID) > sectionIndex(large.Sections, weak.ID) {
		t.Fatalf("increasing budget changed critical support priority: small=%#v large=%#v", small.Sections, large.Sections)
	}
}

func TestFileCandidateUsesOutlineWithoutSourceRead(t *testing.T) {
	store := assemblyStore(t)
	defer store.Close()
	repo, source := "local/outline", "func One() {}\nfunc Two() {}\n"
	one := indexedSymbol("main.go", "One", "func One() {}\n")
	two := indexedSymbol("main.go", "Two", "func Two() {}\n")
	two.ByteOffset = int64(len("func One() {}\n"))
	two.Line, two.EndLine = 2, 2
	if err := store.ReplaceRepoIndex("local", "outline", "local", "", map[string]string{"main.go": source}, map[string]string{"main.go": "go"}, []parser.Symbol{one, two}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveContentFile("local", "outline", "main.go", []byte(source)); err != nil {
		t.Fatal(err)
	}
	fileCandidate := planner.Candidate{ID: "file-main", File: "main.go", Name: "main.go", Kind: "file", Tier: "primary", Score: 9000, ReasonCodes: []string{"exact_file_match"}}
	pkg, err := New(staticPlanner{plan: planner.Plan{Repo: repo, TaskClass: "exact_symbol", IndexState: "complete", Primary: []planner.Candidate{fileCandidate}}}, store).Assemble(context.Background(), Request{Repo: repo, Task: "main.go", MaxContextTokens: 1000, Debug: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(pkg.Sections) != 1 || pkg.Sections[0].ContentKind != "file_outline" || pkg.Debug.SourceReads != 0 || !strings.Contains(pkg.Sections[0].Source, "One") {
		t.Fatalf("unsafe file candidate: %#v", pkg)
	}
}

func TestAssemblerBoundsSourceReadsIndependentlyOfTokenBudget(t *testing.T) {
	store := assemblyStore(t)
	defer store.Close()
	repo := "local/read-limit"
	source := strings.Repeat("x", DefaultMaxSourceBytes+1)
	symbol := indexedSymbol("huge.go", "Huge", source)
	if err := store.ReplaceRepoIndex("local", "read-limit", "local", "", map[string]string{"huge.go": source}, map[string]string{"huge.go": "go"}, []parser.Symbol{symbol}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveContentFile("local", "read-limit", "huge.go", []byte(source)); err != nil {
		t.Fatal(err)
	}
	plan := planner.Plan{Repo: repo, TaskClass: "exact_symbol", IndexState: "complete", Primary: []planner.Candidate{candidate(symbol, "primary", 9000, "exact_symbol_match")}}
	pkg, err := New(staticPlanner{plan: plan}, store).Assemble(context.Background(), Request{Repo: repo, Task: "Huge", MaxContextTokens: 8000, Debug: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(pkg.Sections) != 1 || !pkg.Sections[0].Partial || pkg.Debug.SourceReads != 1 || pkg.Debug.SourceBytesRead > DefaultMaxSymbolBytes || pkg.Omitted.SourceReadLimit != 0 {
		t.Fatalf("oversized primary was not bounded and admitted: %#v", pkg)
	}
}

func TestOversizedFocusedSymbolPreservesBoundedHeadTail(t *testing.T) {
	store := assemblyStore(t)
	defer store.Close()
	repo := "local/oversized-focus"
	unit := "middle🙂\n"
	source := "HEAD_SENTINEL_🙂\n" + strings.Repeat(unit, DefaultMaxSourceBytes/len(unit)+1) + "TAIL_SENTINEL_終\n"
	symbol := indexedSymbol("huge.go", "HugeFocus", source)
	writeAssemblyRepo(t, store, repo, map[string]string{symbol.File: source}, []parser.Symbol{symbol})
	plan := planner.Plan{Repo: repo, TaskClass: "exact_symbol", TaskConfidence: "high", IndexState: "complete", Primary: []planner.Candidate{candidate(symbol, "primary", 10000, "explicit_focus")}}
	pkg, err := New(staticPlanner{plan: plan}, store).Assemble(context.Background(), Request{Repo: repo, Task: "focus HugeFocus", FocusSymbolID: symbol.ID, MaxContextTokens: 1000, Debug: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(pkg.Sections) != 1 || !pkg.Sections[0].Partial || pkg.Sections[0].OriginalTokens != 0 || pkg.Debug.SourceBytesRead > DefaultMaxSymbolBytes || !strings.Contains(pkg.Sections[0].Source, "HEAD_SENTINEL") || !strings.Contains(pkg.Sections[0].Source, "TAIL_SENTINEL") || !strings.Contains(pkg.Sections[0].Source, omissionMarker) {
		t.Fatalf("focused oversized symbol was not represented truthfully: %#v", pkg)
	}
	if !utf8.ValidString(pkg.Sections[0].Source) {
		t.Fatal("bounded symbol source is not valid UTF-8")
	}
	data, _ := json.Marshal(pkg)
	counter, _ := NewTokenCounter(TokenizerO200K)
	if counter.Count(string(data)) > 1000 {
		t.Fatalf("serialized focused package exceeded budget: %d", counter.Count(string(data)))
	}
}

func TestMidSizedPrimaryUsesPerSymbolBoundBeforeGlobalBudget(t *testing.T) {
	store := assemblyStore(t)
	defer store.Close()
	repo := "local/midsized-primary"
	unit := "middle🙂\n"
	source := "HEAD_MID_SYMBOL_🙂\n" + strings.Repeat(unit, 1500000/len(unit)+1) + "TAIL_MID_SYMBOL_終\n"
	symbol := indexedSymbol("mid.go", "MidSized", source)
	writeAssemblyRepo(t, store, repo, map[string]string{symbol.File: source}, []parser.Symbol{symbol})
	plan := planner.Plan{Repo: repo, TaskClass: "exact_symbol", TaskConfidence: "high", IndexState: "complete", Primary: []planner.Candidate{candidate(symbol, "primary", 10000, "exact_symbol_match")}}
	pkg, err := New(staticPlanner{plan: plan}, store).Assemble(context.Background(), Request{Repo: repo, Task: "MidSized", MaxContextTokens: 8000, Debug: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(pkg.Sections) != 1 || !pkg.Sections[0].Partial || pkg.Sections[0].OriginalTokens != 0 || pkg.Debug.SourceBytesRead > DefaultMaxSymbolBytes || !strings.Contains(pkg.Sections[0].Source, "HEAD_MID_SYMBOL") || !strings.Contains(pkg.Sections[0].Source, "TAIL_MID_SYMBOL") || !utf8.ValidString(pkg.Sections[0].Source) {
		t.Fatalf("mid-sized symbol bypassed per-symbol bound: %#v", pkg)
	}
	data, _ := json.Marshal(pkg)
	counter, _ := NewTokenCounter(TokenizerO200K)
	if counter.Count(string(data)) > 8000 {
		t.Fatalf("mid-sized package exceeded serialized budget: %d", counter.Count(string(data)))
	}
}

func TestAssemblerUsesRealPlannerGenericRelationships(t *testing.T) {
	store := assemblyStore(t)
	defer store.Close()
	repo := "local/real-plan"
	sources := map[string]string{"save.go": "func SaveUser() { writeDB() }\n", "db.go": "func writeDB() {}\n", "handler.go": "func Handler() { SaveUser() }\n"}
	save, db, handler := indexedSymbol("save.go", "SaveUser", sources["save.go"]), indexedSymbol("db.go", "writeDB", sources["db.go"]), indexedSymbol("handler.go", "Handler", sources["handler.go"])
	symbols := []parser.Symbol{save, db, handler}
	if err := store.ReplaceRepoIndex("local", "real-plan", "local", "", map[string]string{"save.go": sources["save.go"], "db.go": sources["db.go"], "handler.go": sources["handler.go"]}, map[string]string{"save.go": "go", "db.go": "go", "handler.go": "go"}, symbols); err != nil {
		t.Fatal(err)
	}
	for file, source := range sources {
		if err := store.SaveContentFile("local", "real-plan", file, []byte(source)); err != nil {
			t.Fatal(err)
		}
	}
	repoID, _ := store.GetRepoID(repo)
	entity := func(symbol parser.Symbol) semantic.Entity {
		return semantic.Entity{ID: semantic.StableID("code_symbol", repo, symbol.ID), Analyzer: semantic.AnalyzerGenericGraph, Repo: repo, File: symbol.File, SymbolID: symbol.ID, Kind: generic.KindCodeSymbol, Name: symbol.Name, Line: symbol.Line, EndLine: symbol.EndLine}
	}
	saveEntity, dbEntity, handlerEntity := entity(save), entity(db), entity(handler)
	relationships := []semantic.Relationship{{ID: "save-db", Analyzer: semantic.AnalyzerGenericGraph, Repo: repo, FromEntityID: saveEntity.ID, ToEntityID: dbEntity.ID, Kind: generic.RelationshipCalls, Confidence: 1, File: save.File}, {ID: "handler-save", Analyzer: semantic.AnalyzerGenericGraph, Repo: repo, FromEntityID: handlerEntity.ID, ToEntityID: saveEntity.ID, Kind: generic.RelationshipCalls, Confidence: 1, File: handler.File}}
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerGenericGraph, semantic.Result{Entities: []semantic.Entity{saveEntity, dbEntity, handlerEntity}, Relationships: relationships}); err != nil {
		t.Fatal(err)
	}
	pkg, err := New(planner.New(store), store).Assemble(context.Background(), Request{Repo: repo, Task: "fix SaveUser", MaxContextTokens: 2000, IncludeImpact: true})
	if err != nil {
		t.Fatal(err)
	}
	if !containsSection(pkg.Sections, save.ID) || !containsSection(pkg.Sections, db.ID) || !containsSection(pkg.Sections, handler.ID) {
		t.Fatalf("real planner relationship context missing: %#v", pkg.Sections)
	}
}

func TestRealGenericFindStopsEarlierThanFix(t *testing.T) {
	store := assemblyStore(t)
	defer store.Close()
	repo := "local/real-generic-sufficiency"
	sources := map[string]string{"save.go": "package app\nfunc SaveUser() { writeDB() }\n", "db.go": "package app\nfunc writeDB() {}\n", "handler.go": "package app\nfunc Handler() { SaveUser() }\n"}
	files := map[string]string{}
	languages := map[string]string{}
	symbols := map[string][]parser.Symbol{}
	allSymbols := []parser.Symbol{}
	for file, source := range sources {
		files[file] = parser.ComputeContentHash([]byte(source))
		languages[file] = "go"
		parsed, err := parser.ParseFile([]byte(source), file, "go")
		if err != nil {
			t.Fatal(err)
		}
		symbols[file] = parsed
		allSymbols = append(allSymbols, parsed...)
	}
	if err := store.ReplaceRepoIndex("local", "real-generic-sufficiency", "local", "", files, languages, allSymbols); err != nil {
		t.Fatal(err)
	}
	for file, source := range sources {
		if err := store.SaveContentFile("local", "real-generic-sufficiency", file, []byte(source)); err != nil {
			t.Fatal(err)
		}
	}
	repoID, err := store.GetRepoID(repo)
	if err != nil {
		t.Fatal(err)
	}
	genericResult, err := generic.NewAnalyzer().AnalyzeRepository(context.Background(), semantic.RepositoryInput{Repo: repo, Files: byteFiles(sources), Languages: languages, Symbols: symbols})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerGenericGraph, genericResult); err != nil {
		t.Fatal(err)
	}
	findPackage, err := New(planner.New(store), store).Assemble(context.Background(), Request{Repo: repo, Task: "find SaveUser", MaxContextTokens: 2000})
	if err != nil {
		t.Fatal(err)
	}
	fixPackage, err := New(planner.New(store), store).Assemble(context.Background(), Request{Repo: repo, Task: "fix SaveUser", MaxContextTokens: 2000, IncludeImpact: true})
	if err != nil {
		t.Fatal(err)
	}
	if findPackage.Sufficiency.Status != sufficiency.StatusSufficient || len(findPackage.Rounds) != 1 || len(findPackage.Sections) >= len(fixPackage.Sections) || fixPackage.Sufficiency.Status != sufficiency.StatusSufficient {
		t.Fatalf("real generic find/fix stopping policy failed: find=%+v fix=%+v", findPackage, fixPackage)
	}
	if findPackage.Budget.UsedTokens >= fixPackage.Budget.UsedTokens {
		t.Fatalf("find task did not consume less context: find=%d fix=%d", findPackage.Budget.UsedTokens, fixPackage.Budget.UsedTokens)
	}
}

func TestAssemblerPreservesFiveMFrameworkAuthorityWithoutFabricatingProvider(t *testing.T) {
	store := assemblyStore(t)
	defer store.Close()
	repo := "local/fivem-pack"
	callerSource, providerSource := "exports.ox_inventory:AddItem(source, 'water', 2)\n", "exports('AddItem', function(source, item, count) end)\n"
	caller, provider := indexedSymbol("resources/app/server.lua", "Run", callerSource), indexedSymbol("resources/ox_inventory/server.lua", "AddItem", providerSource)
	if err := store.ReplaceRepoIndex("local", "fivem-pack", "local", "", map[string]string{caller.File: callerSource, provider.File: providerSource}, map[string]string{caller.File: "lua", provider.File: "lua"}, []parser.Symbol{caller, provider}); err != nil {
		t.Fatal(err)
	}
	for file, source := range map[string]string{caller.File: callerSource, provider.File: providerSource} {
		if err := store.SaveContentFile("local", "fivem-pack", file, []byte(source)); err != nil {
			t.Fatal(err)
		}
	}
	callerCandidate := candidate(caller, "primary", 9000, "framework_operation_match")
	callerCandidate.Resource, callerCandidate.TargetResource, callerCandidate.Framework, callerCandidate.Side, callerCandidate.Authority = "app", "ox_inventory", "ox_inventory", "server", "local_verified"
	providerCandidate := candidate(provider, "supporting", 8000, "framework_provider")
	providerCandidate.Resource, providerCandidate.Framework, providerCandidate.Side, providerCandidate.Authority = "ox_inventory", "ox_inventory", "server", "local_verified"
	plan := planner.Plan{Repo: repo, TaskClass: "cross_resource", IndexState: "complete", Primary: []planner.Candidate{callerCandidate}, Supporting: []planner.Candidate{providerCandidate}}
	pkg, err := New(staticPlanner{plan: plan}, store).Assemble(context.Background(), Request{Repo: repo, Task: "fix inventory flow", MaxContextTokens: 2000})
	if err != nil {
		t.Fatal(err)
	}
	if len(pkg.Sections) != 2 || pkg.Sufficiency.Status != sufficiency.StatusSufficient || pkg.Sections[0].Resource != "app" || pkg.Sections[0].TargetResource != "ox_inventory" || pkg.Sections[1].Authority != "local_verified" {
		t.Fatalf("framework ownership was lost: %#v", pkg.Sections)
	}

	external := callerCandidate
	external.Authority = "external_unverified"
	external.TargetResource = "missing_inventory"
	externalPkg, err := New(staticPlanner{plan: planner.Plan{Repo: repo, TaskClass: "exact_semantic", IndexState: "complete", Primary: []planner.Candidate{external}}}, store).Assemble(context.Background(), Request{Repo: repo, Task: "inventory_add_item", MaxContextTokens: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if len(externalPkg.Sections) != 1 || externalPkg.Sufficiency.Status != sufficiency.StatusBlocked || externalPkg.Sections[0].SymbolID != caller.ID {
		t.Fatalf("external usage fabricated provider context: %#v", externalPkg.Sections)
	}
}

func TestAssembleRealisticFiveMWorkspaceContext(t *testing.T) {
	root := t.TempDir()
	repo := "local/realistic-fivem"
	files := map[string]string{
		"server.cfg": "ensure banana_core\nensure ox_inventory\nensure banana_jobs\nensure banana_ui\n",
		"resources/[core]/banana_core/fxmanifest.lua":       "fx_version 'cerulean'\nserver_script 'server/player.lua'\nserver_script 'server/inventory.lua'\n",
		"resources/[core]/banana_core/server/player.lua":    "local function GetPlayer(source) return { source = source } end\nexports('GetPlayer', GetPlayer)\n",
		"resources/[core]/banana_core/server/inventory.lua": "exports('AddCharacterItem', function(source, item) return true end)\n",
		"resources/[ox]/ox_inventory/fxmanifest.lua":        "fx_version 'cerulean'\nserver_script 'server/main.lua'\n",
		"resources/[ox]/ox_inventory/server/main.lua":       "local function AddItem(source, item, count) return true end\nexports('AddItem', AddItem)\nexports('RemoveItem', function(source, item, count) return true end)\nexports('Search', function(source, item) return {} end)\n",
		"resources/[jobs]/banana_jobs/fxmanifest.lua":       "fx_version 'cerulean'\nclient_script 'client/main.lua'\nserver_script 'server/main.lua'\n",
		"resources/[jobs]/banana_jobs/server/main.lua":      "local function LoadCharacter(source)\n local Player = exports.banana_core:GetPlayer(source)\n exports.ox_inventory:AddItem(source, 'water', 1)\n TriggerClientEvent('banana:characterLoaded', source)\n return Player\nend\nRegisterNetEvent('banana:loadCharacter', function() LoadCharacter(source) end)\nexports('LoadCharacter', LoadCharacter)\nlib.callback.register('banana:loadCharacter', function(source) return LoadCharacter(source) end)\nRegisterCommand('loadcharacter', function(source) LoadCharacter(source) end)\n",
		"resources/[jobs]/banana_jobs/client/main.lua":      "RegisterNetEvent('banana:characterLoaded', function() end)\nTriggerServerEvent('banana:loadCharacter')\nlib.callback.await('banana:loadCharacter', false)\n",
		"resources/[ui]/banana_ui/fxmanifest.lua":           "fx_version 'cerulean'\nclient_script 'client/main.lua'\n",
		"resources/[ui]/banana_ui/client/main.lua":          "RegisterNUICallback('purchaseItem', function(data, cb) TriggerServerEvent('banana:loadCharacter'); cb({}) end)\n",
	}
	contents, languages, symbols, hashes := map[string][]byte{}, map[string]string{}, map[string][]parser.Symbol{}, map[string]string{}
	allSymbols := []parser.Symbol{}
	for path, source := range files {
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
		language := parser.DetectLanguage(path)
		parsed, err := parser.ParseFile([]byte(source), path, language)
		if err != nil {
			t.Fatal(err)
		}
		contents[path], languages[path], symbols[path], hashes[path] = []byte(source), language, parsed, workspace.ContentHash([]byte(source))
		allSymbols = append(allSymbols, parsed...)
	}
	store := assemblyStore(t)
	defer store.Close()
	if err := store.ReplaceRepoIndex("local", "realistic-fivem", "local", "", hashes, languages, allSymbols, root); err != nil {
		t.Fatal(err)
	}
	for path, content := range contents {
		if err := store.SaveContentFile("local", "realistic-fivem", path, content); err != nil {
			t.Fatal(err)
		}
	}
	repoID, err := store.GetRepoID(repo)
	if err != nil {
		t.Fatal(err)
	}
	discovery, err := workspace.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workspaceindex.Index(context.Background(), store, repoID, repo, root, contents, languages, symbols, discovery); err != nil {
		t.Fatal(err)
	}
	genericResult, err := generic.NewAnalyzer().AnalyzeRepository(context.Background(), semantic.RepositoryInput{Repo: repo, Files: contents, Languages: languages, Symbols: symbols})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerGenericGraph, genericResult); err != nil {
		t.Fatal(err)
	}
	realPlan, err := planner.New(store).Plan(context.Background(), planner.Request{Repo: repo, Task: "inventory_add_item", IncludeImpact: true})
	if err != nil {
		t.Fatal(err)
	}
	verifiedProvider := false
	for _, candidate := range append(append([]planner.Candidate{}, realPlan.Primary...), realPlan.Supporting...) {
		if !containsAny(candidate.ReasonCodes, "framework_provider", "export_provider") {
			continue
		}
		if candidate.Name == "AddItem" && candidate.Resource == "ox_inventory" && strings.Contains(candidate.File, "resources/[ox]/ox_inventory/") && (candidate.Authority == framework.ProviderStatusLocalVerified || containsAny(candidate.Authorities, framework.ProviderStatusLocalVerified)) {
			verifiedProvider = true
			break
		}
	}
	if !verifiedProvider {
		t.Fatalf("real framework provider authority was lost before context assembly: %#v", realPlan)
	}
	pkg, err := New(planner.New(store), store).Assemble(context.Background(), Request{Repo: repo, Task: "inventory_add_item", MaxContextTokens: 4000, IncludeImpact: true})
	if err != nil {
		t.Fatal(err)
	}
	counter, _ := NewTokenCounter(TokenizerO200K)
	data, _ := json.Marshal(pkg)
	if counter.Count(string(data)) > 4000 || !containsFile(pkg.Sections, "resources/[jobs]/banana_jobs/server/main.lua") || !containsFile(pkg.Sections, "resources/[ox]/ox_inventory/server/main.lua") {
		t.Fatalf("real workspace package lacks bounded cross-resource context: %#v", pkg)
	}
	natural, err := New(planner.New(store), store).Assemble(context.Background(), Request{Repo: repo, Task: "fix character inventory loading", MaxContextTokens: 4000, IncludeImpact: true})
	if err != nil {
		t.Fatal(err)
	}
	naturalData, _ := json.Marshal(natural)
	if natural.TaskClass != "broad_unknown" || natural.TaskConfidence != "low" || natural.Sufficiency.Status != sufficiency.StatusIndeterminate || counter.Count(string(naturalData)) > 4000 || !containsFile(natural.Sections, "resources/[jobs]/banana_jobs/server/main.lua") || !containsFile(natural.Sections, "resources/[ox]/ox_inventory/server/main.lua") || !sectionsContainText(natural.Sections, "exports.banana_core:GetPlayer") || !sectionsContainText(natural.Sections, "exports.ox_inventory:AddItem") {
		t.Fatalf("natural workspace task lacked bounded character/inventory context: %#v", natural)
	}
	naturalAgain, err := New(planner.New(store), store).Assemble(context.Background(), Request{Repo: repo, Task: "fix character inventory loading", MaxContextTokens: 4000, IncludeImpact: true})
	if err != nil {
		t.Fatal(err)
	}
	naturalAgainData, _ := json.Marshal(naturalAgain)
	if string(naturalData) != string(naturalAgainData) {
		t.Fatal("natural workspace context assembly was not deterministic")
	}
	var loadCharacterID string
	for _, symbol := range allSymbols {
		if symbol.Name == "LoadCharacter" {
			loadCharacterID = symbol.ID
			break
		}
	}
	focused, err := New(planner.New(store), store).Assemble(context.Background(), Request{Repo: repo, Task: "fix character inventory loading", FocusSymbolID: loadCharacterID, MaxContextTokens: 4000, IncludeImpact: true})
	if err != nil {
		t.Fatal(err)
	}
	if focused.Sufficiency.Status != sufficiency.StatusBlocked || !containsFile(focused.Sections, "resources/[jobs]/banana_jobs/server/main.lua") || !strings.Contains(strings.Join(focused.Sufficiency.ReasonCodes, ","), "required_source_partial") {
		t.Fatalf("focused implementation task falsely claimed sufficient or lost target: %+v", focused.Sufficiency)
	}
}

func TestAssembleCancellationAndBudgetValidation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := New(staticPlanner{}, nil).Assemble(ctx, Request{})
	if err != context.Canceled {
		t.Fatalf("expected cancellation, got %v", err)
	}
	store := assemblyStore(t)
	defer store.Close()
	assembler := New(staticPlanner{}, store)
	for _, budget := range []int{-1, 1, MinContextTokenBudget - 1, HardMaxContextTokenBudget + 1} {
		if _, err := assembler.Assemble(context.Background(), Request{MaxContextTokens: budget}); err == nil {
			t.Fatalf("budget %d accepted", budget)
		}
	}
}

func TestAmbiguousDeclarationsDoNotConsumeFullBudget(t *testing.T) {
	store := assemblyStore(t)
	defer store.Close()
	repo := "local/ambiguous-pack"
	files, languages := map[string]string{}, map[string]string{}
	symbols := []parser.Symbol{}
	candidates := []planner.Candidate{}
	for i := 0; i < 12; i++ {
		file, source := fmt.Sprintf("init-%02d.go", i), fmt.Sprintf("func init() { work%d() }\n", i)
		files[file], languages[file] = source, "go"
		symbol := indexedSymbol(file, "init", source)
		symbols = append(symbols, symbol)
		candidates = append(candidates, candidate(symbol, "primary", 9000-i, "exact_symbol_match"))
	}
	if err := store.ReplaceRepoIndex("local", "ambiguous-pack", "local", "", files, languages, symbols); err != nil {
		t.Fatal(err)
	}
	for file, source := range files {
		if err := store.SaveContentFile("local", "ambiguous-pack", file, []byte(source)); err != nil {
			t.Fatal(err)
		}
	}
	plan := planner.Plan{Repo: repo, TaskClass: "broad_unknown", IndexState: "complete", Primary: candidates, Ambiguities: []planner.Ambiguity{{Kind: "source_anchor", Query: "init", CandidateCount: 12}}}
	pkg, err := New(staticPlanner{plan: plan}, store).Assemble(context.Background(), Request{Repo: repo, Task: "fix init", MaxContextTokens: 8000})
	if err != nil {
		t.Fatal(err)
	}
	if len(pkg.Sections) > 3 || pkg.Omitted.LowerPriority != 9 {
		t.Fatalf("ambiguity dumped excessive implementations: %#v", pkg)
	}
}

func assemblyFixture(t *testing.T) (*storage.IndexStore, string, parser.Symbol, parser.Symbol, parser.Symbol) {
	t.Helper()
	store := assemblyStore(t)
	repo := "local/assembly"
	primarySource := "func Primary() {\n" + strings.Repeat("\tdoPrimaryWork()\n", 180) + "}\n"
	supportSource := "func Support() {\n" + strings.Repeat("\thelp()\n", 50) + "}\n"
	weakSource := "func TinyWeak() {}\n"
	primary, support, weak := indexedSymbol("primary.go", "Primary", primarySource), indexedSymbol("support.go", "Support", supportSource), indexedSymbol("weak.go", "TinyWeak", weakSource)
	if err := store.ReplaceRepoIndex("local", "assembly", "local", "", map[string]string{"primary.go": primarySource, "support.go": supportSource, "weak.go": weakSource}, map[string]string{"primary.go": "go", "support.go": "go", "weak.go": "go"}, []parser.Symbol{primary, support, weak}); err != nil {
		t.Fatal(err)
	}
	for file, source := range map[string]string{"primary.go": primarySource, "support.go": supportSource, "weak.go": weakSource} {
		if err := store.SaveContentFile("local", "assembly", file, []byte(source)); err != nil {
			t.Fatal(err)
		}
	}
	return store, repo, primary, support, weak
}

func assemblyStore(t *testing.T) *storage.IndexStore {
	t.Helper()
	store, err := storage.NewIndexStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func writeAssemblyRepo(t *testing.T, store *storage.IndexStore, repo string, sources map[string]string, symbols []parser.Symbol) {
	t.Helper()
	parts := strings.Split(repo, "/")
	if len(parts) != 2 {
		t.Fatalf("invalid fixture repo %q", repo)
	}
	languages := make(map[string]string, len(sources))
	for file := range sources {
		languages[file] = "go"
	}
	if err := store.ReplaceRepoIndex(parts[0], parts[1], parts[0], "", sources, languages, symbols); err != nil {
		t.Fatal(err)
	}
	for file, source := range sources {
		if err := store.SaveContentFile(parts[0], parts[1], file, []byte(source)); err != nil {
			t.Fatal(err)
		}
	}
}

func byteFiles(sources map[string]string) map[string][]byte {
	result := make(map[string][]byte, len(sources))
	for file, source := range sources {
		result[file] = []byte(source)
	}
	return result
}

func sectionIndex(sections []Section, symbolID string) int {
	for i, section := range sections {
		if section.SymbolID == symbolID {
			return i
		}
	}
	return -1
}

func containsFile(sections []Section, file string) bool {
	for _, section := range sections {
		if section.File == file {
			return true
		}
	}
	return false
}

func sectionsContainText(sections []Section, text string) bool {
	for _, section := range sections {
		if strings.Contains(section.Source, text) {
			return true
		}
	}
	return false
}

func containsAny(values []string, targets ...string) bool {
	for _, value := range values {
		for _, target := range targets {
			if value == target {
				return true
			}
		}
	}
	return false
}

func indexedSymbol(file, name, source string) parser.Symbol {
	return parser.Symbol{ID: parser.MakeSymbolID(file, name, parser.KindFunction), File: file, Name: name, QualifiedName: name, Kind: parser.KindFunction, Language: "go", Signature: fmt.Sprintf("func %s()", name), Line: 1, EndLine: strings.Count(source, "\n") + 1, ByteLength: int64(len(source)), ContentHash: parser.ComputeContentHash([]byte(source))}
}
func candidate(symbol parser.Symbol, tier string, score int, reason string) planner.Candidate {
	return planner.Candidate{ID: "candidate-" + symbol.ID + "-" + reason, SymbolID: symbol.ID, File: symbol.File, Line: symbol.Line, EndLine: symbol.EndLine, Name: symbol.Name, Kind: symbol.Kind, Tier: tier, Score: score, ReasonCodes: []string{reason}, EstimatedScope: symbol.EndLine - symbol.Line + 1}
}
func containsSection(sections []Section, symbolID string) bool {
	for _, section := range sections {
		if section.SymbolID == symbolID {
			return true
		}
	}
	return false
}
