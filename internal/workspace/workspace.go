package workspace

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Athernaa/code-scale-mcpv2/internal/security"
)

const (
	KindGeneric        = "generic"
	KindFiveMResource  = "fivem_resource"
	KindFiveMWorkspace = "fivem_workspace"
)

type Resource struct {
	Name, RelativePath, ManifestPath, ManifestType, GroupPath string
	EnabledState                                              string
	StartOrder                                                int
}

type ConfigCommand struct {
	Path, Command, Resource string
	Line                    int
	Order                   int
}
type ConfigFile struct {
	Path    string
	Content []byte
}
type Discovery struct {
	Mode           string
	Root           string
	Resources      []Resource
	ConfigFiles    []ConfigFile
	Commands       []ConfigCommand
	DuplicateNames []string
}

func NormalizePath(path string) string { return filepath.ToSlash(filepath.Clean(path)) }

func DetectMode(root string) (string, error) {
	if _, err := os.Stat(filepath.Join(root, "fxmanifest.lua")); err == nil {
		return KindFiveMResource, nil
	}
	if _, err := os.Stat(filepath.Join(root, "__resource.lua")); err == nil {
		return KindFiveMResource, nil
	}
	d, err := Discover(root)
	if err != nil {
		return "", err
	}
	return d.Mode, nil
}

// Discover conservatively classifies a root and discovers manifest-bearing
// resource directories. A resources directory alone is not sufficient.
func Discover(root string) (Discovery, error) {
	return discover(root, nil)
}

// DiscoverWithIgnore applies the same repository ignore policy used by source
// indexing to manifest discovery, preventing a workspace overview from
// claiming resources whose files were intentionally excluded.
func DiscoverWithIgnore(root string, ignored func(path string, isDir bool) bool) (Discovery, error) {
	return discover(root, ignored)
}

func discover(root string, ignored func(path string, isDir bool) bool) (Discovery, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Discovery{}, err
	}
	d := Discovery{Root: root, Mode: KindGeneric}
	rootManifest := ""
	for _, name := range []string{"fxmanifest.lua", "__resource.lua"} {
		if _, e := os.Stat(filepath.Join(root, name)); e == nil {
			rootManifest = name
			break
		}
	}
	if rootManifest != "" {
		if ignored == nil || !ignored(filepath.Join(root, rootManifest), false) {
			d.Mode = KindFiveMResource
			return d, nil
		}
	}
	manifestEvidence := false
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			if path != root && ignored != nil && ignored(path, true) {
				return filepath.SkipDir
			}
			if path != root && shouldSkip(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if ignored != nil && ignored(path, false) {
			return nil
		}
		name := strings.ToLower(entry.Name())
		if name != "fxmanifest.lua" && name != "__resource.lua" {
			return nil
		}
		manifestEvidence = true
		rel, _ := filepath.Rel(root, filepath.Dir(path))
		rel = NormalizePath(rel)
		if rel == "." {
			return nil
		}
		group := ""
		parts := strings.Split(rel, "/")
		if len(parts) > 1 && strings.HasPrefix(parts[len(parts)-2], "[") && strings.HasSuffix(parts[len(parts)-2], "]") {
			group = parts[len(parts)-2]
		}
		namePart := filepath.Base(filepath.Dir(path))
		d.Resources = append(d.Resources, Resource{Name: namePart, RelativePath: rel, ManifestPath: NormalizePath(filepath.Join(rel, entry.Name())), ManifestType: name, GroupPath: group})
		return nil
	})
	serverConfig := filepath.Join(root, "server.cfg")
	if _, e := os.Stat(serverConfig); e == nil && (ignored == nil || !ignored(serverConfig, false)) {
		manifestEvidence = true
	}
	if !manifestEvidence {
		return d, nil
	}
	d.Mode = KindFiveMWorkspace
	sort.Slice(d.Resources, func(i, j int) bool { return d.Resources[i].RelativePath < d.Resources[j].RelativePath })
	seen := map[string]int{}
	for i := range d.Resources {
		seen[d.Resources[i].Name]++
		if seen[d.Resources[i].Name] > 1 {
			d.DuplicateNames = appendUnique(d.DuplicateNames, d.Resources[i].Name)
		}
	}
	d.ConfigFiles, d.Commands = loadConfigs(root)
	order := 0
	for i := range d.Resources {
		d.Resources[i].EnabledState = "unknown"
	}
	byName := map[string][]int{}
	for i, r := range d.Resources {
		byName[r.Name] = append(byName[r.Name], i)
	}
	for ci := range d.Commands {
		c := &d.Commands[ci]
		if c.Resource == "" {
			continue
		}
		if c.Command == "ensure" || c.Command == "start" || c.Command == "restart" {
			order++
			c.Order = order
		}
		targets := byName[c.Resource]
		// A runtime resource name is not enough to identify one resource when
		// duplicate basenames exist. Keep both states unknown and leave the
		// command entity unresolved until a unique target is available.
		if len(targets) != 1 {
			continue
		}
		for _, i := range targets {
			switch c.Command {
			case "ensure", "start", "restart":
				d.Resources[i].EnabledState = "enabled"
				if d.Resources[i].StartOrder == 0 {
					d.Resources[i].StartOrder = order
				}
			case "stop":
				d.Resources[i].EnabledState = "disabled"
			}
		}
	}
	return d, nil
}

func shouldSkip(name string) bool {
	switch strings.ToLower(name) {
	case ".git", "node_modules", "cache", "logs", "crashes", "build", "dist":
		return true
	}
	return false
}

func loadConfigs(root string) ([]ConfigFile, []ConfigCommand) {
	var files []ConfigFile
	var commands []ConfigCommand
	visited := map[string]bool{}
	var visit func(string)
	visit = func(path string) {
		abs, ok := safeConfigPath(root, path)
		if !ok {
			return
		}
		if visited[abs] {
			return
		}
		visited[abs] = true
		data, err := os.ReadFile(abs)
		if err != nil {
			return
		}
		rel, _ := filepath.Rel(root, abs)
		rel = NormalizePath(rel)
		files = append(files, ConfigFile{Path: rel, Content: data})
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		line := 0
		for scanner.Scan() {
			line++
			text := strings.TrimSpace(scanner.Text())
			if text == "" || strings.HasPrefix(text, "#") || strings.HasPrefix(text, ";") || strings.HasPrefix(text, "//") {
				continue
			}
			fields := strings.Fields(text)
			if len(fields) < 2 {
				continue
			}
			cmd := strings.ToLower(fields[0])
			arg := strings.Trim(fields[1], "\"'")
			if cmd == "exec" {
				if !strings.ContainsAny(arg, "$%{") {
					visit(filepath.Join(filepath.Dir(abs), filepath.FromSlash(arg)))
				}
				continue
			}
			switch cmd {
			case "ensure", "start", "stop", "restart":
				commands = append(commands, ConfigCommand{Path: rel, Command: cmd, Resource: arg, Line: line})
			}
		}
	}
	if _, err := os.Stat(filepath.Join(root, "server.cfg")); err == nil {
		visit(filepath.Join(root, "server.cfg"))
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, commands
}

// safeConfigPath keeps static exec processing inside the indexed workspace.
// It validates both lexical containment and the resolved target so a cfg file
// cannot escape through traversal or a symlink.
func safeConfigPath(root, path string) (string, bool) {
	abs, err := filepath.Abs(path)
	if err != nil || !security.ValidatePath(root, abs) || security.IsSymlinkEscape(root, abs) {
		return "", false
	}
	for current := filepath.Clean(abs); security.ValidatePath(root, current); current = filepath.Dir(current) {
		if security.IsSymlinkEscape(root, current) {
			return "", false
		}
		if filepath.Clean(current) == filepath.Clean(root) {
			break
		}
	}
	abs = filepath.Clean(abs)
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// Some Windows filesystem providers do not resolve an otherwise
		// ordinary file through EvalSymlinks. The lexical boundary and the
		// leaf symlink check above still apply; retain the canonical absolute
		// path when the file itself exists.
		if _, statErr := os.Stat(abs); statErr != nil {
			return "", false
		}
		resolved = abs
	}
	if !security.ValidatePath(root, resolved) {
		return "", false
	}
	return filepath.Clean(resolved), true
}

func RelativeFile(root, path string) (string, bool) {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return NormalizePath(rel), true
}
func ResourceForPath(resources []Resource, path string) (Resource, bool) {
	path = NormalizePath(path)
	var best Resource
	found := false
	for _, r := range resources {
		prefix := r.RelativePath + "/"
		if path == r.RelativePath || strings.HasPrefix(path, prefix) {
			if !found || len(r.RelativePath) > len(best.RelativePath) {
				best = r
				found = true
			}
		}
	}
	return best, found
}
func ContentHash(data []byte) string { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }
func appendUnique(values []string, value string) []string {
	for _, v := range values {
		if v == value {
			return values
		}
	}
	return append(values, value)
}
func (d Discovery) ResourceCount() int { return len(d.Resources) }
func (d Discovery) String() string     { return fmt.Sprintf("%s (%d resources)", d.Mode, len(d.Resources)) }
