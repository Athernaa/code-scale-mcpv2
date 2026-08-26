package benchmark

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Athernaa/code-scale-mcpv2/internal/parser"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic/generic"
	"github.com/Athernaa/code-scale-mcpv2/internal/storage"
	"github.com/Athernaa/code-scale-mcpv2/internal/workspace"
	workspaceindex "github.com/Athernaa/code-scale-mcpv2/internal/workspace/indexer"
)

type FixtureIndex struct {
	Store    *storage.IndexStore
	BasePath string
	RepoIDs  map[string]int64
	Files    map[string][]FixtureFile
	Symbols  map[string]map[string][]parser.Symbol
}

type FixtureFile struct {
	Path     string
	Language string
	Content  []byte
}

func BuildFixtureIndex(ctx context.Context, corpus Corpus, fixtureRoot string) (*FixtureIndex, func(), error) {
	base, err := os.MkdirTemp("", "code-scale-bench-store-")
	if err != nil {
		return nil, nil, err
	}
	store, err := storage.NewIndexStore(base)
	if err != nil {
		_ = os.RemoveAll(base)
		return nil, nil, err
	}
	index := &FixtureIndex{Store: store, BasePath: base, RepoIDs: map[string]int64{}, Files: map[string][]FixtureFile{}, Symbols: map[string]map[string][]parser.Symbol{}}
	cleanup := func() {
		_ = store.Close()
		_ = os.RemoveAll(base)
	}
	for _, repo := range corpus.Repositories {
		if err := indexRepository(ctx, index, repo, filepath.Join(fixtureRoot, repo.Path)); err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("index fixture %s: %w", repo.Name, err)
		}
	}
	return index, cleanup, nil
}

func indexRepository(ctx context.Context, index *FixtureIndex, spec RepositorySpec, root string) error {
	files, languages, symbols, hashes, allSymbols, err := readFixtureFiles(root)
	if err != nil {
		return err
	}
	parts := strings.SplitN(spec.Name, "/", 2)
	if len(parts) != 2 {
		return fmt.Errorf("repository name must be owner/name: %s", spec.Name)
	}
	if err := index.Store.ReplaceRepoIndex(parts[0], parts[1], "local", "", hashes, languages, allSymbols, root); err != nil {
		return err
	}
	for path, data := range files {
		if err := index.Store.SaveContentFile(parts[0], parts[1], path, data); err != nil {
			return err
		}
	}
	repoID, err := index.Store.GetRepoID(spec.Name)
	if err != nil {
		return err
	}
	index.RepoIDs[spec.Name] = repoID
	index.Files[spec.Name] = fixtureFiles(files, languages)
	index.Symbols[spec.Name] = symbols

	if spec.Kind == "fivem" {
		discovery, err := workspace.Discover(root)
		if err != nil {
			return err
		}
		if _, err := workspaceindex.Index(ctx, index.Store, repoID, spec.Name, root, files, languages, symbols, discovery); err != nil {
			return err
		}
	}
	modulePath := ""
	if data, readErr := os.ReadFile(filepath.Join(root, "go.mod")); readErr == nil {
		modulePath = moduleFromGoMod(string(data))
	}
	result, err := generic.NewAnalyzer().AnalyzeRepository(ctx, semantic.RepositoryInput{Repo: spec.Name, Files: files, Languages: languages, Symbols: symbols, ModulePath: modulePath})
	if err != nil {
		return err
	}
	return index.Store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerGenericGraph, result)
}

func readFixtureFiles(root string) (map[string][]byte, map[string]string, map[string][]parser.Symbol, map[string]string, []parser.Symbol, error) {
	files := map[string][]byte{}
	languages := map[string]string{}
	symbols := map[string][]parser.Symbol{}
	hashes := map[string]string{}
	var allSymbols []parser.Symbol
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		language := parser.DetectLanguage(rel)
		if language == "" {
			return nil
		}
		if _, ok := parser.LanguageRegistry[language]; !ok {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		parsed, err := parser.ParseFile(data, rel, language)
		if err != nil {
			return err
		}
		hash := sha256.Sum256(data)
		files[rel] = data
		languages[rel] = language
		symbols[rel] = parsed
		hashes[rel] = hex.EncodeToString(hash[:])
		allSymbols = append(allSymbols, parsed...)
		return nil
	})
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	return files, languages, symbols, hashes, allSymbols, nil
}

func fixtureFiles(files map[string][]byte, languages map[string]string) []FixtureFile {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	result := make([]FixtureFile, 0, len(paths))
	for _, path := range paths {
		result = append(result, FixtureFile{Path: path, Language: languages[path], Content: files[path]})
	}
	return result
}

func moduleFromGoMod(content string) string {
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "module" {
			return fields[1]
		}
	}
	return ""
}
