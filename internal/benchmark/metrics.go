package benchmark

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Athernaa/code-scale-mcpv2/internal/contextpack"
)

func scoreResult(task Task, mode Mode, budget, repeat int, output modeOutput, counter contextpack.TokenCounter, baselineContextTokens int) TaskResult {
	contextTokens := output.ContextTokens
	if contextTokens == 0 {
		output.ContextText = renderItems(output.Items)
		contextTokens = counter.Count(output.ContextText)
	}
	sourceTokens := output.SourceTokens
	if sourceTokens == 0 {
		for _, item := range output.Items {
			sourceTokens += counter.Count(item.Source)
		}
	}
	if baselineContextTokens <= 0 {
		baselineContextTokens = contextTokens
	}
	result := TaskResult{TaskID: task.ID, Category: task.Category, Repo: task.Repo, Mode: mode, Budget: budget, Repeat: repeat, EligibleForAcceptance: task.EligibleForAcceptance, ContextTokens: contextTokens, MetadataTokens: output.MetadataTokens, SourceTokens: sourceTokens, BaselineContextTokens: baselineContextTokens, CandidateCount: len(output.Ranked), SourceReads: output.SourceReads, SourceBytes: output.SourceBytes, RetrievalCalls: output.RetrievalCalls, PlannerMilliseconds: durationMilliseconds(output.PlannerTime), AssemblyMilliseconds: durationMilliseconds(output.AssemblyTime), TotalMilliseconds: durationMilliseconds(output.TotalTime)}
	result.Retrieved = summaries(output.Items)
	result.Ranked = summaries(output.Ranked)
	if baselineContextTokens > 0 {
		result.TokenSaving = 1 - float64(contextTokens)/float64(baselineContextTokens)
	}
	for index, required := range task.Required {
		if requiredFound(required, output.Items) {
			result.RequiredFound++
		} else {
			result.RequiredMissing = append(result.RequiredMissing, requiredLabel(index, required))
		}
	}
	result.RequiredTotal = len(task.Required)
	if result.RequiredTotal > 0 {
		result.DependencyRecall = float64(result.RequiredFound) / float64(result.RequiredTotal)
	}
	result.SymbolRecall = typeRecall(task, output.Items, "symbol")
	result.FileRecall = typeRecall(task, output.Items, "file")
	result.RelationshipRecall = typeRecall(task, output.Items, "relationship")
	result.ProviderRecall = typeRecall(task, output.Items, "provider")
	result.Precision = precision(task, output.Items)
	result.Top5Recall = topKRecall(task, output.Ranked, 5)
	result.Top10Recall = topKRecall(task, output.Ranked, 10)
	if output.Package != nil {
		result.ReportedUsedTokens = output.Package.Budget.UsedTokens
		result.SufficiencyStatus = output.Package.Sufficiency.Status
		result.SufficiencyStage = output.Package.Sufficiency.EvaluatedAfterStage
		result.SufficiencyReasons = append([]string(nil), output.Package.Sufficiency.ReasonCodes...)
		result.RetrievalRounds = len(output.Package.Rounds)
		if len(output.Package.Rounds) > 0 {
			result.StopStage = output.Package.Rounds[len(output.Package.Rounds)-1].Stage
		}
		result.FalseSufficiency = result.SufficiencyStatus == "sufficient" && len(result.RequiredMissing) > 0
		result.FalseInsufficiency = result.SufficiencyStatus != "sufficient" && len(result.RequiredMissing) == 0
		if result.ContextTokens > budget {
			result.RuntimeError = "serialized budget exceeded"
		}
	} else {
		result.SufficiencyStatus = "not_evaluated"
	}
	result.ProviderFabrication = providerFabrication(task, output.Items, output.Ranked)
	result.CrossRepoLeak = crossRepoLeak(task, output.Items)
	result.DeterminismFingerprint = fingerprint(task, mode, budget, output)
	return result
}

func summaries(items []retrievedItem) []RetrievedSummary {
	result := make([]RetrievedSummary, 0, len(items))
	for _, item := range items {
		result = append(result, RetrievedSummary{Key: item.Key, Name: item.Name, File: item.File, SymbolID: item.SymbolID, Kind: item.Kind, Authority: item.Authority})
	}
	return result
}

func requiredFound(required GroundTruthItem, items []retrievedItem) bool {
	switch required.Kind {
	case "relationship":
		for fromIndex, fromItem := range items {
			if !itemNameMatches(required.From, fromItem) {
				continue
			}
			for toIndex, toItem := range items {
				if fromIndex != toIndex && itemNameMatches(required.To, toItem) {
					return true
				}
			}
		}
		return false
	default:
		for _, item := range items {
			if itemMatches(required, item) {
				return true
			}
		}
		return false
	}
}

func itemMatches(required GroundTruthItem, item retrievedItem) bool {
	if required.Kind == "file" {
		return normalizePath(required.File) == normalizePath(item.File)
	}
	if required.File != "" && normalizePath(required.File) != normalizePath(item.File) {
		return false
	}
	if required.Name != "" && !itemNameMatches(required.Name, item) {
		return false
	}
	if required.Kind == "provider" && required.Authority != "" && item.Authority != required.Authority {
		return false
	}
	return required.Name != "" || required.File != ""
}

func itemNameMatches(name string, item retrievedItem) bool {
	if item.Name == name {
		return true
	}
	if strings.Contains(item.SymbolID, "::"+name+"#") || strings.HasSuffix(item.SymbolID, "::"+name) {
		return true
	}
	return item.Source != "" && strings.Contains(item.Source, name)
}

func findName(name, file string, items []retrievedItem) (bool, int) {
	for index, item := range items {
		if (file == "" || normalizePath(item.File) == normalizePath(file)) && itemNameMatches(name, item) {
			return true, index
		}
	}
	return false, -1
}

func typeRecall(task Task, items []retrievedItem, kind string) float64 {
	total, found := 0, 0
	for _, required := range task.Required {
		if required.Kind != kind {
			continue
		}
		total++
		if requiredFound(required, items) {
			found++
		}
	}
	if total == 0 {
		return 1
	}
	return float64(found) / float64(total)
}

func topKRecall(task Task, items []retrievedItem, k int) float64 {
	if len(task.Required) == 0 {
		return 1
	}
	limit := minInt(k, len(items))
	found := 0
	for _, required := range task.Required {
		if requiredFound(required, items[:limit]) {
			found++
		}
	}
	return float64(found) / float64(len(task.Required))
}

func precision(task Task, items []retrievedItem) float64 {
	if len(items) == 0 {
		return 1
	}
	relevant := map[string]bool{}
	for _, file := range task.RelevantFiles {
		relevant[normalizePath(file)] = true
	}
	found := 0
	for _, item := range items {
		if relevant[normalizePath(item.File)] {
			found++
		}
	}
	return float64(found) / float64(len(items))
}

func providerFabrication(task Task, items, ranked []retrievedItem) bool {
	if task.ForbiddenVerified == "" {
		return false
	}
	for _, item := range append(append([]retrievedItem{}, items...), ranked...) {
		if itemNameMatches(task.ForbiddenVerified, item) && item.Authority == "local_verified" {
			return true
		}
	}
	return false
}

func crossRepoLeak(task Task, items []retrievedItem) bool {
	for _, marker := range task.ForbiddenMarkers {
		for _, item := range items {
			if strings.Contains(item.Source, marker) {
				return true
			}
		}
	}
	return false
}

func requiredLabel(index int, item GroundTruthItem) string {
	data, _ := json.Marshal(item)
	return string(rune('a'+index)) + ":" + string(data)
}

func fingerprint(task Task, mode Mode, budget int, output modeOutput) string {
	var b strings.Builder
	b.WriteString(task.ID)
	b.WriteString("|")
	b.WriteString(string(mode))
	b.WriteString("|")
	b.WriteString(fmtInt(budget))
	for _, item := range output.Ranked {
		b.WriteString("|")
		b.WriteString(item.Key)
		b.WriteString(":")
		b.WriteString(item.Authority)
	}
	if output.Package != nil {
		b.WriteString("|")
		b.WriteString(output.Package.Sufficiency.Status)
		b.WriteString("|")
		b.WriteString(output.Package.StopReason)
		for _, section := range output.Package.Sections {
			b.WriteString("|")
			b.WriteString(section.CandidateID)
			b.WriteString(":")
			b.WriteString(section.Source)
		}
	}
	hash := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(hash[:])
}

func aggregateResults(results []TaskResult) Aggregate {
	phase := filterMode(results, ModePhase7)
	if len(phase) == 0 {
		phase = results
	}
	a := Aggregate{Results: len(phase)}
	seenTasks := map[string]bool{}
	for _, result := range phase {
		seenTasks[result.TaskID] = true
		if result.RequiredFound == result.RequiredTotal && result.RuntimeError == "" {
			a.PassedRetrieval++
		}
		if result.SufficiencyStatus == "sufficient" {
			a.Sufficient++
		}
		switch result.SufficiencyStatus {
		case "blocked":
			a.Blocked++
		case "indeterminate":
			a.Indeterminate++
		}
		if result.FalseSufficiency {
			a.FalseSufficiency++
		}
		if result.FalseInsufficiency {
			a.FalseInsufficiency++
		}
		if result.ProviderFabrication {
			a.ProviderFabrication++
		}
		if result.CrossRepoLeak {
			a.CrossRepoLeaks++
		}
		if result.Nondeterministic {
			a.Nondeterministic++
		}
	}
	a.Tasks = len(seenTasks)
	a.MedianRecall = median(field(phase, func(r TaskResult) float64 { return r.DependencyRecall }))
	a.MeanRecall = mean(field(phase, func(r TaskResult) float64 { return r.DependencyRecall }))
	a.MedianTop5Recall = median(field(phase, func(r TaskResult) float64 { return r.Top5Recall }))
	a.MedianTop10Recall = median(field(phase, func(r TaskResult) float64 { return r.Top10Recall }))
	a.MedianPrecision = median(field(phase, func(r TaskResult) float64 { return r.Precision }))
	a.MedianContextTokens = median(field(phase, func(r TaskResult) float64 { return float64(r.ContextTokens) }))
	a.MedianTokenSaving = median(field(phase, func(r TaskResult) float64 { return r.TokenSaving }))
	a.SupportedMedianTokenSaving = median(field(eligibleAtMaximumBudget(phase), func(r TaskResult) float64 { return r.TokenSaving }))
	a.MeanTokenSaving = mean(field(phase, func(r TaskResult) float64 { return r.TokenSaving }))
	savings := field(phase, func(r TaskResult) float64 { return r.TokenSaving })
	a.P25TokenSaving = percentile(savings, .25)
	a.P75TokenSaving = percentile(savings, .75)
	a.P90TokenSaving = percentile(savings, .90)
	a.MedianSourceReads = median(field(phase, func(r TaskResult) float64 { return float64(r.SourceReads) }))
	a.MedianRetrievalRounds = median(field(phase, func(r TaskResult) float64 { return float64(r.RetrievalRounds) }))
	a.MedianLatencyMilliseconds = median(field(phase, func(r TaskResult) float64 { return r.TotalMilliseconds }))
	a.P95LatencyMilliseconds = percentile(field(phase, func(r TaskResult) float64 { return r.TotalMilliseconds }), .95)
	for _, result := range phase {
		if result.ContextTokens > result.Budget {
			a.SerializedBudgetViolations++
		}
	}
	return a
}

func summarizeCategories(results []TaskResult) map[string]Summary {
	result := map[string]Summary{}
	groups := map[string][]TaskResult{}
	for _, item := range filterMode(results, ModePhase7) {
		groups[item.Category] = append(groups[item.Category], item)
	}
	for category, items := range groups {
		summary := Summary{Results: len(items), Tasks: len(items)}
		for _, item := range items {
			if item.FalseSufficiency {
				summary.FalseSufficiency++
			}
			if item.ProviderFabrication {
				summary.ProviderErrors++
			}
		}
		summary.MedianRecall = median(field(items, func(r TaskResult) float64 { return r.DependencyRecall }))
		summary.MedianSaving = median(field(items, func(r TaskResult) float64 { return r.TokenSaving }))
		result[category] = summary
	}
	return result
}

func summarizeModes(results []TaskResult) map[string]Summary {
	result := map[string]Summary{}
	groups := map[Mode][]TaskResult{}
	for _, item := range results {
		groups[item.Mode] = append(groups[item.Mode], item)
	}
	for mode, items := range groups {
		summary := Summary{Results: len(items), Tasks: len(items)}
		for _, item := range items {
			if item.FalseSufficiency {
				summary.FalseSufficiency++
			}
			if item.ProviderFabrication {
				summary.ProviderErrors++
			}
		}
		summary.MedianRecall = median(field(items, func(r TaskResult) float64 { return r.DependencyRecall }))
		summary.MedianSaving = median(field(items, func(r TaskResult) float64 { return r.TokenSaving }))
		result[string(mode)] = summary
	}
	return result
}

func validateReport(results []TaskResult, aggregate Aggregate) Validation {
	phase := filterMode(results, ModePhase7)
	v := Validation{ProviderFabricationZero: aggregate.ProviderFabrication == 0, CrossRepoLeakageZero: aggregate.CrossRepoLeaks == 0, SerializedBudgetZero: aggregate.SerializedBudgetViolations == 0, DeterminismFailures: aggregate.Nondeterministic}
	eligible := eligibleAtMaximumBudget(phase)
	v.SupportedRecallAtLeast95 = mean(field(eligible, func(r TaskResult) float64 { return r.DependencyRecall })) >= .95
	falseEligible := 0
	for _, item := range eligible {
		if item.FalseSufficiency {
			falseEligible++
		}
	}
	if len(eligible) == 0 {
		v.FalseSufficiencyBelow5 = false
	} else {
		v.FalseSufficiencyBelow5 = float64(falseEligible)/float64(len(eligible)) < .05
	}
	v.TokenReductionAtLeast50 = aggregate.SupportedMedianTokenSaving >= .50
	return v
}

func eligibleAtMaximumBudget(results []TaskResult) []TaskResult {
	groups := map[string][]TaskResult{}
	for _, item := range results {
		if item.EligibleForAcceptance && item.RequiredTotal > 0 && item.RuntimeError == "" {
			key := item.TaskID + "\x00" + fmtInt(item.Repeat)
			groups[key] = append(groups[key], item)
		}
	}
	var result []TaskResult
	for _, group := range groups {
		maxBudget := 0
		for _, item := range group {
			if item.Budget > maxBudget {
				maxBudget = item.Budget
			}
		}
		for _, item := range group {
			if item.Budget == maxBudget {
				result = append(result, item)
			}
		}
	}
	return result
}

func budgetMonotonicityFailures(results []TaskResult) int {
	groups := map[string][]TaskResult{}
	for _, result := range filterMode(results, ModePhase7) {
		if result.RuntimeError != "" {
			continue
		}
		key := result.TaskID + "\x00" + fmtInt(result.Repeat)
		groups[key] = append(groups[key], result)
	}
	failures := 0
	for _, group := range groups {
		sort.Slice(group, func(i, j int) bool { return group[i].Budget < group[j].Budget })
		for i := 1; i < len(group); i++ {
			if group[i].DependencyRecall+1e-9 < group[i-1].DependencyRecall {
				failures++
			}
		}
	}
	return failures
}

func filterMode(results []TaskResult, mode Mode) []TaskResult {
	result := make([]TaskResult, 0)
	for _, item := range results {
		if item.Mode == mode {
			result = append(result, item)
		}
	}
	return result
}

func field(results []TaskResult, fn func(TaskResult) float64) []float64 {
	values := make([]float64, 0, len(results))
	for _, result := range results {
		if result.RuntimeError == "" {
			values = append(values, fn(result))
		}
	}
	return values
}

func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var total float64
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}

func median(values []float64) float64 { return percentile(values, .5) }

func percentile(values []float64, fraction float64) float64 {
	if len(values) == 0 {
		return 0
	}
	values = append([]float64(nil), values...)
	sort.Float64s(values)
	if len(values) == 1 {
		return values[0]
	}
	position := fraction * float64(len(values)-1)
	low := int(math.Floor(position))
	high := int(math.Ceil(position))
	if low == high {
		return values[low]
	}
	return values[low] + (values[high]-values[low])*(position-float64(low))
}

func durationMilliseconds(value time.Duration) float64 {
	return float64(value) / float64(time.Millisecond)
}

func fmtInt(value int) string {
	return strconv.Itoa(value)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func normalizePath(path string) string {
	return strings.ReplaceAll(strings.TrimPrefix(path, "./"), "\\", "/")
}
