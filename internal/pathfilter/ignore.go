package pathfilter

import (
	"os"
	"path/filepath"
	"strings"

	gitignore "github.com/sabhiram/go-gitignore"
)

// Matcher applies repository-root ignore rules to absolute paths.
type Matcher struct {
	root   string
	ignore *gitignore.GitIgnore
}

func New(root string, extra []string) (*Matcher, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	root = filepath.Clean(root)
	lines := append([]string{}, extra...)
	gitignorePath := filepath.Join(root, ".gitignore")
	if content, readErr := os.ReadFile(gitignorePath); readErr == nil {
		lines = append(lines, strings.Split(string(content), "\n")...)
	} else if !os.IsNotExist(readErr) {
		return nil, readErr
	}
	return &Matcher{root: root, ignore: gitignore.CompileIgnoreLines(lines...)}, nil
}

// NewFromLines is used by remote indexers that already fetched .gitignore.
func NewFromLines(root string, lines []string, extra []string) (*Matcher, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	all := append([]string{}, extra...)
	all = append(all, lines...)
	return &Matcher{root: filepath.Clean(root), ignore: gitignore.CompileIgnoreLines(all...)}, nil
}

func (m *Matcher) Ignored(path string, isDir bool) bool {
	rel, err := filepath.Rel(m.root, path)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return m.ignoredRelative(filepath.ToSlash(rel), isDir)
}

func (m *Matcher) IgnoredRelative(rel string, isDir bool) bool {
	return m.ignoredRelative(filepath.ToSlash(filepath.Clean(rel)), isDir)
}

func (m *Matcher) ignoredRelative(rel string, isDir bool) bool {
	if m.ignore.MatchesPath(rel) {
		return true
	}
	return isDir && m.ignore.MatchesPath(rel+"/")
}
