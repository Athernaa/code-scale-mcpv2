package pathmatch

import (
	"path"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// Match applies slash-normalized doublestar matching to a repository-relative
// path. Patterns without a directory component also match a basename, which
// preserves the behavior users expect from file-pattern filters.
func Match(pattern, name string) bool {
	pattern = filepath.ToSlash(pattern)
	name = filepath.ToSlash(name)
	if matched, err := doublestar.Match(pattern, name); err == nil && matched {
		return true
	}
	if !strings.Contains(pattern, "/") {
		matched, err := doublestar.Match(pattern, path.Base(name))
		return err == nil && matched
	}
	return false
}
