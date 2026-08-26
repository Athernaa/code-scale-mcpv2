package benchmark

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
)

func FinalizeReport(report Report) Report {
	report.GoVersion = runtime.Version()
	report.CodeScaleCommit = gitCommit()
	report.TreeSHA = gitTree()
	report.DirtyWorktree = gitDirty()
	report.ByMode = summarizeModes(report.Results)
	return report
}

func WriteReport(report Report, outputPath, markdownPath string) error {
	report = FinalizeReport(report)
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepathDir(outputPath), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(outputPath, append(data, '\n'), 0644); err != nil {
		return err
	}
	if markdownPath == "" {
		return nil
	}
	if err := os.MkdirAll(filepathDir(markdownPath), 0755); err != nil {
		return err
	}
	return os.WriteFile(markdownPath, []byte(MarkdownReport(report)), 0644)
}

func MarkdownReport(report Report) string {
	status := reportStatus(report)
	var b strings.Builder
	fmt.Fprintf(&b, "# Phase 7.4 Final Validation\n\n%s\n\n", status)
	fmt.Fprintf(&b, "## Code-Scale Commit\n\n`%s`\n\nTree: `%s`; dirty worktree: **%t**.\n\n", report.CodeScaleCommit, report.TreeSHA, report.DirtyWorktree)
	fmt.Fprintf(&b, "## Corpus Version\n\n`%s` / fixture `%s` / schema `%d`\n\n", report.CorpusVersion, report.FixtureRevision, 1)
	fmt.Fprintf(&b, "## Benchmark Environment\n\nGo `%s`, tokenizer `%s`, repeats `%d`, budgets `%v`.\n\n", report.GoVersion, report.Tokenizer, report.Repeat, report.Budgets)
	fmt.Fprintf(&b, "## Corpus Summary\n\nTasks selected: **%d**. Results recorded: **%d**. Supported tasks: **%d total / %d passed / %d failed**. Diagnostic/open-ended: **%d**. Adversarial safety: **%d**. Excluded from recall acceptance: **%d**.\n\n", report.TasksRun, len(report.Results), report.Acceptance.SupportedTotal, report.Acceptance.SupportedPassed, report.Acceptance.SupportedFailed, report.Acceptance.DiagnosticTotal, report.Acceptance.AdversarialTotal, report.Acceptance.ExcludedTotal)
	for _, taskID := range sortedStringKeys(report.Acceptance.ExcludedTasks) {
		reason := report.Acceptance.ExcludedTasks[taskID]
		fmt.Fprintf(&b, "- excluded `%s`: %s\n", taskID, reason)
	}
	if len(report.Acceptance.ExcludedTasks) > 0 {
		b.WriteString("\n")
	}
	for _, tier := range sortedSummaryKeys(report.ByTier) {
		summary := report.ByTier[tier]
		fmt.Fprintf(&b, "- `%s` fixtures: %d results, median recall %s, broad reduction %s, scoped reduction %s.\n", tier, summary.Results, percent(summary.MedianRecall), percent(summary.MedianSaving), percent(summary.MedianScopedSaving))
	}
	b.WriteString("\n")
	b.WriteString("## Categories\n\n")
	for _, category := range sortedSummaryKeys(report.ByCategory) {
		summary := report.ByCategory[category]
		fmt.Fprintf(&b, "- `%s`: %d results, median recall %s, broad saving %s, scoped saving %s, false sufficiency %d.\n", category, summary.Results, percent(summary.MedianRecall), percent(summary.MedianSaving), percent(summary.MedianScopedSaving), summary.FalseSufficiency)
	}
	b.WriteString("\n## Ground Truth Method\n\nGround truth is manually authored in `benchmarks/corpus.json`; it is not generated from Planner output. Required items are scored separately from relevant files and unrelated fixture noise.\n\n")
	b.WriteString("## Modes Compared\n\n")
	for _, mode := range AllModes {
		summary := report.ByMode[string(mode)]
		saving := summary.MedianSaving
		if mode == ModeScopedPanoramic {
			saving = summary.MedianScopedSaving
		}
		fmt.Fprintf(&b, "- **%s**: %d results, median recall %s, relevant-baseline saving %s.\n", mode, summary.Results, percent(summary.MedianRecall), percent(saving))
	}
	b.WriteString("\n### Manual/Baseline\n\nDeterministic ground-truth minimum files; no model behavior is simulated.\n\n### Panoramic / Repomix-Like\n\nOffline whole-fixture broad-file snapshot. It is not Repomix and no Repomix claim is made.\n\n### Scoped Panoramic\n\nOffline relevant-file snapshot using the same task scope available to every mode.\n\n### Primitive Code-Scale\n\nExisting storage symbol/semantic search, bounded source reads, and bounded relationship traces.\n\n### Phase-7 assemble_context\n\nProduction Planner, ContextAssembler, TokenCounter, and Sufficiency path.\n\n### Phase-7 no early stop\n\nBenchmark-only paired run using the same Planner/ranking policy while continuing eligible packing stages. It is not a production MCP mode.\n\n")
	b.WriteString("## Retrieval Quality\n\n")
	fmt.Fprintf(&b, "- Required dependency recall across the complete budget matrix: **%s mean / %s median**.\n- Precision: **%s median**.\n- Top-5 recall: **%s median**.\n- Top-10 recall: **%s median**.\n- Supported-task acceptance recall is evaluated at each task's maximum requested budget; small-budget omissions remain visible in the raw results.\n\n", percent(report.Aggregate.MeanRecall), percent(report.Aggregate.MedianRecall), percent(report.Aggregate.MedianPrecision), percent(report.Aggregate.MedianTop5Recall), percent(report.Aggregate.MedianTop10Recall))
	b.WriteString("## Token Efficiency\n\n")
	fmt.Fprintf(&b, "- Median Phase-7 context tokens: **%.0f**.\n- Median reduction versus broad baseline: **%s**; supported maximum-budget reduction: **%s**.\n- Median reduction versus scoped baseline: **%s**; supported maximum-budget scoped reduction: **%s**.\n- Distribution against broad baseline: p25 **%s**, p75 **%s**, p90 **%s**.\n\n", report.Aggregate.MedianContextTokens, percent(report.Aggregate.MedianTokenSaving), percent(report.Aggregate.SupportedMedianTokenSaving), percent(report.Aggregate.MedianScopedTokenSaving), percent(report.Aggregate.SupportedMedianScopedTokenSaving), percent(report.Aggregate.P25TokenSaving), percent(report.Aggregate.P75TokenSaving), percent(report.Aggregate.P90TokenSaving))
	b.WriteString("## Sufficiency\n\n")
	fmt.Fprintf(&b, "Sufficient **%d**, blocked **%d**, indeterminate **%d**, false sufficiency **%d**, false insufficiency **%d**. False sufficiency: all budgets **%d**, maximum budget **%d**, supported **%d**, diagnostic **%d**, adversarial **%d**. Empty retrievals **%d**.\n\n", report.Aggregate.Sufficient, report.Aggregate.Blocked, report.Aggregate.Indeterminate, report.Aggregate.FalseSufficiency, report.Aggregate.FalseInsufficiency, report.Aggregate.FalseSufficiencyAllBudgets, report.Aggregate.FalseSufficiencyMaxBudget, report.Aggregate.FalseSufficiencySupported, report.Aggregate.FalseSufficiencyDiagnostic, report.Aggregate.FalseSufficiencyAdversarial, report.Aggregate.EmptyRetrievalCount)
	fmt.Fprintf(&b, "## Baseline Accounting\n\nPanoramic self-baseline violations: **%d**; scoped panoramic self-baseline violations: **%d**. Both must be zero.\n\n", countModeBaselineViolations(report.Results, ModePanoramic), countModeBaselineViolations(report.Results, ModeScopedPanoramic))
	fmt.Fprintf(&b, "## Early Stop\n\nMedian retrieval rounds **%.1f**, median source reads **%.1f**, median latency **%.1f ms**, p95 latency **%.1f ms**. Paired early-stop totals: **%d tokens**, **%d source reads**, **%d rounds** avoided.\n\n", report.Aggregate.MedianRetrievalRounds, report.Aggregate.MedianSourceReads, report.Aggregate.MedianLatencyMilliseconds, report.Aggregate.P95LatencyMilliseconds, report.Aggregate.EarlyStopTokensAvoided, report.Aggregate.EarlyStopSourceReadsAvoided, report.Aggregate.EarlyStopRoundsAvoided)
	for _, category := range sortedEarlyStopKeys(report.EarlyStopByCategory) {
		summary := report.EarlyStopByCategory[category]
		fmt.Fprintf(&b, "- `%s`: %d paired runs, %d tokens, %d source reads, %d rounds avoided.\n", category, summary.Tasks, summary.TokensAvoided, summary.SourceReadsAvoided, summary.RoundsAvoided)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "## Provider Correctness\n\nFabrication cases: **%d**.\n\n## Cross-Resource Correctness\n\nCross-repository leaks: **%d**.\n\n## Execution-Side Correctness\n\nProvider and side cases are included in the adversarial corpus; authority is scored from persisted semantic metadata and returned candidates.\n\n", report.Aggregate.ProviderFabrication, report.Aggregate.CrossRepoLeaks)
	fmt.Fprintf(&b, "## Incremental vs Full Index\n\nMismatches: **%d**.\n\n## Repo Isolation\n\nLeaks: **%d**.\n\n## Budget Compliance\n\nSerialized budget violations: **%d**; baseline accounting violations: **%d**.\n\n## Determinism\n\nFailures: **%d**.\n\n", report.Validation.IncrementalFullMismatches, report.Aggregate.CrossRepoLeaks, report.Aggregate.SerializedBudgetViolations, report.Aggregate.BaselineAccountingViolations, report.Validation.DeterminismFailures)
	b.WriteString("## Latency\n\nLatency is reported descriptively and is not used as a fragile pass/fail threshold.\n\n## Generic Go Results\n\nSee category/task rows in the machine-readable report.\n\n## TypeScript Results\n\nSee category/task rows in the machine-readable report.\n\n## Lua Results\n\nSee category/task rows in the machine-readable report.\n\n## FiveM Results\n\nSee category/task rows in the machine-readable report.\n\n## Framework Results\n\nProvider, authority, and execution-side tasks are scored separately from generic recall.\n\n## Adversarial Results\n\nDuplicate symbols, duplicate providers, external/dynamic providers, wrong-side calls, and repository markers are explicit corpus cases.\n\n## Metamorphic Results\n\nThe runner checks repeated-output fingerprints, budget recall monotonicity, and an incremental-refresh versus fresh-full-index FiveM comparison.\n\n")
	b.WriteString("## Benchmark-Discovered Bugs\n\n")
	for _, note := range report.BenchmarkNotes {
		if strings.Contains(note, "defect") || strings.Contains(note, "fidelity") {
			fmt.Fprintf(&b, "- %s\n", note)
		}
	}
	b.WriteString("\n")
	b.WriteString("## Verification Commands\n\nThe benchmark CLI is offline and uses the same repository Go toolchain. Release commands are listed in `benchmarks/README.md`.\n\n## jCodeMunch Final Audit\n\njCodeMunch was used during development for architecture, caller, dependency, changed-symbol, and blast-radius inspection. It is not imported by the harness or runtime.\n\n")
	fmt.Fprintf(&b, "## Measured Claims\n\nOnly the percentages and counts above are claims, and they are computed from this report's task results. No external-agent success or Repomix claim is implied.\n\n## Remaining Limitations\n\n%s\n\n## Final Recommendation\n\n%s\n", limitations(report), recommendation(report))
	return b.String()
}

func Status(report Report) string {
	if !report.Validation.ProviderFabricationZero || !report.Validation.CrossRepoLeakageZero || !report.Validation.SerializedBudgetZero || !report.Validation.FalseSufficiencyZero || !report.Validation.SupportedRecallAtLeast95 || !report.Validation.RuntimeErrorsZero || !report.Validation.BaselineAccountingZero || report.Validation.DeterminismFailures > 0 || report.Validation.BudgetMonotonicityFailures > 0 || report.Validation.IncrementalFullMismatches > 0 {
		return "FAIL"
	}
	if !report.Validation.TokenReductionAtLeast50 {
		return "CONDITIONAL PASS"
	}
	return "PASS"
}

func reportStatus(report Report) string {
	return Status(report)
}

func recommendation(report Report) string {
	if Status(report) == "PASS" {
		return "PHASE 7 COMPLETE — READY FOR DOWNSTREAM INTEGRATION"
	}
	return "Phase 7.4 status " + Status(report) + "; review the hard-gate and provisional limitations reported above before downstream integration."
}

func limitations(report Report) string {
	var values []string
	if !report.Validation.SupportedRecallAtLeast95 {
		values = append(values, "supported deterministic recall is below 95%")
	}
	if !report.Validation.TokenReductionAtLeast50 {
		values = append(values, "supported-task median token reduction is below the provisional 50% target")
	}
	if !report.Validation.FalseSufficiencyBelow5 {
		values = append(values, "false sufficiency is at or above 5%")
	}
	if report.Validation.DeterminismFailures > 0 {
		values = append(values, "determinism failures were observed")
	}
	if report.Aggregate.RuntimeErrors > 0 {
		values = append(values, "runtime errors were observed")
	}
	if !report.Validation.ProviderFabricationZero {
		values = append(values, "provider fabrication was observed")
	}
	if !report.Validation.CrossRepoLeakageZero {
		values = append(values, "cross-repository leakage was observed")
	}
	if !report.Validation.SerializedBudgetZero {
		values = append(values, "serialized budget violations were observed")
	}
	if report.Aggregate.FalseSufficiencyAllBudgets > 0 {
		values = append(values, "false sufficiency was observed")
	}
	if report.Validation.BudgetMonotonicityFailures > 0 {
		values = append(values, "budget monotonicity failures were observed")
	}
	if report.Validation.IncrementalFullMismatches > 0 {
		values = append(values, "incremental/full semantic mismatches were observed")
	}
	if len(values) == 0 {
		return "No provisional acceptance limitation was observed by the deterministic run. External-agent task execution and Repomix comparison were not part of this offline benchmark."
	}
	return strings.Join(values, "; ") + ". External-agent task execution and Repomix comparison were not part of this offline benchmark."
}

func percent(value float64) string { return fmt.Sprintf("%.1f%%", value*100) }

func sortedSummaryKeys(values map[string]Summary) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedEarlyStopKeys(values map[string]EarlyStopSummary) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedStringKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func filepathDir(path string) string {
	if index := strings.LastIndexAny(path, "/\\"); index >= 0 {
		return path[:index]
	}
	return "."
}

func gitCommit() string {
	command := exec.Command("git", "rev-parse", "HEAD")
	data, err := command.Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(data))
}

func gitTree() string {
	command := exec.Command("git", "rev-parse", "HEAD^{tree}")
	data, err := command.Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(data))
}

func gitDirty() bool {
	command := exec.Command("git", "status", "--porcelain", "--untracked-files=all")
	data, err := command.Output()
	return err != nil || len(strings.TrimSpace(string(data))) > 0
}
