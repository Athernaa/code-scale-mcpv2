package sufficiency

import (
	"testing"

	"github.com/Athernaa/code-scale-mcpv2/internal/planner"
)

func TestExactSymbolSatisfiesAfterCompleteAnchor(t *testing.T) {
	anchor := testCandidate("anchor", "primary", "exact_symbol_match")
	decision := New().Evaluate(Input{Plan: planner.Plan{TaskClass: "exact_symbol", TaskConfidence: "high", IndexState: "complete", Primary: []planner.Candidate{anchor}}, Stage: "anchor", Sections: []Section{completeSection(anchor)}})
	if decision.Status != StatusSufficient || decision.CanContinue || decision.EvaluatedAfterStage != "anchor" {
		t.Fatalf("exact symbol did not stop after anchor: %+v", decision)
	}
}

func TestLocalizedChangeRequiresCriticalDirectSupportThenStops(t *testing.T) {
	anchor := testCandidate("anchor", "primary", "exact_symbol_match")
	callee := testCandidate("callee", "supporting", "direct_callee")
	plan := planner.Plan{TaskClass: "localized_change", TaskConfidence: "high", IndexState: "complete", Primary: []planner.Candidate{anchor}, Supporting: []planner.Candidate{callee}}
	first := New().Evaluate(Input{Plan: plan, Stage: "anchor", Sections: []Section{completeSection(anchor)}})
	if first.Status != StatusNeedsMoreContext || !first.CanContinue {
		t.Fatalf("localized change did not request direct support: %+v", first)
	}
	second := New().Evaluate(Input{Plan: plan, Stage: "direct_support", Sections: []Section{completeSection(anchor), completeSection(callee)}})
	if second.Status != StatusSufficient || second.CanContinue || second.Coverage.CriticalSupportSatisfied != 1 {
		t.Fatalf("localized change did not stop after support: %+v", second)
	}
}

func TestCompleteZeroEdgeTraceCanBeSufficient(t *testing.T) {
	anchor := testCandidate("anchor", "primary", "exact_symbol_match")
	decision := New().Evaluate(Input{Plan: planner.Plan{TaskClass: "relationship_trace", TaskConfidence: "high", TraceDirection: "incoming", IndexState: "complete", Primary: []planner.Candidate{anchor}}, Stage: "anchor", Sections: []Section{completeSection(anchor)}})
	if decision.Status != StatusSufficient {
		t.Fatalf("complete zero-edge trace was blocked: %+v", decision)
	}
}

func TestHardBlockersNeverBecomeSufficient(t *testing.T) {
	anchor := testCandidate("anchor", "primary", "exact_symbol_match")
	base := Input{Plan: planner.Plan{TaskClass: "exact_symbol", TaskConfidence: "high", IndexState: "complete", Primary: []planner.Candidate{anchor}}, Stage: "anchor", Sections: []Section{completeSection(anchor)}}
	cases := []struct {
		name   string
		mutate func(*Input)
		reason string
	}{
		{"planner_truncated", func(input *Input) { input.Plan.Truncated = true }, "planner_truncated"},
		{"index_incomplete", func(input *Input) { input.Plan.IndexIncomplete = true }, "index_incomplete"},
		{"ambiguity", func(input *Input) {
			input.Plan.Ambiguities = []planner.Ambiguity{{Kind: "source_anchor", Query: "Save", CandidateCount: 2}}
		}, "source_ambiguity"},
		{"unresolved_high_signal", func(input *Input) { input.Plan.UnresolvedHighSignal = []string{"SaveUser"} }, "unresolved_high_signal_hint"},
		{"partial_anchor", func(input *Input) { input.Sections[0].Partial = true }, "required_source_partial"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := cloneInput(base)
			tc.mutate(&input)
			decision := New().Evaluate(input)
			if decision.Status == StatusSufficient || !hasReasonCode(decision, tc.reason) {
				t.Fatalf("hard blocker was not preserved: %+v", decision)
			}
		})
	}
}

func TestWeakBroadTaskIsIndeterminate(t *testing.T) {
	anchor := testCandidate("weak", "supporting", "lexical_fallback")
	decision := New().Evaluate(Input{Plan: planner.Plan{TaskClass: "broad_unknown", TaskConfidence: "low", AnchorStrength: "weak_lexical", IndexState: "complete", Supporting: []planner.Candidate{anchor}}, Stage: "direct_support", Sections: []Section{completeSection(anchor)}})
	if decision.Status != StatusIndeterminate || hasReasonCode(decision, "required_evidence_covered") {
		t.Fatalf("weak broad task claimed deterministic sufficiency: %+v", decision)
	}
}

func TestExternalProviderCannotSatisfyCrossResourceCoverage(t *testing.T) {
	anchor := testCandidate("caller", "primary", "framework_operation_match")
	provider := testCandidate("provider", "supporting", "framework_provider")
	provider.Authority = "external_unverified"
	decision := New().Evaluate(Input{Plan: planner.Plan{TaskClass: "cross_resource", TaskConfidence: "high", IndexState: "complete", Primary: []planner.Candidate{anchor}, Supporting: []planner.Candidate{provider}}, Stage: "direct_support", Sections: []Section{completeSection(anchor), completeSection(provider)}})
	if decision.Status != StatusBlocked || !hasReasonCode(decision, "provider_external_unverified") {
		t.Fatalf("external provider incorrectly satisfied coverage: %+v", decision)
	}
}

func TestExactSemanticOperationRequiresLocalProviderFact(t *testing.T) {
	call := testCandidate("call", "primary", "framework_operation_match")
	call.TargetResource, call.Authority = "inventory", "local_verified"
	input := Input{Plan: planner.Plan{TaskClass: "exact_semantic", TaskConfidence: "high", IndexState: "complete", Primary: []planner.Candidate{call}}, Stage: "anchor", Sections: []Section{completeSection(call)}}
	decision := New().Evaluate(input)
	if decision.Status != StatusBlocked || !hasReasonCode(decision, "required_provider_missing") {
		t.Fatalf("missing local provider was not blocked: %+v", decision)
	}
	provider := testCandidate("provider", "supporting", "framework_provider")
	provider.Authority = "local_verified"
	input.Plan.Supporting = []planner.Candidate{provider}
	input.Stage = "direct_support"
	input.Sections = append(input.Sections, completeSection(provider))
	decision = New().Evaluate(input)
	if decision.Status != StatusSufficient {
		t.Fatalf("verified provider did not satisfy exact semantic coverage: %+v", decision)
	}
}

func TestExactSymbolDoesNotRequireOptionalProviderContext(t *testing.T) {
	anchor := testCandidate("anchor", "primary", "exact_symbol_match")
	provider := testCandidate("provider", "supporting", "framework_provider")
	provider.Authority = "external_unverified"
	decision := New().Evaluate(Input{Plan: planner.Plan{TaskClass: "exact_symbol", TaskConfidence: "high", IndexState: "complete", Primary: []planner.Candidate{anchor}, Supporting: []planner.Candidate{provider}}, Stage: "anchor", Sections: []Section{completeSection(anchor)}})
	if decision.Status != StatusSufficient {
		t.Fatalf("exact symbol incorrectly required optional provider: %+v", decision)
	}
}

func TestUnrelatedInjectionCannotChangeProvenSufficiency(t *testing.T) {
	anchor := testCandidate("anchor", "primary", "exact_symbol_match")
	callee := testCandidate("callee", "supporting", "direct_callee")
	base := Input{Plan: planner.Plan{TaskClass: "localized_change", TaskConfidence: "high", IndexState: "complete", Primary: []planner.Candidate{anchor}, Supporting: []planner.Candidate{callee}}, Stage: "direct_support", Sections: []Section{completeSection(anchor), completeSection(callee)}}
	first := New().Evaluate(base)
	base.Plan.Peripheral = []planner.Candidate{testCandidate("unrelated", "peripheral", "lexical_fallback")}
	second := New().Evaluate(base)
	if first.Status != StatusSufficient || second.Status != StatusSufficient || first.Status != second.Status {
		t.Fatalf("unrelated candidate changed sufficiency: first=%+v second=%+v", first, second)
	}
	base.Plan.Truncated = true
	third := New().Evaluate(base)
	if third.Status == StatusSufficient || !hasReasonCode(third, "planner_truncated") {
		t.Fatalf("truncation improved sufficiency: %+v", third)
	}
}

func TestUnrelatedFrameworkDegradationDoesNotPoisonGenericAnchor(t *testing.T) {
	anchor := testCandidate("anchor", "primary", "exact_symbol_match")
	input := Input{Plan: planner.Plan{TaskClass: "exact_symbol", TaskConfidence: "high", IndexState: "complete", DegradedResources: []string{"unused_bad_resource"}, Primary: []planner.Candidate{anchor}}, Stage: "anchor", Sections: []Section{completeSection(anchor)}}
	if decision := New().Evaluate(input); decision.Status != StatusSufficient {
		t.Fatalf("unrelated degradation poisoned generic sufficiency: %+v", decision)
	}
	input.Plan.Primary[0].Resource = "unused_bad_resource"
	if decision := New().Evaluate(input); decision.Status != StatusBlocked || !hasReasonCode(decision, "relevant_framework_degraded") {
		t.Fatalf("relevant degradation did not block sufficiency: %+v", decision)
	}
}

func testCandidate(id, tier, reason string) planner.Candidate {
	return planner.Candidate{ID: id, SymbolID: id + "::symbol", File: id + ".go", Name: id, Kind: "function", Tier: tier, ReasonCodes: []string{reason}, Score: 100}
}

func completeSection(candidate planner.Candidate) Section {
	return Section{CandidateID: candidate.ID, SymbolID: candidate.SymbolID, File: candidate.File, Name: candidate.Name, Kind: candidate.Kind, Tier: candidate.Tier, Stage: candidateStageForTest(candidate), ReasonCodes: append([]string(nil), candidate.ReasonCodes...), SourceAvailable: true}
}

func candidateStageForTest(candidate planner.Candidate) string {
	if candidate.Tier == "primary" {
		return "anchor"
	}
	return "direct_support"
}

func cloneInput(input Input) Input {
	copyInput := input
	copyInput.Plan.Ambiguities = append([]planner.Ambiguity(nil), input.Plan.Ambiguities...)
	copyInput.Plan.UnresolvedHighSignal = append([]string(nil), input.Plan.UnresolvedHighSignal...)
	copyInput.Sections = append([]Section(nil), input.Sections...)
	return copyInput
}

func hasReasonCode(decision Decision, reason string) bool {
	for _, value := range decision.ReasonCodes {
		if value == reason {
			return true
		}
	}
	return false
}
