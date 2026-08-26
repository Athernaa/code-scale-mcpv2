package benchmark

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Athernaa/code-scale-mcpv2/internal/parser"
	"github.com/Athernaa/code-scale-mcpv2/internal/planner"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic/framework"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic/generic"
	"github.com/Athernaa/code-scale-mcpv2/internal/storage"
	"github.com/Athernaa/code-scale-mcpv2/internal/workspace"
	workspaceindex "github.com/Athernaa/code-scale-mcpv2/internal/workspace/indexer"
)

func incrementalFullMismatches(ctx context.Context, corpus Corpus, fixtureRoot, tokenizer string) (int, error) {
	base := filepath.Join(fixtureRoot, "fivem-workspace")
	tempRoot, err := os.MkdirTemp("", "code-scale-bench-metamorphic-")
	if err != nil {
		return 1, err
	}
	defer os.RemoveAll(tempRoot)
	incrementalRoot := filepath.Join(tempRoot, "incremental")
	fullRoot := filepath.Join(tempRoot, "full")
	if err := copyDirectory(base, incrementalRoot); err != nil {
		return 1, err
	}
	if err := copyDirectory(base, fullRoot); err != nil {
		return 1, err
	}
	mutateProviderFixture := func(root string) error {
		manifest := filepath.Join(root, "resources", "[inventory]", "inventory", "fxmanifest.lua")
		content, err := os.ReadFile(manifest)
		if err != nil {
			return err
		}
		content = append(content, []byte("server_script 'other.lua'\n")...)
		if err := os.WriteFile(manifest, content, 0600); err != nil {
			return err
		}
		other := filepath.Join(root, "resources", "[inventory]", "inventory", "other.lua")
		return os.WriteFile(other, []byte("exports('AddItem', function(source, item, count) return source, item, count end)\n"), 0600)
	}
	if err := mutateProviderFixture(fullRoot); err != nil {
		return 1, err
	}
	incremental, incrementalCleanup, err := buildSingleFixtureIndex(ctx, incrementalRoot, "bench/metamorphic")
	if err != nil {
		return 1, err
	}
	defer incrementalCleanup()
	if err := mutateProviderFixture(incrementalRoot); err != nil {
		return 1, err
	}
	full, fullCleanup, err := buildSingleFixtureIndex(ctx, fullRoot, "bench/metamorphic")
	if err != nil {
		return 1, err
	}
	defer fullCleanup()

	initialFiles, initialLanguages, initialSymbols, _, _, err := readFixtureFiles(incrementalRoot)
	if err != nil {
		return 1, err
	}
	// The current disk state is the changed-resource input supplied to the
	// real incremental RefreshResource path.
	discovery, err := workspace.Discover(incrementalRoot)
	if err != nil {
		return 1, err
	}
	incrementalID := incremental.RepoIDs["bench/metamorphic"]
	if _, err := workspaceindex.RefreshResource(ctx, incremental.Store, incrementalID, "bench/metamorphic", incrementalRoot, "resources/[inventory]/inventory", initialFiles, initialLanguages, initialSymbols, discovery); err != nil {
		return 1, err
	}
	if err := refreshGenericAndFiles(ctx, incremental, "bench/metamorphic", incrementalRoot); err != nil {
		return 1, err
	}

	incSnapshot, err := authoritySnapshot(incremental.Store, incrementalID)
	if err != nil {
		return 1, err
	}
	fullID := full.RepoIDs["bench/metamorphic"]
	fullSnapshot, err := authoritySnapshot(full.Store, fullID)
	if err != nil {
		return 1, err
	}
	if incSnapshot != fullSnapshot {
		return 1, fmt.Errorf("authority snapshot mismatch: incremental=%s full=%s", incSnapshot, fullSnapshot)
	}
	task := Task{}
	for _, candidate := range corpus.Tasks {
		if candidate.ID == "fivem_export_flow" {
			task = candidate
			task.Repo = "bench/metamorphic"
			break
		}
	}
	if task.ID == "" {
		return 1, fmt.Errorf("metamorphic export task missing")
	}
	incRunner := &runner{index: incremental, corpus: corpus, tokenizer: tokenizer}
	incOutput, err := incRunner.phase7(ctx, task, 8000)
	if err != nil {
		return 1, err
	}
	fullRunner := &runner{index: full, corpus: corpus, tokenizer: tokenizer}
	fullOutput, err := fullRunner.phase7(ctx, task, 8000)
	if err != nil {
		return 1, err
	}
	if fingerprint(task, ModePhase7, 8000, incOutput) != fingerprint(task, ModePhase7, 8000, fullOutput) {
		return 1, fmt.Errorf("context fingerprint mismatch: incremental=%s full=%s incremental_detail=%s full_detail=%s", fingerprint(task, ModePhase7, 8000, incOutput), fingerprint(task, ModePhase7, 8000, fullOutput), phaseDetail(incOutput), phaseDetail(fullOutput))
	}
	return 0, nil
}

func phaseDetail(output modeOutput) string {
	var parts []string
	if output.Plan != nil {
		for _, item := range append(append(append([]planner.Candidate{}, output.Plan.Primary...), output.Plan.Supporting...), output.Plan.Peripheral...) {
			parts = append(parts, item.ID+":"+item.Name+":"+item.File+":"+item.Authority)
		}
	}
	if output.Package != nil {
		parts = append(parts, "status="+output.Package.Sufficiency.Status, "stop="+output.Package.StopReason)
		for _, section := range output.Package.Sections {
			parts = append(parts, "section="+section.CandidateID+":"+section.Name+":"+section.File+":"+section.Authority)
		}
	}
	return strings.Join(parts, ",")
}

func buildSingleFixtureIndex(ctx context.Context, root, repoName string) (*FixtureIndex, func(), error) {
	base, err := os.MkdirTemp("", "code-scale-bench-single-")
	if err != nil {
		return nil, nil, err
	}
	store, err := storage.NewIndexStore(base)
	if err != nil {
		_ = os.RemoveAll(base)
		return nil, nil, err
	}
	index := &FixtureIndex{Store: store, BasePath: base, RepoIDs: map[string]int64{}, Files: map[string][]FixtureFile{}, Symbols: map[string]map[string][]parser.Symbol{}}
	cleanup := func() { _ = store.Close(); _ = os.RemoveAll(base) }
	if err := indexRepository(ctx, index, RepositorySpec{Name: repoName, Path: ".", Kind: "fivem"}, root); err != nil {
		cleanup()
		return nil, nil, err
	}
	return index, cleanup, nil
}

func refreshGenericAndFiles(ctx context.Context, index *FixtureIndex, repoName, root string) error {
	files, languages, symbols, hashes, allSymbols, err := readFixtureFiles(root)
	if err != nil {
		return err
	}
	parts := strings.SplitN(repoName, "/", 2)
	if err := index.Store.ReplaceRepoIndex(parts[0], parts[1], "local", "", hashes, languages, allSymbols, root); err != nil {
		return err
	}
	for path, content := range files {
		if err := index.Store.SaveContentFile(parts[0], parts[1], path, content); err != nil {
			return err
		}
	}
	result, err := generic.NewAnalyzer().AnalyzeRepository(ctx, semantic.RepositoryInput{Repo: repoName, Files: files, Languages: languages, Symbols: symbols})
	if err != nil {
		return err
	}
	if err := index.Store.ReplaceSemanticIndexForAnalyzer(index.RepoIDs[repoName], semantic.AnalyzerGenericGraph, result); err != nil {
		return err
	}
	if err := refreshFrameworkResource(ctx, index, repoName, root, files, languages, symbols); err != nil {
		return err
	}
	index.Files[repoName] = fixtureFiles(files, languages)
	index.Symbols[repoName] = symbols
	return nil
}

func refreshFrameworkResource(ctx context.Context, index *FixtureIndex, repoName, root string, files map[string][]byte, languages map[string]string, symbols map[string][]parser.Symbol) error {
	discovery, err := workspace.Discover(root)
	if err != nil {
		return err
	}
	resource, found := workspace.ResourceForPath(discovery.Resources, "resources/[inventory]/inventory")
	if !found {
		return fmt.Errorf("inventory resource missing from metamorphic fixture")
	}
	localFiles := map[string][]byte{}
	localLanguages := map[string]string{}
	localSymbols := map[string][]parser.Symbol{}
	prefix := resource.RelativePath + "/"
	for path, data := range files {
		if path == resource.RelativePath || strings.HasPrefix(path, prefix) {
			local := strings.TrimPrefix(path, prefix)
			localFiles[local] = data
			localLanguages[local] = languages[path]
			localSymbols[local] = symbols[path]
		}
	}
	repoID := index.RepoIDs[repoName]
	allFiveM, err := index.Store.GetSemanticEntitiesForAnalyzer(repoID, semantic.AnalyzerFiveM)
	if err != nil {
		return err
	}
	resourceFacts := make([]semantic.Entity, 0)
	for _, fact := range allFiveM {
		if path, _ := fact.Metadata["source_resource_path"].(string); workspace.NormalizePath(path) == workspace.NormalizePath(resource.RelativePath) {
			resourceFacts = append(resourceFacts, fact)
		}
	}
	registry := make([]semantic.ResourceIdentity, 0, len(discovery.Resources))
	for _, candidate := range discovery.Resources {
		registry = append(registry, semantic.ResourceIdentity{Name: candidate.Name, Path: candidate.RelativePath, ID: semantic.StableID("workspace_resource", repoName, candidate.RelativePath)})
	}
	result, err := framework.NewAnalyzer().AnalyzeRepository(ctx, semantic.RepositoryInput{Repo: repoName, Resource: resource.Name, SourceType: "local", Files: localFiles, Languages: localLanguages, Symbols: localSymbols, SemanticEntities: resourceFacts, ResourceRegistry: registry})
	if err != nil {
		return err
	}
	result = workspaceindex.NormalizeFrameworkResult(repoName, resource, result, nil)
	if err := index.Store.ReplaceSemanticResourceForAnalyzer(repoID, semantic.AnalyzerFramework, resource.RelativePath, result); err != nil {
		return err
	}
	allFramework, err := index.Store.GetSemanticEntitiesForAnalyzer(repoID, semantic.AnalyzerFramework)
	if err != nil {
		return err
	}
	return index.Store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerFramework, framework.RebuildFacts(repoName, allFramework, registry))
}

func copyDirectory(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0600)
	})
}

func authoritySnapshot(store *storage.IndexStore, repoID int64) (string, error) {
	entities, err := store.GetSemanticEntitiesForAnalyzer(repoID, semantic.AnalyzerFiveM)
	if err != nil {
		return "", err
	}
	status, verified, providerID := "", false, ""
	for _, entity := range entities {
		if entity.Kind != "export_call" || entity.Name != "AddItem" {
			continue
		}
		status, _ = entity.Metadata["provider_status"].(string)
		verified, _ = entity.Metadata["provider_verified"].(bool)
		providerID, _ = entity.Metadata["provider_entity_id"].(string)
		break
	}
	relationships, err := store.GetSemanticRelationshipsForAnalyzer(repoID, semantic.AnalyzerFiveMWorkspace)
	if err != nil {
		return "", err
	}
	edges := 0
	for _, relationship := range relationships {
		if relationship.Kind == "cross_resource_export" {
			edges++
		}
	}
	return fmt.Sprintf("%s|%t|%s|%d", status, verified, providerID, edges), nil
}
