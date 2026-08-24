package watcher

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/Athernaa/code-scale-mcpv2/internal/parser"
	"github.com/Athernaa/code-scale-mcpv2/internal/repository"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic/fivem"
	"github.com/Athernaa/code-scale-mcpv2/internal/storage"
	"github.com/fsnotify/fsnotify"
)

func TestWatchUsesCanonicalLocalRepositoryIdentity(t *testing.T) {
	root := filepath.Join(t.TempDir(), "resource")
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	store, err := storage.NewIndexStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	id, err := repository.Local(root)
	if err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(store)
	if err := mgr.Watch(filepath.Join(root, ".", "nested", "..")); err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()

	watches := mgr.ListWatches()
	if len(watches) != 1 {
		t.Fatalf("expected one watch, got %d", len(watches))
	}
	if watches[0].Repo != id.Repo {
		t.Fatalf("watch identity %q does not match index identity %q", watches[0].Repo, id.Repo)
	}
	if watches[0].Path != id.CanonicalPath {
		t.Fatalf("watch path %q is not canonical %q", watches[0].Path, id.CanonicalPath)
	}
}

func TestAddDirectoryRecursiveWatchesNestedDirectoriesAndSkipsSecurityDirs(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "nested", "deep")
	ignored := filepath.Join(root, ".git", "objects")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(ignored, 0755); err != nil {
		t.Fatal(err)
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()

	if err := addDirectoryRecursive(w, root, root); err != nil {
		t.Fatal(err)
	}
	watched := map[string]bool{}
	for _, path := range w.WatchList() {
		watched[path] = true
	}
	if !watched[root] || !watched[filepath.Join(root, "nested")] || !watched[nested] {
		t.Fatalf("nested directories were not all registered: %v", w.WatchList())
	}
	if watched[filepath.Join(root, ".git")] || watched[ignored] {
		t.Fatalf("security directories should not be watched: %v", w.WatchList())
	}
}

func TestRestoreWatchesRemovesStalePersistedWatch(t *testing.T) {
	store, err := storage.NewIndexStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	stale := filepath.Join(t.TempDir(), "removed-resource")
	if err := store.SaveWatch(stale, "local/stale"); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager(store)
	if err := mgr.RestoreWatches(); err != nil {
		t.Fatal(err)
	}
	saved, err := store.ListSavedWatches()
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) != 0 {
		t.Fatalf("stale persisted watch was not removed: %#v", saved)
	}
}

func TestWatcherIncrementalUpdatePreservesAndRefreshesFiveMSemantics(t *testing.T) {
	root := filepath.Join(t.TempDir(), "resource")
	paths := map[string]string{
		"fxmanifest.lua":  "fx_version 'cerulean'\nclient_script 'client/main.lua'\nserver_script 'server/main.lua'\n",
		"client/main.lua": "TriggerServerEvent('avenlo:create')\n",
		"server/main.lua": "RegisterNetEvent('avenlo:create')\nAddEventHandler('avenlo:create', function() end)\n",
	}
	for rel, content := range paths {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	store, err := storage.NewIndexStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	id, err := repository.Local(root)
	if err != nil {
		t.Fatal(err)
	}
	hashes := make(map[string]string)
	langs := make(map[string]string)
	contents := make(map[string][]byte)
	symbols := make(map[string][]parser.Symbol)
	var allSymbols []parser.Symbol
	for rel, content := range paths {
		bytes := []byte(content)
		digest := sha256.Sum256(bytes)
		hashes[rel] = hex.EncodeToString(digest[:])
		langs[rel] = parser.DetectLanguage(rel)
		contents[rel] = bytes
		parsed, parseErr := parser.ParseFile(bytes, rel, langs[rel])
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		symbols[rel] = parsed
		allSymbols = append(allSymbols, parsed...)
		if err := store.SaveContentFile(id.Owner, id.Name, rel, bytes); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.ReplaceRepoIndex(id.Owner, id.Name, "local", "", hashes, langs, allSymbols, id.CanonicalPath); err != nil {
		t.Fatal(err)
	}
	result, err := fivem.NewAnalyzer().AnalyzeRepository(context.Background(), semantic.RepositoryInput{Repo: id.Repo, Resource: id.Name, Files: contents, Languages: langs, Symbols: symbols})
	if err != nil {
		t.Fatal(err)
	}
	repoID, err := store.GetRepoID(id.Repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceSemanticIndex(repoID, result); err != nil {
		t.Fatal(err)
	}

	updatedPath := filepath.Join(root, "client", "main.lua")
	updatedContent := "TriggerServerEvent('avenlo:updated')\n"
	if err := os.WriteFile(updatedPath, []byte(updatedContent), 0600); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(store)
	mgr.reindexFiles(&FolderWatch{Path: id.CanonicalPath, Repo: id.Repo, stop: make(chan struct{})}, []string{updatedPath})

	entities, err := store.GetSemanticEntities(repoID)
	if err != nil {
		t.Fatal(err)
	}
	var updated, serverHandler bool
	for _, entity := range entities {
		if entity.Kind == fivem.KindEventTrigger && entity.Name == "avenlo:updated" {
			updated = true
		}
		if entity.Kind == fivem.KindEventHandler && entity.Name == "avenlo:create" && entity.File == "server/main.lua" {
			serverHandler = true
		}
	}
	if !updated || !serverHandler {
		t.Fatalf("incremental semantic update lost unrelated data or missed new entity: %#v", entities)
	}
}

func indexWatcherFiles(t *testing.T, root string, paths map[string]string) (*storage.IndexStore, repository.LocalIdentity, int64) {
	t.Helper()
	store, err := storage.NewIndexStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id, err := repository.Local(root)
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	hashes := make(map[string]string)
	langs := make(map[string]string)
	var symbols []parser.Symbol
	for rel, content := range paths {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			_ = store.Close()
			t.Fatal(err)
		}
		bytes := []byte(content)
		if err := os.WriteFile(full, bytes, 0600); err != nil {
			_ = store.Close()
			t.Fatal(err)
		}
		digest := sha256.Sum256(bytes)
		hashes[rel] = hex.EncodeToString(digest[:])
		langs[rel] = parser.DetectLanguage(rel)
		parsed, err := parser.ParseFile(bytes, rel, langs[rel])
		if err != nil {
			_ = store.Close()
			t.Fatal(err)
		}
		symbols = append(symbols, parsed...)
		if err := store.SaveContentFile(id.Owner, id.Name, rel, bytes); err != nil {
			_ = store.Close()
			t.Fatal(err)
		}
	}
	if err := store.ReplaceRepoIndex(id.Owner, id.Name, "local", "", hashes, langs, symbols, id.CanonicalPath); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	repoID, err := store.GetRepoID(id.Repo)
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	return store, id, repoID
}

func TestWatcherManifestDeletionClearsFiveMSemanticsButKeepsGenericFiles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "resource")
	paths := map[string]string{
		"fxmanifest.lua": "fx_version 'cerulean'\nserver_script 'server.lua'\n",
		"server.lua":     "RegisterNetEvent('test:event')\nAddEventHandler('test:event', function() end)\n",
	}
	store, id, repoID := indexWatcherFiles(t, root, paths)
	defer func() { _ = store.Close() }()
	contents := make(map[string][]byte, len(paths))
	langs := make(map[string]string, len(paths))
	symbols := make(map[string][]parser.Symbol, len(paths))
	for rel, content := range paths {
		contents[rel] = []byte(content)
		langs[rel] = parser.DetectLanguage(rel)
		parsed, err := parser.ParseFile([]byte(content), rel, langs[rel])
		if err != nil {
			t.Fatal(err)
		}
		symbols[rel] = parsed
	}
	result, err := fivem.NewAnalyzer().AnalyzeRepository(context.Background(), semantic.RepositoryInput{Repo: id.Repo, Resource: "resource", Files: contents, Languages: langs, Symbols: symbols})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceSemanticIndex(repoID, result); err != nil {
		t.Fatal(err)
	}
	entities, err := store.GetSemanticEntities(repoID)
	if err != nil || len(entities) == 0 {
		t.Fatalf("expected initial FiveM semantics: %#v err=%v", entities, err)
	}

	manifestPath := filepath.Join(root, "fxmanifest.lua")
	if err := os.Remove(manifestPath); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(store)
	mgr.reindexFiles(&FolderWatch{Path: id.CanonicalPath, Repo: id.Repo, stop: make(chan struct{})}, []string{manifestPath})
	entities, err = store.GetSemanticEntities(repoID)
	if err != nil {
		t.Fatal(err)
	}
	relationships, err := store.GetSemanticRelationships(repoID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entities) != 0 || len(relationships) != 0 {
		t.Fatalf("manifest deletion left stale semantics: entities=%#v relationships=%#v", entities, relationships)
	}
	files, err := store.GetFiles(repoID)
	if err != nil || len(files) != 1 || files[0].Path != "server.lua" {
		t.Fatalf("manifest deletion damaged generic index: files=%#v err=%v", files, err)
	}
}

func TestWatcherKeepsGenericLuaRepositorySemanticFree(t *testing.T) {
	root := filepath.Join(t.TempDir(), "plain")
	paths := map[string]string{"main.lua": "return 1\n"}
	store, id, repoID := indexWatcherFiles(t, root, paths)
	defer func() { _ = store.Close() }()
	mgr := NewManager(store)
	updatedPath := filepath.Join(root, "main.lua")
	if err := os.WriteFile(updatedPath, []byte("TriggerServerEvent('test:event')\n"), 0600); err != nil {
		t.Fatal(err)
	}
	mgr.reindexFiles(&FolderWatch{Path: id.CanonicalPath, Repo: id.Repo, stop: make(chan struct{})}, []string{updatedPath})
	entities, err := store.GetSemanticEntities(repoID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entities) != 0 {
		t.Fatalf("generic Lua repository accumulated FiveM semantics: %#v", entities)
	}
}
