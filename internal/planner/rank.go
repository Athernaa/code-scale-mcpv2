package planner

import (
	"sort"
	"strings"

	"github.com/Athernaa/code-scale-mcpv2/internal/semantic"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic/framework"
)

// rankPolicyVersion is intentionally transient. Ranking is query-time policy,
// not persisted semantic data.
const rankPolicyVersion = "7.1"

const (
	minPlannerScore  = 0
	maxPlannerScore  = 10000
	maxCorroboration = 900
)

// taskProfile keeps task direction and evidence preference in one place. It
// is deliberately small: Phase 7.1 ranks trusted evidence; it does not try to
// infer arbitrary natural-language intent.
type taskProfile struct {
	target         int
	callee         int
	caller         int
	reference      int
	imported       int
	provider       int
	flow           int
	operation      int
	impact         int
	allowDiversity bool
}

// RankPolicy is the single ranking seam for seed priority, candidate scores,
// relationship relevance, and tier assignment.
type RankPolicy struct{}

func NewRankPolicy() RankPolicy { return RankPolicy{} }

func (RankPolicy) Version() string { return rankPolicyVersion }

func (RankPolicy) profile(intent TaskIntent) taskProfile {
	profile := taskProfile{
		target: 1000, callee: 700, caller: 700, reference: 280,
		imported: 220, provider: 650, flow: 700, operation: 850,
		impact: 360,
	}
	switch intent.TaskClass {
	case "exact_symbol":
		profile.target, profile.callee, profile.caller = 1100, 40, 40
		profile.reference, profile.imported, profile.provider, profile.flow = 0, 0, 0, 30
	case "exact_semantic":
		profile.target, profile.operation, profile.flow, profile.provider = 1100, 1150, 900, 700
		profile.callee, profile.caller, profile.reference, profile.imported = 140, 140, 40, 30
	case "localized_change":
		profile.target, profile.callee, profile.caller = 1150, 760, 760
		profile.reference, profile.imported, profile.provider, profile.flow = 300, 240, 720, 650
		profile.impact = 420
	case "relationship_trace":
		profile.target, profile.callee, profile.caller = 950, 760, 760
		profile.reference, profile.imported, profile.provider, profile.flow = 260, 200, 700, 900
		profile.impact = 400
	case "cross_resource":
		profile.target, profile.callee, profile.caller = 900, 520, 520
		profile.reference, profile.imported, profile.provider, profile.flow = 120, 100, 850, 1150
		profile.impact = 360
	case "broad_unknown":
		profile.target, profile.callee, profile.caller = 800, 340, 340
		profile.reference, profile.imported, profile.provider, profile.flow = 160, 180, 500, 520
		profile.operation = 650
		profile.allowDiversity = true
	}
	return profile
}

// ScoreBreakdown is internal except for the bounded debug projection. Every
// score component is derived from evidence or an explicit request scope.
type ScoreBreakdown struct {
	Anchor              int
	TaskAlignment       int
	RelationshipQuality int
	AuthorityQuality    int
	FocusRelevance      int
	Corroboration       int
	Locality            int
	DistancePenalty     int
	UncertaintyPenalty  int
	FallbackPenalty     int
	RedundancyPenalty   int
	Total               int
}

type scoredEvidence struct {
	evidence  Evidence
	value     int
	category  string
	anchor    int
	task      int
	relation  int
	authority int
	uncertain int
	fallback  int
	distance  int
}

// ScoreAccumulator ranks already-normalized candidates. It never queries the
// store and evaluates authority per evidence, so mixed candidates cannot turn
// external evidence into locally verified evidence.
func (policy RankPolicy) ScoreAccumulator(a *candidateAccumulator, intent TaskIntent, focusFile, focusResource string) ScoreBreakdown {
	profile := policy.profile(intent)
	items := make([]scoredEvidence, 0, len(a.evidenceByID))
	for _, evidence := range a.evidenceByID {
		items = append(items, policy.scoreEvidence(evidence, intent, profile))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].value != items[j].value {
			return items[i].value > items[j].value
		}
		left := evidenceSortKey(items[i].evidence)
		right := evidenceSortKey(items[j].evidence)
		return left < right
	})

	breakdown := ScoreBreakdown{}
	seenCategories := map[string]bool{}
	for index, item := range items {
		factorNumerator, factorDenominator := 1, 1
		if index > 0 {
			if !seenCategories[item.category] {
				factorNumerator, factorDenominator = 1, 5
			} else {
				factorNumerator, factorDenominator = 1, 50
			}
			// Multiple exact/weak anchors and repeated relationship facts
			// are corroboration, not a linear score multiplier.
			breakdown.Corroboration += bounded(item.value*factorNumerator/factorDenominator, 0, 300)
		}
		breakdown.Anchor += item.anchor * factorNumerator / factorDenominator
		seenCategories[item.category] = true
		breakdown.TaskAlignment += item.task * factorNumerator / factorDenominator
		breakdown.RelationshipQuality += item.relation * factorNumerator / factorDenominator
		breakdown.AuthorityQuality += item.authority * factorNumerator / factorDenominator
		breakdown.UncertaintyPenalty += item.uncertain * factorNumerator / factorDenominator
		breakdown.FallbackPenalty += item.fallback * factorNumerator / factorDenominator
		breakdown.DistancePenalty += item.distance * factorNumerator / factorDenominator
		if index >= 11 {
			// The remaining entries still contribute to reason preservation,
			// but never create unbounded ranking gain.
			break
		}
	}
	if breakdown.Corroboration > maxCorroboration {
		breakdown.Corroboration = maxCorroboration
	}

	if focusFile != "" && normalizePath(focusFile) == normalizePath(a.file) {
		breakdown.FocusRelevance += 280
	}
	if focusResource != "" {
		if a.resources[focusResource] {
			breakdown.FocusRelevance += 260
		} else if a.targetResources[focusResource] {
			breakdown.FocusRelevance += 180
		}
	}
	if hasReason(a, "explicit_focus") {
		// Explicit user focus is authority, not an inferred relevance hint.
		// The final comparator also gives it a deterministic precedence when a
		// saturated score ties another candidate.
		breakdown.FocusRelevance += 2200
	}
	if a.symbol == nil {
		breakdown.UncertaintyPenalty += 260
	}
	if a.distance > 1 {
		breakdown.DistancePenalty += distancePenalty(a.distance) - distancePenalty(1)
	}
	if profile.allowDiversity && isWeakAccumulator(a) {
		// This small locality term only stabilizes broad weak candidates. It
		// is never used to make a weak candidate outrank direct evidence.
		if a.file != "" {
			breakdown.Locality += 20
		}
	}

	breakdown.Corroboration = bounded(breakdown.Corroboration, 0, maxCorroboration)
	breakdown.DistancePenalty = bounded(breakdown.DistancePenalty, 0, 1800)
	breakdown.UncertaintyPenalty = bounded(breakdown.UncertaintyPenalty, 0, 1800)
	breakdown.FallbackPenalty = bounded(breakdown.FallbackPenalty, 0, 900)
	breakdown.Total = breakdown.Anchor + breakdown.TaskAlignment + breakdown.RelationshipQuality + breakdown.AuthorityQuality + breakdown.FocusRelevance + breakdown.Corroboration + breakdown.Locality - breakdown.DistancePenalty - breakdown.UncertaintyPenalty - breakdown.FallbackPenalty - breakdown.RedundancyPenalty
	breakdown.Total = bounded(breakdown.Total, minPlannerScore, maxPlannerScore)
	return breakdown
}

func (policy RankPolicy) scoreEvidence(evidence Evidence, intent TaskIntent, profile taskProfile) scoredEvidence {
	item := scoredEvidence{evidence: evidence, category: evidenceCategory(evidence)}
	item.anchor = anchorContribution(evidence.NoteCode)
	item.relation = relationshipContribution(evidence.NoteCode)
	item.task = taskContribution(evidence.NoteCode, intent, profile)
	item.authority = authorityContribution(evidence.Authority)
	item.distance = distancePenalty(evidence.Depth)
	if evidence.Dynamic {
		item.uncertain += 520
	}
	if evidence.Authority == framework.ProviderStatusLocalAmbiguous || evidence.Authority == framework.ProviderStatusLocalMissing {
		item.uncertain += 420
	}
	if evidence.NoteCode == "lexical_fallback" || evidence.NoteCode == "broad_entry_point" {
		item.fallback += 180
	}
	item.value = item.anchor + item.relation + item.task + item.authority - item.distance - item.uncertain - item.fallback
	if item.value < 1 {
		item.value = 1
	}
	return item
}

func anchorContribution(reason string) int {
	switch reason {
	case "explicit_focus":
		return 5200
	case "exact_symbol_match":
		return 4500
	case "exact_semantic_match":
		return 4300
	case "framework_operation_match":
		return 4400
	case "weak_exact_match":
		return 2750
	case "exact_file_match":
		return 950
	case "focus_file", "same_resource":
		return 0
	default:
		return 0
	}
}

func relationshipContribution(reason string) int {
	switch reason {
	case "direct_callee", "direct_caller":
		return 2700
	case "export_provider", "framework_provider":
		return 2650
	case "event_peer", "callback_peer":
		return 2500
	case "direct_semantic":
		return 2050
	case "direct_reference":
		return 1450
	case "direct_import":
		return 1200
	case "usage_match":
		return 520
	case "impact_direct":
		return 1050
	case "impact_transitive":
		return 450
	}
	// Entity kinds are retained separately from NoteCode, but are not graph
	// relationships. Only known relationship reasons receive this component;
	// unknown kinds must not masquerade as direct edges.
	return 0
}

func taskContribution(reason string, intent TaskIntent, profile taskProfile) int {
	switch reason {
	case "explicit_focus", "exact_symbol_match", "weak_exact_match", "exact_semantic_match":
		return profile.target
	case "framework_operation_match":
		return profile.operation
	case "direct_callee":
		if intent.TraceDirection == "incoming" {
			return profile.impact
		}
		return profile.callee
	case "direct_caller":
		if intent.TraceDirection == "outgoing" && !strings.Contains(strings.ToLower(intent.RawTask), "include") {
			return profile.impact
		}
		return profile.caller
	case "direct_reference":
		return profile.reference
	case "direct_import":
		return profile.imported
	case "export_provider", "framework_provider":
		return profile.provider
	case "event_peer", "callback_peer":
		return profile.flow
	case "impact_direct", "impact_transitive":
		return profile.impact
	case "lexical_fallback", "broad_entry_point":
		return 80
	}
	return 0
}

func authorityContribution(authority string) int {
	switch authority {
	case framework.ProviderStatusLocalVerified:
		return 380
	case framework.ProviderStatusExternal:
		return -220
	case framework.ProviderStatusLocalAmbiguous:
		return -420
	case framework.ProviderStatusLocalMissing:
		return -380
	default:
		return 0
	}
}

func distancePenalty(depth int) int {
	switch {
	case depth <= 0:
		return 0
	case depth == 1:
		return 220
	default:
		return 220 + (depth-1)*300
	}
}

func evidenceCategory(evidence Evidence) string {
	switch evidence.NoteCode {
	case "explicit_focus", "exact_symbol_match", "weak_exact_match", "exact_semantic_match", "framework_operation_match":
		return "anchor"
	case "direct_callee", "direct_caller", "direct_reference", "direct_import":
		return "generic_relationship"
	case "event_peer":
		return "event_flow"
	case "callback_peer":
		return "callback_flow"
	case "export_provider":
		return "export_flow"
	case "framework_provider":
		return "framework_flow"
	case "impact_direct", "impact_transitive":
		return "impact"
	case "lexical_fallback", "broad_entry_point":
		return "fallback"
	default:
		return evidence.NoteCode
	}
}

func evidenceSortKey(evidence Evidence) string {
	return evidence.NoteCode + "\x00" + evidence.SourceID + "\x00" + evidence.RelationshipID + "\x00" + evidence.Relationship
}

func (policy RankPolicy) SeedScore(item plannerSeedEntity, a *candidateAccumulator, intent TaskIntent, focusFile, focusResource string) int {
	if a == nil {
		return 0
	}
	breakdown := policy.ScoreAccumulator(a, intent, focusFile, focusResource)
	// Priority is only a deterministic tie-breaker between otherwise equivalent
	// seeds. It never creates relevance from source order or IDs.
	return breakdown.Total - item.priority
}

func rankSeedEntities(policy RankPolicy, seeds []plannerSeedEntity, acc map[string]*candidateAccumulator, intent TaskIntent, focusFile, focusResource string) []plannerSeedEntity {
	result := append([]plannerSeedEntity(nil), seeds...)
	sort.Slice(result, func(i, j int) bool {
		left := acc[candidateKeyForEntity(result[i].entity)]
		right := acc[candidateKeyForEntity(result[j].entity)]
		leftScore := policy.SeedScore(result[i], left, intent, focusFile, focusResource)
		rightScore := policy.SeedScore(result[j], right, intent, focusFile, focusResource)
		if leftScore != rightScore {
			return leftScore > rightScore
		}
		if result[i].priority != result[j].priority {
			return result[i].priority < result[j].priority
		}
		if result[i].anchor != result[j].anchor {
			return result[i].anchor < result[j].anchor
		}
		return result[i].entity.ID < result[j].entity.ID
	})
	return result
}

func candidateKeyForEntity(entity semantic.Entity) string {
	if id := contextSymbolID(entity); id != "" {
		return "symbol:" + id
	}
	return "entity:" + entity.ID
}

func candidateTier(a *candidateAccumulator, intent TaskIntent) string {
	if hasReason(a, "explicit_focus", "exact_symbol_match", "exact_semantic_match", "framework_operation_match") {
		return "primary"
	}
	if hasReason(a, "weak_exact_match") && !intent.BroadIntent {
		return "primary"
	}
	if hasReason(a, "weak_exact_match") && intent.BroadIntent {
		return "peripheral"
	}
	if hasReason(a, "lexical_fallback", "broad_entry_point") && len(a.evidenceByID) <= 2 {
		return "peripheral"
	}
	if a.distance <= 1 || hasReason(a, "direct_callee", "direct_caller", "framework_provider", "export_provider", "event_peer", "callback_peer") {
		return "supporting"
	}
	return "peripheral"
}

func isWeakAccumulator(a *candidateAccumulator) bool {
	if a == nil {
		return true
	}
	if hasReason(a, "explicit_focus", "exact_symbol_match", "exact_semantic_match", "framework_operation_match", "direct_callee", "direct_caller", "framework_provider", "export_provider", "event_peer", "callback_peer") {
		return false
	}
	return hasReason(a, "lexical_fallback", "broad_entry_point", "weak_exact_match", "direct_reference", "direct_import", "usage_match")
}

func candidateWeakForDiversity(candidate Candidate) bool {
	for _, reason := range candidate.ReasonCodes {
		switch reason {
		case "explicit_focus", "exact_symbol_match", "exact_semantic_match", "framework_operation_match", "direct_callee", "direct_caller", "framework_provider", "export_provider", "event_peer", "callback_peer":
			return false
		}
	}
	for _, reason := range candidate.ReasonCodes {
		switch reason {
		case "lexical_fallback", "broad_entry_point", "weak_exact_match", "direct_reference", "direct_import", "usage_match":
			return true
		}
	}
	return false
}

func diversityOrder(candidates []Candidate, max int) []Candidate {
	if max <= 0 || len(candidates) <= max {
		return candidates
	}
	strong := make([]Candidate, 0, len(candidates))
	weak := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidateWeakForDiversity(candidate) {
			weak = append(weak, candidate)
		} else {
			strong = append(strong, candidate)
		}
	}
	if len(strong) >= max || len(weak) <= 1 {
		return candidates
	}
	// Select strong candidates first, then weak candidates in deterministic
	// file/resource rounds. This only changes ordering among weak comparable
	// candidates; it never demotes a direct/verified candidate.
	selected := append([]Candidate(nil), strong...)
	usedFiles := map[string]bool{}
	usedResources := map[string]bool{}
	for len(selected) < max && len(weak) > 0 {
		best := -1
		for i, candidate := range weak {
			if best < 0 {
				best = i
				continue
			}
			candidateNovel := !usedFiles[candidate.File] || !usedResources[candidate.Resource]
			bestNovel := !usedFiles[weak[best].File] || !usedResources[weak[best].Resource]
			if candidateNovel && !bestNovel {
				best = i
				continue
			}
			if candidateNovel == bestNovel && candidateLess(candidate, weak[best]) {
				best = i
			}
		}
		candidate := weak[best]
		selected = append(selected, candidate)
		usedFiles[candidate.File] = true
		usedResources[candidate.Resource] = true
		weak = append(weak[:best], weak[best+1:]...)
	}
	return selected
}

func candidateLess(left, right Candidate) bool {
	if left.Score != right.Score {
		return left.Score > right.Score
	}
	if left.File != right.File {
		return left.File < right.File
	}
	if left.Line != right.Line {
		return left.Line < right.Line
	}
	return left.ID < right.ID
}

func candidateHasReason(candidate Candidate, reason string) bool {
	for _, value := range candidate.ReasonCodes {
		if value == reason {
			return true
		}
	}
	return false
}

func rankingDebugCandidate(candidate Candidate, breakdown ScoreBreakdown) RankingDebugCandidate {
	return RankingDebugCandidate{ID: candidate.ID, Score: candidate.Score, Anchor: breakdown.Anchor, TaskAlignment: breakdown.TaskAlignment, RelationshipQuality: breakdown.RelationshipQuality, AuthorityQuality: breakdown.AuthorityQuality, FocusRelevance: breakdown.FocusRelevance, Corroboration: breakdown.Corroboration, Locality: breakdown.Locality, DistancePenalty: breakdown.DistancePenalty, UncertaintyPenalty: breakdown.UncertaintyPenalty, FallbackPenalty: breakdown.FallbackPenalty, RedundancyPenalty: breakdown.RedundancyPenalty, Tier: candidate.Tier}
}

func bounded(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}
