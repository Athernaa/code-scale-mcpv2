package repository

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

// LocalIdentity is the stable identity of a local source directory.
type LocalIdentity struct {
	CanonicalPath string
	Owner         string
	Name          string
	Repo          string
}

// Local returns a deterministic repository identity for a local directory.
// Symlinks are resolved when possible so aliases of the same physical folder
// share an identity. Windows paths are normalized case-insensitively because
// the default Windows filesystems are case-insensitive.
func Local(path string) (LocalIdentity, error) {
	canonical, err := CanonicalPath(path)
	if err != nil {
		return LocalIdentity{}, err
	}

	base := safeComponent(filepath.Base(canonical))
	if runtime.GOOS == "windows" {
		base = strings.ToLower(base)
	}
	if base == "" || base == "." || base == string(filepath.Separator) {
		base = "root"
	}

	hash := sha256.Sum256([]byte(identityPath(canonical)))
	suffix := hex.EncodeToString(hash[:])[:12]
	name := fmt.Sprintf("%s-%s", base, suffix)

	return LocalIdentity{
		CanonicalPath: canonical,
		Owner:         "local",
		Name:          name,
		Repo:          "local/" + name,
	}, nil
}

// CanonicalPath returns an absolute, cleaned path and resolves symlinks when
// the path exists. It intentionally preserves case on non-Windows systems.
func CanonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path: %w", err)
	}
	abs = filepath.Clean(abs)
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = filepath.Clean(resolved)
	}
	return abs, nil
}

// LocalResourceName returns the human-readable source directory basename used
// by ecosystem analyzers. It is deliberately separate from LocalIdentity.Name,
// which is the collision-resistant storage/cache component.
func LocalResourceName(path string) (string, error) {
	canonical, err := CanonicalPath(path)
	if err != nil {
		return "", err
	}
	name := filepath.Base(canonical)
	if name == "" || name == "." || name == string(filepath.Separator) {
		return "", fmt.Errorf("source path has no resource name: %q", path)
	}
	return name, nil
}

// ContentDir returns the cache directory for a repository identity. The
// delimiter and digest make distinct owner/name pairs map to distinct paths,
// even when their concatenated display names would collide.
func ContentDir(basePath, owner, name string) (string, error) {
	if !isSafeComponent(owner) || !isSafeComponent(name) {
		return "", fmt.Errorf("invalid repository component: owner=%q name=%q", owner, name)
	}
	key := owner + "\x00" + name
	hash := sha256.Sum256([]byte(key))
	dirName := fmt.Sprintf("%s-%s-%s", owner, name, hex.EncodeToString(hash[:])[:12])
	return filepath.Join(basePath, dirName), nil
}

func isSafeComponent(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func identityPath(path string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(filepath.ToSlash(path))
	}
	return filepath.Clean(path)
}

func safeComponent(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), ".")
}
