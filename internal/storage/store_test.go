package storage

import (
	"testing"

	"github.com/Athernaa/code-scale-mcpv2/internal/parser"
)

func testSymbol(file, name string) parser.Symbol {
	return parser.Symbol{
		ID:            file + "::" + name + "#function",
		File:          file,
		Name:          name,
		QualifiedName: name,
		Kind:          "function",
		Language:      "lua",
		Signature:     "function " + name + "()",
		Decorators:    []string{},
		Keywords:      []string{},
	}
}

func ftsContains(t *testing.T, store *IndexStore, query string) bool {
	t.Helper()
	var count int
	if err := store.DB().QueryRow("SELECT COUNT(*) FROM symbols_fts WHERE symbols_fts MATCH ?", query).Scan(&count); err != nil {
		t.Fatalf("FTS query %q: %v", query, err)
	}
	return count > 0
}

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

func TestSearchTextUsesExactVerificationAndRecursivePatterns(t *testing.T) {
	store := newTestStore(t)
	files := map[string]string{"src/a.ts": "one exact:event", "src/components/a.ts": "two exact:event", "src/components/deep/a.ts": "three exact:event"}
	langs := map[string]string{"src/a.ts": "typescript", "src/components/a.ts": "typescript", "src/components/deep/a.ts": "typescript"}
	for path, content := range files { if err := store.SaveContentFile("test", "text", path, []byte(content)); err != nil { t.Fatal(err) } }
	if err := store.ReplaceRepoIndex("test", "text", "local", "", files, langs, nil); err != nil { t.Fatal(err) }
	repoID, err := store.GetRepoID("test/text"); if err != nil { t.Fatal(err) }
	results, err := store.SearchText(repoID, "exact:event", "src/**/*.ts", 10, 0)
	if err != nil { t.Fatal(err) }
	if len(results) != 3 { t.Fatalf("expected all punctuation-heavy exact matches, got %d", len(results)) }
}

func TestReplaceRepoIndexReplacesAllRepositoryData(t *testing.T) {
	store := newTestStore(t)
	if err := store.ReplaceRepoIndex("test", "replace", "local", "", map[string]string{
		"old.lua": "old",
		"keep.lua": "keep",
	}, map[string]string{
		"old.lua": "lua",
		"keep.lua": "lua",
	}, []parser.Symbol{testSymbol("old.lua", "oldFunction"), testSymbol("keep.lua", "keepFunction")}); err != nil {
		t.Fatalf("initial ReplaceRepoIndex: %v", err)
	}

	if err := store.ReplaceRepoIndex("test", "replace", "local", "", map[string]string{
		"new.lua": "new",
	}, map[string]string{"new.lua": "lua"}, []parser.Symbol{testSymbol("new.lua", "newFunction")}); err != nil {
		t.Fatalf("replacement ReplaceRepoIndex: %v", err)
	}

	repoID, err := store.GetRepoID("test/replace")
	if err != nil {
		t.Fatal(err)
	}
	files, err := store.GetFiles(repoID)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "new.lua" {
		t.Fatalf("expected only new.lua after replacement, got %#v", files)
	}
}

func TestIncrementalFileUpdatesPreserveUnrelatedDataAndFTS(t *testing.T) {
	store := newTestStore(t)
	files := map[string]string{"client.lua": "client-v1", "server.lua": "server-v1"}
	langs := map[string]string{"client.lua": "lua", "server.lua": "lua"}
	initial := []parser.Symbol{testSymbol("client.lua", "clientFunction"), testSymbol("server.lua", "serverFunction")}
	if err := store.ReplaceRepoIndex("local", "game", "local", "", files, langs, initial); err != nil {
		t.Fatalf("initial index: %v", err)
	}
	repoID, err := store.GetRepoID("local/game")
	if err != nil {
		t.Fatal(err)
	}

	repos, err := store.ListRepos()
	if err != nil || len(repos) != 1 || repos[0].FileCount != 2 {
		t.Fatalf("expected two indexed files, repos=%#v err=%v", repos, err)
	}
	if _, err := store.GetSymbolByID(repoID, "client.lua::clientFunction#function"); err != nil {
		t.Fatalf("clientFunction missing: %v", err)
	}
	if _, err := store.GetSymbolByID(repoID, "server.lua::serverFunction#function"); err != nil {
		t.Fatalf("serverFunction missing: %v", err)
	}

	updated := testSymbol("client.lua", "newClientFunction")
	if err := store.UpsertFileIndex("local", "game", "local", "", "client.lua", "client-v2", "lua", []parser.Symbol{updated}); err != nil {
		t.Fatalf("incremental update: %v", err)
	}

	repos, err = store.ListRepos()
	if err != nil || len(repos) != 1 || repos[0].FileCount != 2 {
		t.Fatalf("expected two files after incremental update, repos=%#v err=%v", repos, err)
	}
	if _, err := store.GetSymbolByID(repoID, updated.ID); err != nil {
		t.Fatalf("new client symbol missing: %v", err)
	}
	if _, err := store.GetSymbolByID(repoID, "server.lua::serverFunction#function"); err != nil {
		t.Fatalf("unrelated server symbol was lost: %v", err)
	}
	if _, err := store.GetSymbolByID(repoID, "client.lua::clientFunction#function"); err == nil {
		t.Fatal("old client symbol still exists")
	}
	if !ftsContains(t, store, "newClientFunction") || ftsContains(t, store, "clientFunction") {
		t.Fatal("FTS did not reflect the incremental symbol replacement")
	}

	if err := store.DeleteFileFromIndex("local", "game", "client.lua"); err != nil {
		t.Fatalf("delete client file: %v", err)
	}
	files, err := store.GetFiles(repoID)
	if err != nil || len(files) != 1 || files[0].Path != "server.lua" {
		t.Fatalf("server file did not survive deletion, files=%#v err=%v", files, err)
	}
	if _, err := store.GetSymbolByID(repoID, "server.lua::serverFunction#function"); err != nil {
		t.Fatalf("server symbol did not survive deletion: %v", err)
	}
	if ftsContains(t, store, "newClientFunction") {
		t.Fatal("deleted client symbol is still in FTS")
	}
}

func TestUpsertFileIndexAddsNewFile(t *testing.T) {
	store := newTestStore(t)
	if err := store.ReplaceRepoIndex("local", "new-file", "local", "", map[string]string{"one.lua": "one"}, map[string]string{"one.lua": "lua"}, []parser.Symbol{testSymbol("one.lua", "oneFunction")}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertFileIndex("local", "new-file", "local", "", "two.lua", "two", "lua", []parser.Symbol{testSymbol("two.lua", "twoFunction")}); err != nil {
		t.Fatal(err)
	}
	repoID, _ := store.GetRepoID("local/new-file")
	files, err := store.GetFiles(repoID)
	if err != nil || len(files) != 2 {
		t.Fatalf("expected two files after new-file upsert, files=%#v err=%v", files, err)
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
