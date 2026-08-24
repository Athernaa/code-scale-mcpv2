package pathfilter

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMatcherCombinesGitignoreAndExtraPatterns(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("ignored/\n"), 0600); err != nil {
		t.Fatal(err)
	}
	matcher, err := New(root, []string{"*.secret"})
	if err != nil {
		t.Fatal(err)
	}
	if !matcher.Ignored(filepath.Join(root, "ignored", "file.lua"), false) {
		t.Fatal("gitignore directory rule was not applied")
	}
	if !matcher.Ignored(filepath.Join(root, "token.secret"), false) {
		t.Fatal("extra ignore pattern was not applied")
	}
}
