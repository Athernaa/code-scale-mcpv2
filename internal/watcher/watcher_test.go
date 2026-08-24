package watcher

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Athernaa/code-scale-mcpv2/internal/parser"
	"github.com/Athernaa/code-scale-mcpv2/internal/pathfilter"
	"github.com/Athernaa/code-scale-mcpv2/internal/repository"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic/fivem"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic/framework"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic/generic"
	"github.com/Athernaa/code-scale-mcpv2/internal/storage"
	"github.com/Athernaa/code-scale-mcpv2/internal/workspace"
	workspaceindex "github.com/Athernaa/code-scale-mcpv2/internal/workspace/indexer"
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
	genericResult, err := generic.NewAnalyzer().AnalyzeRepository(context.Background(), semantic.RepositoryInput{Repo: id.Repo, Files: contents, Languages: langs, Symbols: symbols})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerGenericGraph, genericResult); err != nil {
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

func indexWatcherWorkspace(t *testing.T, root string, paths map[string]string) (*storage.IndexStore, repository.LocalIdentity, int64) {
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
	hashes := map[string]string{}
	languages := map[string]string{}
	contents := map[string][]byte{}
	symbols := map[string][]parser.Symbol{}
	var allSymbols []parser.Symbol
	for rel, text := range paths {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			_ = store.Close()
			t.Fatal(err)
		}
		data := []byte(text)
		if err := os.WriteFile(full, data, 0600); err != nil {
			_ = store.Close()
			t.Fatal(err)
		}
		lang := parser.DetectLanguage(rel)
		if lang == "" {
			continue
		}
		parsed, err := parser.ParseFile(data, rel, lang)
		if err != nil {
			_ = store.Close()
			t.Fatal(err)
		}
		contents[rel] = data
		languages[rel] = lang
		symbols[rel] = parsed
		hashes[rel] = workspace.ContentHash(data)
		allSymbols = append(allSymbols, parsed...)
		if err := store.SaveContentFile(id.Owner, id.Name, rel, data); err != nil {
			_ = store.Close()
			t.Fatal(err)
		}
	}
	if err := store.ReplaceRepoIndex(id.Owner, id.Name, "local", "", hashes, languages, allSymbols, id.CanonicalPath); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	repoID, err := store.GetRepoID(id.Repo)
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	matcher, err := pathfilter.New(root, nil)
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	discovery, err := workspace.DiscoverWithIgnore(root, matcher.Ignored)
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if _, err := workspaceindex.Index(context.Background(), store, repoID, id.Repo, root, contents, languages, symbols, discovery); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	genericResult, err := generic.NewAnalyzer().AnalyzeRepository(context.Background(), semantic.RepositoryInput{Repo: id.Repo, Files: contents, Languages: languages, Symbols: symbols})
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerGenericGraph, genericResult); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	return store, id, repoID
}

func TestWatcherWorkspaceToGenericAfterLastResourceDirectoryRemoval(t *testing.T) {
	root := filepath.Join(t.TempDir(), "server-data")
	resourceDir := filepath.Join(root, "resources", "only_resource")
	store, id, repoID := indexWatcherWorkspace(t, root, map[string]string{
		"resources/only_resource/fxmanifest.lua": "fx_version 'cerulean'\nserver_script 'server.lua'\n",
		"resources/only_resource/server.lua":     "RegisterNetEvent('transition:event')\n",
	})
	defer func() { _ = store.Close() }()
	if err := os.RemoveAll(resourceDir); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(store)
	if err := mgr.handleRemovedDirectory(id.CanonicalPath, id.Repo, resourceDir); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetWorkspace(repoID); !storage.IsNotFound(err) {
		t.Fatalf("workspace state survived last resource removal: err=%v", err)
	}
	for _, analyzer := range []string{semantic.AnalyzerFiveM, semantic.AnalyzerFiveMWorkspace} {
		entities, err := store.GetSemanticEntitiesForAnalyzer(repoID, analyzer)
		if err != nil || len(entities) != 0 {
			t.Fatalf("%s facts survived workspace transition: %#v err=%v", analyzer, entities, err)
		}
	}
	files, err := store.GetFiles(repoID)
	if err != nil || len(files) != 0 {
		t.Fatalf("deleted resource files survived transition: %#v err=%v", files, err)
	}
	if _, err := store.GetRepoID(id.Repo); err != nil {
		t.Fatalf("repository was removed instead of transitioning to generic mode: %v", err)
	}
}

func TestWatcherLastManifestRemovalTransitionsWorkspaceToGeneric(t *testing.T) {
	root := filepath.Join(t.TempDir(), "server-data")
	resourceDir := filepath.Join(root, "resources", "only_resource")
	store, id, repoID := indexWatcherWorkspace(t, root, map[string]string{
		"resources/only_resource/fxmanifest.lua": "fx_version 'cerulean'\nserver_script 'server.lua'\n",
		"resources/only_resource/server.lua":     "local function KeepGeneric() end\n",
	})
	defer func() { _ = store.Close() }()
	manifest := filepath.Join(resourceDir, "fxmanifest.lua")
	if err := os.Remove(manifest); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(store)
	mgr.reindexFiles(&FolderWatch{Path: id.CanonicalPath, Repo: id.Repo, stop: make(chan struct{})}, []string{manifest})
	if _, err := store.GetWorkspace(repoID); !storage.IsNotFound(err) {
		t.Fatalf("workspace state survived last manifest removal: err=%v", err)
	}
	fiveM, err := store.GetSemanticEntitiesForAnalyzer(repoID, semantic.AnalyzerFiveM)
	if err != nil || len(fiveM) != 0 {
		t.Fatalf("FiveM facts survived last manifest removal: %#v err=%v", fiveM, err)
	}
	files, err := store.GetFiles(repoID)
	if err != nil || len(files) != 1 || files[0].Path != "resources/only_resource/server.lua" {
		t.Fatalf("generic source index was not preserved: %#v err=%v", files, err)
	}
	if genericFacts, genericErr := store.GetSemanticEntitiesForAnalyzer(repoID, semantic.AnalyzerGenericGraph); genericErr != nil || len(genericFacts) == 0 {
		t.Fatalf("generic facts were not preserved: %#v err=%v", genericFacts, genericErr)
	}
}

func TestWatcherServerConfigOnlyWorkspaceTransitionsToGeneric(t *testing.T) {
	root := filepath.Join(t.TempDir(), "server-data")
	store, id, repoID := indexWatcherWorkspace(t, root, map[string]string{"server.cfg": "ensure missing_resource\n"})
	defer func() { _ = store.Close() }()
	if _, err := store.GetWorkspace(repoID); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(root, "server.cfg")
	if err := os.Remove(cfg); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(store)
	mgr.reindexFiles(&FolderWatch{Path: id.CanonicalPath, Repo: id.Repo, stop: make(chan struct{})}, []string{cfg})
	if _, err := store.GetWorkspace(repoID); !storage.IsNotFound(err) {
		t.Fatalf("server.cfg-only workspace state survived deletion: err=%v", err)
	}
	if _, err := store.GetRepoID(id.Repo); err != nil {
		t.Fatalf("repository was removed after config-only transition: %v", err)
	}
}

func TestWatcherCompletenessHealsAfterSuccessfulSourceIndex(t *testing.T) {
	root := filepath.Join(t.TempDir(), "server-data")
	store, id, repoID := indexWatcherWorkspace(t, root, map[string]string{
		"resources/app/resource_x/fxmanifest.lua": "fx_version 'cerulean'\nserver_script 'server.lua'\n",
		"resources/app/resource_x/server.lua":     "RegisterNetEvent('heal:event')\n",
	})
	defer func() { _ = store.Close() }()
	if err := store.UpdateWorkspaceCompleteness(repoID, storage.WorkspaceCompleteness{FilesDiscoveredTotal: 3, FilesIndexed: 2, Incomplete: true, ResourcesWithSemantics: 1}); err != nil {
		t.Fatal(err)
	}
	newPath := filepath.Join(root, "resources", "app", "resource_x", "new.lua")
	if err := os.WriteFile(newPath, []byte("RegisterNetEvent('healed')\n"), 0600); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(store)
	mgr.reindexFiles(&FolderWatch{Path: id.CanonicalPath, Repo: id.Repo, stop: make(chan struct{})}, []string{newPath})
	info, err := store.GetWorkspace(repoID)
	if err != nil {
		t.Fatal(err)
	}
	if info.FilesDiscoveredTotal != 3 || info.FilesIndexed != 3 || info.Incomplete {
		t.Fatalf("workspace completeness did not heal after source recovery: %#v", info)
	}
}

func TestWatcherLiveGitignoreReconcilesExcludeAndUnignore(t *testing.T) {
	root := filepath.Join(t.TempDir(), "server-data")
	extra := "resources/app/resource_x/extra.lua"
	store, id, repoID := indexWatcherWorkspace(t, root, map[string]string{
		".gitignore": "",
		"resources/app/resource_x/fxmanifest.lua": "fx_version 'cerulean'\nserver_script 'server.lua'\n",
		"resources/app/resource_x/server.lua":     "RegisterNetEvent('keep:event')\n",
		extra:                                     "RegisterNetEvent('extra:event')\n",
	})
	defer func() { _ = store.Close() }()
	ignorePath := filepath.Join(root, ".gitignore")
	if err := os.WriteFile(ignorePath, []byte(extra+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(store)
	watch := &FolderWatch{Path: id.CanonicalPath, Repo: id.Repo, stop: make(chan struct{})}
	mgr.reindexFiles(watch, []string{ignorePath})
	files, err := store.GetFiles(repoID)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.Path == extra {
			t.Fatal("live .gitignore exclusion left an indexed file")
		}
	}
	if err := os.WriteFile(ignorePath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	mgr.reindexFiles(watch, []string{ignorePath})
	files, err = store.GetFiles(repoID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, file := range files {
		if file.Path == extra {
			found = true
		}
	}
	if !found {
		t.Fatalf("removing ignore rule did not reindex visible source: %#v", files)
	}
}

func waitForWatcherCondition(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.NewTimer(8 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		if condition() {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("watcher condition was not observed before timeout")
		case <-ticker.C:
		}
	}
}

func TestWatcherGitignoreRealEventPathReconciles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "server-data")
	extra := "resources/app/resource_x/extra.lua"
	store, id, repoID := indexWatcherWorkspace(t, root, map[string]string{
		".gitignore": "",
		"resources/app/resource_x/fxmanifest.lua": "fx_version 'cerulean'\nserver_script 'server.lua'\n",
		"resources/app/resource_x/server.lua":     "RegisterNetEvent('keep:event')\n",
		extra:                                     "RegisterNetEvent('extra:event')\n",
	})
	defer func() { _ = store.Close() }()

	mgr := NewManager(store)
	if err := mgr.Watch(id.CanonicalPath); err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()

	ignorePath := filepath.Join(root, ".gitignore")
	if err := os.WriteFile(ignorePath, []byte(extra+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	waitForWatcherCondition(t, func() bool {
		files, err := store.GetFiles(repoID)
		if err != nil {
			return false
		}
		for _, file := range files {
			if file.Path == extra {
				return false
			}
		}
		return true
	})

	if err := os.Remove(ignorePath); err != nil {
		t.Fatal(err)
	}
	waitForWatcherCondition(t, func() bool {
		files, err := store.GetFiles(repoID)
		if err != nil {
			return false
		}
		for _, file := range files {
			if file.Path == extra {
				return true
			}
		}
		return false
	})
}

func TestWatcherGenericRepositoryEntersWorkspaceOnServerConfig(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	store, id, repoID := indexWatcherFiles(t, root, map[string]string{"main.go": "package main\nfunc Main() {}\n"})
	defer func() { _ = store.Close() }()

	mainContent := []byte("package main\nfunc Main() {}\n")
	mainSymbols, err := parser.ParseFile(mainContent, "main.go", "go")
	if err != nil {
		t.Fatal(err)
	}
	graph, err := generic.NewAnalyzer().AnalyzeRepository(context.Background(), semantic.RepositoryInput{
		Repo:      id.Repo,
		Files:     map[string][]byte{"main.go": mainContent},
		Languages: map[string]string{"main.go": "go"},
		Symbols:   map[string][]parser.Symbol{"main.go": mainSymbols},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerGenericGraph, graph); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager(store)
	if err := mgr.Watch(root); err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()

	if err := os.WriteFile(filepath.Join(root, "server.cfg"), []byte("ensure missing_resource\n"), 0600); err != nil {
		t.Fatal(err)
	}
	waitForWatcherCondition(t, func() bool {
		info, err := store.GetWorkspace(repoID)
		return err == nil && info.Kind == workspace.KindFiveMWorkspace
	})

	configs, err := store.GetWorkspaceConfigs(repoID)
	if err != nil || len(configs) != 1 || configs[0].Path != "server.cfg" {
		t.Fatalf("server.cfg workspace entry was not persisted: %#v err=%v", configs, err)
	}
	files, err := store.GetFiles(repoID)
	if err != nil || len(files) != 1 || files[0].Path != "main.go" {
		t.Fatalf("generic source index changed during workspace entry: %#v err=%v", files, err)
	}
	graphEntities, err := store.GetSemanticEntitiesForAnalyzer(repoID, semantic.AnalyzerGenericGraph)
	if err != nil || len(graphEntities) == 0 {
		t.Fatalf("generic graph was lost during workspace entry: %#v err=%v", graphEntities, err)
	}
}

func TestWatcherGenericCfgEditDoesNotCreateWorkspaceRefresh(t *testing.T) {
	root := filepath.Join(t.TempDir(), "generic")
	store, id, repoID := indexWatcherFiles(t, root, map[string]string{"main.go": "package main\nfunc Main() {}\n"})
	defer func() { _ = store.Close() }()
	cfg := filepath.Join(root, "settings.cfg")
	if err := os.WriteFile(cfg, []byte("set foo bar\n"), 0600); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(store)
	mgr.reindexFiles(&FolderWatch{Path: id.CanonicalPath, Repo: id.Repo, stop: make(chan struct{})}, []string{cfg})
	if _, err := store.GetWorkspace(repoID); !storage.IsNotFound(err) {
		t.Fatalf("generic cfg edit created or refreshed workspace state: err=%v", err)
	}
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
	genericResult, err := generic.NewAnalyzer().AnalyzeRepository(context.Background(), semantic.RepositoryInput{Repo: id.Repo, Files: contents, Languages: langs, Symbols: symbols})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerGenericGraph, genericResult); err != nil {
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
	var fivemCount, genericCount int
	for _, entity := range entities {
		if entity.Analyzer == semantic.AnalyzerFiveM {
			fivemCount++
		}
		if entity.Analyzer == semantic.AnalyzerGenericGraph {
			genericCount++
		}
	}
	if fivemCount != 0 || genericCount == 0 {
		t.Fatalf("manifest deletion did not isolate analyzer data: entities=%#v relationships=%#v", entities, relationships)
	}
	for _, relationship := range relationships {
		if relationship.Analyzer == semantic.AnalyzerFiveM {
			t.Fatalf("manifest deletion left stale FiveM relationships: %#v", relationships)
		}
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
	for _, entity := range entities {
		if entity.Analyzer == semantic.AnalyzerFiveM {
			t.Fatalf("generic Lua repository accumulated FiveM semantics: %#v", entities)
		}
	}
	if len(entities) == 0 {
		t.Fatal("generic Lua repository did not receive generic graph facts")
	}
}

func TestWatcherGenericGraphRefreshesRenamedImportTarget(t *testing.T) {
	root := filepath.Join(t.TempDir(), "typescript")
	paths := map[string]string{
		"a.ts": "export function foo() {}\n",
		"b.ts": "import { foo } from './a'\nexport function run() { foo() }\n",
	}
	store, id, repoID := indexWatcherFiles(t, root, paths)
	defer func() { _ = store.Close() }()
	contents := make(map[string][]byte, len(paths))
	langs := make(map[string]string, len(paths))
	symbols := make(map[string][]parser.Symbol, len(paths))
	for rel, content := range paths {
		contents[rel] = []byte(content)
		langs[rel] = parser.DetectLanguage(rel)
		parsed, err := parser.ParseFile(contents[rel], rel, langs[rel])
		if err != nil {
			t.Fatal(err)
		}
		symbols[rel] = parsed
	}
	result, err := generic.NewAnalyzer().AnalyzeRepository(context.Background(), semantic.RepositoryInput{Repo: id.Repo, Files: contents, Languages: langs, Symbols: symbols})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerGenericGraph, result); err != nil {
		t.Fatal(err)
	}
	graphEdges := func() []semantic.Relationship {
		edges, edgeErr := store.GetSemanticRelationshipsForAnalyzer(repoID, semantic.AnalyzerGenericGraph)
		if edgeErr != nil {
			t.Fatal(edgeErr)
		}
		return edges
	}
	initial := graphEdges()
	if len(initial) == 0 {
		t.Fatal("initial import/call relationship was not indexed")
	}
	if err := os.WriteFile(filepath.Join(root, "a.ts"), []byte("export function bar() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(store)
	mgr.reindexFiles(&FolderWatch{Path: id.CanonicalPath, Repo: id.Repo, stop: make(chan struct{})}, []string{filepath.Join(root, "a.ts")})
	for _, edge := range graphEdges() {
		if edge.Name == "foo" {
			t.Fatalf("stale relationship survived renamed target: %#v", edge)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "b.ts"), []byte("import { bar } from './a'\nexport function run() { bar() }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	mgr.reindexFiles(&FolderWatch{Path: id.CanonicalPath, Repo: id.Repo, stop: make(chan struct{})}, []string{filepath.Join(root, "b.ts")})
	var foundBar bool
	for _, edge := range graphEdges() {
		if edge.Name == "bar" && edge.Kind == generic.RelationshipCalls {
			foundBar = true
		}
	}
	if !foundBar {
		t.Fatal("updated import/call relationship was not indexed")
	}
}

func TestWatcherUsesGitignoreAndMaintainsWorkspaceCoverageOnSourceChanges(t *testing.T) {
	root := filepath.Join(t.TempDir(), "server-data")
	resourceRoot := filepath.Join(root, "resources", "app", "resource_x")
	if err := os.MkdirAll(resourceRoot, 0700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"server.cfg": "ensure resource_x\n",
		".gitignore": "ignored.lua\n",
		"resources/app/resource_x/fxmanifest.lua": "fx_version 'cerulean'\nserver_script 'server.lua'\n",
		"resources/app/resource_x/server.lua":     "RegisterNetEvent('coverage:test')\n",
	}
	for rel, text := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
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
	contents := map[string][]byte{}
	languages := map[string]string{}
	symbols := map[string][]parser.Symbol{}
	hashes := map[string]string{}
	var allSymbols []parser.Symbol
	for rel, text := range files {
		if parser.DetectLanguage(rel) == "" {
			continue
		}
		data := []byte(text)
		lang := parser.DetectLanguage(rel)
		parsed, parseErr := parser.ParseFile(data, rel, lang)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		contents[rel] = data
		languages[rel] = lang
		symbols[rel] = parsed
		hashes[rel] = workspace.ContentHash(data)
		allSymbols = append(allSymbols, parsed...)
		if err := store.SaveContentFile(id.Owner, id.Name, rel, data); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.ReplaceRepoIndex(id.Owner, id.Name, "local", "", hashes, languages, allSymbols, id.CanonicalPath); err != nil {
		t.Fatal(err)
	}
	repoID, err := store.GetRepoID(id.Repo)
	if err != nil {
		t.Fatal(err)
	}
	matcher, err := pathfilter.New(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	discovery, err := workspace.DiscoverWithIgnore(root, matcher.Ignored)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workspaceindex.Index(context.Background(), store, repoID, id.Repo, root, contents, languages, symbols, discovery); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(store)
	watch := &FolderWatch{Path: id.CanonicalPath, Repo: id.Repo, stop: make(chan struct{})}
	ignoredPath := filepath.Join(resourceRoot, "ignored.lua")
	if err := os.WriteFile(ignoredPath, []byte("RegisterNetEvent('ignored')\n"), 0600); err != nil {
		t.Fatal(err)
	}
	mgr.reindexFiles(watch, []string{ignoredPath})
	indexed, err := store.GetFiles(repoID)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range indexed {
		if file.Path == "resources/app/resource_x/ignored.lua" {
			t.Fatal("watcher indexed a .gitignored source file")
		}
	}

	addedPath := filepath.Join(resourceRoot, "new.lua")
	if err := os.WriteFile(addedPath, []byte("RegisterNetEvent('added')\n"), 0600); err != nil {
		t.Fatal(err)
	}
	mgr.reindexFiles(watch, []string{addedPath})
	info, err := store.GetWorkspace(repoID)
	if err != nil {
		t.Fatal(err)
	}
	if info.FilesDiscoveredTotal != 3 || info.FilesIndexed != 3 || info.Incomplete {
		t.Fatalf("source addition did not update truthful coverage: %#v", info)
	}

	if err := os.Remove(addedPath); err != nil {
		t.Fatal(err)
	}
	mgr.reindexFiles(watch, []string{addedPath})
	info, err = store.GetWorkspace(repoID)
	if err != nil {
		t.Fatal(err)
	}
	if info.FilesDiscoveredTotal != 2 || info.FilesIndexed != 2 || info.Incomplete {
		t.Fatalf("source removal did not update truthful coverage: %#v", info)
	}
}

func TestWatcherIndexesPopulatedResourceDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "server-data")
	resourceDir := filepath.Join(root, "resources", "[app]", "new_resource")
	for rel, content := range map[string]string{
		"server.cfg": "ensure new_resource\n",
		"resources/[app]/new_resource/fxmanifest.lua": "fx_version 'cerulean'\nserver_script 'server.lua'\n",
		"resources/[app]/new_resource/server.lua":     "RegisterNetEvent('populated:event')\n",
	} {
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
	defer store.Close()
	id, err := repository.Local(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceRepoIndex(id.Owner, id.Name, "local", "", map[string]string{}, map[string]string{}, nil, id.CanonicalPath); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(store)
	mgr.reindexFiles(&FolderWatch{Path: id.CanonicalPath, Repo: id.Repo, stop: make(chan struct{})}, discoverSupportedFiles(root, resourceDir))
	repoID, err := store.GetRepoID(id.Repo)
	if err != nil {
		t.Fatal(err)
	}
	files, err := store.GetFiles(repoID)
	if err != nil || len(files) != 2 {
		t.Fatalf("populated resource files were not indexed: files=%#v err=%v", files, err)
	}
	entities, err := store.GetSemanticEntitiesForAnalyzer(repoID, semantic.AnalyzerFiveM)
	if err != nil {
		t.Fatal(err)
	}
	if len(entities) == 0 {
		t.Fatal("populated resource did not produce FiveM facts")
	}
}

func TestWatcherWorkspaceManifestLifecycleAndCfgRefresh(t *testing.T) {
	root := filepath.Join(t.TempDir(), "server-data")
	resourceDir := filepath.Join(root, "resources", "app", "resource_x")
	if err := os.MkdirAll(resourceDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "server.cfg"), []byte("ensure resource_x\n"), 0600); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(resourceDir, "client.lua")
	if err := os.WriteFile(sourcePath, []byte("TriggerServerEvent('lifecycle:event')\n"), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := storage.NewIndexStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	id, err := repository.Local(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceRepoIndex(id.Owner, id.Name, "local", "", map[string]string{}, map[string]string{}, nil, id.CanonicalPath); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(store)
	watch := &FolderWatch{Path: id.CanonicalPath, Repo: id.Repo, stop: make(chan struct{})}
	mgr.reindexFiles(watch, []string{sourcePath})
	manifestPath := filepath.Join(resourceDir, "fxmanifest.lua")
	if err := os.WriteFile(manifestPath, []byte("fx_version 'cerulean'\nclient_script 'client.lua'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	mgr.reindexFiles(watch, []string{manifestPath})
	repoID, err := store.GetRepoID(id.Repo)
	if err != nil {
		t.Fatal(err)
	}
	fiveM, err := store.GetSemanticEntitiesForAnalyzer(repoID, semantic.AnalyzerFiveM)
	if err != nil || len(fiveM) == 0 {
		t.Fatalf("manifest addition did not activate resource semantics: %#v err=%v", fiveM, err)
	}
	originalFrameworkAnalyzer := analyzeFrameworkFn
	frameworkCalls := 0
	analyzeFrameworkFn = func(ctx context.Context, input semantic.RepositoryInput) (semantic.Result, error) {
		frameworkCalls++
		return originalFrameworkAnalyzer(ctx, input)
	}
	defer func() { analyzeFrameworkFn = originalFrameworkAnalyzer }()
	if err := os.WriteFile(filepath.Join(root, "server.cfg"), []byte("# ensure resource_x\n"), 0600); err != nil {
		t.Fatal(err)
	}
	mgr.reindexFiles(watch, []string{filepath.Join(root, "server.cfg")})
	resources, err := store.GetWorkspaceResources(repoID)
	if err != nil || len(resources) != 1 || resources[0].EnabledState != "unknown" {
		t.Fatalf("cfg refresh did not update enabled state: %#v err=%v", resources, err)
	}
	if frameworkCalls != 0 {
		t.Fatalf("cfg-only refresh invoked framework source analysis %d times", frameworkCalls)
	}
	if err := os.Remove(manifestPath); err != nil {
		t.Fatal(err)
	}
	mgr.reindexFiles(watch, []string{manifestPath})
	fiveM, err = store.GetSemanticEntitiesForAnalyzer(repoID, semantic.AnalyzerFiveM)
	if err != nil || len(fiveM) != 0 {
		t.Fatalf("manifest removal left FiveM facts: %#v err=%v", fiveM, err)
	}
	if mode, _ := workspace.DetectMode(root); mode != workspace.KindFiveMWorkspace {
		t.Fatalf("workspace mode should remain based on server.cfg: %q", mode)
	}
}

func TestWatcherWorkspaceSourceEditPreservesUnrelatedResourceFacts(t *testing.T) {
	root := filepath.Join(t.TempDir(), "server-data")
	resources := map[string]string{"resource_a": "a:event", "resource_b": "b:event"}
	for name, event := range resources {
		dir := filepath.Join(root, "resources", "group", name)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "fxmanifest.lua"), []byte("fx_version 'cerulean'\nserver_script 'server.lua'\n"), 0600); err != nil {
			t.Fatal(err)
		}
		content := "RegisterNetEvent('" + event + "')\n"
		if name == "resource_a" {
			content = "exports('GetValue', function() end)\n" + content
		} else {
			content = "exports.resource_a:GetValue()\n" + content
		}
		if err := os.WriteFile(filepath.Join(dir, "server.lua"), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "server.cfg"), []byte("ensure resource_a\nensure resource_b\n"), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := storage.NewIndexStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	id, err := repository.Local(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceRepoIndex(id.Owner, id.Name, "local", "", map[string]string{}, map[string]string{}, nil, id.CanonicalPath); err != nil {
		t.Fatal(err)
	}
	watch := &FolderWatch{Path: id.CanonicalPath, Repo: id.Repo, stop: make(chan struct{})}
	var initial []string
	for name := range resources {
		initial = append(initial, discoverSupportedFiles(root, filepath.Join(root, "resources", "group", name))...)
	}
	mgr := NewManager(store)
	mgr.reindexFiles(watch, initial)
	repoID, err := store.GetRepoID(id.Repo)
	if err != nil {
		t.Fatal(err)
	}
	frameworkBefore, err := store.GetSemanticEntitiesForAnalyzer(repoID, semantic.AnalyzerFramework)
	if err != nil {
		t.Fatal(err)
	}
	stable := map[string]string{}
	for _, entity := range frameworkBefore {
		if entity.Kind == framework.KindAPIProvider || entity.Kind == framework.KindAPICall {
			stable[entity.Name+"\x00"+entity.File] = entity.ID
		}
	}
	relationshipsBefore, err := store.GetSemanticRelationshipsForAnalyzer(repoID, semantic.AnalyzerFramework)
	if err != nil {
		t.Fatal(err)
	}
	stableRelationships := map[string]string{}
	for _, relationship := range relationshipsBefore {
		stableRelationships[relationship.FromEntityID+"\x00"+relationship.ToEntityID+"\x00"+relationship.Kind] = relationship.ID
	}
	updatedPath := filepath.Join(root, "resources", "group", "resource_b", "server.lua")
	if err := os.WriteFile(updatedPath, []byte("exports.resource_a:GetValue()\nRegisterNetEvent('b:updated')\n"), 0600); err != nil {
		t.Fatal(err)
	}
	mgr.reindexFiles(watch, []string{updatedPath})
	entities, err := store.GetSemanticEntitiesForAnalyzer(repoID, semantic.AnalyzerFiveM)
	if err != nil {
		t.Fatal(err)
	}
	var oldA, newB, oldB bool
	for _, entity := range entities {
		sourcePath, _ := entity.Metadata["source_resource_path"].(string)
		if sourcePath == "resources/group/resource_a" && entity.Name == "a:event" {
			oldA = true
		}
		if sourcePath == "resources/group/resource_b" && entity.Name == "b:updated" {
			newB = true
		}
		if sourcePath == "resources/group/resource_b" && entity.Name == "b:event" {
			oldB = true
		}
	}
	if !oldA || !newB || oldB {
		t.Fatalf("resource-scoped refresh was incorrect: oldA=%v newB=%v oldB=%v entities=%#v", oldA, newB, oldB, entities)
	}
	frameworkAfter, err := store.GetSemanticEntitiesForAnalyzer(repoID, semantic.AnalyzerFramework)
	if err != nil {
		t.Fatal(err)
	}
	for _, entity := range frameworkAfter {
		if oldID := stable[entity.Name+"\x00"+entity.File]; oldID != "" && oldID != entity.ID {
			t.Fatalf("resource refresh changed unchanged framework identity: old=%s new=%s entity=%#v", oldID, entity.ID, entity)
		}
	}
	relationshipsAfter, err := store.GetSemanticRelationshipsForAnalyzer(repoID, semantic.AnalyzerFramework)
	if err != nil {
		t.Fatal(err)
	}
	for _, relationship := range relationshipsAfter {
		key := relationship.FromEntityID + "\x00" + relationship.ToEntityID + "\x00" + relationship.Kind
		if oldID := stableRelationships[key]; oldID != "" && oldID != relationship.ID {
			t.Fatalf("resource refresh changed unchanged framework relationship identity: old=%s new=%s", oldID, relationship.ID)
		}
	}
}
