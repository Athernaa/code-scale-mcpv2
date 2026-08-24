package storage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Athernaa/code-scale-mcpv2/internal/parser"
	"github.com/Athernaa/code-scale-mcpv2/internal/repository"
)

func TestSameBasenameLocalRepositoriesCoexistAndUpdateIndependently(t *testing.T) {
	root := t.TempDir()
	aPath := filepath.Join(root, "a", "resource")
	bPath := filepath.Join(root, "b", "resource")
	if err := os.MkdirAll(aPath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(bPath, 0755); err != nil {
		t.Fatal(err)
	}
	aID, err := repository.Local(aPath)
	if err != nil {
		t.Fatal(err)
	}
	bID, err := repository.Local(bPath)
	if err != nil {
		t.Fatal(err)
	}

	store := newTestStore(t)
	file := map[string]string{"main.lua": "v1"}
	lang := map[string]string{"main.lua": "lua"}
	aSymbol := parser.Symbol{ID: "main.lua::aFunction#function", File: "main.lua", Name: "aFunction", QualifiedName: "aFunction", Kind: "function", Language: "lua", Decorators: []string{}, Keywords: []string{}}
	bSymbol := parser.Symbol{ID: "main.lua::bFunction#function", File: "main.lua", Name: "bFunction", QualifiedName: "bFunction", Kind: "function", Language: "lua", Decorators: []string{}, Keywords: []string{}}
	if err := store.ReplaceRepoIndex(aID.Owner, aID.Name, "local", "", file, lang, []parser.Symbol{aSymbol}, aID.CanonicalPath); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceRepoIndex(bID.Owner, bID.Name, "local", "", file, lang, []parser.Symbol{bSymbol}, bID.CanonicalPath); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveContentFile(aID.Owner, aID.Name, "main.lua", []byte("a")); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveContentFile(bID.Owner, bID.Name, "main.lua", []byte("b")); err != nil {
		t.Fatal(err)
	}

	repos, err := store.ListRepos()
	if err != nil || len(repos) != 2 {
		t.Fatalf("expected two local repositories, repos=%#v err=%v", repos, err)
	}
	aRepoID, _ := store.GetRepoID(aID.Repo)
	bRepoID, _ := store.GetRepoID(bID.Repo)
	if err := store.UpsertFileIndex(aID.Owner, aID.Name, "local", "", "main.lua", "v2", "lua", []parser.Symbol{{ID: "main.lua::newAFunction#function", File: "main.lua", Name: "newAFunction", QualifiedName: "newAFunction", Kind: "function", Language: "lua", Decorators: []string{}, Keywords: []string{}}}, aID.CanonicalPath); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetSymbolByID(bRepoID, bSymbol.ID); err != nil {
		t.Fatalf("updating first same-basename repository affected second: %v", err)
	}
	if _, err := store.GetSymbolByID(aRepoID, "main.lua::newAFunction#function"); err != nil {
		t.Fatalf("updated first repository missing new symbol: %v", err)
	}

	aCache, _ := repository.ContentDir(store.BasePath(), aID.Owner, aID.Name)
	bCache, _ := repository.ContentDir(store.BasePath(), bID.Owner, bID.Name)
	if aCache == bCache {
		t.Fatal("cache directories collided")
	}
	if _, err := os.Stat(filepath.Join(aCache, "main.lua")); err != nil {
		t.Fatalf("first cache file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(bCache, "main.lua")); err != nil {
		t.Fatalf("second cache file missing: %v", err)
	}
}
