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
	anchor.TargetResource = "inventory"
	provider := testCandidate("provider", "supporting", "framework_provider")
	provider.Resource, provider.Authority = "inventory", "external_unverified"
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

func TestPrimaryTierMixedCriticalEvidenceRemainsRequired(t *testing.T) {
	focus := testCandidate("focus", "primary", "explicit_focus")
	focus.SymbolID = "focus::symbol"
	second := testCandidate("second", "primary", "exact_symbol_match")
	second.ReasonCodes = []string{"exact_symbol_match", "direct_callee", "direct_callee"}
	plan := planner.Plan{TaskClass: "localized_change", TaskConfidence: "high", IndexState: "complete", Primary: []planner.Candidate{focus, second}}
	input := Input{Plan: plan, FocusSymbolID: focus.SymbolID, FocusAnchor: "focus-anchor", Stage: "anchor", Sections: []Section{completeSection(focus)}}
	blocked := New().Evaluate(input)
	if blocked.Status == StatusSufficient || blocked.Coverage.CriticalSupportRequired != 1 {
		t.Fatalf("primary-tier critical evidence was erased: %+v", blocked)
	}
	input.Sections = append(input.Sections, completeSection(second))
	complete := New().Evaluate(input)
	if complete.Status != StatusSufficient || complete.Coverage.CriticalSupportSatisfied != 1 {
		t.Fatalf("complete mixed primary support did not satisfy coverage: %+v", complete)
	}
}

func TestPrimaryTierMixedProviderAndFlowEvidenceRemainRequired(t *testing.T) {
	for _, reason := range []string{"framework_provider", "event_peer", "callback_peer", "export_provider"} {
		t.Run(reason, func(t *testing.T) {
			focus := testCandidate("focus-"+reason, "primary", "explicit_focus")
			focus.SymbolID = "focus-" + reason + "::symbol"
			second := testCandidate("second-"+reason, "primary", "exact_symbol_match")
			second.ReasonCodes = []string{"exact_symbol_match", reason}
			if reason == "framework_provider" || reason == "export_provider" {
				second.Resource, second.TargetResource, second.Authority = "provider", "caller", "local_verified"
			}
			plan := planner.Plan{TaskClass: "localized_change", TaskConfidence: "high", IndexState: "complete", Primary: []planner.Candidate{focus, second}}
			input := Input{Plan: plan, FocusSymbolID: focus.SymbolID, Stage: "anchor", Sections: []Section{completeSection(focus)}}
			if decision := New().Evaluate(input); decision.Status == StatusSufficient {
				t.Fatalf("mixed %s evidence disappeared with primary tier: %+v", reason, decision)
			}
			input.Sections = append(input.Sections, completeSection(second))
			if decision := New().Evaluate(input); decision.Status != StatusSufficient {
				t.Fatalf("mixed %s evidence did not satisfy after source arrival: %+v", reason, decision)
			}
		})
	}
}

func TestFocusOnlyResolvesItsOwnAmbiguity(t *testing.T) {
	focus := testCandidate("focus", "primary", "explicit_focus")
	focus.SymbolID = "focus::symbol"
	base := Input{Plan: planner.Plan{TaskClass: "exact_symbol", TaskConfidence: "high", IndexState: "complete", Primary: []planner.Candidate{focus}}, FocusSymbolID: focus.SymbolID, FocusAnchor: "focus-anchor", Stage: "anchor", Sections: []Section{completeSection(focus)}}
	base.Plan.Ambiguities = []planner.Ambiguity{{Kind: "source_anchor", Query: "LoadCharacter", CandidateCount: 2, AnchorIDs: []string{"focus-anchor", "other-anchor"}}}
	if decision := New().Evaluate(base); decision.Status != StatusSufficient {
		t.Fatalf("focus did not resolve its own ambiguity: %+v", decision)
	}
	base.Plan.Ambiguities = append(base.Plan.Ambiguities, planner.Ambiguity{Kind: "source_anchor", Query: "GetPlayer", CandidateCount: 2, AnchorIDs: []string{"unrelated-a", "unrelated-b"}})
	if decision := New().Evaluate(base); decision.Status == StatusSufficient {
		t.Fatalf("focus globally suppressed unrelated ambiguity: %+v", decision)
	}
}

func TestCrossResourceNeedsActualPeerCoverage(t *testing.T) {
	anchor := testCandidate("caller", "primary", "framework_operation_match")
	anchor.Resource, anchor.TargetResource, anchor.Authority = "jobs", "inventory", "local_verified"
	provider := testCandidate("provider", "supporting", "framework_provider")
	provider.Resource, provider.Authority = "inventory", "local_verified"
	base := Input{Plan: planner.Plan{TaskClass: "cross_resource", TaskConfidence: "high", IndexState: "complete", Primary: []planner.Candidate{anchor}}, Stage: "anchor", Sections: []Section{completeSection(anchor)}}
	if decision := New().Evaluate(base); decision.Status == StatusSufficient {
		t.Fatalf("cross-resource anchor alone was sufficient: %+v", decision)
	}
	base.Plan.Supporting = []planner.Candidate{provider}
	base.Stage = "direct_support"
	base.Sections = append(base.Sections, completeSection(provider))
	if decision := New().Evaluate(base); decision.Status != StatusSufficient || decision.Coverage.CrossResourceSatisfied != 1 {
		t.Fatalf("verified cross-resource peer did not satisfy coverage: %+v", decision)
	}
	for _, reason := range []string{"event_peer", "callback_peer", "export_provider"} {
		flow := testCandidate("flow-"+reason, "supporting", reason)
		flow.Resource = "inventory"
		flowAnchor := anchor
		flowAnchor.ReasonCodes = []string{"event_trigger"}
		flowInput := base
		flowInput.Plan.Primary = []planner.Candidate{flowAnchor}
		flowInput.Plan.Supporting = []planner.Candidate{flow}
		flowInput.Sections = []Section{completeSection(flowAnchor), completeSection(flow)}
		if decision := New().Evaluate(flowInput); decision.Status != StatusSufficient {
			t.Fatalf("cross-resource %s peer did not satisfy coverage: %+v", reason, decision)
		}
	}
	base.Plan.Supporting[0].Authority = "external_unverified"
	if decision := New().Evaluate(base); decision.Status == StatusSufficient {
		t.Fatalf("external cross-resource provider was sufficient: %+v", decision)
	}
	base.Plan.Supporting = nil
	anchor.TargetResource = ""
	if decision := New().Evaluate(base); decision.Status == StatusSufficient {
		t.Fatalf("missing target resource identity was sufficient: %+v", decision)
	}
}

func TestMixedProviderAuthorityIsMonotonic(t *testing.T) {
	anchor := testCandidate("caller", "primary", "framework_operation_match")
	anchor.Resource, anchor.TargetResource, anchor.Authority = "jobs", "inventory", "local_verified"
	provider := testCandidate("provider", "supporting", "framework_provider")
	provider.Resource, provider.Authority, provider.Authorities = "inventory", "local_verified", []string{"external_unverified", "local_verified"}
	input := Input{Plan: planner.Plan{TaskClass: "cross_resource", TaskConfidence: "high", IndexState: "complete", Primary: []planner.Candidate{anchor}, Supporting: []planner.Candidate{provider}}, Stage: "direct_support", Sections: []Section{completeSection(anchor), completeSection(provider)}}
	verified := New().Evaluate(input)
	if verified.Status != StatusSufficient {
		t.Fatalf("mixed authority erased verified provider: %+v", verified)
	}
	input.Plan.Supporting[0].Authorities = []string{"external_unverified", "local_verified", "external_unverified"}
	if duplicate := New().Evaluate(input); duplicate.Status != StatusSufficient {
		t.Fatalf("duplicate weak authority changed verified result: %+v", duplicate)
	}
	input.Plan.Supporting[0].Authority = "external_unverified"
	input.Plan.Supporting[0].Authorities = []string{"external_unverified"}
	if downgraded := New().Evaluate(input); downgraded.Status == StatusSufficient {
		t.Fatalf("authority downgrade improved sufficiency: %+v", downgraded)
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
