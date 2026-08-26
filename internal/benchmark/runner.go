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
	}}
	for _, task := range tasks {
		broadBaseline := r.panoramic(task)
		broadBaselineTokens := r.counter.Count(broadBaseline.ContextText)
		for _, mode := range modes {
			for _, budget := range budgets {
				var previousFingerprint string
				for repeat := 1; repeat <= cfg.Repeat; repeat++ {
					if err := ctx.Err(); err != nil {
						return Report{}, err
					}
					output, runErr := r.execute(ctx, task, mode, budget)
					result := scoreResult(task, mode, budget, repeat, output, r.counter, broadBaselineTokens)
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
	report.ByCategory = summarizeCategories(report.Results)
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
	switch mode {
	case ModeManual:
		return r.manual(task), nil
	case ModePanoramic:
		return r.panoramic(task), nil
	case ModePrimitive:
		return r.primitive(ctx, task)
	case ModePhase7:
		return r.phase7(ctx, task, budget)
	default:
		return modeOutput{}, fmt.Errorf("unsupported mode %q", mode)
	}
}

func (r *runner) manual(task Task) modeOutput {
	files := r.index.Files[task.Repo]
	wanted := map[string]bool{}
	for _, path := range task.RelevantFiles {
		wanted[path] = true
	}
	items := make([]retrievedItem, 0)
	for _, file := range files {
		if !wanted[file.Path] {
			continue
		}
		items = append(items, retrievedItem{Key: "file:" + file.Path, File: file.Path, Source: string(file.Content), SourceRead: true, SourceBytes: int64(len(file.Content))})
	}
	return modeOutput{Items: items, Ranked: append([]retrievedItem(nil), items...), ContextText: renderItems(items), SourceReads: len(items), SourceBytes: sumSourceBytes(items), RetrievalCalls: 1 + len(task.Required)}
}

func (r *runner) panoramic(task Task) modeOutput {
	items := make([]retrievedItem, 0, len(r.index.Files[task.Repo]))
	for _, file := range r.index.Files[task.Repo] {
		items = append(items, retrievedItem{Key: "file:" + file.Path, File: file.Path, Source: string(file.Content), SourceRead: true, SourceBytes: int64(len(file.Content))})
	}
	return modeOutput{Items: items, Ranked: append([]retrievedItem(nil), items...), ContextText: renderItems(items), SourceReads: len(items), SourceBytes: sumSourceBytes(items), RetrievalCalls: 1}
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
			add(r.semanticItem(ctx, repoID, edge.From))
			if edge.To != nil {
				add(r.semanticItem(ctx, repoID, *edge.To))
			}
		}
	}
	return modeOutput{Items: items, Ranked: append([]retrievedItem(nil), items...), ContextText: renderItems(items), SourceReads: countSourceReads(items), SourceBytes: sumSourceBytes(items), RetrievalCalls: calls}, nil
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

func (r *runner) phase7(ctx context.Context, task Task, budget int) (modeOutput, error) {
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
	start := time.Now()
	request := contextpack.Request{Repo: task.Repo, Task: task.Text, MaxContextTokens: budget, Tokenizer: r.tokenizer, MaxCandidates: 100, FocusFile: task.FocusFile, FocusSymbolID: task.FocusSymbolID, FocusResource: task.FocusResource, IncludeImpact: task.IncludeImpact}
	pkg, err := assembler.Assemble(ctx, request)
	total := time.Since(start)
	if err != nil {
		return modeOutput{PlannerTime: capturing.duration, TotalTime: total}, err
	}
	debugAssembler := contextpack.New(&capturingPlanner{base: planner.New(r.index.Store)}, r.index.Store)
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
	return modeOutput{Items: items, Ranked: ranked, Package: &clean, Plan: &capturing.plan, ContextText: string(data), ContextTokens: r.counter.Count(string(data)), MetadataTokens: r.counter.Count(string(metadataData)), SourceTokens: sourceTokens, SourceReads: sourceReads, SourceBytes: sourceBytes, RetrievalCalls: 1, PlannerTime: capturing.duration, AssemblyTime: total - capturing.duration, TotalTime: total}, nil
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
