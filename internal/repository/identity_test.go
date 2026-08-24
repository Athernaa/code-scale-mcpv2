package repository

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocalIdentityIsDeterministicAndDistinct(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a", "resource")
	b := filepath.Join(root, "b", "resource")
	if err := os.MkdirAll(a, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(b, 0755); err != nil {
		t.Fatal(err)
	}

	aID, err := Local(a)
	if err != nil {
		t.Fatal(err)
	}
	aAgain, err := Local(filepath.Join(root, "a", ".", "resource"))
	if err != nil {
		t.Fatal(err)
	}
	bID, err := Local(b)
	if err != nil {
		t.Fatal(err)
	}

	if aID.Repo != aAgain.Repo || aID.CanonicalPath != aAgain.CanonicalPath {
		t.Fatalf("identity is not deterministic: first=%#v second=%#v", aID, aAgain)
	}
	if aID.Repo == bID.Repo {
		t.Fatalf("same-basename folders collided: %q", aID.Repo)
	}
	if aID.Name == "resource" || bID.Name == "resource" {
		t.Fatalf("identity did not retain a disambiguating suffix: %#v %#v", aID, bID)
	}

	aCache, err := ContentDir(root, aID.Owner, aID.Name)
	if err != nil {
		t.Fatal(err)
	}
	bCache, err := ContentDir(root, bID.Owner, bID.Name)
	if err != nil {
		t.Fatal(err)
	}
	if aCache == bCache {
		t.Fatalf("same-basename cache directories collided: %q", aCache)
	}
}

func TestContentDirSeparatesAmbiguousDisplayNames(t *testing.T) {
	root := t.TempDir()
	first, err := ContentDir(root, "a-b", "c")
	if err != nil {
		t.Fatal(err)
	}
	second, err := ContentDir(root, "a", "b-c")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("ambiguous owner/name pairs mapped to the same cache directory")
	}
}
