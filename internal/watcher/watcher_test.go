package watcher

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Athernaa/code-scale-mcpv2/internal/repository"
	"github.com/Athernaa/code-scale-mcpv2/internal/storage"
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
