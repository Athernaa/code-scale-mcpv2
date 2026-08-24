package watcher

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Athernaa/code-scale-mcpv2/internal/repository"
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
