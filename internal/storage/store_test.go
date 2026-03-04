package storage

import (
	"testing"

	"github.com/syphon1c/code-scale-mcp/internal/parser"
)

func newTestStore(t *testing.T) *IndexStore {
	t.Helper()
	tmp := t.TempDir()
	store, err := NewIndexStore(tmp)
	if err != nil {
		t.Fatalf("NewIndexStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestSaveAndListRepos(t *testing.T) {
	store := newTestStore(t)

	files := map[string]string{"main.py": "abc123"}
	langs := map[string]string{"main.py": "python"}
	symbols := []parser.Symbol{
		{
			ID: "main.py::hello#function", File: "main.py", Name: "hello",
			QualifiedName: "hello", Kind: "function", Language: "python",
			Signature: "def hello()", Line: 1, EndLine: 2,
			ByteOffset: 0, ByteLength: 20, ContentHash: "abc",
			Decorators: []string{}, Keywords: []string{},
		},
	}

	err := store.SaveIndex("test", "repo", "local", "", files, langs, symbols)
	if err != nil {
		t.Fatalf("SaveIndex: %v", err)
	}

	repos, err := store.ListRepos()
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if len(repos) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(repos))
	}
	if repos[0].Repo != "test/repo" {
		t.Errorf("expected test/repo, got %s", repos[0].Repo)
	}
	if repos[0].FileCount != 1 {
		t.Errorf("expected 1 file, got %d", repos[0].FileCount)
	}
	if repos[0].SymbolCount != 1 {
		t.Errorf("expected 1 symbol, got %d", repos[0].SymbolCount)
	}
}

func TestGetSymbolByID(t *testing.T) {
	store := newTestStore(t)

	symbols := []parser.Symbol{
		{
			ID: "main.py::hello#function", File: "main.py", Name: "hello",
			QualifiedName: "hello", Kind: "function", Language: "python",
			Signature: "def hello()", Line: 1, EndLine: 2,
			ByteOffset: 0, ByteLength: 20, ContentHash: "abc",
			Decorators: []string{}, Keywords: []string{},
		},
		{
			ID: "main.py::world#function", File: "main.py", Name: "world",
			QualifiedName: "world", Kind: "function", Language: "python",
			Signature: "def world()", Line: 3, EndLine: 4,
			ByteOffset: 20, ByteLength: 20, ContentHash: "def",
			Decorators: []string{}, Keywords: []string{},
		},
	}

	_ = store.SaveIndex("test", "repo", "local", "", map[string]string{"main.py": "abc"}, map[string]string{"main.py": "python"}, symbols)

	repoID, err := store.GetRepoID("test/repo")
	if err != nil {
		t.Fatalf("GetRepoID: %v", err)
	}

	sym, err := store.GetSymbolByID(repoID, "main.py::hello#function")
	if err != nil {
		t.Fatalf("GetSymbolByID: %v", err)
	}
	if sym.Name != "hello" {
		t.Errorf("expected hello, got %s", sym.Name)
	}

	_, err = store.GetSymbolByID(repoID, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent symbol")
	}
}

func TestSearchSymbols(t *testing.T) {
	store := newTestStore(t)

	symbols := []parser.Symbol{
		{ID: "main.py::authenticate#function", File: "main.py", Name: "authenticate",
			QualifiedName: "authenticate", Kind: "function", Language: "python",
			Signature: "def authenticate(token)", Docstring: "Verify auth token",
			Decorators: []string{}, Keywords: []string{}},
		{ID: "main.py::get_user#function", File: "main.py", Name: "get_user",
			QualifiedName: "get_user", Kind: "function", Language: "python",
			Signature: "def get_user(id)", Docstring: "Get a user by ID",
			Decorators: []string{}, Keywords: []string{}},
	}

	_ = store.SaveIndex("test", "repo", "local", "", map[string]string{"main.py": "abc"}, map[string]string{"main.py": "python"}, symbols)
	repoID, _ := store.GetRepoID("test/repo")

	results, scores, err := store.SearchSymbols(repoID, "authenticate", "", "", "", 10)
	if err != nil {
		t.Fatalf("SearchSymbols: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected search results")
	}
	if results[0].Name != "authenticate" {
		t.Errorf("expected authenticate as top result, got %s", results[0].Name)
	}
	if scores[0] <= 0 {
		t.Error("expected positive score")
	}
}

func TestDeleteIndex(t *testing.T) {
	store := newTestStore(t)

	_ = store.SaveIndex("test", "repo", "local", "", map[string]string{"main.py": "abc"}, map[string]string{"main.py": "python"}, nil)

	err := store.DeleteIndex("test/repo")
	if err != nil {
		t.Fatalf("DeleteIndex: %v", err)
	}

	repos, _ := store.ListRepos()
	if len(repos) != 0 {
		t.Errorf("expected 0 repos after delete, got %d", len(repos))
	}
}

func TestDetectChanges(t *testing.T) {
	store := newTestStore(t)

	_ = store.SaveIndex("test", "repo", "local", "",
		map[string]string{"a.py": "hash1", "b.py": "hash2", "c.py": "hash3"},
		map[string]string{"a.py": "python", "b.py": "python", "c.py": "python"},
		nil)

	repoID, _ := store.GetRepoID("test/repo")

	current := map[string]string{
		"a.py": "hash1",    // unchanged
		"b.py": "hash_new", // changed
		"d.py": "hash4",    // added
		// c.py deleted
	}

	changed, added, deleted := store.DetectChanges(repoID, current)

	if len(changed) != 1 || changed[0] != "b.py" {
		t.Errorf("expected [b.py] changed, got %v", changed)
	}
	if len(added) != 1 || added[0] != "d.py" {
		t.Errorf("expected [d.py] added, got %v", added)
	}
	if len(deleted) != 1 || deleted[0] != "c.py" {
		t.Errorf("expected [c.py] deleted, got %v", deleted)
	}
}

func TestTokenTracker(t *testing.T) {
	store := newTestStore(t)
	tracker := NewTokenTracker(store.db)

	total, err := tracker.AddSavings(1000)
	if err != nil {
		t.Fatalf("AddSavings: %v", err)
	}
	if total != 1000 {
		t.Errorf("expected 1000, got %d", total)
	}

	total, _ = tracker.AddSavings(500)
	if total != 1500 {
		t.Errorf("expected 1500, got %d", total)
	}

	if tracker.GetTotalSavings() != 1500 {
		t.Errorf("expected 1500 total")
	}
}

func TestEstimateSavings(t *testing.T) {
	saved := EstimateSavings(40000, 200)
	if saved != 9950 {
		t.Errorf("expected 9950, got %d", saved)
	}

	saved = EstimateSavings(100, 200)
	if saved != 0 {
		t.Error("expected 0 for negative savings")
	}
}

func TestCostAvoided(t *testing.T) {
	costs := CostAvoided(1000000)
	if costs["claude_opus"] != 25.0 {
		t.Errorf("expected 25.0 for opus, got %f", costs["claude_opus"])
	}
	if costs["gpt5_latest"] != 10.0 {
		t.Errorf("expected 10.0 for gpt5, got %f", costs["gpt5_latest"])
	}
}
