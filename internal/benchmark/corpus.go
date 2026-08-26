package benchmark

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func LoadCorpus(path string) (Corpus, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Corpus{}, fmt.Errorf("read corpus %s: %w", path, err)
	}
	var corpus Corpus
	if err := json.Unmarshal(data, &corpus); err != nil {
		return Corpus{}, fmt.Errorf("decode corpus %s: %w", path, err)
	}
	if corpus.SchemaVersion != 1 || corpus.CorpusVersion == "" || len(corpus.Repositories) == 0 || len(corpus.Tasks) == 0 {
		return Corpus{}, fmt.Errorf("invalid corpus metadata")
	}
	repos := make(map[string]bool, len(corpus.Repositories))
	for index := range corpus.Repositories {
		repo := &corpus.Repositories[index]
		if repo.Name == "" || repo.Path == "" || (repo.Kind != "generic" && repo.Kind != "fivem") || (repo.Tier != "" && repo.Tier != "small" && repo.Tier != "realistic") {
			return Corpus{}, fmt.Errorf("invalid repository spec %#v", repo)
		}
		if repo.Tier == "" {
			repo.Tier = "small"
		}
		if repos[repo.Name] {
			return Corpus{}, fmt.Errorf("duplicate repository %q", repo.Name)
		}
		repos[repo.Name] = true
	}
	tasks := make(map[string]bool, len(corpus.Tasks))
	for _, task := range corpus.Tasks {
		if task.ID == "" || task.Repo == "" || task.Text == "" || len(task.Required) == 0 || len(task.RelevantFiles) == 0 {
			return Corpus{}, fmt.Errorf("invalid task %q", task.ID)
		}
		if task.AcceptanceClass != AcceptanceSupported && task.AcceptanceClass != AcceptanceDiagnostic && task.AcceptanceClass != AcceptanceAdversarial {
			return Corpus{}, fmt.Errorf("task %q has invalid acceptance_class %q", task.ID, task.AcceptanceClass)
		}
		if task.AcceptanceClass != AcceptanceSupported && task.ExclusionReason == "" {
			return Corpus{}, fmt.Errorf("excluded task %q requires exclusion_reason", task.ID)
		}
		for _, required := range task.Required {
			if required.Kind != "symbol" && required.Kind != "file" && required.Kind != "relationship" && required.Kind != "provider" {
				return Corpus{}, fmt.Errorf("task %q has invalid required kind %q", task.ID, required.Kind)
			}
			if required.Kind == "relationship" && (required.From == "" || required.To == "" || required.Relationship == "") {
				return Corpus{}, fmt.Errorf("task %q has incomplete relationship ground truth", task.ID)
			}
		}
		if !repos[task.Repo] {
			return Corpus{}, fmt.Errorf("task %q references unknown repo %q", task.ID, task.Repo)
		}
		if tasks[task.ID] {
			return Corpus{}, fmt.Errorf("duplicate task %q", task.ID)
		}
		tasks[task.ID] = true
	}
	return corpus, nil
}

func (c Corpus) Repository(name string) (RepositorySpec, bool) {
	for _, repo := range c.Repositories {
		if repo.Name == name {
			return repo, true
		}
	}
	return RepositorySpec{}, false
}

func FixtureRoot(corpusPath string) string {
	return filepath.Join(filepath.Dir(corpusPath), "fixtures")
}

func SelectTasks(corpus Corpus, taskID, category string) []Task {
	result := make([]Task, 0, len(corpus.Tasks))
	for _, task := range corpus.Tasks {
		if taskID != "" && task.ID != taskID {
			continue
		}
		if category != "" && task.Category != category {
			continue
		}
		result = append(result, task)
	}
	return result
}

func SelectModes(value string) ([]Mode, error) {
	if value == "" || value == "all" {
		return append([]Mode(nil), AllModes...), nil
	}
	for _, mode := range AllModes {
		if string(mode) == value {
			return []Mode{mode}, nil
		}
	}
	return nil, fmt.Errorf("unsupported benchmark mode %q", value)
}

func ParseBudgets(value string) ([]int, error) {
	if value == "" {
		return []int{512, 2048, 8000, 32000}, nil
	}
	var result []int
	for _, raw := range splitComma(value) {
		var budget int
		if _, err := fmt.Sscan(raw, &budget); err != nil || budget <= 0 {
			return nil, fmt.Errorf("invalid budget %q", raw)
		}
		result = append(result, budget)
	}
	return result, nil
}

func splitComma(value string) []string {
	var result []string
	start := 0
	for i := 0; i <= len(value); i++ {
		if i != len(value) && value[i] != ',' {
			continue
		}
		if part := value[start:i]; part != "" {
			result = append(result, part)
		}
		start = i + 1
	}
	return result
}
