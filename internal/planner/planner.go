package planner

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Athernaa/code-scale-mcpv2/internal/parser"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic/framework"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic/generic"
	"github.com/Athernaa/code-scale-mcpv2/internal/storage"
)

func (p *Planner) plan(ctx context.Context, request Request) (Plan, error) {
	if p == nil || p.Store == nil {
		return Plan{}, fmt.Errorf("planner store is not configured")
	}
	if strings.TrimSpace(request.Repo) == "" {
		return Plan{}, fmt.Errorf("repo is required")
	}
	if request.Task == "" {
		return Plan{}, fmt.Errorf("task is required")
	}
	if len([]byte(request.Task)) > MaxTaskLength {
		return Plan{}, fmt.Errorf("task exceeds maximum length of %d bytes", MaxTaskLength)
	}
	if request.MaxCandidates < 0 || request.MaxCandidates > HardMaxCandidates {
		return Plan{}, fmt.Errorf("max_candidates must be between 0 and %d", HardMaxCandidates)
	}
	maxCandidates := request.MaxCandidates
	if maxCandidates == 0 {
		maxCandidates = DefaultMaxCandidates
	}
	if ctx == nil {
		ctx = context.Background()
	}
	repoID, err := p.Store.GetRepoID(request.Repo)
	if err != nil {
		return Plan{}, err
	}
	files, err := p.Store.GetFiles(repoID)
	if err != nil {
		return Plan{}, err
	}
	fileSet := make(map[string]bool, len(files))
	for _, file := range files {
		fileSet[normalizePath(file.Path)] = true
	}
	if request.FocusFile != "" && !fileSet[normalizePath(request.FocusFile)] {
		return Plan{}, fmt.Errorf("focus_file %q is not indexed", request.FocusFile)
	}
	intent := interpretTask(request.Task)
	health, err := p.indexHealth(repoID)
	if err != nil {
		return Plan{}, err
	}
	result := Plan{Repo: request.Repo, TaskClass: intent.TaskClass, TaskConfidence: intent.Confidence, IndexState: health.state, IndexIncomplete: health.incomplete, Diagnostics: append([]string(nil), health.diagnostics...), DegradedResources: append([]string(nil), health.degraded...)}
	if request.FocusResource != "" {
		request.FocusResource = p.applyResourceFocus(repoID, request.FocusResource, &result, &intent)
	}
	if request.FocusResource == "" {
		request.FocusResource = p.taskResourceHint(repoID, intent.Terms, &result)
	}

	acc := make(map[string]*candidateAccumulator)
	seedEntities := make([]semantic.Entity, 0)
	seedSymbols := make([]parser.Symbol, 0)
	seenSeed := make(map[string]bool)
	seenSymbols := make(map[string]bool)
	exactSymbolCount, exactSemanticCount := 0, 0

	if request.FocusSymbolID != "" {
		symbol, lookupErr := p.Store.GetSymbolByID(repoID, request.FocusSymbolID)
		if lookupErr != nil {
			return Plan{}, fmt.Errorf("focus_symbol_id: %w", lookupErr)
		}
		seedSymbols = append(seedSymbols, *symbol)
		seenSymbols[symbol.ID] = true
		result.Seeds = append(result.Seeds, Seed{ID: symbol.ID, Type: "symbol", SourceID: symbol.ID, SymbolID: symbol.ID, File: symbol.File, Name: symbol.Name, Match: "explicit_focus"})
		addSymbolCandidate(acc, request.Repo, *symbol, []string{"explicit_focus"}, 0, fileSet)
		entity := genericEntityForSymbol(request.Repo, *symbol)
		seedEntities = append(seedEntities, entity)
		seenSeed[entity.ID] = true
	}

	fileHint := firstExistingFileHint(intent.FileHints, fileSet)
	for _, hint := range appendUniqueAll(nil, intent.SymbolHints...) {
		if err := ctx.Err(); err != nil {
			return Plan{}, err
		}
		symbols, searchErr := p.Store.SearchSymbolsExact(repoID, hint, fileHint, HardMaxCandidates+1)
		if searchErr != nil {
			return Plan{}, searchErr
		}
		if len(symbols) > 0 {
			exactSymbolCount += len(symbols)
			if len(symbols) > 1 {
				result.Ambiguities = append(result.Ambiguities, Ambiguity{Kind: "symbol", Query: hint, CandidateCount: len(symbols)})
			}
			for _, symbol := range symbols {
				if !seenSymbols[symbol.ID] {
					seedSymbols = append(seedSymbols, symbol)
					seenSymbols[symbol.ID] = true
					addSymbolCandidate(acc, request.Repo, symbol, []string{"exact_symbol_match"}, 0, fileSet)
					entity := genericEntityForSymbol(request.Repo, symbol)
					if !seenSeed[entity.ID] {
						seedEntities = append(seedEntities, entity)
						seenSeed[entity.ID] = true
					}
				}
			}
		}
	}

	semanticHints := appendUniqueAll(nil, intent.SemanticHints...)
	for _, hint := range semanticHints {
		entities, truncated, searchErr := p.Store.SearchSemanticWithResourceTargetFrameworkOptions(repoID, hint, "", "", "", "", "", "", true, HardMaxCandidates+1)
		if searchErr != nil {
			return Plan{}, searchErr
		}
		exact := make([]semantic.Entity, 0)
		for _, entity := range entities {
			if entity.Analyzer == semantic.AnalyzerFiveMWorkspace {
				// Workspace entities are topology/relationship endpoints. Seed
				// the canonical per-resource fact and traverse workspace edges
				// separately so analyzer ownership remains unambiguous.
				continue
			}
			operation, _ := entity.Metadata["operation"].(string)
			if fileHint != "" && normalizePath(entity.File) != fileHint {
				continue
			}
			if entity.Name == hint || operation == hint {
				exact = append(exact, entity)
			}
		}
		if truncated && len(exact) >= HardMaxCandidates {
			result.Ambiguities = append(result.Ambiguities, Ambiguity{Kind: "semantic", Query: hint, CandidateCount: HardMaxCandidates + 1})
		}
		if len(exact) > 0 {
			exactSemanticCount += len(exact)
			if len(exact) > 1 {
				result.Ambiguities = append(result.Ambiguities, Ambiguity{Kind: "semantic", Query: hint, CandidateCount: len(exact)})
			}
			for _, entity := range exact {
				if entity.Kind == framework.KindOperation {
					intent.ExpansionDepth = 2
				}
				if !seenSeed[entity.ID] {
					seedEntities = append(seedEntities, entity)
					seenSeed[entity.ID] = true
				}
				addEntityCandidate(acc, entity, []string{semanticReason(entity, hint)}, 0, fileSet)
				result.Seeds = append(result.Seeds, seedFromEntity(entity, "exact_semantic_match"))
			}
		}
	}
	if len(seedSymbols) > 0 {
		// Symbol seeds are emitted once, in stable order, even when a symbol
		// also has several semantic facts.
		for _, symbol := range seedSymbols {
			if !seedAlready(result.Seeds, symbol.ID) {
				result.Seeds = append(result.Seeds, Seed{ID: symbol.ID, Type: "symbol", SourceID: symbol.ID, SymbolID: symbol.ID, File: symbol.File, Name: symbol.Name, Match: "exact_symbol_match"})
			}
		}
	}
	adjustTaskClass(&intent, exactSymbolCount, exactSemanticCount)
	result.TaskClass, result.TaskConfidence = intent.TaskClass, intent.Confidence
	for _, hint := range intent.Terms {
		if !hasMatchingSeed(result.Seeds, hint) && !hasMatchingFile(hint, fileSet) {
			result.UnresolvedHints = appendUnique(result.UnresolvedHints, hint)
		}
	}

	if (len(seedEntities) > 0 && intent.TaskClass != "exact_symbol") || request.IncludeImpact {
		p.expand(ctx, repoID, seedEntities, intent, request, acc, fileSet, &result)
	}
	// A find/locate request deliberately remains seed-only. An explicit focus
	// still receives relationship context for a fix/trace request.
	if len(seedEntities) == 0 && len(seedSymbols) == 0 && len(result.UnresolvedHints) == 0 && len(intent.FileHints) > 0 {
		for _, hint := range intent.FileHints {
			if fileSet[normalizePath(hint)] {
				addFileCandidate(acc, request.Repo, normalizePath(hint), fileSet)
			}
		}
	}
	p.hydrateCandidates(repoID, acc)
	result = finalize(result, acc, maxCandidates, request.FocusFile, request.FocusResource, health)
	if request.Debug {
		result.Debug = &DebugDetails{EvidenceCount: evidenceCount(acc), CandidatesConsidered: len(acc), SeedsConsidered: len(result.Seeds)}
	}
	return result, nil
}

func (p *Planner) hydrateCandidates(repoID int64, acc map[string]*candidateAccumulator) {
	ids := make([]string, 0)
	for _, candidate := range acc {
		if candidate.symbol == nil && candidate.entity != nil && candidate.entity.SymbolID != "" {
			ids = append(ids, candidate.entity.SymbolID)
		}
	}
	symbols, err := p.Store.GetSymbolsByIDs(repoID, ids)
	if err != nil {
		return
	}
	for _, candidate := range acc {
		if candidate.symbol != nil || candidate.entity == nil {
			continue
		}
		if symbol, ok := symbols[candidate.entity.SymbolID]; ok {
			copySymbol := symbol
			candidate.symbol = &copySymbol
			candidate.file = normalizePath(symbol.File)
			candidate.line = symbol.Line
			candidate.endLine = symbol.EndLine
			candidate.name = symbol.Name
			candidate.kind = symbol.Kind
			candidate.repo = candidate.entity.Repo
		}
	}
}

type healthState struct {
	state       string
	incomplete  bool
	diagnostics []string
	degraded    []string
}

func (p *Planner) indexHealth(repoID int64) (healthState, error) {
	health := healthState{state: "complete"}
	workspaceInfo, err := p.Store.GetWorkspace(repoID)
	if err == nil {
		health.incomplete = workspaceInfo.Incomplete || workspaceInfo.IndexTruncated || workspaceInfo.FilesDiscoveredTotal != workspaceInfo.FilesIndexed
		if health.incomplete {
			health.state = "incomplete"
			health.diagnostics = append(health.diagnostics, "workspace_index_incomplete")
		}
	} else if !storage.IsNotFound(err) {
		return healthState{}, err
	}
	frameworkFacts, _, err := p.Store.SearchSemanticWithResourceTargetFrameworkOptions(repoID, "", framework.KindStatus, "", semantic.AnalyzerFramework, "", "", "", true, 100)
	if err != nil {
		return healthState{}, err
	}
	seen := map[string]bool{}
	for _, entity := range frameworkFacts {
		if entity.Kind != framework.KindStatus || seen[entity.Name] {
			continue
		}
		status, _ := entity.Metadata["status"].(string)
		if status == "failed" {
			seen[entity.Name] = true
			health.degraded = append(health.degraded, entity.Name)
		}
	}
	sort.Strings(health.degraded)
	if len(health.degraded) > 0 {
		health.diagnostics = append(health.diagnostics, "framework_analysis_degraded")
		if !health.incomplete {
			health.state = "degraded"
		}
	}
	return health, nil
}

func (p *Planner) applyResourceFocus(repoID int64, focus string, result *Plan, intent *TaskIntent) string {
	resources, err := p.Store.GetWorkspaceResources(repoID)
	if err != nil {
		if !storage.IsNotFound(err) {
			result.Diagnostics = append(result.Diagnostics, "focus_resource_lookup_failed")
		}
		return focus
	}
	matches := 0
	resolved := ""
	for _, resource := range resources {
		if resource.Name == focus || normalizePath(resource.RelativePath) == normalizePath(focus) {
			matches++
			resolved = resource.Name
		}
	}
	if matches == 0 {
		result.Diagnostics = append(result.Diagnostics, "focus_resource_not_found")
		return focus
	}
	if matches > 1 {
		result.Ambiguities = append(result.Ambiguities, Ambiguity{Kind: "resource", Query: focus, CandidateCount: matches})
		intent.ResourceHints = appendUnique(intent.ResourceHints, focus)
		return focus
	}
	intent.ResourceHints = appendUnique(intent.ResourceHints, focus)
	return resolved
}

func (p *Planner) taskResourceHint(repoID int64, terms []string, result *Plan) string {
	resources, err := p.Store.GetWorkspaceResources(repoID)
	if err != nil {
		return ""
	}
	for _, term := range terms {
		matches := 0
		resolved := ""
		for _, resource := range resources {
			if resource.Name == term || normalizePath(resource.RelativePath) == normalizePath(term) {
				matches++
				resolved = resource.Name
			}
		}
		if matches > 1 {
			result.Ambiguities = append(result.Ambiguities, Ambiguity{Kind: "resource", Query: term, CandidateCount: matches})
			continue
		}
		if matches == 1 {
			return resolved
		}
	}
	return ""
}

func (p *Planner) expand(ctx context.Context, repoID int64, seeds []semantic.Entity, intent TaskIntent, request Request, acc map[string]*candidateAccumulator, fileSet map[string]bool, result *Plan) {
	depth := intent.ExpansionDepth
	if depth <= 0 {
		depth = 1
	}
	if depth > 2 {
		depth = 2
	}
	if intent.TaskClass == "localized_change" && !request.IncludeImpact {
		depth = 1
	}
	for _, seed := range seeds {
		if err := ctx.Err(); err != nil {
			return
		}
		if seed.Analyzer == semantic.AnalyzerFramework {
			switch entityAuthority(seed) {
			case framework.ProviderStatusExternal, framework.ProviderStatusLocalAmbiguous, framework.ProviderStatusLocalMissing:
				// Raw/unverified framework syntax is useful as a candidate, but
				// it must never expand into a provider or object lineage.
				continue
			}
		}
		analyzers := []string{seed.Analyzer}
		if seed.Analyzer == semantic.AnalyzerFiveM || seed.Analyzer == semantic.AnalyzerFramework {
			analyzers = append(analyzers, semantic.AnalyzerFiveMWorkspace)
		}
		for _, analyzer := range analyzers {
			edges, truncated, err := p.Store.TraceSemanticWithOptions(repoID, seed.ID, analyzer, intent.TraceDirection, nil, depth, 100)
			if err != nil {
				continue
			}
			if truncated {
				result.Truncated = true
			}
			for _, edge := range edges {
				addTraceEntity(acc, edge.From, seed, edge, fileSet)
				if edge.To != nil {
					addTraceEntity(acc, *edge.To, seed, edge, fileSet)
				}
			}
		}
	}
}

func addTraceEntity(acc map[string]*candidateAccumulator, entity semantic.Entity, seed semantic.Entity, edge semantic.TraceEdge, fileSet map[string]bool) {
	reasons := []string{relationshipReason(edge, seed)}
	if edge.Depth > 1 {
		reasons = append(reasons, "impact_transitive")
	}
	addEntityCandidate(acc, entity, reasons, edge.Depth, fileSet)
}

func relationshipReason(edge semantic.TraceEdge, seed semantic.Entity) string {
	switch edge.Kind {
	case generic.RelationshipCalls:
		if edge.From.ID == seed.ID {
			return "direct_callee"
		}
		return "direct_caller"
	case generic.RelationshipReferences:
		return "direct_reference"
	case generic.RelationshipImports:
		return "direct_import"
	case "cross_resource_event":
		return "event_peer"
	case "cross_resource_callback":
		return "callback_peer"
	case "cross_resource_export":
		return "export_provider"
	case framework.RelationshipFrameworkCalls, framework.RelationshipObjectCall, framework.RelationshipProvidedBy:
		return "framework_provider"
	case framework.RelationshipDerivedFrom:
		return "same_symbol"
	default:
		return "direct_semantic"
	}
}

func addSymbolCandidate(acc map[string]*candidateAccumulator, repo string, symbol parser.Symbol, reasons []string, distance int, files map[string]bool) {
	if symbol.File == "" || !files[normalizePath(symbol.File)] {
		return
	}
	key := "symbol:" + symbol.ID
	a := acc[key]
	if a == nil {
		a = &candidateAccumulator{key: key, repo: repo, symbol: &symbol, reasons: map[string]Evidence{}, authorities: map[string]bool{}, analyzers: map[string]bool{}, distance: distance, file: normalizePath(symbol.File), line: symbol.Line, endLine: symbol.EndLine, name: symbol.Name, kind: symbol.Kind}
		acc[key] = a
	}
	for _, reason := range reasons {
		a.reasons[reason] = Evidence{Kind: "symbol", SourceID: symbol.ID, Depth: distance, Strength: reasonStrength(reason), NoteCode: reason}
	}
	if distance < a.distance {
		a.distance = distance
	}
}

func addEntityCandidate(acc map[string]*candidateAccumulator, entity semantic.Entity, reasons []string, distance int, files map[string]bool) {
	if entity.File != "" && !files[normalizePath(entity.File)] {
		return
	}
	key := "entity:" + entity.ID
	if entity.SymbolID != "" {
		key = "symbol:" + entity.SymbolID
	}
	a := acc[key]
	if a == nil {
		copyEntity := entity
		a = &candidateAccumulator{key: key, repo: entity.Repo, entity: &copyEntity, reasons: map[string]Evidence{}, authorities: map[string]bool{}, analyzers: map[string]bool{}, distance: distance, file: normalizePath(entity.File), line: entity.Line, endLine: entity.EndLine, name: entity.Name, kind: entity.Kind, resource: entityResource(entity), resourcePath: entityResourcePath(entity), targetResource: entityTargetResource(entity), framework: entity.Framework, side: entity.Side}
		acc[key] = a
	} else if a.entity != nil && a.entity.ID != entity.ID {
		a.reasons["same_symbol"] = Evidence{Kind: "semantic", SourceID: entity.ID, Depth: distance, Strength: reasonStrength("same_symbol"), NoteCode: "same_symbol"}
	}
	a.analyzers[entity.Analyzer] = true
	authority := entityAuthority(entity)
	if authority != "" {
		a.authorities[authority] = true
	}
	for _, reason := range reasons {
		a.reasons[reason] = Evidence{Kind: "semantic", SourceID: entity.ID, Relationship: entity.Kind, Depth: distance, Strength: reasonStrength(reason), Authority: authority, NoteCode: reason}
	}
	if distance < a.distance {
		a.distance = distance
	}
}

func addFileCandidate(acc map[string]*candidateAccumulator, repo, file string, files map[string]bool) {
	if !files[file] {
		return
	}
	key := "file:" + file
	if acc[key] == nil {
		acc[key] = &candidateAccumulator{key: key, repo: repo, reasons: map[string]Evidence{"exact_file_match": {Kind: "file", SourceID: file, Strength: 700, NoteCode: "exact_file_match"}}, authorities: map[string]bool{}, analyzers: map[string]bool{}, distance: 0, file: file, name: filepath.Base(filepath.FromSlash(file)), kind: "file"}
	}
}

func finalize(result Plan, acc map[string]*candidateAccumulator, max int, focusFile, focusResource string, health healthState) Plan {
	all := make([]Candidate, 0, len(acc))
	for _, item := range acc {
		candidate := candidateFromAccumulator(item, focusFile, focusResource)
		if candidate.File == "" {
			continue
		}
		all = append(all, candidate)
	}
	sort.Slice(all, func(i, j int) bool {
		if tierRank(all[i].Tier) != tierRank(all[j].Tier) {
			return tierRank(all[i].Tier) < tierRank(all[j].Tier)
		}
		if all[i].Score != all[j].Score {
			return all[i].Score > all[j].Score
		}
		if all[i].File != all[j].File {
			return all[i].File < all[j].File
		}
		if all[i].Line != all[j].Line {
			return all[i].Line < all[j].Line
		}
		return all[i].ID < all[j].ID
	})
	if len(all) > max {
		result.Truncated = true
		all = all[:max]
	}
	for _, candidate := range all {
		switch candidate.Tier {
		case "primary":
			result.Primary = append(result.Primary, candidate)
		case "supporting":
			result.Supporting = append(result.Supporting, candidate)
		default:
			result.Peripheral = append(result.Peripheral, candidate)
		}
	}
	return result
}

func candidateFromAccumulator(a *candidateAccumulator, focusFile, focusResource string) Candidate {
	authority := bestAuthority(a.authorities)
	score := 100
	for reason := range a.reasons {
		if value := reasonStrength(reason); value > score {
			score = value
		}
	}
	if focusFile != "" && normalizePath(focusFile) == a.file {
		a.reasons["focus_file"] = Evidence{Kind: "file", SourceID: a.file, Strength: 735, NoteCode: "focus_file"}
		score += 35
	}
	if focusResource != "" && a.resource == focusResource {
		a.reasons["same_resource"] = Evidence{Kind: "resource", SourceID: focusResource, Strength: 650, NoteCode: "same_resource"}
		score += 35
	}
	if a.distance > 1 {
		score -= 120
	}
	score += authorityAdjustment(authority)
	if a.symbol == nil {
		score -= 80
	}
	tier := "peripheral"
	if hasReason(a.reasons, "explicit_focus", "exact_symbol_match", "exact_semantic_match", "framework_operation_match") {
		tier = "primary"
	} else if (a.distance <= 1 || hasReason(a.reasons, "framework_provider", "export_provider")) && len(a.reasons) > 0 {
		tier = "supporting"
	}
	if a.distance == 0 && a.symbol != nil && tier == "peripheral" {
		tier = "primary"
	}
	reasons := make([]string, 0, len(a.reasons))
	for reason := range a.reasons {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	analyzers := make([]string, 0, len(a.analyzers))
	for analyzer := range a.analyzers {
		analyzers = append(analyzers, analyzer)
	}
	sort.Strings(analyzers)
	id := semantic.StableID("planner_candidate", a.repo, a.key)
	return Candidate{ID: id, SymbolID: symbolID(a), File: a.file, Line: a.line, EndLine: a.endLine, Name: a.name, Kind: a.kind, Resource: a.resource, ResourcePath: a.resourcePath, TargetResource: a.targetResource, Framework: a.framework, Side: a.side, Analyzers: analyzers, Score: score, Tier: tier, ReasonCodes: reasons, Authority: authority, Distance: a.distance, EstimatedScope: maxInt(0, a.endLine-a.line+1)}
}

func symbolID(a *candidateAccumulator) string {
	if a.symbol != nil {
		return a.symbol.ID
	}
	if a.entity != nil {
		return a.entity.SymbolID
	}
	return ""
}

func seedFromEntity(entity semantic.Entity, match string) Seed {
	return Seed{ID: entity.ID, Type: "semantic_entity", SourceID: entity.ID, SymbolID: entity.SymbolID, File: entity.File, Name: entity.Name, Match: match, Authority: entityAuthority(entity)}
}

func genericEntityForSymbol(repo string, symbol parser.Symbol) semantic.Entity {
	return semantic.Entity{ID: semantic.StableID("generic_symbol", repo, symbol.ID), Analyzer: semantic.AnalyzerGenericGraph, Repo: repo, File: symbol.File, SymbolID: symbol.ID, Kind: generic.KindCodeSymbol, Name: symbol.Name, Side: "unknown", Line: symbol.Line, EndLine: symbol.EndLine}
}

func semanticReason(entity semantic.Entity, hint string) string {
	if entity.Kind == framework.KindOperation || entity.Name == hint {
		if entity.Kind == framework.KindOperation {
			return "framework_operation_match"
		}
		return "exact_semantic_match"
	}
	return "exact_semantic_match"
}

func entityResource(entity semantic.Entity) string {
	if entity.Metadata != nil {
		if value, ok := entity.Metadata["source_resource"].(string); ok && value != "" {
			return value
		}
		if value, ok := entity.Metadata["resource"].(string); ok {
			return value
		}
	}
	return ""
}

func entityResourcePath(entity semantic.Entity) string {
	if entity.Metadata != nil {
		if value, ok := entity.Metadata["source_resource_path"].(string); ok {
			return value
		}
		if value, ok := entity.Metadata["resource_path"].(string); ok {
			return value
		}
	}
	return ""
}

func entityTargetResource(entity semantic.Entity) string {
	if entity.Metadata != nil {
		if value, ok := entity.Metadata["target_resource"].(string); ok {
			return value
		}
	}
	return ""
}

func entityAuthority(entity semantic.Entity) string {
	if entity.Metadata == nil {
		return ""
	}
	status, _ := entity.Metadata["provider_status"].(string)
	if status != "" {
		return status
	}
	if verified, ok := entity.Metadata["provider_verified"].(bool); ok && verified {
		return framework.ProviderStatusLocalVerified
	}
	return ""
}

func bestAuthority(values map[string]bool) string {
	order := []string{framework.ProviderStatusLocalAmbiguous, framework.ProviderStatusLocalMissing, framework.ProviderStatusExternal, framework.ProviderStatusLocalVerified}
	for _, value := range order {
		if values[value] {
			return value
		}
	}
	return ""
}

func authorityAdjustment(authority string) int {
	switch authority {
	case framework.ProviderStatusLocalVerified:
		return 90
	case framework.ProviderStatusExternal:
		return -100
	case framework.ProviderStatusLocalAmbiguous:
		return -160
	case framework.ProviderStatusLocalMissing:
		return -130
	default:
		return 0
	}
}

func reasonStrength(reason string) int {
	switch reason {
	case "explicit_focus":
		return 1000
	case "exact_symbol_match":
		return 950
	case "exact_semantic_match":
		return 940
	case "framework_operation_match":
		return 930
	case "export_provider", "framework_provider":
		return 880
	case "direct_callee", "direct_caller":
		return 820
	case "same_symbol":
		return 780
	case "direct_reference", "direct_import", "event_peer", "callback_peer", "direct_semantic":
		return 760
	case "same_resource":
		return 650
	case "impact_transitive":
		return 500
	case "exact_file_match":
		return 700
	default:
		return 300
	}
}

func hasReason(values map[string]Evidence, reasons ...string) bool {
	for _, reason := range reasons {
		if _, ok := values[reason]; ok {
			return true
		}
	}
	return false
}
func tierRank(tier string) int {
	switch tier {
	case "primary":
		return 0
	case "supporting":
		return 1
	default:
		return 2
	}
}
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func normalizePath(path string) string {
	return strings.TrimPrefix(filepath.ToSlash(filepath.Clean(filepath.FromSlash(path))), "./")
}
func firstExistingFileHint(hints []string, files map[string]bool) string {
	for _, hint := range hints {
		if files[normalizePath(hint)] {
			return normalizePath(hint)
		}
	}
	return ""
}
func hasMatchingFile(hint string, files map[string]bool) bool { return files[normalizePath(hint)] }
func hasMatchingSeed(seeds []Seed, hint string) bool {
	for _, seed := range seeds {
		if seed.Name == hint || seed.File == hint || seed.SymbolID == hint {
			return true
		}
	}
	return false
}
func seedAlready(seeds []Seed, id string) bool {
	for _, seed := range seeds {
		if seed.SourceID == id {
			return true
		}
	}
	return false
}
func evidenceCount(acc map[string]*candidateAccumulator) int {
	total := 0
	for _, item := range acc {
		total += len(item.reasons)
	}
	return total
}
func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
