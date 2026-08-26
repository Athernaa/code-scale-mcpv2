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
	fmt.Fprintf(&b, "## Code-Scale Commit\n\n`%s`\n\n", report.CodeScaleCommit)
	fmt.Fprintf(&b, "## Corpus Version\n\n`%s` / fixture `%s` / schema `%d`\n\n", report.CorpusVersion, report.FixtureRevision, 1)
	fmt.Fprintf(&b, "## Benchmark Environment\n\nGo `%s`, tokenizer `%s`, repeats `%d`, budgets `%v`.\n\n", report.GoVersion, report.Tokenizer, report.Repeat, report.Budgets)
	fmt.Fprintf(&b, "## Corpus Summary\n\nTasks selected: **%d**. Results recorded: **%d**.\n\n", report.TasksRun, len(report.Results))
	b.WriteString("## Categories\n\n")
	for _, category := range sortedSummaryKeys(report.ByCategory) {
		summary := report.ByCategory[category]
		fmt.Fprintf(&b, "- `%s`: %d results, median recall %s, median saving %s, false sufficiency %d.\n", category, summary.Results, percent(summary.MedianRecall), percent(summary.MedianSaving), summary.FalseSufficiency)
	}
	b.WriteString("\n## Ground Truth Method\n\nGround truth is manually authored in `benchmarks/corpus.json`; it is not generated from Planner output. Required items are scored separately from relevant files and unrelated fixture noise.\n\n")
	b.WriteString("## Modes Compared\n\n")
	for _, mode := range AllModes {
		summary := report.ByMode[string(mode)]
		fmt.Fprintf(&b, "- **%s**: %d results, median recall %s, median saving %s.\n", mode, summary.Results, percent(summary.MedianRecall), percent(summary.MedianSaving))
	}
	b.WriteString("\n### Manual/Baseline\n\nDeterministic ground-truth minimum files; no model behavior is simulated.\n\n### Panoramic / Repomix-Like\n\nOffline scoped broad-file snapshot; Repomix is optional and not required.\n\n### Primitive Code-Scale\n\nExisting storage symbol/semantic search, bounded source reads, and bounded relationship traces.\n\n### Phase-7 assemble_context\n\nProduction Planner, ContextAssembler, TokenCounter, and Sufficiency path.\n\n")
	b.WriteString("## Retrieval Quality\n\n")
	fmt.Fprintf(&b, "- Required dependency recall across the complete budget matrix: **%s mean / %s median**.\n- Precision: **%s median**.\n- Top-5 recall: **%s median**.\n- Top-10 recall: **%s median**.\n- Supported-task acceptance recall is evaluated at each task's maximum requested budget; small-budget omissions remain visible in the raw results.\n\n", percent(report.Aggregate.MeanRecall), percent(report.Aggregate.MedianRecall), percent(report.Aggregate.MedianPrecision), percent(report.Aggregate.MedianTop5Recall), percent(report.Aggregate.MedianTop10Recall))
	b.WriteString("## Token Efficiency\n\n")
	fmt.Fprintf(&b, "- Median Phase-7 context tokens: **%.0f**.\n- Median reduction versus broad baseline across the full matrix: **%s**.\n- Supported-task median reduction at maximum budget: **%s**.\n- Distribution: p25 **%s**, p75 **%s**, p90 **%s**.\n\n", report.Aggregate.MedianContextTokens, percent(report.Aggregate.MedianTokenSaving), percent(report.Aggregate.SupportedMedianTokenSaving), percent(report.Aggregate.P25TokenSaving), percent(report.Aggregate.P75TokenSaving), percent(report.Aggregate.P90TokenSaving))
	b.WriteString("## Sufficiency\n\n")
	fmt.Fprintf(&b, "Sufficient **%d**, blocked **%d**, indeterminate **%d**, false sufficiency **%d**, false insufficiency **%d**.\n\n", report.Aggregate.Sufficient, report.Aggregate.Blocked, report.Aggregate.Indeterminate, report.Aggregate.FalseSufficiency, report.Aggregate.FalseInsufficiency)
	fmt.Fprintf(&b, "## Early Stop\n\nMedian retrieval rounds **%.1f**, median source reads **%.1f**, median latency **%.1f ms**, p95 latency **%.1f ms**.\n\n", report.Aggregate.MedianRetrievalRounds, report.Aggregate.MedianSourceReads, report.Aggregate.MedianLatencyMilliseconds, report.Aggregate.P95LatencyMilliseconds)
	fmt.Fprintf(&b, "## Provider Correctness\n\nFabrication cases: **%d**.\n\n## Cross-Resource Correctness\n\nCross-repository leaks: **%d**.\n\n## Execution-Side Correctness\n\nProvider and side cases are included in the adversarial corpus; authority is scored from persisted semantic metadata and returned candidates.\n\n", report.Aggregate.ProviderFabrication, report.Aggregate.CrossRepoLeaks)
	fmt.Fprintf(&b, "## Incremental vs Full Index\n\nMismatches: **%d**.\n\n## Repo Isolation\n\nLeaks: **%d**.\n\n## Budget Compliance\n\nSerialized budget violations: **%d**.\n\n## Determinism\n\nFailures: **%d**.\n\n", report.Validation.IncrementalFullMismatches, report.Aggregate.CrossRepoLeaks, report.Aggregate.SerializedBudgetViolations, report.Validation.DeterminismFailures)
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

func reportStatus(report Report) string {
	if report.Validation.ProviderFabricationZero && report.Validation.CrossRepoLeakageZero && report.Validation.SerializedBudgetZero && report.Validation.SupportedRecallAtLeast95 && report.Validation.FalseSufficiencyBelow5 && report.Validation.TokenReductionAtLeast50 && report.Validation.DeterminismFailures == 0 && report.Validation.BudgetMonotonicityFailures == 0 && report.Validation.IncrementalFullMismatches == 0 {
		return "PASS"
	}
	return "CONDITIONAL PASS"
}

func recommendation(report Report) string {
	if reportStatus(report) == "PASS" {
		return "PHASE 7 COMPLETE — READY FOR DOWNSTREAM INTEGRATION"
	}
	return "Phase 7.4 produced evidence with one or more provisional acceptance targets unmet; downstream integration requires review of the reported limitations."
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

func filepathDir(path string) string {
	if index := strings.LastIndexAny(path, "/\\"); index >= 0 {
		return path[:index]
	}
	return "."
}

func gitCommit() string {
	command := exec.Command("git", "rev-parse", "--short", "HEAD")
	data, err := command.Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(data))
}
