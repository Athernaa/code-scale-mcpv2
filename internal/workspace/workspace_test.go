package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverWorkspaceExecAndModes(t *testing.T) {
	root := t.TempDir()
	if mode, err := DetectMode(root); err != nil || mode != KindGeneric {
		t.Fatalf("empty root mode=%q err=%v", mode, err)
	}
	if err := os.WriteFile(filepath.Join(root, "server.cfg"), []byte("exec resources.cfg\n# ensure ignored\nensure app_a\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "resources.cfg"), []byte("ensure core_a\nensure app_a\nexec loop.cfg\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "loop.cfg"), []byte("exec resources.cfg\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"resources/[core]/core_a/fxmanifest.lua", "resources/[app]/app_a/fxmanifest.lua"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, path), []byte("fx_version 'cerulean'"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	d, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if d.Mode != KindFiveMWorkspace || len(d.Resources) != 2 {
		t.Fatalf("unexpected discovery: %#v", d)
	}
	if d.Resources[0].GroupPath == "" {
		t.Fatal("group path was not preserved")
	}
	orders := map[string]int{}
	for _, r := range d.Resources {
		orders[r.Name] = r.StartOrder
		if r.EnabledState != "enabled" {
			t.Fatalf("resource not enabled: %#v", r)
		}
	}
	if orders["core_a"] != 1 || orders["app_a"] != 2 {
		t.Fatalf("start metadata missing: %#v", d.Resources)
	}
	if len(d.Commands) < 2 {
		t.Fatalf("exec configs were not loaded: %#v", d.Commands)
	}
}

func TestDuplicateResourceNamesRemainAmbiguous(t *testing.T) {
	root := t.TempDir()
	for _, group := range []string{"[a]", "[b]"} {
		dir := filepath.Join(root, "resources", group, "inventory")
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "fxmanifest.lua"), []byte("fx_version 'cerulean'"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	d, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.DuplicateNames) != 1 || d.DuplicateNames[0] != "inventory" {
		t.Fatalf("duplicates not reported: %#v", d.DuplicateNames)
	}
}

func TestDuplicateResourceEnsureDoesNotMarkEitherResourceEnabled(t *testing.T) {
	root := t.TempDir()
	for _, group := range []string{"[a]", "[b]"} {
		dir := filepath.Join(root, "resources", group, "inventory")
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "fxmanifest.lua"), []byte("fx_version 'cerulean'"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "server.cfg"), []byte("ensure inventory\n"), 0600); err != nil {
		t.Fatal(err)
	}
	d, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, resource := range d.Resources {
		if resource.EnabledState != "unknown" || resource.StartOrder != 0 {
			t.Fatalf("ambiguous ensure assigned definite state: %#v", d.Resources)
		}
	}
	for _, command := range d.Commands {
		if command.Command == "ensure" && command.Order != 1 {
			t.Fatalf("ensure order was not retained: %#v", d.Commands)
		}
	}
}

func TestDiscoverWithIgnoreDoesNotUseIgnoredManifestAsWorkspaceEvidence(t *testing.T) {
	root := t.TempDir()
	resourceDir := filepath.Join(root, "resources", "ignored")
	if err := os.MkdirAll(resourceDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resourceDir, "fxmanifest.lua"), []byte("fx_version 'cerulean'"), 0600); err != nil {
		t.Fatal(err)
	}
	ignored := filepath.Join(root, ".gitignore")
	if err := os.WriteFile(ignored, []byte("resources/ignored/\n"), 0600); err != nil {
		t.Fatal(err)
	}
	d, err := DiscoverWithIgnore(root, func(path string, isDir bool) bool {
		if path == ignored {
			return false
		}
		return filepath.Clean(path) == filepath.Clean(resourceDir) || filepath.Clean(filepath.Dir(path)) == filepath.Clean(resourceDir)
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.Mode != KindGeneric || len(d.Resources) != 0 {
		t.Fatalf("ignored FiveM evidence still classified workspace: %#v", d)
	}
}

func TestConfigExecRejectsTraversalOutsideWorkspace(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.cfg")
	if err := os.WriteFile(outside, []byte("ensure escaped\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "server.cfg"), []byte("exec ../../../outside.cfg\n"), 0600); err != nil {
		t.Fatal(err)
	}
	resourceDir := filepath.Join(root, "resources", "app")
	if err := os.MkdirAll(resourceDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resourceDir, "fxmanifest.lua"), []byte("fx_version 'cerulean'"), 0600); err != nil {
		t.Fatal(err)
	}
	d, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.ConfigFiles) != 1 || len(d.Commands) != 0 {
		t.Fatalf("outside exec escaped workspace: configs=%#v commands=%#v", d.ConfigFiles, d.Commands)
	}
}

func TestConfigExecRejectsSymlinkOutsideWorkspace(t *testing.T) {
	root := t.TempDir()
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "outside.cfg")
	if err := os.WriteFile(outside, []byte("ensure escaped\n"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked.cfg")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	resourceDir := filepath.Join(root, "resources", "app")
	if err := os.MkdirAll(resourceDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resourceDir, "fxmanifest.lua"), []byte("fx_version 'cerulean'"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "server.cfg"), []byte("exec linked.cfg\n"), 0600); err != nil {
		t.Fatal(err)
	}
	d, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.ConfigFiles) != 1 || len(d.Commands) != 0 {
		t.Fatalf("symlink exec escaped workspace: configs=%#v commands=%#v", d.ConfigFiles, d.Commands)
	}
}

func TestConfigStartStateUsesFinalStateAndFirstStartOrder(t *testing.T) {
	root := t.TempDir()
	resource := filepath.Join(root, "resources", "app")
	if err := os.MkdirAll(resource, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resource, "fxmanifest.lua"), []byte("fx_version 'cerulean'"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "server.cfg"), []byte("ensure app\nstop app\nensure app\n"), 0600); err != nil {
		t.Fatal(err)
	}
	d, err := Discover(root)
	if err != nil || len(d.Resources) != 1 {
		t.Fatalf("discovery failed: %#v err=%v", d, err)
	}
	if d.Resources[0].EnabledState != "enabled" || d.Resources[0].StartOrder != 1 {
		t.Fatalf("unexpected final state/order: %#v", d.Resources[0])
	}
}
