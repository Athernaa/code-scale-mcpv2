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

type plannerFileIndex struct {
	store  *storage.IndexStore
	repoID int64
	known  map[string]bool
	work   *plannerBudget
	ctx    context.Context
}

func (f *plannerFileIndex) batch(paths []string) (map[string]bool, error) {
	if err := contextErr(f.ctx); err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return map[string]bool{}, nil
	}
	normalized := make([]string, 0, len(paths))
	for _, path := range paths {
		path = normalizePath(path)
		if path != "" {
			normalized = append(normalized, path)
		}
	}
	if len(normalized) == 0 {
		return map[string]bool{}, nil
	}
	f.work.fileLookups++
	result, err := f.store.FilesExist(f.repoID, normalized)
	if err != nil {
		return nil, err
	}
	for path := range result {
		f.known[path] = true
	}
	return result, contextErr(f.ctx)
}

func (f *plannerFileIndex) exists(path string) (bool, error) {
	path = normalizePath(path)
	if path == "" {
		return false, nil
	}
	if value, ok := f.known[path]; ok {
		return value, nil
	}
	result, err := f.batch([]string{path})
	return result[path], err
}

type seedCollector struct {
	repo        string
	seeds       map[string]*Seed
	seedAnchors map[string]string
	entities    map[string]plannerSeedEntity
	ambiguous   map[string]bool
	priorities  map[string]int
	work        *plannerBudget
}

func newSeedCollector(repo string, work *plannerBudget) *seedCollector {
	return &seedCollector{repo: repo, seeds: map[string]*Seed{}, seedAnchors: map[string]string{}, entities: map[string]plannerSeedEntity{}, ambiguous: map[string]bool{}, priorities: map[string]int{}, work: work}
}

func (c *seedCollector) add(entity semantic.Entity, typ, match string, priority int, expand bool) bool {
	anchor := entitySourceAnchor(entity)
	contextID := contextSymbolID(entity)
	if _, exists := c.seeds[anchor]; !exists {
		if c.work.seedsUsed >= c.work.maxSeeds {
			c.work.seedExhausted = true
			return false
		}
		c.work.seedsUsed++
		c.priorities[anchor] = priority
		seedID := semantic.StableID("planner_seed", c.repo, anchor)
		c.seedAnchors[seedID] = anchor
		c.seeds[anchor] = &Seed{ID: seedID, Type: typ, SourceID: entity.ID, SourceIDs: []string{entity.ID}, SymbolID: contextID, File: normalizePath(entity.File), Name: entity.Name, Match: match, Authority: entityAuthority(entity), Authorities: nonEmptyValues(entityAuthority(entity))}
	} else {
		seed := c.seeds[anchor]
		seed.SourceIDs = appendUnique(seed.SourceIDs, entity.ID)
		sort.Strings(seed.SourceIDs)
		if seed.SourceID == "" || entity.ID < seed.SourceID {
			seed.SourceID = entity.ID
		}
		if seed.Type != "symbol" && typ == "symbol" {
			seed.Type = typ
		}
		if seed.Match == "" || matchPriority(match) > matchPriority(seed.Match) {
			seed.Match = match
		}
		mergeSeedAuthority(seed, entityAuthority(entity))
		if entity.File != "" && (seed.File == "" || normalizePath(entity.File) < seed.File) {
			seed.File = normalizePath(entity.File)
		}
		if entity.Name != "" && (seed.Name == "" || entity.Name < seed.Name) {
			seed.Name = entity.Name
		}
	}
	if priority < c.priorities[anchor] {
		c.priorities[anchor] = priority
	}
	if existing, ok := c.entities[anchor]; ok {
		existing.expand = existing.expand || expand
		existing.priority = minInt(existing.priority, priority)
		if existing.entity.ID != entity.ID {
			known := false
			for _, alternate := range existing.alternates {
				if alternate.ID == entity.ID {
					known = true
					break
				}
			}
			if !known {
				existing.alternates = append(existing.alternates, entity)
				sort.Slice(existing.alternates, func(i, j int) bool {
					return seedEntityLess(existing.alternates[i], existing.alternates[j])
				})
			}
		}
		if seedEntityLess(entity, existing.entity) {
			if existing.entity.ID != entity.ID {
				existing.alternates = append(existing.alternates, existing.entity)
			}
			existing.entity = entity
			sort.Slice(existing.alternates, func(i, j int) bool {
				return seedEntityLess(existing.alternates[i], existing.alternates[j])
			})
		}
		c.entities[anchor] = existing
	} else {
		c.entities[anchor] = plannerSeedEntity{entity: entity, anchor: anchor, priority: priority, expand: expand}
	}
	return true
}

func seedEntityLess(left, right semantic.Entity) bool {
	leftSemantic := left.Analyzer != semantic.AnalyzerGenericGraph
	rightSemantic := right.Analyzer != semantic.AnalyzerGenericGraph
	if leftSemantic != rightSemantic {
		return leftSemantic
	}
	if left.Analyzer != right.Analyzer {
		return left.Analyzer < right.Analyzer
	}
	if left.ID != right.ID {
		return left.ID < right.ID
	}
	return left.Kind < right.Kind
}

func (item plannerSeedEntity) roots() []semantic.Entity {
	result := make([]semantic.Entity, 0, len(item.alternates)+1)
	result = append(result, item.entity)
	result = append(result, item.alternates...)
	return result
}

func (c *seedCollector) markAmbiguous(anchor string) {
	if anchor == "" {
		return
	}
	c.ambiguous[anchor] = true
	if seed, ok := c.seeds[anchor]; ok {
		seed.Ambiguous = true
	}
}

func (c *seedCollector) sortedSeeds() []Seed {
	anchors := make([]string, 0, len(c.seeds))
	for anchor := range c.seeds {
		anchors = append(anchors, anchor)
	}
	sort.Strings(anchors)
	result := make([]Seed, 0, len(anchors))
	for _, anchor := range anchors {
		seed := *c.seeds[anchor]
		seed.Ambiguous = c.ambiguous[anchor]
		if len(seed.Authorities) > 0 {
			seed.Authority = authorityLabel(seed.Authorities)
		}
		result = append(result, seed)
	}
	sort.Slice(result, func(i, j int) bool {
		pi := c.priorities[c.seedAnchors[result[i].ID]]
		pj := c.priorities[c.seedAnchors[result[j].ID]]
		if pi != pj {
			return pi < pj
		}
		return result[i].ID < result[j].ID
	})
	return result
}

func (c *seedCollector) sortedEntities() []plannerSeedEntity {
	result := make([]plannerSeedEntity, 0, len(c.entities))
	for _, item := range c.entities {
		item.ambiguous = c.ambiguous[item.anchor]
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
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
	if err := contextErr(ctx); err != nil {
		return Plan{}, err
	}
	maxCandidates := request.MaxCandidates
	if maxCandidates == 0 {
		maxCandidates = DefaultMaxCandidates
	}
	repoID, err := p.Store.GetRepoID(request.Repo)
	if err != nil {
		return Plan{}, err
	}
	work := newPlannerBudget()
	fileIndex := &plannerFileIndex{store: p.Store, repoID: repoID, known: map[string]bool{}, work: work, ctx: ctx}
	intent := interpretTask(request.Task)
	fileHints, err := fileIndex.batch(intent.FileHints)
	if err != nil {
		return Plan{}, err
	}
	fileHint := firstExistingFileHint(intent.FileHints, fileHints)
	if request.FocusFile != "" {
		focusFile := normalizePath(request.FocusFile)
		exists, lookupErr := fileIndex.exists(focusFile)
		if lookupErr != nil {
			return Plan{}, lookupErr
		}
		if !exists {
			return Plan{}, fmt.Errorf("focus_file %q is not indexed", request.FocusFile)
		}
		request.FocusFile = focusFile
	}
	health, err := p.indexHealth(repoID)
	if err != nil {
		return Plan{}, err
	}
	result := Plan{Repo: request.Repo, TaskClass: intent.TaskClass, TaskConfidence: intent.Confidence, IndexState: health.state, IndexIncomplete: health.incomplete, Diagnostics: append([]string(nil), health.diagnostics...), DegradedResources: append([]string(nil), health.degraded...), Truncated: health.truncated}
	if request.FocusResource != "" {
		request.FocusResource = p.applyResourceFocus(repoID, request.FocusResource, &result, &intent)
	}
	if request.FocusResource == "" {
		request.FocusResource = p.taskResourceHint(repoID, intent.Terms, &result)
	}

	acc := make(map[string]*candidateAccumulator)
	collector := newSeedCollector(request.Repo, work)
	declarationAnchorsByHint := map[string]map[string]bool{}
	declarationTruncatedByHint := map[string]bool{}
	exactSymbolAnchors := map[string]bool{}
	strongExactAnchors := map[string]bool{}
	semanticFamilies := map[string]bool{}
	matchedHints := map[string]bool{}

	if request.FocusSymbolID != "" {
		symbol, lookupErr := p.Store.GetSymbolByID(repoID, request.FocusSymbolID)
		if lookupErr != nil {
			return Plan{}, fmt.Errorf("focus_symbol_id: %w", lookupErr)
		}
		entity := genericEntityForSymbol(request.Repo, *symbol)
		collector.add(entity, "symbol", "explicit_focus", 0, true)
		addSymbolCandidate(acc, request.Repo, *symbol, []string{"explicit_focus"}, 0, work)
		exactSymbolAnchors[entitySourceAnchor(entity)] = true
	}

	for _, hint := range orderedLookupHints(intent) {
		if err := contextErr(ctx); err != nil {
			return Plan{}, err
		}
		if !work.allowExactQuery() || work.exactRows >= work.maxExactAnchors {
			work.exactExhausted = true
			break
		}
		work.exactQueries++
		remaining := work.maxExactAnchors - work.exactRows
		perQuery := minInt(DefaultMaxExactAnchors, remaining)
		symbols, searchErr := p.Store.SearchSymbolsExact(repoID, hint, fileHint, perQuery+1)
		if searchErr != nil {
			return Plan{}, searchErr
		}
		anchors := declarationAnchorsByHint[hint]
		if anchors == nil {
			anchors = map[string]bool{}
			declarationAnchorsByHint[hint] = anchors
		}
		if len(symbols) > perQuery {
			declarationTruncatedByHint[hint] = true
			result.Truncated = true
			symbols = symbols[:perQuery]
		}
		for _, symbol := range symbols {
			work.exactRows++
			entity := genericEntityForSymbol(request.Repo, symbol)
			anchor := entitySourceAnchor(entity)
			anchors[anchor] = true
			exactSymbolAnchors[anchor] = true
			matchedHints[hint] = true
			reason := exactMatchReason(intent, hint)
			if reason == "exact_symbol_match" {
				strongExactAnchors[anchor] = true
			}
			collector.add(entity, "symbol", reason, 10, true)
			addSymbolCandidate(acc, request.Repo, symbol, []string{reason}, 0, work)
		}
	}

	processedSemanticIDs := map[string]bool{}
	semanticExactMatches := map[string]int{}
	currentSemanticHint := ""
	processSemanticEntity := func(entity semantic.Entity, hint string) {
		if entity.ID != "" && processedSemanticIDs[entity.ID] {
			return
		}
		if entity.ID != "" {
			processedSemanticIDs[entity.ID] = true
		}
		if entity.Analyzer == semantic.AnalyzerFiveMWorkspace || entity.Kind == framework.KindStatus {
			return
		}
		if fileHint != "" && normalizePath(entity.File) != fileHint {
			return
		}
		role := semanticSeedRole(entity)
		anchor := entitySourceAnchor(entity)
		if role == roleDeclaration {
			anchors := declarationAnchorsByHint[hint]
			if anchors == nil {
				anchors = map[string]bool{}
				declarationAnchorsByHint[hint] = anchors
			}
			anchors[anchor] = true
		}
		if family := semanticFamilyKey(entity, hint); family != "" {
			semanticFamilies[family] = true
		}
		matchedHints[hint] = true
		semanticExactMatches[hint]++
		if role == roleUsage {
			contextID := contextSymbolID(entity)
			_, hasExistingContextCandidate := acc["symbol:"+contextID]
			if (shouldMaterializeUsage(intent, request) || hasExistingContextCandidate) && contextID != "" {
				addEntityCandidate(acc, entity, []string{"usage_match"}, 1, work)
			}
			return
		}
		if role == roleTopology || role == roleStatus {
			return
		}
		priority := 20
		if entity.Kind == framework.KindOperation {
			priority = 15
			intent.ExpansionDepth = 2
		}
		reason := semanticReason(entity, hint, exactMatchReason(intent, hint) == "exact_symbol_match")
		if reason == "exact_semantic_match" || reason == "framework_operation_match" {
			strongExactAnchors[anchor] = true
		}
		collector.add(entity, "semantic_entity", reason, priority, true)
		addEntityCandidate(acc, entity, []string{reason}, 0, work)
	}
	consumeSemanticEntities := func(entities []semantic.Entity) {
		for _, entity := range entities {
			if work.semanticRows >= work.maxSemanticRows {
				work.exactExhausted = true
				return
			}
			work.semanticRows++
			processSemanticEntity(entity, currentSemanticHint)
		}
	}

	for _, hint := range orderedLookupHints(intent) {
		if err := contextErr(ctx); err != nil {
			return Plan{}, err
		}
		if !work.allowSemanticQuery() || work.semanticRows >= work.maxSemanticRows {
			work.exactExhausted = true
			break
		}
		currentSemanticHint = hint
		work.semanticQueries++
		providerEntities, providerTruncated, searchErr := p.Store.SearchSemanticExactByKinds(repoID, hint, []string{framework.KindAPIProvider}, minInt(DefaultMaxExactAnchors, work.maxSemanticRows-work.semanticRows))
		if searchErr != nil {
			return Plan{}, searchErr
		}
		providerVisible := fileHint == ""
		if !providerVisible {
			for _, entity := range providerEntities {
				if normalizePath(entity.File) == fileHint {
					providerVisible = true
					break
				}
			}
		}
		if providerTruncated && providerVisible {
			declarationTruncatedByHint[hint] = true
			result.Truncated = true
		}
		consumeSemanticEntities(providerEntities)
		if work.semanticRows >= work.maxSemanticRows {
			break
		}
		if !work.allowSemanticQuery() {
			work.exactExhausted = true
			break
		}
		work.semanticQueries++
		remaining := work.maxSemanticRows - work.semanticRows
		perQuery := minInt(DefaultMaxSemanticRows/2, remaining)
		entities, truncated, searchErr := p.Store.SearchSemanticExact(repoID, hint, perQuery)
		if searchErr != nil {
			return Plan{}, searchErr
		}
		if truncated {
			result.Truncated = true
			// The general semantic window mixes declarations, usage rows, and
			// flow endpoints. Only a declaration-scoped query may establish
			// that declaration ambiguity itself was truncated.
			if !declarationTruncatedByHint[hint] && work.allowSemanticQuery() {
				work.semanticQueries++
				_, declarationTruncated, declarationErr := p.Store.SearchSemanticExactByKinds(repoID, hint, []string{generic.KindCodeSymbol}, perQuery)
				if declarationErr != nil {
					return Plan{}, declarationErr
				}
				if declarationTruncated {
					declarationTruncatedByHint[hint] = true
				}
			}
		}
		consumeSemanticEntities(entities)
		if looksLikeOperation(hint) && work.semanticRows < work.maxSemanticRows {
			if !work.allowSemanticQuery() {
				work.exactExhausted = true
				break
			}
			work.semanticQueries++
			operationEntities, operationTruncated, operationErr := p.Store.SearchSemanticOperationExact(repoID, hint, perQuery)
			if operationErr != nil {
				return Plan{}, operationErr
			}
			if operationTruncated {
				result.Truncated = true
			}
			consumeSemanticEntities(operationEntities)
		}
		if truncated && semanticExactMatches[hint] >= perQuery {
			result.Truncated = true
		}
	}

	for _, hint := range sortedKeys(declarationAnchorsByHint) {
		anchors := declarationAnchorsByHint[hint]
		count := len(anchors)
		if declarationTruncatedByHint[hint] {
			count++
		}
		if count > 1 {
			addAmbiguity(&result, Ambiguity{Kind: "source_anchor", Query: hint, CandidateCount: count, Truncated: declarationTruncatedByHint[hint]})
			for anchor := range anchors {
				collector.markAmbiguous(anchor)
			}
		}
	}

	// Weak prose is never allowed to create a large exact-seed set. Fallback
	// lookup is deliberately small and reuses the indexed symbol search tiers.
	fallbackTerms := append([]string{}, intent.HighSignalHints...)
	weakFallbackAllowed := intent.BroadIntent || (intent.TaskClass == "localized_change" && len(exactSymbolAnchors) == 0 && len(semanticFamilies) == 0)
	if weakFallbackAllowed {
		fallbackTerms = append(fallbackTerms, intent.WeakTerms...)
	}
	expandedFallbacks := 0
	for _, hint := range sortedUnique(fallbackTerms) {
		if matchedHints[hint] || !work.allowFallbackQuery() || work.fallbackMatches >= work.maxFallbackMatches {
			if work.fallbackMatches >= work.maxFallbackMatches {
				work.fallbackExhausted = true
			}
			continue
		}
		if err := contextErr(ctx); err != nil {
			return Plan{}, err
		}
		work.fallbackQueries++
		perQuery := minInt(work.maxFallbackPerTerm, work.maxFallbackMatches-work.fallbackMatches)
		fallback, searchErr := p.Store.SearchSymbolsLexicalBounded(repoID, hint, "", "", fileHint, perQuery)
		if searchErr != nil {
			return Plan{}, searchErr
		}
		accepted := 0
		for _, item := range fallback {
			work.fallbackMatches++
			if item.Tier == storage.MatchTierFuzzy {
				continue
			}
			entity := genericEntityForSymbol(request.Repo, item.Symbol)
			expand := expandedFallbacks < 2
			collector.add(entity, "symbol", "lexical_fallback", 80, expand)
			addSymbolCandidate(acc, request.Repo, item.Symbol, []string{"lexical_fallback", "broad_entry_point"}, 0, work)
			accepted++
			if expand {
				expandedFallbacks++
			}
			if accepted >= work.maxFallbackPerTerm {
				break
			}
		}
		if accepted > 0 {
			matchedHints[hint] = true
		}
	}
	if work.fallbackMatches >= work.maxFallbackMatches {
		work.fallbackExhausted = true
	}

	uniqueExactAnchors := map[string]bool{}
	for _, anchors := range declarationAnchorsByHint {
		for anchor := range anchors {
			uniqueExactAnchors[anchor] = true
		}
	}
	hasSymbolExact := false
	for anchor := range exactSymbolAnchors {
		if uniqueExactAnchors[anchor] {
			hasSymbolExact = true
			break
		}
	}
	strongExact := false
	for anchor := range strongExactAnchors {
		if uniqueExactAnchors[anchor] {
			strongExact = true
			break
		}
	}
	logicalExactCount := len(uniqueExactAnchors)
	if logicalExactCount == 0 && len(semanticFamilies) > 0 {
		logicalExactCount = 1
	}
	adjustTaskClass(&intent, logicalExactCount, hasSymbolExact, strongExact)
	result.TaskClass, result.TaskConfidence = intent.TaskClass, intent.Confidence
	result.Seeds = collector.sortedSeeds()
	for _, hint := range intent.Terms {
		if !matchedHints[hint] && !hasMatchingFile(hint, fileHints) {
			appendUnresolved(&result, hint)
		}
	}

	seedEntities := collector.sortedEntities()
	seedEntities = rankSeedEntities(NewRankPolicy(), seedEntities, acc, intent, request.FocusFile, request.FocusResource)
	if (len(seedEntities) > 0 && intent.TaskClass != "exact_symbol") || request.IncludeImpact {
		if err := p.expand(ctx, repoID, seedEntities, intent, request, acc, work, &result); err != nil {
			return Plan{}, err
		}
	}
	if len(seedEntities) == 0 && len(result.UnresolvedHints) == 0 && len(intent.FileHints) > 0 {
		for _, hint := range intent.FileHints {
			if fileHints[normalizePath(hint)] {
				addFileCandidate(acc, request.Repo, normalizePath(hint), work)
			}
		}
	}
	applyFocusEvidence(acc, request.FocusFile, request.FocusResource, work)
	if err := contextErr(ctx); err != nil {
		return Plan{}, err
	}
	if err := p.hydrateCandidates(ctx, repoID, acc); err != nil {
		return Plan{}, err
	}
	result.Seeds, err = p.filterCurrentSeeds(ctx, repoID, result.Seeds, fileIndex)
	if err != nil {
		return Plan{}, err
	}
	files, err := fileIndex.batch(candidateFiles(acc))
	if err != nil {
		return Plan{}, err
	}
	result = finalize(result, acc, maxCandidates, request.FocusFile, request.FocusResource, files, intent)
	applyBudgetDiagnostics(&result, work)
	if request.Debug {
		ranking := append([]RankingDebugCandidate(nil), result.rankingDebug...)
		if len(ranking) > maxCandidates {
			ranking = ranking[:maxCandidates]
		}
		result.Debug = &DebugDetails{EvidenceCount: evidenceCount(acc), CandidatesConsidered: len(acc), SeedsConsidered: len(result.Seeds), SeedBudgetUsed: work.seedsUsed, EvidenceBudgetUsed: work.evidenceUsed, GraphEdgesConsidered: work.graphEdges, TraceQueries: work.traceQueries, ExactQueries: work.exactQueries, SemanticQueries: work.semanticQueries, FallbackQueries: work.fallbackQueries, ExactMatchesConsidered: work.exactRows, SemanticMatchesConsidered: work.semanticRows, FallbackMatchesConsidered: work.fallbackMatches, FileLookups: work.fileLookups, RankingPolicy: NewRankPolicy().Version(), RankedCandidates: ranking}
		result.rankingDebug = nil
	}
	if err := contextErr(ctx); err != nil {
		return Plan{}, err
	}
	return result, nil
}

func (p *Planner) hydrateCandidates(ctx context.Context, repoID int64, acc map[string]*candidateAccumulator) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	ids := make([]string, 0)
	for _, candidate := range acc {
		if candidate.symbol == nil && candidate.entity != nil && contextSymbolID(*candidate.entity) != "" {
			ids = append(ids, contextSymbolID(*candidate.entity))
		}
	}
	symbols, err := p.Store.GetSymbolsByIDs(repoID, ids)
	if err != nil {
		return err
	}
	for _, candidate := range acc {
		if candidate.symbol != nil || candidate.entity == nil {
			continue
		}
		if symbol, ok := symbols[contextSymbolID(*candidate.entity)]; ok {
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
	return contextErr(ctx)
}

func (p *Planner) filterCurrentSeeds(ctx context.Context, repoID int64, seeds []Seed, files *plannerFileIndex) ([]Seed, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(seeds))
	paths := make([]string, 0, len(seeds))
	for _, seed := range seeds {
		if seed.SymbolID != "" {
			ids = append(ids, seed.SymbolID)
		}
		if seed.File != "" {
			paths = append(paths, seed.File)
		}
	}
	symbols, err := p.Store.GetSymbolsByIDs(repoID, ids)
	if err != nil {
		return nil, err
	}
	validFiles, err := files.batch(paths)
	if err != nil {
		return nil, err
	}
	result := make([]Seed, 0, len(seeds))
	for _, seed := range seeds {
		if seed.SymbolID != "" {
			if _, ok := symbols[seed.SymbolID]; !ok {
				continue
			}
		}
		if seed.File != "" && !validFiles[normalizePath(seed.File)] {
			continue
		}
		result = append(result, seed)
	}
	return result, contextErr(ctx)
}

type healthState struct {
	state       string
	incomplete  bool
	diagnostics []string
	degraded    []string
	truncated   bool
}

func (p *Planner) indexHealth(repoID int64) (healthState, error) {
	health := healthState{state: "complete"}
	fileCount, err := p.Store.CountFiles(repoID)
	if err != nil {
		return healthState{}, err
	}
	if fileCount == 0 {
		health.state = "unknown"
		health.diagnostics = append(health.diagnostics, "empty_index")
	}
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
	frameworkFacts, truncated, err := p.Store.SearchSemanticWithResourceTargetFrameworkOptions(repoID, "", framework.KindStatus, "", semantic.AnalyzerFramework, "", "", "", true, MaxDegradedResources+1)
	if err != nil {
		return healthState{}, err
	}
	seen := map[string]bool{}
	for _, entity := range frameworkFacts {
		if entity.Kind != framework.KindStatus || seen[entity.Name] {
			continue
		}
		status, _ := entity.Metadata["status"].(string)
		if status != "failed" {
			continue
		}
		seen[entity.Name] = true
		if len(health.degraded) < MaxDegradedResources {
			health.degraded = append(health.degraded, entity.Name)
		} else {
			truncated = true
		}
	}
	sort.Strings(health.degraded)
	if len(health.degraded) > 0 {
		health.diagnostics = append(health.diagnostics, "framework_analysis_degraded")
		if !health.incomplete && health.state != "unknown" {
			health.state = "degraded"
		}
	}
	if truncated {
		health.truncated = true
		health.diagnostics = append(health.diagnostics, "degraded_framework_resources_truncated")
	}
	return health, nil
}

func (p *Planner) applyResourceFocus(repoID int64, focus string, result *Plan, intent *TaskIntent) string {
	resources, err := p.Store.GetWorkspaceResources(repoID)
	if err != nil {
		if !storage.IsNotFound(err) {
			appendDiagnostic(result, "focus_resource_lookup_failed")
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
		appendDiagnostic(result, "focus_resource_not_found")
		return focus
	}
	if matches > 1 {
		addAmbiguity(result, Ambiguity{Kind: "resource", Query: focus, CandidateCount: matches})
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
			addAmbiguity(result, Ambiguity{Kind: "resource", Query: term, CandidateCount: matches})
			continue
		}
		if matches == 1 {
			return resolved
		}
	}
	return ""
}

func (p *Planner) expand(ctx context.Context, repoID int64, seeds []plannerSeedEntity, intent TaskIntent, request Request, acc map[string]*candidateAccumulator, work *plannerBudget, result *Plan) error {
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
	for _, seedItem := range seeds {
		if err := contextErr(ctx); err != nil {
			return err
		}
		if seedItem.ambiguous || !seedItem.expand {
			continue
		}
		for _, seed := range seedItem.roots() {
			if err := contextErr(ctx); err != nil {
				return err
			}
			if seed.Analyzer == semantic.AnalyzerFramework {
				switch entityAuthority(seed) {
				case framework.ProviderStatusExternal, framework.ProviderStatusLocalAmbiguous, framework.ProviderStatusLocalMissing:
					continue
				}
			}
			analyzers := []string{seed.Analyzer}
			if seed.Analyzer == semantic.AnalyzerFiveM || seed.Analyzer == semantic.AnalyzerFramework {
				analyzers = append(analyzers, semantic.AnalyzerFiveMWorkspace)
			}
			baseDirection := intent.TraceDirection
			if baseDirection != "incoming" && baseDirection != "outgoing" && baseDirection != "both" {
				baseDirection = "both"
			}
			directions := []struct {
				value  string
				impact bool
			}{{value: baseDirection}}
			if request.IncludeImpact && baseDirection != "incoming" && baseDirection != "both" {
				directions = append(directions, struct {
					value  string
					impact bool
				}{value: "incoming", impact: true})
			}
			for _, analyzer := range analyzers {
				for _, direction := range directions {
					if !work.allowTrace() {
						work.traceExhausted = true
						return nil
					}
					remaining := work.maxGraphEdges - work.graphEdges
					if remaining <= 0 {
						work.graphExhausted = true
						return nil
					}
					work.traceQueries++
					edges, truncated, err := p.Store.TraceSemanticRankedWithOptions(repoID, seed.ID, analyzer, direction.value, nil, depth, minInt(remaining, 100))
					if err != nil {
						if cancelErr := contextErr(ctx); cancelErr != nil {
							return cancelErr
						}
						if traceRootUnavailable(err) {
							continue
						}
						return err
					}
					if cancelErr := contextErr(ctx); cancelErr != nil {
						return cancelErr
					}
					work.graphEdges += len(edges)
					if truncated {
						result.Truncated = true
					}
					for _, edge := range edges {
						addTraceEntity(acc, edge.From, seed, edge, direction.impact, work)
						if edge.To != nil {
							addTraceEntity(acc, *edge.To, seed, edge, direction.impact, work)
						}
					}
				}
			}
		}
	}
	return contextErr(ctx)
}

func addTraceEntity(acc map[string]*candidateAccumulator, entity semantic.Entity, seed semantic.Entity, edge semantic.TraceEdge, impact bool, work *plannerBudget) {
	reasons := []string{relationshipReason(edge, seed)}
	if impact {
		reasons = append(reasons, "impact_direct")
	}
	if edge.Depth > 1 {
		reasons = append(reasons, "impact_transitive")
	}
	addEntityCandidateWithRelationshipKind(acc, entity, reasons, edge.Depth, edge.Relationship.ID, edge.Kind, edge.Dynamic || entity.Dynamic, work)
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

func addSymbolCandidate(acc map[string]*candidateAccumulator, repo string, symbol parser.Symbol, reasons []string, distance int, work *plannerBudget) {
	key := "symbol:" + symbol.ID
	a := acc[key]
	created := false
	if a == nil {
		a = newAccumulator(key, repo)
		acc[key] = a
		created = true
	}
	accepted := false
	for _, reason := range reasons {
		if addEvidence(a, Evidence{Kind: "symbol", SourceID: symbol.ID, Depth: distance, Strength: evidenceStrengthHint(reason), NoteCode: reason, Analyzer: semantic.AnalyzerGenericGraph, Role: string(roleDeclaration)}, work) {
			accepted = true
		}
	}
	if created && !accepted {
		delete(acc, key)
		return
	}
	if a.symbol == nil || symbolLess(symbol, *a.symbol) {
		copySymbol := symbol
		a.symbol = &copySymbol
		a.file, a.line, a.endLine, a.name, a.kind = normalizePath(symbol.File), symbol.Line, symbol.EndLine, symbol.Name, symbol.Kind
	}
	if distance < a.distance {
		a.distance = distance
	}
}

func addEntityCandidate(acc map[string]*candidateAccumulator, entity semantic.Entity, reasons []string, distance int, work *plannerBudget) {
	addEntityCandidateWithRelationshipKind(acc, entity, reasons, distance, "", entity.Kind, entity.Dynamic, work)
}

func addEntityCandidateWithRelationship(acc map[string]*candidateAccumulator, entity semantic.Entity, reasons []string, distance int, relationshipID string, work *plannerBudget) {
	addEntityCandidateWithRelationshipKind(acc, entity, reasons, distance, relationshipID, entity.Kind, entity.Dynamic, work)
}

func addEntityCandidateWithRelationshipKind(acc map[string]*candidateAccumulator, entity semantic.Entity, reasons []string, distance int, relationshipID, relationshipKind string, dynamic bool, work *plannerBudget) {
	key := "entity:" + entity.ID
	if contextID := contextSymbolID(entity); contextID != "" {
		key = "symbol:" + contextID
	}
	a := acc[key]
	created := false
	if a == nil {
		a = newAccumulator(key, entity.Repo)
		acc[key] = a
		created = true
	}
	accepted := false
	for _, reason := range reasons {
		if addEvidence(a, Evidence{Kind: "semantic", SourceID: entity.ID, RelationshipID: relationshipID, Relationship: relationshipKind, Depth: distance, Strength: evidenceStrengthHint(reason), Authority: entityAuthority(entity), NoteCode: reason, Analyzer: entity.Analyzer, Role: string(semanticSeedRole(entity)), Framework: entity.Framework, Dynamic: dynamic || entity.Dynamic}, work) {
			accepted = true
		}
	}
	if created && !accepted {
		delete(acc, key)
		return
	}
	mergeEntityMetadata(a, entity)
	if entity.File != "" && candidateLocationLess(entity, a.file, a.line, a.endLine) {
		a.file = normalizePath(entity.File)
		a.line = entity.Line
		a.endLine = entity.EndLine
	}
	if entity.Name != "" && (a.name == "" || entity.Name < a.name) {
		a.name = entity.Name
	}
	if entity.Kind != "" && (a.kind == "" || entity.Kind < a.kind) {
		a.kind = entity.Kind
	}
	if a.entity == nil || entity.ID < a.entity.ID {
		copyEntity := entity
		a.entity = &copyEntity
	}
	if distance < a.distance {
		a.distance = distance
	}
}

func candidateLocationLess(entity semantic.Entity, currentFile string, currentLine, currentEndLine int) bool {
	file := normalizePath(entity.File)
	if currentFile == "" {
		return file != ""
	}
	if file != currentFile {
		return file < currentFile
	}
	if entity.Line != currentLine {
		return entity.Line < currentLine
	}
	return entity.EndLine < currentEndLine
}

func newAccumulator(key, repo string) *candidateAccumulator {
	return &candidateAccumulator{key: key, repo: repo, evidenceByID: map[string]Evidence{}, reasonCodes: map[string]bool{}, authorities: map[string]bool{}, analyzers: map[string]bool{}, frameworks: map[string]bool{}, resources: map[string]bool{}, resourcePaths: map[string]bool{}, targetResources: map[string]bool{}, sides: map[string]bool{}, distance: 1 << 30}
}

func addEvidence(a *candidateAccumulator, evidence Evidence, work *plannerBudget) bool {
	identity := semantic.StableID("planner_evidence", evidence.Kind, evidence.SourceID, evidence.RelationshipID, evidence.Relationship, fmt.Sprintf("%d", evidence.Depth), evidence.NoteCode)
	if _, exists := a.evidenceByID[identity]; exists {
		a.reasonCodes[evidence.NoteCode] = true
		return true
	}
	if work.evidenceUsed >= work.maxEvidence {
		work.evidenceExhausted = true
		return false
	}
	work.evidenceUsed++
	a.evidenceByID[identity] = evidence
	a.reasonCodes[evidence.NoteCode] = true
	if evidence.Authority != "" {
		a.authorities[evidence.Authority] = true
	}
	return true
}

func mergeEntityMetadata(a *candidateAccumulator, entity semantic.Entity) {
	if entity.Analyzer != "" {
		a.analyzers[entity.Analyzer] = true
	}
	if value := entityAuthority(entity); value != "" {
		a.authorities[value] = true
	}
	if entity.Framework != "" {
		a.frameworks[entity.Framework] = true
	}
	if value := entityResource(entity); value != "" {
		a.resources[value] = true
	}
	if value := entityResourcePath(entity); value != "" {
		a.resourcePaths[normalizePath(value)] = true
	}
	if value := entityTargetResource(entity); value != "" {
		a.targetResources[value] = true
	}
	if entity.Side != "" {
		a.sides[entity.Side] = true
	}
}

func addFileCandidate(acc map[string]*candidateAccumulator, repo, file string, work *plannerBudget) {
	key := "file:" + file
	a := acc[key]
	if a == nil {
		a = newAccumulator(key, repo)
		acc[key] = a
	}
	addEvidence(a, Evidence{Kind: "file", SourceID: file, Strength: 700, NoteCode: "exact_file_match"}, work)
	a.file, a.name, a.kind, a.distance = file, filepath.Base(filepath.FromSlash(file)), "file", 0
}

func finalize(result Plan, acc map[string]*candidateAccumulator, max int, focusFile, focusResource string, files map[string]bool, intent TaskIntent) Plan {
	all := make([]Candidate, 0, len(acc))
	breakdowns := make(map[string]ScoreBreakdown, len(acc))
	policy := NewRankPolicy()
	for _, item := range acc {
		candidate, breakdown := candidateFromAccumulator(item, focusFile, focusResource, intent, policy)
		if candidate.File == "" || !files[candidate.File] {
			continue
		}
		if candidate.SymbolID != "" && item.symbol == nil {
			continue
		}
		all = append(all, candidate)
		breakdowns[candidate.ID] = breakdown
	}
	sort.Slice(all, func(i, j int) bool {
		leftFocused := candidateHasReason(all[i], "explicit_focus")
		rightFocused := candidateHasReason(all[j], "explicit_focus")
		if leftFocused != rightFocused {
			return leftFocused
		}
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
	overflow := len(all) > max
	if intent.TaskClass == "broad_unknown" {
		all = diversityOrder(all, max)
	}
	if overflow {
		result.Truncated = true
	}
	if len(all) > max {
		all = all[:max]
	}
	result.rankingDebug = result.rankingDebug[:0]
	for _, candidate := range all {
		result.rankingDebug = append(result.rankingDebug, rankingDebugCandidate(candidate, breakdowns[candidate.ID]))
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
	sort.Slice(result.Ambiguities, func(i, j int) bool {
		if result.Ambiguities[i].Kind != result.Ambiguities[j].Kind {
			return result.Ambiguities[i].Kind < result.Ambiguities[j].Kind
		}
		return result.Ambiguities[i].Query < result.Ambiguities[j].Query
	})
	sort.Strings(result.UnresolvedHints)
	sort.Strings(result.Diagnostics)
	return result
}

func candidateFromAccumulator(a *candidateAccumulator, focusFile, focusResource string, intent TaskIntent, policy RankPolicy) (Candidate, ScoreBreakdown) {
	authority := authorityLabel(sortedSet(a.authorities))
	breakdown := policy.ScoreAccumulator(a, intent, focusFile, focusResource)
	tier := candidateTier(a, intent)
	reasons := sortedSet(a.reasonCodes)
	analyzers := sortedSet(a.analyzers)
	frameworks := sortedSet(a.frameworks)
	resources := sortedSet(a.resources)
	resourcePaths := sortedSet(a.resourcePaths)
	targetResources := sortedSet(a.targetResources)
	sides := sortedSet(a.sides)
	return Candidate{ID: semantic.StableID("planner_candidate", a.repo, a.key), SymbolID: symbolID(a), File: a.file, Line: a.line, EndLine: a.endLine, Name: a.name, Kind: a.kind, Resource: singleValue(resources), ResourcePath: singleValue(resourcePaths), TargetResource: singleValue(targetResources), Framework: singleValue(frameworks), Frameworks: frameworks, Side: singleValue(sides), Sides: sides, Resources: resources, ResourcePaths: resourcePaths, TargetResources: targetResources, Analyzers: analyzers, Score: breakdown.Total, Tier: tier, ReasonCodes: reasons, Authority: authority, Authorities: sortedSet(a.authorities), Distance: maxInt(0, a.distance), EstimatedScope: maxInt(0, a.endLine-a.line+1)}, breakdown
}

func applyFocusEvidence(acc map[string]*candidateAccumulator, focusFile, focusResource string, work *plannerBudget) {
	keys := make([]string, 0, len(acc))
	for key := range acc {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		a := acc[key]
		if focusFile != "" && normalizePath(focusFile) == a.file {
			addEvidence(a, Evidence{Kind: "file", SourceID: a.file, Strength: 735, NoteCode: "focus_file"}, work)
		}
		if focusResource != "" && a.resources[focusResource] {
			addEvidence(a, Evidence{Kind: "resource", SourceID: focusResource, Strength: 650, NoteCode: "same_resource"}, work)
		}
	}
}

func candidateFiles(acc map[string]*candidateAccumulator) []string {
	files, seen := []string{}, map[string]bool{}
	for _, item := range acc {
		if item.file != "" && !seen[item.file] {
			seen[item.file] = true
			files = append(files, item.file)
		}
	}
	sort.Strings(files)
	return files
}

func symbolID(a *candidateAccumulator) string {
	if a.symbol != nil {
		return a.symbol.ID
	}
	if a.entity != nil {
		return contextSymbolID(*a.entity)
	}
	return ""
}

func genericEntityForSymbol(repo string, symbol parser.Symbol) semantic.Entity {
	return semantic.Entity{ID: semantic.StableID("generic_symbol", repo, symbol.ID), Analyzer: semantic.AnalyzerGenericGraph, Repo: repo, File: symbol.File, SymbolID: symbol.ID, Kind: generic.KindCodeSymbol, Name: symbol.Name, Side: "unknown", Line: symbol.Line, EndLine: symbol.EndLine}
}

func semanticReason(entity semantic.Entity, hint string, strong bool) string {
	if entity.Kind == framework.KindOperation && (entity.Name == hint || metadataOperation(entity) == hint) {
		return "framework_operation_match"
	}
	if !strong {
		return "weak_exact_match"
	}
	return "exact_semantic_match"
}

func shouldMaterializeUsage(intent TaskIntent, request Request) bool {
	if request.IncludeImpact {
		return true
	}
	return intent.TaskClass == "relationship_trace" || intent.TaskClass == "cross_resource"
}

func exactMatchReason(intent TaskIntent, hint string) string {
	for _, value := range intent.HighSignalHints {
		if value == hint {
			return "exact_symbol_match"
		}
	}
	return "weak_exact_match"
}

func orderedLookupHints(intent TaskIntent) []string {
	result := make([]string, 0, len(intent.HighSignalHints)+len(intent.WeakTerms))
	for _, group := range [][]string{intent.QuotedIdentifiers, intent.HighSignalHints, intent.WeakTerms} {
		for _, hint := range sortedUnique(group) {
			result = appendUnique(result, hint)
		}
	}
	return result
}

func metadataOperation(entity semantic.Entity) string {
	if entity.Metadata == nil {
		return ""
	}
	value, _ := entity.Metadata["operation"].(string)
	return value
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

func authorityLabel(values []string) string {
	values = sortedUnique(values)
	if len(values) == 1 {
		return values[0]
	}
	if len(values) > 1 {
		return "mixed"
	}
	return ""
}

func evidenceStrengthHint(reason string) int {
	switch reason {
	case "explicit_focus":
		return 1000
	case "exact_symbol_match":
		return 950
	case "weak_exact_match":
		return 720
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
	case "usage_match":
		return 700
	case "same_resource":
		return 650
	case "exact_file_match":
		return 700
	case "impact_direct":
		return 690
	case "impact_transitive":
		return 500
	case "broad_entry_point":
		return 360
	case "lexical_fallback":
		return 340
	default:
		return 300
	}
}

func hasReason(a *candidateAccumulator, reasons ...string) bool {
	for _, reason := range reasons {
		if a.reasonCodes[reason] {
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

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func normalizePath(path string) string {
	return strings.TrimPrefix(filepath.ToSlash(filepath.Clean(filepath.FromSlash(path))), "./")
}

func firstExistingFileHint(hints []string, files map[string]bool) string {
	for _, hint := range hints {
		hint = normalizePath(hint)
		if files[hint] {
			return hint
		}
	}
	return ""
}

func hasMatchingFile(hint string, files map[string]bool) bool { return files[normalizePath(hint)] }

func evidenceCount(acc map[string]*candidateAccumulator) int {
	total := 0
	for _, item := range acc {
		total += len(item.evidenceByID)
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

func sortedUnique(values []string) []string {
	result, seen := []string{}, map[string]bool{}
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func sortedKeys(values map[string]map[string]bool) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

func sortedSet(values map[string]bool) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func singleValue(values []string) string {
	if len(values) == 1 {
		return values[0]
	}
	return ""
}

func nonEmptyValues(value string) []string {
	if value == "" {
		return nil
	}
	return []string{value}
}

func symbolLess(left, right parser.Symbol) bool {
	if left.File != right.File {
		return left.File < right.File
	}
	if left.Line != right.Line {
		return left.Line < right.Line
	}
	return left.ID < right.ID
}

func sourceAnchor(file, symbolID string, line, endLine int, kind, name string) string {
	if symbolID != "" {
		return "symbol:" + symbolID
	}
	return semantic.StableID("source_anchor", normalizePath(file), fmt.Sprintf("%d", line), fmt.Sprintf("%d", endLine), kind, name)
}

func entitySourceAnchor(entity semantic.Entity) string {
	return sourceAnchor(entity.File, contextSymbolID(entity), entity.Line, entity.EndLine, entity.Kind, entity.Name)
}

func seedAnchorFromSeed(seed Seed) string {
	if seed.SymbolID != "" {
		return "symbol:" + seed.SymbolID
	}
	return sourceAnchor(seed.File, "", 0, 0, seed.Type, seed.Name)
}

func matchPriority(match string) int {
	switch match {
	case "explicit_focus":
		return 100
	case "exact_symbol_match":
		return 90
	case "weak_exact_match":
		return 70
	case "framework_operation_match", "exact_semantic_match":
		return 80
	case "lexical_fallback":
		return 20
	default:
		return 10
	}
}

func mergeSeedAuthority(seed *Seed, authority string) {
	if authority == "" {
		return
	}
	seed.Authorities = appendUnique(seed.Authorities, authority)
	sort.Strings(seed.Authorities)
	seed.Authority = authorityLabel(seed.Authorities)
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func traceRootUnavailable(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "semantic entity") && (strings.Contains(message, "not found in analyzer") || strings.Contains(message, "belongs to analyzer"))
}

func (b *plannerBudget) allowExactQuery() bool    { return b.exactQueries < b.maxExactQueries }
func (b *plannerBudget) allowSemanticQuery() bool { return b.semanticQueries < b.maxSemanticQueries }
func (b *plannerBudget) allowFallbackQuery() bool { return b.fallbackQueries < b.maxFallbackQueries }

func (b *plannerBudget) allowTrace() bool {
	return b.traceQueries < b.maxTraceQueries
}

func applyBudgetDiagnostics(result *Plan, work *plannerBudget) {
	if work.seedExhausted {
		result.Truncated = true
		appendDiagnostic(result, "planner_seed_budget_exhausted")
	}
	if work.evidenceExhausted {
		result.Truncated = true
		appendDiagnostic(result, "planner_evidence_budget_exhausted")
	}
	if work.graphExhausted {
		result.Truncated = true
		appendDiagnostic(result, "planner_graph_budget_exhausted")
	}
	if work.traceExhausted {
		result.Truncated = true
		appendDiagnostic(result, "planner_trace_query_budget_exhausted")
	}
	if work.exactExhausted {
		result.Truncated = true
		appendDiagnostic(result, "planner_lookup_budget_exhausted")
	}
	if work.fallbackExhausted {
		result.Truncated = true
		appendDiagnostic(result, "planner_fallback_budget_exhausted")
	}
}

func addAmbiguity(result *Plan, ambiguity Ambiguity) {
	for index, existing := range result.Ambiguities {
		if existing.Kind == ambiguity.Kind && existing.Query == ambiguity.Query {
			if ambiguity.CandidateCount > existing.CandidateCount {
				result.Ambiguities[index].CandidateCount = ambiguity.CandidateCount
			}
			result.Ambiguities[index].Truncated = existing.Truncated || ambiguity.Truncated
			return
		}
	}
	if len(result.Ambiguities) >= MaxAmbiguities {
		result.Truncated = true
		appendDiagnostic(result, "planner_ambiguity_budget_exhausted")
		return
	}
	result.Ambiguities = append(result.Ambiguities, ambiguity)
}

func appendUnresolved(result *Plan, hint string) {
	if hint == "" || containsValue(result.UnresolvedHints, hint) {
		return
	}
	if len(result.UnresolvedHints) >= MaxUnresolvedHints {
		result.Truncated = true
		appendDiagnostic(result, "planner_unresolved_hint_budget_exhausted")
		return
	}
	result.UnresolvedHints = append(result.UnresolvedHints, hint)
}

func appendDiagnostic(result *Plan, diagnostic string) {
	if diagnostic == "" || containsValue(result.Diagnostics, diagnostic) || len(result.Diagnostics) >= MaxDiagnostics {
		return
	}
	result.Diagnostics = append(result.Diagnostics, diagnostic)
}

func containsValue(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
