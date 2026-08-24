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
