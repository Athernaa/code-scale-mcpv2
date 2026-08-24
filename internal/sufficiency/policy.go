package sufficiency

import (
	"sort"
	"strings"

	"github.com/Athernaa/code-scale-mcpv2/internal/planner"
)

type Policy struct{}

func New() Policy { return Policy{} }

func (Policy) Evaluate(input Input) Decision {
	decision := Decision{Status: StatusNeedsMoreContext, CanContinue: false, EvaluatedAfterStage: input.Stage}
	sections := make(map[string]Section, len(input.Sections))
	for _, section := range input.Sections {
		sections[section.CandidateID] = section
	}
	stageRank := stageRank(input.Stage)
	taskClass := effectiveTaskClass(input)

	if input.Plan.IndexIncomplete || (input.Plan.IndexState != "" && input.Plan.IndexState != "complete") {
		return blocked(input, decision, "index_incomplete")
	}
	if input.Plan.Truncated {
		return blocked(input, decision, "planner_truncated")
	}
	if relevantAmbiguity(input.Plan.Ambiguities, input.FocusSymbolID) {
		return blocked(input, decision, "source_ambiguity")
	}
	if len(input.Plan.UnresolvedHighSignal) > 0 {
		return blocked(input, decision, "unresolved_high_signal_hint")
	}
	if relevantDegradation(input) {
		return blocked(input, decision, "relevant_framework_degraded")
	}

	anchorCandidates := anchorCandidates(input.Plan, input.FocusSymbolID)
	decision.Coverage.AnchorsRequired = 1
	anchorSatisfied := false
	for _, candidate := range anchorCandidates {
		if section, ok := sections[candidate.ID]; ok {
			anchorSatisfied = true
			decision.Coverage.AnchorsSatisfied = 1
			if !completeSource(section) {
				decision = missingRequired(input, decision, candidate, "anchor", sourceReason(section))
				return finalizeIncomplete(input, decision, stageRank, candidateStage(candidate))
			}
			break
		}
	}
	if !anchorSatisfied {
		decision = addMissing(decision, Missing{Kind: "anchor", Reason: "required_anchor_missing"})
		if input.Plan.AnchorStrength == "weak_lexical" || input.Plan.TaskClass == "broad_unknown" || input.Plan.TaskConfidence == "low" {
			decision.Status = StatusIndeterminate
			decision.ReasonCodes = addReason(decision.ReasonCodes, "low_confidence_weak_anchor")
			decision.CanContinue = hasFutureStages(input.Plan, stageRank)
			return decision
		}
		return finalizeIncomplete(input, decision, stageRank, 0)
	}

	if taskClass == "broad_unknown" || input.Plan.BroadIntent || input.Plan.AnchorStrength == "weak_lexical" {
		decision.Status = StatusIndeterminate
		decision.ReasonCodes = addReason(decision.ReasonCodes, "broad_task_open_ended")
		decision.CanContinue = hasFutureStages(input.Plan, stageRank)
		return decision
	}
	if taskClass == "exact_symbol" || taskClass == "exact_semantic" {
		if taskClass == "exact_semantic" && providerRequirementNeeded(input) {
			providers := requiredCandidates(input).providers
			decision.Coverage.ProvidersRequired = len(providers)
			if len(providers) == 0 {
				return blocked(input, decision, providerBlockReason(input))
			}
			for _, provider := range providers {
				section, ok := sections[provider.ID]
				if !ok {
					if candidateStage(provider) > stageRank {
						decision = addMissing(decision, Missing{Kind: "provider", CandidateID: provider.ID, Reason: "required_provider_missing", Resource: provider.Resource, TargetResource: provider.TargetResource})
						decision.Status = StatusNeedsMoreContext
						decision.CanContinue = true
						decision.ReasonCodes = addReason(decision.ReasonCodes, "required_provider_missing")
						return decision
					}
					return blocked(input, decision, "required_provider_missing")
				}
				if !completeSource(section) {
					return blocked(input, decision, sourceReason(section))
				}
				decision.Coverage.ProvidersSatisfied++
			}
		}
		if taskClass == "exact_semantic" && providerBlocked(input, sections, decision) {
			return blocked(input, decision, providerBlockReason(input))
		}
		decision.Status = StatusSufficient
		decision.ReasonCodes = addReason(decision.ReasonCodes, "anchor_covered")
		return decision
	}

	required := requiredCandidates(input)
	decision.Coverage.CriticalSupportRequired = len(required.critical)
	decision.Coverage.CriticalSupportSatisfied = 0
	decision.Coverage.ProvidersRequired = len(required.providers)
	decision.Coverage.ProvidersSatisfied = 0
	decision.Coverage.FlowPeersRequired = len(required.flow)
	decision.Coverage.FlowPeersSatisfied = 0
	if input.IncludeImpact {
		decision.Coverage.ImpactRequired = countImpact(required.critical)
	}

	missingFuture := false
	for _, candidate := range required.critical {
		section, ok := sections[candidate.ID]
		if !ok {
			if candidateStage(candidate) > stageRank {
				missingFuture = true
				decision = addMissing(decision, Missing{Kind: supportKind(candidate), CandidateID: candidate.ID, Reason: "required_direct_support_missing", Resource: candidate.Resource, TargetResource: candidate.TargetResource})
				continue
			}
			decision = missingRequired(input, decision, candidate, supportKind(candidate), missingReason(input))
			continue
		}
		if !section.SourceAvailable {
			decision = missingRequired(input, decision, candidate, supportKind(candidate), "required_source_unavailable")
			continue
		}
		if section.Partial || section.OutlineTruncated || section.ContentKind == "file_outline" {
			decision = missingRequired(input, decision, candidate, supportKind(candidate), "required_source_partial")
			continue
		}
		decision.Coverage.CriticalSupportSatisfied++
		if isProvider(candidate) {
			decision.Coverage.ProvidersSatisfied++
		}
		if isFlow(candidate) {
			decision.Coverage.FlowPeersSatisfied++
		}
		if hasReason(candidate, "impact_direct") {
			decision.Coverage.ImpactSatisfied++
		}
	}

	if missingFuture {
		decision.Status = StatusNeedsMoreContext
		decision.CanContinue = true
		decision.ReasonCodes = addReason(decision.ReasonCodes, "required_direct_support_missing")
		return decision
	}
	if len(decision.Missing) > 0 {
		return finalizeIncomplete(input, decision, stageRank, 1)
	}
	if taskClass == "relationship_trace" || taskClass == "cross_resource" || taskClass == "localized_change" {
		if providerRequirementNeeded(input) && len(required.providers) == 0 {
			return blocked(input, decision, providerBlockReason(input))
		}
		if providerBlocked(input, sections, decision) {
			return blocked(input, decision, providerBlockReason(input))
		}
		decision.Status = StatusSufficient
		decision.ReasonCodes = addReason(decision.ReasonCodes, "required_evidence_covered")
		return decision
	}
	decision.Status = StatusSufficient
	decision.ReasonCodes = addReason(decision.ReasonCodes, "required_evidence_covered")
	return decision
}

func effectiveTaskClass(input Input) string {
	if input.Plan.AnchorStrength == "explicit_focus" && !input.Plan.BroadIntent && input.Plan.RequestedTaskClass != "" {
		return input.Plan.RequestedTaskClass
	}
	return input.Plan.TaskClass
}

type requiredSet struct {
	critical  []planner.Candidate
	providers []planner.Candidate
	flow      []planner.Candidate
}

func requiredCandidates(input Input) requiredSet {
	result := requiredSet{}
	all := append(append(append([]planner.Candidate{}, input.Plan.Primary...), input.Plan.Supporting...), input.Plan.Peripheral...)
	seen := map[string]bool{}
	for _, candidate := range all {
		if seen[candidate.ID] {
			continue
		}
		seen[candidate.ID] = true
		if candidate.Tier == "primary" {
			continue
		}
		if !relevantDirection(candidate, input.Plan.TraceDirection) {
			continue
		}
		if planner.IsCriticalSupportReason(firstCriticalReason(candidate)) {
			result.critical = append(result.critical, candidate)
			if isProvider(candidate) {
				result.providers = append(result.providers, candidate)
			}
			if isFlow(candidate) {
				result.flow = append(result.flow, candidate)
			}
		}
	}
	sort.Slice(result.critical, func(i, j int) bool { return result.critical[i].ID < result.critical[j].ID })
	return result
}

func anchorCandidates(plan planner.Plan, focus string) []planner.Candidate {
	all := append(append([]planner.Candidate{}, plan.Primary...), plan.Supporting...)
	if focus != "" {
		result := make([]planner.Candidate, 0, 1)
		for _, candidate := range all {
			if candidate.SymbolID == focus {
				result = append(result, candidate)
			}
		}
		if len(result) > 0 {
			return result
		}
	}
	result := make([]planner.Candidate, 0, len(all))
	for _, candidate := range all {
		if candidate.Tier == "primary" || hasAnchorReason(candidate) {
			result = append(result, candidate)
		}
	}
	return result
}

func hasAnchorReason(candidate planner.Candidate) bool {
	for _, reason := range candidate.ReasonCodes {
		switch reason {
		case "exact_symbol_match", "exact_semantic_match", "framework_operation_match", "explicit_focus", "weak_exact_match", "lexical_fallback":
			return true
		}
	}
	return false
}

func relevantAmbiguity(values []planner.Ambiguity, focus string) bool {
	if len(values) == 0 {
		return false
	}
	if focus == "" {
		return true
	}
	for _, value := range values {
		if value.Kind != "source_anchor" {
			return true
		}
	}
	return false
}

func relevantDegradation(input Input) bool {
	if len(input.Plan.DegradedResources) == 0 {
		return false
	}
	degraded := map[string]bool{}
	for _, value := range input.Plan.DegradedResources {
		degraded[strings.ToLower(value)] = true
	}
	if degraded[strings.ToLower(input.FocusResource)] {
		return true
	}
	for _, candidate := range append(append([]planner.Candidate{}, input.Plan.Primary...), input.Plan.Supporting...) {
		values := append([]string{candidate.Resource, candidate.TargetResource}, candidate.Resources...)
		values = append(values, candidate.TargetResources...)
		for _, value := range values {
			if degraded[strings.ToLower(value)] {
				return true
			}
		}
	}
	for _, section := range input.Sections {
		if degraded[strings.ToLower(section.Resource)] || degraded[strings.ToLower(section.TargetResource)] {
			return true
		}
	}
	return false
}

func providerBlocked(input Input, sections map[string]Section, decision Decision) bool {
	for _, candidate := range requiredCandidates(input).providers {
		if candidate.Authority == "external_unverified" || candidate.Authority == "local_ambiguous" || candidate.Authority == "local_api_missing" {
			return true
		}
		for _, authority := range candidate.Authorities {
			if authority == "external_unverified" || authority == "local_ambiguous" || authority == "local_api_missing" {
				return true
			}
		}
		if section, ok := sections[candidate.ID]; ok && !completeSource(section) {
			return true
		}
	}
	return false
}

func completeSource(section Section) bool {
	return section.SourceAvailable && !section.Partial && !section.OutlineTruncated && section.ContentKind != "file_outline"
}

func sourceReason(section Section) string {
	if !section.SourceAvailable {
		return "required_source_unavailable"
	}
	return "required_source_partial"
}

func providerRequirementNeeded(input Input) bool {
	taskClass := effectiveTaskClass(input)
	if taskClass != "exact_semantic" && taskClass != "cross_resource" && taskClass != "localized_change" {
		return false
	}
	for _, candidate := range append(append(append([]planner.Candidate{}, input.Plan.Primary...), input.Plan.Supporting...), input.Plan.Peripheral...) {
		if requiresProvider(candidate) {
			return true
		}
	}
	return false
}

func requiresProvider(candidate planner.Candidate) bool {
	return hasReason(candidate, "framework_operation_match") && candidate.TargetResource != ""
}

func providerBlockReason(input Input) string {
	for _, candidate := range requiredCandidates(input).providers {
		switch candidate.Authority {
		case "external_unverified":
			return "provider_external_unverified"
		case "local_ambiguous":
			return "provider_local_ambiguous"
		case "local_api_missing":
			return "provider_local_api_missing"
		}
	}
	for _, candidate := range append(append(append([]planner.Candidate{}, input.Plan.Primary...), input.Plan.Supporting...), input.Plan.Peripheral...) {
		if !requiresProvider(candidate) {
			continue
		}
		switch candidate.Authority {
		case "external_unverified":
			return "provider_external_unverified"
		case "local_ambiguous":
			return "provider_local_ambiguous"
		case "local_api_missing":
			return "provider_local_api_missing"
		}
	}
	return "required_provider_missing"
}

func relevantDirection(candidate planner.Candidate, direction string) bool {
	if direction == "" || direction == "both" {
		return true
	}
	if direction == "incoming" {
		return hasReason(candidate, "direct_caller") || hasReason(candidate, "impact_direct") || isFlow(candidate)
	}
	if direction == "outgoing" {
		return hasReason(candidate, "direct_callee") || isProvider(candidate) || isFlow(candidate)
	}
	return true
}

func firstCriticalReason(candidate planner.Candidate) string {
	for _, reason := range candidate.ReasonCodes {
		if planner.IsCriticalSupportReason(reason) {
			return reason
		}
	}
	return ""
}

func isProvider(candidate planner.Candidate) bool {
	return hasReason(candidate, "framework_provider") || hasReason(candidate, "export_provider")
}

func isFlow(candidate planner.Candidate) bool {
	return hasReason(candidate, "event_peer") || hasReason(candidate, "callback_peer") || hasReason(candidate, "export_provider")
}

func supportKind(candidate planner.Candidate) string {
	if isProvider(candidate) {
		return "provider"
	}
	if isFlow(candidate) {
		return "flow_peer"
	}
	return "critical_support"
}

func countImpact(values []planner.Candidate) int {
	count := 0
	for _, candidate := range values {
		if hasReason(candidate, "impact_direct") {
			count++
		}
	}
	return count
}

func hasReason(candidate planner.Candidate, reason string) bool {
	for _, value := range candidate.ReasonCodes {
		if value == reason {
			return true
		}
	}
	return false
}

func candidateStage(candidate planner.Candidate) int {
	if candidate.Tier == "primary" || hasAnchorReason(candidate) {
		return 0
	}
	if firstCriticalReason(candidate) != "" {
		return 1
	}
	if candidate.Tier == "peripheral" {
		return 3
	}
	return 2
}

func stageRank(stage string) int {
	switch stage {
	case "anchor":
		return 0
	case "direct_support":
		return 1
	case "domain_support":
		return 2
	case "peripheral":
		return 3
	default:
		return 3
	}
}

func hasFutureStages(plan planner.Plan, current int) bool {
	for _, candidate := range append(append(append([]planner.Candidate{}, plan.Primary...), plan.Supporting...), plan.Peripheral...) {
		if candidateStage(candidate) > current {
			return true
		}
	}
	return false
}

func missingReason(input Input) string {
	if input.Omitted.SourceUnavailable > 0 {
		return "required_source_unavailable"
	}
	if input.Omitted.SourceReadLimit > 0 {
		return "source_read_limit"
	}
	if input.Omitted.TokenBudget > 0 {
		return "token_budget_exhausted"
	}
	return "required_direct_support_missing"
}

func missingRequired(input Input, decision Decision, candidate planner.Candidate, kind, reason string) Decision {
	return addMissing(decision, Missing{Kind: kind, CandidateID: candidate.ID, Reason: reason, Resource: candidate.Resource, TargetResource: candidate.TargetResource})
}

func addMissing(decision Decision, missing Missing) Decision {
	for _, value := range decision.Missing {
		if value.CandidateID == missing.CandidateID && value.Kind == missing.Kind {
			return decision
		}
	}
	if len(decision.Missing) < MaxMissing {
		decision.Missing = append(decision.Missing, missing)
	}
	sort.Slice(decision.Missing, func(i, j int) bool {
		if decision.Missing[i].Kind != decision.Missing[j].Kind {
			return decision.Missing[i].Kind < decision.Missing[j].Kind
		}
		return decision.Missing[i].CandidateID < decision.Missing[j].CandidateID
	})
	decision.ReasonCodes = addReason(decision.ReasonCodes, missing.Reason)
	return decision
}

func addReason(values []string, reason string) []string {
	for _, value := range values {
		if value == reason {
			return values
		}
	}
	if len(values) < MaxReasonCodes {
		values = append(values, reason)
		sort.Strings(values)
	}
	return values
}

func blocked(input Input, decision Decision, reason string) Decision {
	decision.Status = StatusBlocked
	decision.CanContinue = false
	decision.ReasonCodes = addReason(decision.ReasonCodes, reason)
	return decision
}

func finalizeIncomplete(input Input, decision Decision, current, requiredStage int) Decision {
	if requiredStage > current && hasFutureStages(input.Plan, current) {
		decision.Status = StatusNeedsMoreContext
		decision.CanContinue = true
		return decision
	}
	decision.Status = StatusBlocked
	decision.CanContinue = false
	return decision
}
