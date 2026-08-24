package contextpack

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Athernaa/code-scale-mcpv2/internal/parser"
	"github.com/Athernaa/code-scale-mcpv2/internal/planner"
)

type Assembler struct {
	Planner PlanProvider
	Store   SourceStore
}

func New(planProvider PlanProvider, store SourceStore) *Assembler {
	return &Assembler{Planner: planProvider, Store: store}
}

type stagedCandidate struct {
	candidate    planner.Candidate
	stage        string
	stageRank    int
	supportClass supportClass
	estimate     int
}

func (a *Assembler) Assemble(ctx context.Context, request Request) (Package, error) {
	if err := ctx.Err(); err != nil {
		return Package{}, err
	}
	budget, err := validateBudget(request.MaxContextTokens)
	if err != nil {
		return Package{}, err
	}
	counter, err := NewTokenCounter(request.Tokenizer)
	if err != nil {
		return Package{}, err
	}
	plan, err := a.Planner.Plan(ctx, planner.Request{Repo: request.Repo, Task: request.Task, MaxCandidates: request.MaxCandidates, FocusFile: request.FocusFile, FocusSymbolID: request.FocusSymbolID, FocusResource: request.FocusResource, IncludeImpact: request.IncludeImpact, Debug: false})
	if err != nil {
		return Package{}, err
	}
	if err := ctx.Err(); err != nil {
		return Package{}, err
	}
	repoID, err := a.Store.GetRepoID(request.Repo)
	if err != nil {
		return Package{}, err
	}
	pool := stagedCandidates(plan)
	ambiguityDropped := 0
	if request.FocusSymbolID == "" && hasSourceAnchorAmbiguity(plan.Ambiguities) {
		pool, ambiguityDropped = limitAmbiguousAnchors(pool, 3)
	}
	ids := make([]string, 0, len(pool))
	fileCandidates := make([]string, 0, len(pool))
	for _, item := range pool {
		if item.candidate.SymbolID != "" {
			ids = append(ids, item.candidate.SymbolID)
		} else if item.candidate.File != "" {
			fileCandidates = append(fileCandidates, item.candidate.File)
		}
	}
	symbols, err := a.Store.GetSymbolsByIDs(repoID, ids)
	if err != nil {
		return Package{}, err
	}
	fileOutline, err := a.Store.GetSymbolsByFilesBounded(repoID, fileCandidates, DefaultOutlineSymbolsPerFile, DefaultOutlineSymbolsTotal)
	if err != nil {
		return Package{}, err
	}
	pool = hydrateEstimates(pool, symbols)
	sortStagedCandidates(pool)

	reserve := metadataReserve(budget)
	usable := budget - reserve
	pkg := Package{Repo: request.Repo, TaskClass: plan.TaskClass, TaskConfidence: plan.TaskConfidence, IndexState: plan.IndexState, IndexIncomplete: plan.IndexIncomplete, Ambiguities: boundedAmbiguities(plan.Ambiguities, 8), Diagnostics: boundedStrings(plan.Diagnostics, 8), DegradedResources: boundedStrings(plan.DegradedResources, 16), PlannerTruncated: plan.Truncated, Budget: Budget{RequestedTokens: budget, UsableTokens: usable, Tokenizer: counter.Name(), Exact: counter.Exact()}}
	pkg.Omitted.LowerPriority = ambiguityDropped
	debug := &Debug{}
	originals := map[string]string{}
	remaining := usable
	supportAllocations, supportReserve := directSupportAllocations(pool, usable)
	supportReserveRemaining := supportReserve
	seenSymbols, seenRanges := map[string]bool{}, map[string]bool{}
	stop := "candidates_exhausted"
	for stageRank, stage := range []string{"anchor", "direct_support", "domain_support", "peripheral"} {
		if err := ctx.Err(); err != nil {
			return Package{}, err
		}
		round := Round{Stage: stage}
		for _, item := range pool {
			if item.stageRank != stageRank {
				continue
			}
			round.CandidatesConsidered++
			debug.SourceCandidatesConsidered++
			candidate := item.candidate
			if candidate.SymbolID != "" && seenSymbols[candidate.SymbolID] {
				continue
			}
			rangeKey := fmt.Sprintf("%s:%d:%d", candidate.File, candidate.Line, candidate.EndLine)
			if candidate.SymbolID == "" && seenRanges[rangeKey] {
				continue
			}
			if debug.SourceReads >= DefaultMaxSourceReads || debug.SourceBytesRead >= DefaultMaxSourceBytes {
				pkg.Omitted.SourceReadLimit++
				addOmittedID(&pkg.Omitted, candidate.ID)
				stop = "source_read_limit"
				continue
			}
			allocation := remaining
			if stage == "anchor" && supportReserve < allocation {
				allocation -= supportReserve
			}
			if stage == "direct_support" && item.supportClass == supportCritical {
				if reserved := supportAllocations[candidate.ID]; reserved > 0 {
					allocation = minInt(allocation, reserved)
				}
			}
			if allocation < 24 {
				pkg.Omitted.TokenBudget++
				addOmittedID(&pkg.Omitted, candidate.ID)
				stop = "token_budget_exhausted"
				continue
			}
			remainingBytes := int64(DefaultMaxSourceBytes) - debug.SourceBytesRead
			section, original, bytesRead, sourceRead, ok, loadErr := a.loadSection(ctx, repoID, candidate, symbols, fileOutline.Symbols, fileOutline.TruncatedFiles[candidate.File], allocation, remainingBytes, counter)
			if sourceRead {
				debug.SourceReads++
			}
			debug.SourceBytesRead += bytesRead
			if err := ctx.Err(); err != nil {
				return Package{}, err
			}
			if loadErr != nil || !ok {
				pkg.Omitted.SourceUnavailable++
				addOmittedID(&pkg.Omitted, candidate.ID)
				continue
			}
			if section.TokenCount > remaining {
				pkg.Omitted.TokenBudget++
				addOmittedID(&pkg.Omitted, candidate.ID)
				stop = "token_budget_exhausted"
				continue
			}
			pkg.Sections = append(pkg.Sections, section)
			pkg.Sections[len(pkg.Sections)-1].Stage = stage
			originals[section.CandidateID] = original
			remaining -= section.TokenCount
			if stage == "direct_support" && item.supportClass == supportCritical {
				supportReserveRemaining -= minInt(supportReserveRemaining, section.TokenCount)
			}
			round.Included++
			round.TokensAdded += section.TokenCount
			if section.Partial {
				debug.SectionsPartial++
			}
			if candidate.SymbolID != "" {
				seenSymbols[candidate.SymbolID] = true
			} else {
				seenRanges[rangeKey] = true
			}
		}
		pkg.Rounds = append(pkg.Rounds, round)
		if stage == "direct_support" && supportReserveRemaining > 0 {
			reclaimCapacity := minInt(remaining, supportReserveRemaining)
			reclaimRemaining := reclaimAnchorReserve(pkg.Sections, originals, reclaimCapacity, counter)
			reclaimed := maxInt(0, reclaimCapacity-reclaimRemaining)
			remaining -= reclaimed
			supportReserveRemaining -= reclaimed
		}
	}
	debug.SectionsIncluded = len(pkg.Sections)
	if len(pkg.Sections) == 0 && pkg.Omitted.SourceUnavailable > 0 {
		stop = "source_unavailable"
	}
	if hasExactPartial(pkg.Sections) {
		stop = "token_budget_exhausted"
	}
	pkg.StopReason = stop
	pkg.ContextTruncated = pkg.Omitted.TokenBudget > 0 || pkg.Omitted.SourceReadLimit > 0 || debug.SectionsPartial > 0
	pkg.Truncated = pkg.PlannerTruncated || pkg.ContextTruncated
	pkg.Debug = debug
	if err := finalizePackage(ctx, &pkg, originals, debug, counter, budget); err != nil {
		return Package{}, err
	}
	if !request.Debug {
		pkg.Debug = nil
		stabilizeBudget(&pkg, counter, budget)
	}
	return pkg, nil
}

func hasSourceAnchorAmbiguity(values []planner.Ambiguity) bool {
	for _, value := range values {
		if value.Kind == "source_anchor" && value.CandidateCount > 1 {
			return true
		}
	}
	return false
}

func limitAmbiguousAnchors(pool []stagedCandidate, maxAnchors int) ([]stagedCandidate, int) {
	result := make([]stagedCandidate, 0, len(pool))
	anchors, dropped := 0, 0
	for _, item := range pool {
		if item.stage == "anchor" {
			anchors++
			if anchors > maxAnchors {
				dropped++
				continue
			}
		}
		result = append(result, item)
	}
	return result, dropped
}

func reclaimAnchorReserve(sections []Section, originals map[string]string, remaining int, counter TokenCounter) int {
	for i := range sections {
		section := &sections[i]
		if remaining <= 0 || section.Stage != "anchor" || !section.Partial || !section.originalExact {
			continue
		}
		old := section.TokenCount
		section.Source, section.Partial = sliceToTokens(originals[section.CandidateID], old+remaining, counter)
		section.TokenCount = counter.Count(section.Source)
		remaining -= maxInt(0, section.TokenCount-old)
		if section.Partial {
			section.OriginalTokens = counter.Count(originals[section.CandidateID])
		} else {
			section.OriginalTokens = 0
		}
	}
	return remaining
}

func hasExactPartial(sections []Section) bool {
	for _, section := range sections {
		if section.Partial && section.originalExact {
			return true
		}
	}
	return false
}

func finalizePackage(ctx context.Context, pkg *Package, originals map[string]string, debug *Debug, counter TokenCounter, budget int) error {
	for i := 0; i < 16; i++ {
		before := sectionFingerprint(pkg.Sections)
		recomputeRoundTotals(pkg)
		debug.SectionsIncluded, debug.SectionsPartial = len(pkg.Sections), 0
		for _, section := range pkg.Sections {
			if section.Partial {
				debug.SectionsPartial++
			}
		}
		if err := enforceSerializedBudget(ctx, pkg, originals, counter, budget); err != nil {
			return err
		}
		if before == sectionFingerprint(pkg.Sections) {
			return nil
		}
	}
	return fmt.Errorf("unable to stabilize context package")
}

func sectionFingerprint(sections []Section) string {
	var b strings.Builder
	for _, section := range sections {
		fmt.Fprintf(&b, "%s:%d:%t;", section.CandidateID, section.TokenCount, section.Partial)
	}
	return b.String()
}

func validateBudget(value int) (int, error) {
	if value == 0 {
		return DefaultContextTokenBudget, nil
	}
	if value < MinContextTokenBudget {
		return 0, fmt.Errorf("max_context_tokens must be at least %d", MinContextTokenBudget)
	}
	if value > HardMaxContextTokenBudget {
		return 0, fmt.Errorf("max_context_tokens must not exceed %d", HardMaxContextTokenBudget)
	}
	return value, nil
}

func metadataReserve(budget int) int {
	reserve := budget / 20
	if reserve < 256 {
		reserve = 256
	}
	if reserve > 1024 {
		reserve = 1024
	}
	return reserve
}

func stagedCandidates(plan planner.Plan) []stagedCandidate {
	all := make([]stagedCandidate, 0, len(plan.Primary)+len(plan.Supporting)+len(plan.Peripheral))
	add := func(candidates []planner.Candidate, fallback int) {
		for _, candidate := range candidates {
			stage, rank := stageFor(candidate, fallback)
			all = append(all, stagedCandidate{candidate: candidate, stage: stage, stageRank: rank, supportClass: supportClassFor(candidate), estimate: estimateCandidate(candidate)})
		}
	}
	add(plan.Primary, 0)
	add(plan.Supporting, 2)
	add(plan.Peripheral, 3)
	sortStagedCandidates(all)
	return all
}

type supportClass int

const (
	supportNone supportClass = iota
	supportCritical
	supportSecondary
)

func sortStagedCandidates(all []stagedCandidate) {
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].stageRank != all[j].stageRank {
			return all[i].stageRank < all[j].stageRank
		}
		if all[i].stageRank == 1 && all[i].supportClass != all[j].supportClass {
			return all[i].supportClass < all[j].supportClass
		}
		if all[i].stageRank > 0 {
			leftUtility := int64(all[i].candidate.Score) * int64(maxInt(1, all[j].estimate))
			rightUtility := int64(all[j].candidate.Score) * int64(maxInt(1, all[i].estimate))
			if leftUtility != rightUtility {
				return leftUtility > rightUtility
			}
		}
		if all[i].candidate.Score != all[j].candidate.Score {
			return all[i].candidate.Score > all[j].candidate.Score
		}
		if all[i].estimate != all[j].estimate {
			return all[i].estimate < all[j].estimate
		}
		return all[i].candidate.ID < all[j].candidate.ID
	})
}

func stageFor(candidate planner.Candidate, fallback int) (string, int) {
	if candidate.Tier == "primary" {
		return "anchor", 0
	}
	for _, reason := range candidate.ReasonCodes {
		switch reason {
		case "direct_callee", "direct_caller", "framework_provider", "export_provider", "event_peer", "callback_peer", "direct_reference", "direct_import", "impact_direct":
			return "direct_support", 1
		}
	}
	if fallback <= 2 {
		return "domain_support", 2
	}
	return "peripheral", 3
}

func estimateCandidate(candidate planner.Candidate) int {
	if candidate.EstimatedScope > 0 {
		return candidate.EstimatedScope*5 + 24
	}
	return 128
}

func hydrateEstimates(pool []stagedCandidate, symbols map[string]parser.Symbol) []stagedCandidate {
	for i := range pool {
		if symbol, ok := symbols[pool[i].candidate.SymbolID]; ok && symbol.ByteLength > 0 {
			pool[i].estimate = int((symbol.ByteLength+3)/4) + 24
		}
	}
	return pool
}

func supportClassFor(candidate planner.Candidate) supportClass {
	for _, reason := range candidate.ReasonCodes {
		switch reason {
		case "direct_callee", "direct_caller", "framework_provider", "export_provider", "event_peer", "callback_peer", "impact_direct":
			return supportCritical
		}
	}
	for _, reason := range candidate.ReasonCodes {
		if reason == "direct_reference" || reason == "direct_import" {
			return supportSecondary
		}
	}
	return supportNone
}

func directSupportAllocations(pool []stagedCandidate, usable int) (map[string]int, int) {
	allocations := map[string]int{}
	critical := []stagedCandidate{}
	for _, item := range pool {
		if item.stage != "direct_support" || item.supportClass != supportCritical {
			continue
		}
		critical = append(critical, item)
		if len(critical) == 3 {
			break
		}
	}
	if len(critical) == 0 {
		return allocations, 0
	}
	total := 0
	supportCap := usable / 3
	perCandidate := supportCap / len(critical)
	if perCandidate < 1 {
		perCandidate = 1
	}
	for _, item := range critical {
		allocation := minInt(item.estimate, perCandidate)
		allocations[item.candidate.ID] = allocation
		total += allocation
	}
	return allocations, minInt(total, supportCap)
}

func (a *Assembler) loadSection(ctx context.Context, repoID int64, candidate planner.Candidate, symbols map[string]parser.Symbol, fileSymbols map[string][]parser.Symbol, outlineTruncated bool, allocation int, remainingBytes int64, counter TokenCounter) (Section, string, int64, bool, bool, error) {
	if err := ctx.Err(); err != nil {
		return Section{}, "", 0, false, false, err
	}
	section := sectionFromCandidate(candidate)
	section.originalExact = true
	var source string
	var bytesRead int64
	if candidate.SymbolID != "" {
		symbol, ok := symbols[candidate.SymbolID]
		if !ok || symbol.File != candidate.File {
			return Section{}, "", 0, false, false, nil
		}
		if symbol.ByteLength > remainingBytes {
			boundedBytes := remainingBytes
			if boundedBytes > DefaultMaxSymbolBytes {
				boundedBytes = DefaultMaxSymbolBytes
			}
			content, partial, examined, err := a.Store.GetSymbolContentBounded(repoID, candidate.SymbolID, boundedBytes)
			if err != nil {
				return Section{}, "", examined, true, false, err
			}
			source, bytesRead, section.ContentKind = string(content), examined, "symbol_source"
			section.Partial = partial
			section.originalExact = !partial
		} else {
			content, err := a.Store.GetSymbolContent(repoID, candidate.SymbolID)
			if err != nil {
				return Section{}, "", 0, true, false, err
			}
			source, bytesRead, section.ContentKind = content, int64(len(content)), "symbol_source"
			if symbol.ContentHash != "" && parser.ComputeContentHash([]byte(content)) != symbol.ContentHash {
				return Section{}, "", bytesRead, true, false, nil
			}
		}
	} else if candidate.File != "" {
		symbolsInFile := fileSymbols[candidate.File]
		if len(symbolsInFile) > 0 {
			source = fileOutline(symbolsInFile, outlineTruncated)
			bytesRead, section.ContentKind = 0, "file_outline"
			section.OutlineTruncated = outlineTruncated
		} else {
			content, bytePartial, err := a.Store.GetFileContentBounded(repoID, candidate.File, remainingBytes)
			if err != nil {
				return Section{}, "", 0, true, false, err
			}
			source, bytesRead, section.ContentKind = string(content), int64(len(content)), "file_source"
			section.Partial = bytePartial
			section.originalExact = !bytePartial
		}
	} else {
		return Section{}, "", 0, false, false, nil
	}
	if err := ctx.Err(); err != nil {
		return Section{}, "", bytesRead, candidate.SymbolID != "" || section.ContentKind == "file_source", false, err
	}
	originalTokens := counter.Count(source)
	sliced, tokenPartial := sliceToTokens(source, allocation, counter)
	section.Source, section.Partial = sliced, section.Partial || tokenPartial
	section.TokenCount = counter.Count(section.Source)
	if section.Partial && section.originalExact {
		section.OriginalTokens = originalTokens
	}
	sourceRead := candidate.SymbolID != "" || section.ContentKind == "file_source"
	return section, source, bytesRead, sourceRead, true, nil
}

func sectionFromCandidate(c planner.Candidate) Section {
	return Section{CandidateID: c.ID, SymbolID: c.SymbolID, File: c.File, Line: c.Line, EndLine: c.EndLine, Name: c.Name, Kind: c.Kind, Tier: c.Tier, Score: c.Score, ReasonCodes: append([]string(nil), c.ReasonCodes...), Resource: c.Resource, TargetResource: c.TargetResource, Framework: c.Framework, Side: c.Side, Authority: c.Authority, Resources: append([]string(nil), c.Resources...), TargetResources: append([]string(nil), c.TargetResources...), Frameworks: append([]string(nil), c.Frameworks...), Sides: append([]string(nil), c.Sides...), Authorities: append([]string(nil), c.Authorities...), Distance: c.Distance}
}

func fileOutline(symbols []parser.Symbol, truncated bool) string {
	sort.Slice(symbols, func(i, j int) bool {
		if symbols[i].Line != symbols[j].Line {
			return symbols[i].Line < symbols[j].Line
		}
		return symbols[i].ID < symbols[j].ID
	})
	var b strings.Builder
	for _, symbol := range symbols {
		fmt.Fprintf(&b, "%d-%d %s %s", symbol.Line, symbol.EndLine, symbol.Kind, symbol.QualifiedName)
		if symbol.Signature != "" {
			fmt.Fprintf(&b, ": %s", symbol.Signature)
		}
		b.WriteByte('\n')
	}
	if truncated {
		b.WriteString("... additional indexed symbols omitted ...\n")
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func enforceSerializedBudget(ctx context.Context, pkg *Package, originals map[string]string, counter TokenCounter, budget int) error {
	for attempts := 0; attempts < 128; attempts++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		pkg.Budget.SourceTokens = 0
		pkg.Budget.Exact = counter.Exact()
		for _, section := range pkg.Sections {
			pkg.Budget.SourceTokens += section.TokenCount
		}
		data, err := json.Marshal(pkg)
		if err != nil {
			return err
		}
		used := counter.Count(string(data))
		pkg.Budget.UsedTokens = used
		pkg.Budget.RemainingTokens = maxInt(0, budget-used)
		pkg.Budget.OverheadTokens = maxInt(0, used-pkg.Budget.SourceTokens)
		pkg.Budget.Exhausted = used >= budget || pkg.Omitted.TokenBudget > 0 || hasExactPartial(pkg.Sections)
		data, err = json.Marshal(pkg)
		if err != nil {
			return err
		}
		finalUsed := counter.Count(string(data))
		if pkg.Budget.Exact != counter.Exact() {
			pkg.Budget.Exact = counter.Exact()
			continue
		}
		if finalUsed <= budget && finalUsed == pkg.Budget.UsedTokens {
			return nil
		}
		if finalUsed <= budget {
			pkg.Budget.UsedTokens = finalUsed
			pkg.Budget.RemainingTokens = budget - finalUsed
			pkg.Budget.OverheadTokens = maxInt(0, finalUsed-pkg.Budget.SourceTokens)
			continue
		}
		if len(pkg.Sections) == 0 {
			if shrinkMetadata(pkg) {
				pkg.ContextTruncated, pkg.Truncated = true, true
				continue
			}
			return fmt.Errorf("context package metadata exceeds token budget")
		}
		last := len(pkg.Sections) - 1
		section := &pkg.Sections[last]
		over := finalUsed - budget
		reduction := maxInt(16, section.TokenCount/10)
		if over+16 < section.TokenCount/2 {
			reduction = maxInt(reduction, over+16)
		} else {
			reduction = maxInt(reduction, section.TokenCount/2)
		}
		newLimit := section.TokenCount - reduction
		if newLimit >= 4 {
			original := originals[section.CandidateID]
			section.Source, section.Partial = sliceToTokens(original, newLimit, counter)
			section.TokenCount = counter.Count(section.Source)
			if section.originalExact {
				section.OriginalTokens = counter.Count(original)
			}
			pkg.ContextTruncated, pkg.Truncated = true, true
			pkg.StopReason = "token_budget_exhausted"
			continue
		}
		pkg.Omitted.TokenBudget++
		addOmittedID(&pkg.Omitted, section.CandidateID)
		pkg.Sections = pkg.Sections[:last]
		pkg.ContextTruncated, pkg.Truncated = true, true
		pkg.StopReason = "token_budget_exhausted"
	}
	return fmt.Errorf("unable to enforce context token budget")
}

func shrinkMetadata(pkg *Package) bool {
	if pkg.Debug != nil {
		pkg.Debug = nil
		return true
	}
	if n := len(pkg.Omitted.CandidateIDs); n > 0 {
		pkg.Omitted.CandidateIDs = pkg.Omitted.CandidateIDs[:n-1]
		return true
	}
	if n := len(pkg.Diagnostics); n > 0 {
		pkg.Diagnostics = pkg.Diagnostics[:n-1]
		return true
	}
	if n := len(pkg.DegradedResources); n > 0 {
		pkg.DegradedResources = pkg.DegradedResources[:n-1]
		return true
	}
	if n := len(pkg.Ambiguities); n > 0 {
		pkg.Ambiguities = pkg.Ambiguities[:n-1]
		return true
	}
	if n := len(pkg.Rounds); n > 1 {
		pkg.Rounds = pkg.Rounds[:n-1]
		return true
	}
	return false
}

func recomputeRoundTotals(pkg *Package) {
	byStage := map[string]*Round{}
	for i := range pkg.Rounds {
		pkg.Rounds[i].Included, pkg.Rounds[i].TokensAdded = 0, 0
		byStage[pkg.Rounds[i].Stage] = &pkg.Rounds[i]
	}
	for _, section := range pkg.Sections {
		if round := byStage[section.Stage]; round != nil {
			round.Included++
			round.TokensAdded += section.TokenCount
		}
	}
}

func stabilizeBudget(pkg *Package, counter TokenCounter, budget int) {
	for i := 0; i < 8; i++ {
		data, _ := json.Marshal(pkg)
		used := counter.Count(string(data))
		if used == pkg.Budget.UsedTokens {
			return
		}
		pkg.Budget.UsedTokens = used
		pkg.Budget.RemainingTokens = maxInt(0, budget-used)
		pkg.Budget.OverheadTokens = maxInt(0, used-pkg.Budget.SourceTokens)
	}
}

func addOmittedID(omitted *Omitted, id string) {
	if len(omitted.CandidateIDs) < 8 {
		omitted.CandidateIDs = append(omitted.CandidateIDs, id)
	}
}
func boundedStrings(values []string, max int) []string {
	if len(values) > max {
		return append([]string(nil), values[:max]...)
	}
	return append([]string(nil), values...)
}
func boundedAmbiguities(values []planner.Ambiguity, max int) []planner.Ambiguity {
	if len(values) > max {
		return append([]planner.Ambiguity(nil), values[:max]...)
	}
	return append([]planner.Ambiguity(nil), values...)
}
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
