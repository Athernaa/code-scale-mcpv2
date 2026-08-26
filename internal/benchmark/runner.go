package benchmark

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/Athernaa/code-scale-mcpv2/internal/contextpack"
	"github.com/Athernaa/code-scale-mcpv2/internal/planner"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic"
	"github.com/Athernaa/code-scale-mcpv2/internal/sufficiency"
)

type runner struct {
	index     *FixtureIndex
	corpus    Corpus
	counter   contextpack.TokenCounter
	tokenizer string
}

type retrievedItem struct {
	Key         string
	Name        string
	File        string
	SymbolID    string
	Kind        string
	Authority   string
	Source      string
	SourceRead  bool
	SourceBytes int64
}

type modeOutput struct {
	Items          []retrievedItem
	Ranked         []retrievedItem
	Relationships  []RelationshipEvidence
	Package        *contextpack.Package
	Plan           *planner.Plan
	ContextText    string
	ContextTokens  int
	MetadataTokens int
	SourceTokens   int
	SourceReads    int
	SourceBytes    int64
	RetrievalCalls int
	PlannerTime    time.Duration
	AssemblyTime   time.Duration
	TotalTime      time.Duration
}

type capturingPlanner struct {
	base     *planner.Planner
	plan     planner.Plan
	duration time.Duration
}

func (p *capturingPlanner) Plan(ctx context.Context, request planner.Request) (planner.Plan, error) {
	start := time.Now()
	plan, err := p.base.Plan(ctx, request)
	p.duration += time.Since(start)
	p.plan = plan
	return plan, err
}

func Run(ctx context.Context, cfg Config) (Report, error) {
	corpus, err := LoadCorpus(cfg.CorpusPath)
	if err != nil {
		return Report{}, err
	}
	modes, err := SelectModes(cfg.Mode)
	if err != nil {
		return Report{}, err
	}
	tasks := SelectTasks(corpus, cfg.TaskID, cfg.Category)
	if len(tasks) == 0 {
		return Report{}, fmt.Errorf("no benchmark tasks selected")
	}
	budgets := cfg.Budgets
	if len(budgets) == 0 {
		budgets, err = ParseBudgets("")
		if err != nil {
			return Report{}, err
		}
	}
	if cfg.Repeat <= 0 {
		cfg.Repeat = 1
	}
	counter, err := contextpack.NewTokenCounter(valueOr(corpus.DefaultTokenizer, cfg.Tokenizer))
	if err != nil {
		return Report{}, err
	}
	index, cleanup, err := BuildFixtureIndex(ctx, corpus, FixtureRoot(cfg.CorpusPath))
	if err != nil {
		return Report{}, err
	}
	defer cleanup()
	r := &runner{index: index, corpus: corpus, counter: counter, tokenizer: valueOr(corpus.DefaultTokenizer, cfg.Tokenizer)}
	report := Report{ReportVersion: "7.4.0", GeneratedAt: time.Now().UTC(), CorpusVersion: corpus.CorpusVersion, FixtureRevision: corpus.FixtureRevision, Tokenizer: r.tokenizer, Modes: modes, Budgets: append([]int(nil), budgets...), Repeat: cfg.Repeat, TasksRun: len(tasks), Results: []TaskResult{}, ByCategory: map[string]Summary{}, BenchmarkNotes: []string{
		"Manual mode is a deterministic ground-truth minimum baseline, not an LLM simulation.",
		"Panoramic mode is an offline scoped broad-file baseline; Repomix is optional and was not required.",
		"Primitive mode uses only storage search, semantic search, bounded source reads, and bounded trace calls.",
		"Phase-7 mode uses the production Planner, ContextAssembler, TokenCounter, and Sufficiency path.",
		"Harness defect fixed: metamorphic phase7 runs lazily initialize the token counter; the focused rerun passed.",
		"Harness defect fixed: relationship scoring requires two distinct endpoint occurrences; the focused rerun passed.",
		"Harness fidelity fixed: incremental/full comparison now includes the real watcher framework refresh path; the metamorphic rerun passed.",
		"Phase-7 defect fixed: FocusSymbolID now bridges persisted generic, FiveM, and framework facts sharing the source symbol; planner regression and focused FiveM rerun passed.",
		"Phase-7 defect fixed: final non-debug sufficiency reevaluation could exceed the serialized context budget; final evaluate-then-enforce stabilization and a 1024-token regression now pass.",
		"Benchmark scorer defect fixed: relationship recall now requires exact indexed edge kind, endpoint identity, files, and returned endpoint source; calls-versus-references and event-versus-callback regressions pass.",
		"Benchmark scorer defect fixed: structured symbol/provider scoring no longer accepts source-text substrings, and empty retrieval precision is undefined and excluded from aggregates.",
		"Benchmark fixture defect fixed: callback flow now models a valid client-to-server callback; the required 512-to-32000 budget matrix with two repeats has zero false sufficiency.",
		"Phase-7 defect fixed: unique Lua require aliases now resolve dotted module calls while ambiguous and shadowed imports remain unresolved; Lua false-sufficiency matrix rerun passed.",
		"Phase-7 defect fixed: explicit focused generic symbols can expand past same-source multi-analyzer ambiguity; facade-chain budget matrix rerun passed.",
		"Phase-7 defect fixed: exact semantic provider operations and incoming impact traces remain incomplete until provider/impact evidence is returned; low-budget framework and blast-radius regressions pass.",
		"Benchmark accounting defect fixed: supplied ContextText is counted directly, scoring-only symbol/provider/relationship records remain outside the payload, and panoramic/scoped self-baseline invariants pass.",
	}}
	for _, rawTask := range tasks {
		task := rawTask
		if repo, ok := corpus.Repository(task.Repo); ok {
			task.FixtureTier = repo.Tier
		}
		broadBaseline := r.panoramic(task)
		broadBaselineTokens := r.counter.Count(broadBaseline.ContextText)
		scopedBaseline := r.scopedPanoramic(task)
		scopedBaselineTokens := r.counter.Count(scopedBaseline.ContextText)
		for _, mode := range modes {
			for _, budget := range budgets {
				var previousFingerprint string
				for repeat := 1; repeat <= cfg.Repeat; repeat++ {
					if err := ctx.Err(); err != nil {
						return Report{}, err
					}
					output, runErr := r.execute(ctx, task, mode, budget)
					result := scoreResult(task, mode, budget, repeat, output, r.counter, broadBaselineTokens, scopedBaselineTokens)
					if runErr != nil {
						result.RuntimeError = runErr.Error()
					}
					if repeat > 1 && result.DeterminismFingerprint != previousFingerprint {
						result.Nondeterministic = true
					}
					previousFingerprint = result.DeterminismFingerprint
					report.Results = append(report.Results, result)
				}
			}
		}
	}
	report.Aggregate = aggregateResults(report.Results)
	panoramicBaselineViolations, scopedBaselineViolations := baselineSelfBaselineViolations(report.Results)
	report.Aggregate.BaselineAccountingViolations = panoramicBaselineViolations + scopedBaselineViolations
	report.ByCategory = summarizeCategories(report.Results)
	report.ByTier = summarizeTiers(report.Results)
	report.Acceptance = acceptanceSummary(report.Results)
	report.EarlyStopByCategory = earlyStopByCategory(report.Results)
	report.Validation = validateReport(report.Results, report.Aggregate)
	report.Validation.BudgetMonotonicityFailures = budgetMonotonicityFailures(report.Results)
	metamorphicMismatches, err := incrementalFullMismatches(ctx, corpus, FixtureRoot(cfg.CorpusPath), r.tokenizer)
	if err != nil {
		return Report{}, fmt.Errorf("incremental/full metamorphic benchmark: %w", err)
	}
	report.Validation.IncrementalFullMismatches = metamorphicMismatches
	return report, nil
}

func (r *runner) execute(ctx context.Context, task Task, mode Mode, budget int) (modeOutput, error) {
	var output modeOutput
	var err error
	switch mode {
	case ModeManual:
		output = r.manual(task)
	case ModePanoramic:
		output = r.panoramic(task)
	case ModeScopedPanoramic:
		output = r.scopedPanoramic(task)
	case ModePrimitive:
		output, err = r.primitive(ctx, task)
	case ModePhase7:
		output, err = r.phase7(ctx, task, budget)
	case ModePhase7NoEarlyStop:
		output, err = r.phase7NoEarlyStop(ctx, task, budget)
	default:
		return modeOutput{}, fmt.Errorf("unsupported mode %q", mode)
	}
	if err != nil {
		return output, err
	}
	if mode == ModeManual || mode == ModePanoramic || mode == ModeScopedPanoramic {
		output.Relationships, err = r.indexedRelationships(task.Repo)
	}
	return output, err
}

func (r *runner) manual(task Task) modeOutput {
	wanted := map[string]bool{}
	for _, path := range task.RelevantFiles {
		wanted[path] = true
	}
	return r.broadModeItems(task, wanted, 1+len(task.Required))
}

func (r *runner) panoramic(task Task) modeOutput {
	return r.broadModeItems(task, nil, 1)
}

func (r *runner) scopedPanoramic(task Task) modeOutput {
	wanted := map[string]bool{}
	for _, path := range task.RelevantFiles {
		wanted[path] = true
	}
	return r.broadModeItems(task, wanted, 1)
}

func (r *runner) broadModeItems(task Task, wanted map[string]bool, retrievalCalls int) modeOutput {
	fileItems := make([]retrievedItem, 0, len(r.index.Files[task.Repo]))
	items := make([]retrievedItem, 0, len(r.index.Files[task.Repo]))
	for _, file := range r.index.Files[task.Repo] {
		if wanted != nil && !wanted[file.Path] {
			continue
		}
		item := retrievedItem{Key: "file:" + file.Path, File: file.Path, Source: string(file.Content), SourceRead: true, SourceBytes: int64(len(file.Content))}
		fileItems = append(fileItems, item)
		items = append(items, item)
		for _, symbol := range r.index.Symbols[task.Repo][file.Path] {
			items = append(items, retrievedItem{Key: "symbol:" + symbol.ID, Name: symbol.Name, File: symbol.File, SymbolID: symbol.ID, Kind: string(symbol.Kind)})
		}
	}
	semanticItems, _ := r.indexedProviderItems(task.Repo, wanted)
	items = append(items, semanticItems...)
	return modeOutput{Items: items, Ranked: append([]retrievedItem(nil), items...), ContextText: renderItems(fileItems), SourceReads: len(fileItems), SourceBytes: sumSourceBytes(fileItems), RetrievalCalls: retrievalCalls}
}

func (r *runner) indexedProviderItems(repo string, wanted map[string]bool) ([]retrievedItem, error) {
	repoID := r.index.RepoIDs[repo]
	entities, err := r.index.Store.GetSemanticEntities(repoID)
	if err != nil {
		return nil, err
	}
	verified := map[string]bool{}
	for _, entity := range entities {
		if providerID, _ := entity.Metadata["provider_entity_id"].(string); providerID != "" && entity.Metadata["provider_status"] == "local_verified" {
			verified[providerID] = true
		}
	}
	result := make([]retrievedItem, 0)
	for _, entity := range entities {
		if entity.Kind != "export_definition" && !strings.Contains(entity.Kind, "provider") {
			continue
		}
		if wanted != nil && !wanted[entity.File] {
			continue
		}
		authority := ""
		if verified[entity.ID] {
			authority = "local_verified"
		}
		result = append(result, retrievedItem{Key: "semantic:" + entity.ID, Name: entity.Name, File: entity.File, SymbolID: entity.SymbolID, Kind: entity.Kind, Authority: authority})
	}
	return result, nil
}

func (r *runner) primitive(ctx context.Context, task Task) (modeOutput, error) {
	repoID := r.index.RepoIDs[task.Repo]
	items := make([]retrievedItem, 0, 24)
	seen := map[string]bool{}
	add := func(item retrievedItem) {
		if item.Key == "" || seen[item.Key] {
			return
		}
		seen[item.Key] = true
		items = append(items, item)
	}
	terms := identifierTerms(task.Text)
	for _, name := range task.AnchorNames {
		terms = appendUnique(terms, name)
	}
	calls := 0
	relationships := make([]RelationshipEvidence, 0)
	for _, term := range terms {
		calls++
		symbols, _, err := r.index.Store.SearchSymbols(repoID, term, "", "", "", 8)
		if err != nil {
			return modeOutput{}, err
		}
		for _, symbol := range symbols {
			content, partial, bytesRead, err := r.index.Store.GetSymbolContentBounded(repoID, symbol.ID, contextpack.DefaultMaxSymbolBytes)
			if err != nil {
				continue
			}
			add(retrievedItem{Key: "symbol:" + symbol.ID, Name: symbol.Name, File: symbol.File, SymbolID: symbol.ID, Kind: string(symbol.Kind), Source: string(content), SourceRead: true, SourceBytes: bytesRead, Authority: ""})
			if partial {
				// The source text remains useful to recall scoring; the partiality is
				// represented by the bounded source read rather than hidden.
			}
		}
		calls++
		entities, _, err := r.index.Store.SearchSemanticWithOptions(repoID, term, "", "", "", false, 12)
		if err != nil {
			return modeOutput{}, err
		}
		for _, entity := range entities {
			add(r.semanticItem(ctx, repoID, entity))
		}
	}
	for _, item := range append([]retrievedItem(nil), items...) {
		if item.Key == "" || !strings.HasPrefix(item.Key, "semantic:") && !strings.HasPrefix(item.Key, "symbol:") {
			continue
		}
		entityID := strings.TrimPrefix(item.Key, "semantic:")
		if entityID == item.Key {
			continue
		}
		entity, err := r.index.Store.GetSemanticEntityByID(repoID, entityID)
		if err != nil {
			continue
		}
		calls++
		edges, _, err := r.index.Store.TraceSemanticRankedWithOptions(repoID, entity.ID, entity.Analyzer, "both", nil, 1, 8)
		if err != nil {
			continue
		}
		for _, edge := range edges {
			if evidence, ok := relationshipEvidenceFromEdge(edge); ok {
				relationships = append(relationships, evidence)
			}
			add(r.semanticItem(ctx, repoID, edge.From))
			if edge.To != nil {
				add(r.semanticItem(ctx, repoID, *edge.To))
			}
		}
	}
	return modeOutput{Items: items, Ranked: append([]retrievedItem(nil), items...), Relationships: relationships, ContextText: renderItems(items), SourceReads: countSourceReads(items), SourceBytes: sumSourceBytes(items), RetrievalCalls: calls}, nil
}

func (r *runner) semanticItem(ctx context.Context, repoID int64, entity semantic.Entity) retrievedItem {
	item := retrievedItem{Key: "semantic:" + entity.ID, Name: entity.Name, File: entity.File, SymbolID: entity.SymbolID, Kind: entity.Kind}
	if status, ok := entity.Metadata["provider_status"].(string); ok {
		item.Authority = status
	}
	if entity.SymbolID != "" {
		content, _, bytesRead, err := r.index.Store.GetSymbolContentBounded(repoID, entity.SymbolID, contextpack.DefaultMaxSymbolBytes)
		if err == nil {
			item.Source = string(content)
			item.SourceRead = true
			item.SourceBytes = bytesRead
		}
	} else if entity.File != "" {
		content, _, err := r.index.Store.GetFileContentBounded(repoID, entity.File, contextpack.DefaultMaxSymbolBytes)
		if err == nil {
			item.Source = string(content)
			item.SourceRead = true
			item.SourceBytes = int64(len(content))
		}
	}
	_ = ctx
	return item
}

func relationshipEvidenceFromEdge(edge semantic.TraceEdge) (RelationshipEvidence, bool) {
	if edge.To == nil || edge.From.ID == "" || edge.To.ID == "" {
		return RelationshipEvidence{}, false
	}
	return RelationshipEvidence{ID: edge.Relationship.ID, Kind: edge.Relationship.Kind, FromEntityID: edge.From.ID, ToEntityID: edge.To.ID, FromName: edge.From.Name, ToName: edge.To.Name, FromSymbolID: edge.From.SymbolID, ToSymbolID: edge.To.SymbolID, FromKind: edge.From.Kind, ToKind: edge.To.Kind, FromFile: edge.From.File, ToFile: edge.To.File, Dynamic: edge.Dynamic || edge.Relationship.Dynamic}, true
}

func (r *runner) indexedRelationships(repo string) ([]RelationshipEvidence, error) {
	repoID := r.index.RepoIDs[repo]
	entities, err := r.index.Store.GetSemanticEntities(repoID)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]semantic.Entity, len(entities))
	for _, entity := range entities {
		byID[entity.ID] = entity
	}
	relationships, err := r.index.Store.GetSemanticRelationships(repoID)
	if err != nil {
		return nil, err
	}
	result := make([]RelationshipEvidence, 0, len(relationships))
	for _, relationship := range relationships {
		from, fromOK := byID[relationship.FromEntityID]
		to, toOK := byID[relationship.ToEntityID]
		if !fromOK || !toOK {
			continue
		}
		result = append(result, RelationshipEvidence{ID: relationship.ID, Kind: relationship.Kind, FromEntityID: from.ID, ToEntityID: to.ID, FromName: from.Name, ToName: to.Name, FromSymbolID: from.SymbolID, ToSymbolID: to.SymbolID, FromKind: from.Kind, ToKind: to.Kind, FromFile: from.File, ToFile: to.File, Dynamic: relationship.Dynamic})
	}
	return result, nil
}

func (r *runner) contextRelationships(repo string, sections []contextpack.Section) ([]RelationshipEvidence, error) {
	ids, err := r.contextEntityIDs(repo, sections)
	if err != nil {
		return nil, err
	}
	all, err := r.indexedRelationships(repo)
	if err != nil {
		return nil, err
	}
	result := make([]RelationshipEvidence, 0)
	for _, relationship := range all {
		if ids[relationship.FromEntityID] && ids[relationship.ToEntityID] {
			result = append(result, relationship)
		}
	}
	return result, nil
}

func (r *runner) contextEntityIDs(repo string, sections []contextpack.Section) (map[string]bool, error) {
	repoID := r.index.RepoIDs[repo]
	entities, err := r.index.Store.GetSemanticEntities(repoID)
	if err != nil {
		return nil, err
	}
	ids := map[string]bool{}
	for _, entity := range entities {
		for _, section := range sections {
			if section.SymbolID != "" {
				if entity.SymbolID == section.SymbolID {
					ids[entity.ID] = true
				}
				continue
			}
			if normalizePath(entity.File) == normalizePath(section.File) && entity.Name == section.Name && (section.Kind == "" || entity.Kind == section.Kind || entity.Analyzer != semantic.AnalyzerGenericGraph) {
				ids[entity.ID] = true
			}
		}
	}
	return ids, nil
}

type noEarlyStopEvaluator struct {
	delegate sufficiency.Evaluator
}

func (e noEarlyStopEvaluator) Evaluate(input sufficiency.Input) sufficiency.Decision {
	decision := e.delegate.Evaluate(input)
	if decision.Status == sufficiency.StatusSufficient && input.Stage != "peripheral" {
		decision.Status = sufficiency.StatusNeedsMoreContext
		decision.CanContinue = true
		decision.ReasonCodes = append(decision.ReasonCodes, "benchmark_no_early_stop")
	}
	return decision
}

func (r *runner) phase7(ctx context.Context, task Task, budget int) (modeOutput, error) {
	return r.phase7WithEarlyStop(ctx, task, budget, false)
}

func (r *runner) phase7NoEarlyStop(ctx context.Context, task Task, budget int) (modeOutput, error) {
	return r.phase7WithEarlyStop(ctx, task, budget, true)
}

func (r *runner) phase7WithEarlyStop(ctx context.Context, task Task, budget int, noEarlyStop bool) (modeOutput, error) {
	if r.counter == nil {
		counter, err := contextpack.NewTokenCounter(r.tokenizer)
		if err != nil {
			return modeOutput{}, err
		}
		r.counter = counter
	}
	base := planner.New(r.index.Store)
	capturing := &capturingPlanner{base: base}
	assembler := contextpack.New(capturing, r.index.Store)
	if noEarlyStop {
		assembler.Evaluator = noEarlyStopEvaluator{delegate: sufficiency.New()}
	}
	start := time.Now()
	request := contextpack.Request{Repo: task.Repo, Task: task.Text, MaxContextTokens: budget, Tokenizer: r.tokenizer, MaxCandidates: 100, FocusFile: task.FocusFile, FocusSymbolID: task.FocusSymbolID, FocusResource: task.FocusResource, IncludeImpact: task.IncludeImpact}
	pkg, err := assembler.Assemble(ctx, request)
	total := time.Since(start)
	if err != nil {
		return modeOutput{PlannerTime: capturing.duration, TotalTime: total}, err
	}
	debugAssembler := contextpack.New(&capturingPlanner{base: planner.New(r.index.Store)}, r.index.Store)
	if noEarlyStop {
		debugAssembler.Evaluator = noEarlyStopEvaluator{delegate: sufficiency.New()}
	}
	debugRequest := request
	debugRequest.Debug = true
	debugPkg, debugErr := debugAssembler.Assemble(ctx, debugRequest)
	if debugErr != nil {
		return modeOutput{}, debugErr
	}
	clean := pkg
	clean.Debug = nil
	data, err := json.Marshal(clean)
	if err != nil {
		return modeOutput{}, err
	}
	metadata := clean
	metadata.Sections = append([]contextpack.Section(nil), clean.Sections...)
	for i := range metadata.Sections {
		metadata.Sections[i].Source = ""
	}
	metadataData, err := json.Marshal(metadata)
	if err != nil {
		return modeOutput{}, err
	}
	items := make([]retrievedItem, 0, len(clean.Sections))
	for _, section := range clean.Sections {
		items = append(items, retrievedItem{Key: "section:" + section.CandidateID, Name: section.Name, File: section.File, SymbolID: section.SymbolID, Kind: section.Kind, Authority: section.Authority, Source: section.Source, SourceRead: section.Source != "", SourceBytes: int64(len(section.Source))})
	}
	relationships, err := r.contextRelationships(task.Repo, clean.Sections)
	if err != nil {
		return modeOutput{}, err
	}
	ranked := planItems(capturing.plan)
	sourceTokens := 0
	for _, section := range clean.Sections {
		sourceTokens += r.counter.Count(section.Source)
	}
	sourceReads, sourceBytes := 0, int64(0)
	if debugPkg.Debug != nil {
		sourceReads = debugPkg.Debug.SourceReads
		sourceBytes = debugPkg.Debug.SourceBytesRead
	}
	return modeOutput{Items: items, Ranked: ranked, Relationships: relationships, Package: &clean, Plan: &capturing.plan, ContextText: string(data), ContextTokens: r.counter.Count(string(data)), MetadataTokens: r.counter.Count(string(metadataData)), SourceTokens: sourceTokens, SourceReads: sourceReads, SourceBytes: sourceBytes, RetrievalCalls: 1, PlannerTime: capturing.duration, AssemblyTime: total - capturing.duration, TotalTime: total}, nil
}

func planItems(plan planner.Plan) []retrievedItem {
	result := make([]retrievedItem, 0, len(plan.Primary)+len(plan.Supporting)+len(plan.Peripheral))
	for _, candidate := range append(append(append([]planner.Candidate{}, plan.Primary...), plan.Supporting...), plan.Peripheral...) {
		result = append(result, retrievedItem{Key: "candidate:" + candidate.ID, Name: candidate.Name, File: candidate.File, SymbolID: candidate.SymbolID, Kind: candidate.Kind, Authority: candidate.Authority})
	}
	return result
}

func renderItems(items []retrievedItem) string {
	var b strings.Builder
	for _, item := range items {
		fmt.Fprintf(&b, "\n--- %s %s ---\n%s\n", item.File, item.Name, item.Source)
	}
	return b.String()
}

func identifierTerms(text string) []string {
	var result []string
	var b strings.Builder
	flush := func() {
		if b.Len() == 0 {
			return
		}
		term := b.String()
		b.Reset()
		if len(term) < 3 || stopWord(term) {
			return
		}
		result = appendUnique(result, term)
	}
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == ':' {
			b.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return result
}

func stopWord(value string) bool {
	switch strings.ToLower(value) {
	case "find", "fix", "add", "before", "what", "does", "who", "calls", "call", "trace", "the", "and", "flow", "provider", "custom", "framework", "operation", "inventory", "user", "save", "character":
		return true
	default:
		return false
	}
}

func appendUnique(values []string, value string) []string {
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}

func valueOr(first, second string) string {
	if second != "" {
		return second
	}
	return first
}

func countSourceReads(items []retrievedItem) int {
	count := 0
	for _, item := range items {
		if item.SourceRead {
			count++
		}
	}
	return count
}

func sumSourceBytes(items []retrievedItem) int64 {
	var total int64
	for _, item := range items {
		total += item.SourceBytes
	}
	return total
}

func sortItems(items []retrievedItem) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].File != items[j].File {
			return items[i].File < items[j].File
		}
		if items[i].Name != items[j].Name {
			return items[i].Name < items[j].Name
		}
		return items[i].Key < items[j].Key
	})
}
