package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Athernaa/code-scale-mcpv2/internal/parser"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic"
)

func TestSchemaV7BackfillsLegacySemanticOwnership(t *testing.T) {
	tmp := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(tmp, "code-scale.db"))
	if err != nil {
		t.Fatal(err)
	}
	legacy := `
CREATE TABLE repos (id INTEGER PRIMARY KEY, owner TEXT NOT NULL, name TEXT NOT NULL, repo TEXT NOT NULL UNIQUE, indexed_at TEXT NOT NULL, git_head TEXT, source_type TEXT);
CREATE TABLE semantic_entities (id TEXT PRIMARY KEY, repo_id INTEGER NOT NULL, file_path TEXT NOT NULL, symbol_id TEXT NOT NULL DEFAULT '', kind TEXT NOT NULL, name TEXT NOT NULL DEFAULT '', framework TEXT NOT NULL DEFAULT '', side TEXT NOT NULL DEFAULT 'unknown', line INTEGER NOT NULL DEFAULT 0, end_line INTEGER NOT NULL DEFAULT 0, dynamic INTEGER NOT NULL DEFAULT 0, metadata TEXT NOT NULL DEFAULT '{}');
CREATE TABLE semantic_relationships (id TEXT PRIMARY KEY, repo_id INTEGER NOT NULL, from_entity_id TEXT NOT NULL, to_entity_id TEXT NOT NULL DEFAULT '', kind TEXT NOT NULL, name TEXT NOT NULL DEFAULT '', dynamic INTEGER NOT NULL DEFAULT 0, confidence REAL NOT NULL DEFAULT 0, file_path TEXT NOT NULL DEFAULT '', line INTEGER NOT NULL DEFAULT 0);
CREATE TABLE schema_version (version INTEGER PRIMARY KEY);
INSERT INTO repos(id, owner, name, repo, indexed_at, source_type) VALUES (1, 'local', 'legacy', 'local/legacy', 'now', 'local');
INSERT INTO semantic_entities(id, repo_id, file_path, kind, name) VALUES ('legacy-entity', 1, 'main.lua', 'event_handler', 'legacy');
INSERT INTO schema_version(version) VALUES (6);`
	if _, err := db.Exec(legacy); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := NewIndexStore(tmp)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	var analyzer string
	if err := store.DB().QueryRow("SELECT analyzer FROM semantic_entities WHERE id = 'legacy-entity'").Scan(&analyzer); err != nil {
		t.Fatal(err)
	}
	if analyzer != semantic.AnalyzerFiveM {
		t.Fatalf("legacy semantic row was not backfilled as FiveM: %q", analyzer)
	}
	var version int
	if err := store.DB().QueryRow("SELECT MAX(version) FROM schema_version").Scan(&version); err != nil || version != CurrentSchemaVersion {
		t.Fatalf("schema version was not advanced to current version: version=%d err=%v", version, err)
	}
}

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
	for i := 0; i < 100; i++ {
		path := fmt.Sprintf("src/generated/file-%03d.ts", i)
		files[path] = "unrelated content"
		langs[path] = "typescript"
	}
	for path, content := range files {
		if err := store.SaveContentFile("test", "text", path, []byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.ReplaceRepoIndex("test", "text", "local", "", files, langs, nil); err != nil {
		t.Fatal(err)
	}
	repoID, err := store.GetRepoID("test/text")
	if err != nil {
		t.Fatal(err)
	}
	results, err := store.SearchText(repoID, "exact:event", "src/**/*.ts", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("expected all punctuation-heavy exact matches, got %d", len(results))
	}
	results, err = store.SearchText(repoID, "unrelated", "src/**/*.ts", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 10 {
		t.Fatalf("expected bounded candidate results for indexed query, got %d", len(results))
	}
}

func TestReplaceRepoIndexReplacesAllRepositoryData(t *testing.T) {
	store := newTestStore(t)
	if err := store.ReplaceRepoIndex("test", "replace", "local", "", map[string]string{
		"old.lua":  "old",
		"keep.lua": "keep",
	}, map[string]string{
		"old.lua":  "lua",
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
	files := map[string]string{"client.lua": "client-v1", "server.lua": "server-v1", "shared.lua": "shared-v1"}
	langs := map[string]string{"client.lua": "lua", "server.lua": "lua", "shared.lua": "lua"}
	initial := []parser.Symbol{testSymbol("client.lua", "clientFunction"), testSymbol("server.lua", "serverFunction"), testSymbol("shared.lua", "sharedFunction")}
	if err := store.ReplaceRepoIndex("local", "game", "local", "", files, langs, initial); err != nil {
		t.Fatalf("initial index: %v", err)
	}
	repoID, err := store.GetRepoID("local/game")
	if err != nil {
		t.Fatal(err)
	}

	repos, err := store.ListRepos()
	if err != nil || len(repos) != 1 || repos[0].FileCount != 3 {
		t.Fatalf("expected three indexed files, repos=%#v err=%v", repos, err)
	}
	if _, err := store.GetSymbolByID(repoID, "client.lua::clientFunction#function"); err != nil {
		t.Fatalf("clientFunction missing: %v", err)
	}
	if _, err := store.GetSymbolByID(repoID, "server.lua::serverFunction#function"); err != nil {
		t.Fatalf("serverFunction missing: %v", err)
	}
	if _, err := store.GetSymbolByID(repoID, "shared.lua::sharedFunction#function"); err != nil {
		t.Fatalf("sharedFunction missing: %v", err)
	}

	updated := testSymbol("client.lua", "newClientFunction")
	if err := store.UpsertFileIndex("local", "game", "local", "", "client.lua", "client-v2", "lua", []parser.Symbol{updated}); err != nil {
		t.Fatalf("incremental update: %v", err)
	}

	repos, err = store.ListRepos()
	if err != nil || len(repos) != 1 || repos[0].FileCount != 3 {
		t.Fatalf("expected three files after incremental update, repos=%#v err=%v", repos, err)
	}
	if _, err := store.GetSymbolByID(repoID, updated.ID); err != nil {
		t.Fatalf("new client symbol missing: %v", err)
	}
	if _, err := store.GetSymbolByID(repoID, "server.lua::serverFunction#function"); err != nil {
		t.Fatalf("unrelated server symbol was lost: %v", err)
	}
	if _, err := store.GetSymbolByID(repoID, "shared.lua::sharedFunction#function"); err != nil {
		t.Fatalf("unrelated shared symbol was lost: %v", err)
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
	remainingFiles, err := store.GetFiles(repoID)
	if err != nil || len(remainingFiles) != 2 {
		t.Fatalf("server and shared files did not survive deletion, files=%#v err=%v", remainingFiles, err)
	}
	remainingPaths := map[string]bool{}
	for _, file := range remainingFiles {
		remainingPaths[file.Path] = true
	}
	if !remainingPaths["server.lua"] || !remainingPaths["shared.lua"] {
		t.Fatalf("server/shared files did not survive deletion, files=%#v", remainingFiles)
	}
	if _, err := store.GetSymbolByID(repoID, "server.lua::serverFunction#function"); err != nil {
		t.Fatalf("server symbol did not survive deletion: %v", err)
	}
	if _, err := store.GetSymbolByID(repoID, "shared.lua::sharedFunction#function"); err != nil {
		t.Fatalf("shared symbol did not survive deletion: %v", err)
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

func TestSemanticStorageSearchReplaceTraceAndFileIsolation(t *testing.T) {
	store := newTestStore(t)
	files := map[string]string{"client.lua": "client", "server.lua": "server"}
	langs := map[string]string{"client.lua": "lua", "server.lua": "lua"}
	if err := store.ReplaceRepoIndex("local", "semantic", "local", "", files, langs, nil); err != nil {
		t.Fatal(err)
	}
	repoID, err := store.GetRepoID("local/semantic")
	if err != nil {
		t.Fatal(err)
	}
	trigger := semantic.Entity{ID: "trigger", Repo: "local/semantic", File: "client.lua", Kind: "event_trigger", Name: "avenlo:create", Framework: "fivem", Side: "client", Line: 1}
	handler := semantic.Entity{ID: "handler", Repo: "local/semantic", File: "server.lua", Kind: "event_handler", Name: "avenlo:create", Framework: "fivem", Side: "server", Line: 1}
	unrelated := semantic.Entity{ID: "unrelated", Repo: "local/semantic", File: "server.lua", Kind: "command_registration", Name: "revive", Framework: "fivem", Side: "server", Line: 2}
	link := semantic.Relationship{ID: "link", Repo: "local/semantic", FromEntityID: trigger.ID, ToEntityID: handler.ID, Kind: "triggers", Name: trigger.Name, Confidence: 1, File: trigger.File, Line: 1}
	if err := store.ReplaceSemanticIndex(repoID, semantic.Result{Entities: []semantic.Entity{trigger, handler, unrelated}, Relationships: []semantic.Relationship{link}}); err != nil {
		t.Fatal(err)
	}
	results, truncated, err := store.SearchSemantic(repoID, "create", "event_handler", "server", 20)
	if err != nil || len(results) != 1 || results[0].ID != handler.ID {
		t.Fatalf("semantic search filtering failed: %#v err=%v", results, err)
	}
	if truncated {
		t.Fatal("semantic search reported truncation without an extra row")
	}
	edges, truncated, err := store.TraceSemantic(repoID, trigger.ID, "outgoing", 2, 50)
	if err != nil || len(edges) != 1 || edges[0].To == nil || edges[0].To.ID != handler.ID {
		t.Fatalf("semantic trace failed: %#v err=%v", edges, err)
	}
	if truncated {
		t.Fatal("semantic trace reported truncation without an extra edge")
	}
	if _, _, err := store.TraceSemantic(repoID, "missing", "outgoing", 2, 50); err == nil {
		t.Fatal("missing semantic entity should return an error")
	}

	newTrigger := semantic.Entity{ID: "new-trigger", Repo: "local/semantic", File: "client.lua", Kind: "event_trigger", Name: "avenlo:updated", Framework: "fivem", Side: "client", Line: 1}
	if err := store.ReplaceSemanticFile(repoID, "client.lua", []semantic.Entity{newTrigger}); err != nil {
		t.Fatal(err)
	}
	entities, err := store.GetSemanticEntities(repoID)
	if err != nil {
		t.Fatal(err)
	}
	entityIDs := map[string]bool{}
	for _, entity := range entities {
		entityIDs[entity.ID] = true
	}
	if !entityIDs[newTrigger.ID] || !entityIDs[handler.ID] || !entityIDs[unrelated.ID] || entityIDs[trigger.ID] {
		t.Fatalf("file replacement did not preserve unrelated semantic entities: %#v", entities)
	}
	if err := store.ReplaceSemanticRelationships(repoID, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteFileFromIndex("local", "semantic", "client.lua"); err != nil {
		t.Fatal(err)
	}
	entities, err = store.GetSemanticEntities(repoID)
	if err != nil {
		t.Fatal(err)
	}
	for _, entity := range entities {
		if entity.File == "client.lua" {
			t.Fatalf("deleted file semantic entity survived: %#v", entity)
		}
	}
}

func TestAnalyzerScopedSemanticStorageAndIndexedTrace(t *testing.T) {
	store := newTestStore(t)
	if err := store.ReplaceRepoIndex("local", "analyzer-scope", "local", "", map[string]string{"main.ts": "x"}, map[string]string{"main.ts": "typescript"}, nil); err != nil {
		t.Fatal(err)
	}
	repoID, err := store.GetRepoID("local/analyzer-scope")
	if err != nil {
		t.Fatal(err)
	}
	fivem := semantic.Entity{ID: "fivem-entity", Analyzer: semantic.AnalyzerFiveM, Repo: "local/analyzer-scope", File: "main.ts", Kind: "event_handler", Name: "event", Line: 1}
	generic := semantic.Entity{ID: "generic-entity", Analyzer: semantic.AnalyzerGenericGraph, Repo: "local/analyzer-scope", File: "main.ts", SymbolID: "main::run", Kind: "code_symbol", Name: "run", Line: 1}
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerFiveM, semantic.Result{Entities: []semantic.Entity{fivem}}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerGenericGraph, semantic.Result{Entities: []semantic.Entity{generic}}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerFiveM, semantic.Result{}); err != nil {
		t.Fatal(err)
	}
	entities, err := store.GetSemanticEntitiesForAnalyzer(repoID, semantic.AnalyzerGenericGraph)
	if err != nil || len(entities) != 1 || entities[0].ID != generic.ID {
		t.Fatalf("FiveM replacement damaged generic graph: %#v err=%v", entities, err)
	}
	first := semantic.Entity{ID: "cycle-a", Analyzer: semantic.AnalyzerGenericGraph, Repo: "local/analyzer-scope", File: "a.ts", Kind: "code_symbol", Name: "a", SymbolID: "a"}
	second := semantic.Entity{ID: "cycle-b", Analyzer: semantic.AnalyzerGenericGraph, Repo: "local/analyzer-scope", File: "b.ts", Kind: "code_symbol", Name: "b", SymbolID: "b"}
	edges := []semantic.Relationship{
		{ID: "edge-a-b", Analyzer: semantic.AnalyzerGenericGraph, Repo: "local/analyzer-scope", FromEntityID: first.ID, ToEntityID: second.ID, Kind: "calls", File: "a.ts"},
		{ID: "edge-b-a", Analyzer: semantic.AnalyzerGenericGraph, Repo: "local/analyzer-scope", FromEntityID: second.ID, ToEntityID: first.ID, Kind: "calls", File: "b.ts"},
	}
	if err := store.ReplaceSemanticFileForAnalyzer(repoID, semantic.AnalyzerGenericGraph, first.File, []semantic.Entity{first}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceSemanticFileForAnalyzer(repoID, semantic.AnalyzerGenericGraph, second.File, []semantic.Entity{second}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceSemanticRelationshipsForAnalyzer(repoID, semantic.AnalyzerGenericGraph, edges); err != nil {
		t.Fatal(err)
	}
	traced, truncated, err := store.TraceSemanticWithOptions(repoID, first.ID, semantic.AnalyzerGenericGraph, "outgoing", []string{"calls"}, 3, 10)
	if err != nil || truncated || len(traced) != 2 {
		t.Fatalf("indexed trace did not handle cycle/filter correctly: %#v truncated=%v err=%v", traced, truncated, err)
	}
}

func TestRankedSemanticAdjacencyQueryIsStorageBounded(t *testing.T) {
	store := newTestStore(t)
	if err := store.ReplaceRepoIndex("local", "bounded-hub", "local", "", map[string]string{"hub.go": "x"}, map[string]string{"hub.go": "go"}, nil); err != nil {
		t.Fatal(err)
	}
	repo := "local/bounded-hub"
	repoID, err := store.GetRepoID(repo)
	if err != nil {
		t.Fatal(err)
	}
	root := semantic.Entity{ID: "hub", Analyzer: semantic.AnalyzerGenericGraph, Repo: repo, File: "hub.go", Kind: "code_symbol", Name: "Hub"}
	entities := make([]semantic.Entity, 1, 10001)
	entities[0] = root
	relationships := make([]semantic.Relationship, 0, 10000)
	for i := 0; i < 10000; i++ {
		target := semantic.Entity{ID: fmt.Sprintf("target-%05d", i), Analyzer: semantic.AnalyzerGenericGraph, Repo: repo, File: "hub.go", Kind: "code_symbol", Name: fmt.Sprintf("Target%05d", i)}
		entities = append(entities, target)
		relationships = append(relationships, semantic.Relationship{ID: fmt.Sprintf("edge-%05d", i), Analyzer: semantic.AnalyzerGenericGraph, Repo: repo, FromEntityID: root.ID, ToEntityID: target.ID, Kind: "references", Dynamic: true, Confidence: 0.1, File: "hub.go"})
	}
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerGenericGraph, semantic.Result{Entities: entities, Relationships: relationships}); err != nil {
		t.Fatal(err)
	}
	store.mu.RLock()
	edges, err := store.querySemanticEdgesLocked(repoID, semantic.AnalyzerGenericGraph, "outgoing", nil, []string{root.ID}, true, 26)
	store.mu.RUnlock()
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 26 {
		t.Fatalf("ranked adjacency query ignored its storage limit: got %d", len(edges))
	}
}

func TestRankedSemanticAdjacencyPrefersStaticResolvedEdgeWithinKind(t *testing.T) {
	store := newTestStore(t)
	if err := store.ReplaceRepoIndex("local", "ranked-static", "local", "", map[string]string{"hub.go": "x"}, map[string]string{"hub.go": "go"}, nil); err != nil {
		t.Fatal(err)
	}
	repo := "local/ranked-static"
	repoID, err := store.GetRepoID(repo)
	if err != nil {
		t.Fatal(err)
	}
	root := semantic.Entity{ID: "hub", Analyzer: semantic.AnalyzerGenericGraph, Repo: repo, File: "hub.go", Kind: "code_symbol", Name: "Hub"}
	staticTarget := semantic.Entity{ID: "static-target", Analyzer: semantic.AnalyzerGenericGraph, Repo: repo, File: "hub.go", Kind: "code_symbol", Name: "StaticTarget"}
	entities := []semantic.Entity{root, staticTarget}
	relationships := []semantic.Relationship{{ID: "zzzz-static", Analyzer: semantic.AnalyzerGenericGraph, Repo: repo, FromEntityID: root.ID, ToEntityID: staticTarget.ID, Kind: "references", Confidence: 1, File: "hub.go"}}
	for i := 0; i < 40; i++ {
		target := semantic.Entity{ID: fmt.Sprintf("dynamic-target-%02d", i), Analyzer: semantic.AnalyzerGenericGraph, Repo: repo, File: "hub.go", Kind: "code_symbol", Name: fmt.Sprintf("Dynamic%02d", i)}
		entities = append(entities, target)
		relationships = append(relationships, semantic.Relationship{ID: fmt.Sprintf("aaaa-dynamic-%02d", i), Analyzer: semantic.AnalyzerGenericGraph, Repo: repo, FromEntityID: root.ID, ToEntityID: target.ID, Kind: "references", Dynamic: true, Confidence: 0.1, File: "hub.go"})
	}
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerGenericGraph, semantic.Result{Entities: entities, Relationships: relationships}); err != nil {
		t.Fatal(err)
	}
	edges, truncated, err := store.TraceSemanticRankedWithOptions(repoID, root.ID, semantic.AnalyzerGenericGraph, "outgoing", []string{"references"}, 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if !truncated {
		t.Fatal("ranked adjacency did not report truncation")
	}
	for _, edge := range edges {
		if edge.Relationship.ID == "zzzz-static" {
			return
		}
	}
	t.Fatalf("static resolved edge was starved by dynamic peers: %#v", edges)
}

func TestGenericSymbolLookupIgnoresCallFacts(t *testing.T) {
	store := newTestStore(t)
	if err := store.ReplaceRepoIndex("local", "symbol-lookup", "local", "", map[string]string{"main.ts": "x"}, map[string]string{"main.ts": "typescript"}, nil); err != nil {
		t.Fatal(err)
	}
	repoID, err := store.GetRepoID("local/symbol-lookup")
	if err != nil {
		t.Fatal(err)
	}
	symbol := semantic.Entity{ID: "lookup-symbol", Analyzer: semantic.AnalyzerGenericGraph, Repo: "local/symbol-lookup", File: "main.ts", SymbolID: "main.ts::run#function", Kind: "code_symbol", Name: "run"}
	fact := semantic.Entity{ID: "lookup-call", Analyzer: semantic.AnalyzerGenericGraph, Repo: "local/symbol-lookup", File: "main.ts", Kind: "call_site", Name: "save", Metadata: map[string]any{"source_symbol_id": symbol.SymbolID}}
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerGenericGraph, semantic.Result{Entities: []semantic.Entity{symbol, fact}}); err != nil {
		t.Fatal(err)
	}
	resolved, err := store.GetSemanticEntityBySymbolID(repoID, semantic.AnalyzerGenericGraph, symbol.SymbolID)
	if err != nil || resolved.ID != symbol.ID {
		t.Fatalf("generic symbol lookup was polluted by call facts: %#v err=%v", resolved, err)
	}
}

func TestGetFileContentBoundedDoesNotReadWholeCacheFile(t *testing.T) {
	store := newTestStore(t)
	if err := store.ReplaceRepoIndex("local", "bounded-content", "local", "", map[string]string{"large.txt": "x"}, map[string]string{"large.txt": "text"}, nil); err != nil {
		t.Fatal(err)
	}
	content := []byte(strings.Repeat("é", 10000))
	if err := store.SaveContentFile("local", "bounded-content", "large.txt", content); err != nil {
		t.Fatal(err)
	}
	repoID, _ := store.GetRepoID("local/bounded-content")
	got, partial, err := store.GetFileContentBounded(repoID, "large.txt", 101)
	if err != nil {
		t.Fatal(err)
	}
	if !partial || len(got) > 101 || !utf8.Valid(got) {
		t.Fatalf("bounded UTF-8 read failed: bytes=%d partial=%v", len(got), partial)
	}
}

func TestGetSymbolContentBoundedReadsPersistedRangeWithoutFullVerification(t *testing.T) {
	store, err := NewIndexStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	unit := "middle🙂\n"
	symbolSource := "HEAD_SYMBOL_🙂\n" + strings.Repeat(unit, 10000) + "TAIL_SYMBOL_終\n"
	source := "package huge\n" + symbolSource
	symbol := parser.Symbol{ID: parser.MakeSymbolID("huge.go", "Huge", parser.KindFunction), File: "huge.go", Name: "Huge", QualifiedName: "Huge", Kind: parser.KindFunction, Language: "go", Line: 2, EndLine: strings.Count(source, "\n") + 1, ByteOffset: int64(len("package huge\n")), ByteLength: int64(len(symbolSource)), ContentHash: parser.ComputeContentHash([]byte(symbolSource))}
	if err := store.ReplaceRepoIndex("owner", "symbol-bounded", "owner", "", map[string]string{"huge.go": source}, map[string]string{"huge.go": "go"}, []parser.Symbol{symbol}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveContentFile("owner", "symbol-bounded", "huge.go", []byte(source)); err != nil {
		t.Fatal(err)
	}
	repoID, err := store.GetRepoID("owner/symbol-bounded")
	if err != nil {
		t.Fatal(err)
	}
	content, partial, examined, err := store.GetSymbolContentBounded(repoID, symbol.ID, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if !partial || examined > 4096 || len(content) > 4096 || !strings.Contains(string(content), "HEAD_SYMBOL") || !strings.Contains(string(content), "TAIL_SYMBOL") || !utf8.Valid(content) {
		t.Fatalf("symbol range was not bounded safely: partial=%v examined=%d bytes=%d", partial, examined, len(content))
	}
}

func TestGetSymbolsByFilesBoundedCapsLargeOutlineMetadata(t *testing.T) {
	store, err := NewIndexStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	const count = 10001
	source := "package large\n"
	symbols := make([]parser.Symbol, 0, count)
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("Symbol%05d", i)
		symbols = append(symbols, parser.Symbol{ID: parser.MakeSymbolID("large.go", name, parser.KindFunction), File: "large.go", Name: name, QualifiedName: name, Kind: parser.KindFunction, Language: "go", Line: i + 1, EndLine: i + 1})
	}
	if err := store.ReplaceRepoIndex("owner", "outline-large", "owner", "", map[string]string{"large.go": source}, map[string]string{"large.go": "go"}, symbols); err != nil {
		t.Fatal(err)
	}
	repoID, err := store.GetRepoID("owner/outline-large")
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.GetSymbolsByFilesBounded(repoID, []string{"large.go"}, 64, 64)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Symbols["large.go"]) != 64 || !result.TruncatedFiles["large.go"] {
		t.Fatalf("unbounded or untruthful outline result: %d %#v", len(result.Symbols["large.go"]), result.TruncatedFiles)
	}
}

func TestUpsertFileIndexWithContentRestoresCacheWhenIndexUpdateFails(t *testing.T) {
	store := newTestStore(t)
	if err := store.SaveContentFile("local", "missing", "main.lua", []byte("old content")); err != nil {
		t.Fatal(err)
	}
	err := store.UpsertFileIndexWithContent("local", "missing", "local", "", "main.lua", "new", "lua", nil, []byte("new content"))
	if err == nil {
		t.Fatal("expected update against an unindexed repository to fail")
	}
	content, readErr := os.ReadFile(filepath.Join(mustContentDir(t, store, "local", "missing"), "main.lua"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != "old content" {
		t.Fatalf("cache was not restored after failed update: %q", content)
	}
}

func mustContentDir(t *testing.T, store *IndexStore, owner, name string) string {
	t.Helper()
	dir, err := store.contentDir(owner, name)
	if err != nil {
		t.Fatal(err)
	}
	return dir
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
