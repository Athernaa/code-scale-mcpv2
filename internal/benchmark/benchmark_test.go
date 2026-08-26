package benchmark

import (
	"context"
	"testing"

	"github.com/Athernaa/code-scale-mcpv2/internal/contextpack"
)

func TestCorpusLoadsIndependentGroundTruthAndFixtureIndexes(t *testing.T) {
	corpus, err := LoadCorpus("../../benchmarks/corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(corpus.Tasks) != 30 || len(corpus.Repositories) != 7 {
		t.Fatalf("unexpected corpus size: tasks=%d repos=%d", len(corpus.Tasks), len(corpus.Repositories))
	}
	index, cleanup, err := BuildFixtureIndex(context.Background(), corpus, "../../benchmarks/fixtures")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	for _, repo := range corpus.Repositories {
		if index.RepoIDs[repo.Name] == 0 || len(index.Files[repo.Name]) == 0 {
			t.Fatalf("fixture repo was not indexed: %s", repo.Name)
		}
	}
}

func TestRelationshipGroundTruthRequiresDistinctEndpoints(t *testing.T) {
	item := retrievedItem{Name: "SaveUser", File: "user/service.go", Source: "SaveUser"}
	task := Task{Required: []GroundTruthItem{{Kind: "relationship", From: "SaveUser", To: "SaveUser"}}}
	if requiredFound(task.Required[0], []retrievedItem{item}) {
		t.Fatal("one endpoint satisfied a two-endpoint relationship")
	}
	if !requiredFound(task.Required[0], []retrievedItem{item, item}) {
		t.Fatal("distinct endpoint occurrences were not accepted")
	}
	if _, err := contextpack.NewTokenCounter(contextpack.TokenizerO200K); err != nil {
		t.Fatal(err)
	}
}

func TestPhase7KeepsNormalSerializedPackageWithinSmallBudget(t *testing.T) {
	corpus, err := LoadCorpus("../../benchmarks/corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	index, cleanup, err := BuildFixtureIndex(context.Background(), corpus, "../../benchmarks/fixtures")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	var task Task
	for _, candidate := range corpus.Tasks {
		if candidate.ID == "fivem_focused_inventory" {
			task = candidate
			break
		}
	}
	runner := &runner{index: index, corpus: corpus, tokenizer: corpus.DefaultTokenizer}
	output, err := runner.phase7(context.Background(), task, 1024)
	if err != nil {
		t.Fatal(err)
	}
	counter, err := contextpack.NewTokenCounter(corpus.DefaultTokenizer)
	if err != nil {
		t.Fatal(err)
	}
	if got := counter.Count(output.ContextText); got > 1024 {
		t.Fatalf("normal Phase-7 package exceeded serialized budget: %d", got)
	}
}
