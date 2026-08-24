package tools

import (
	"context"
	"strings"

	"github.com/Athernaa/code-scale-mcpv2/internal/parser"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic/fivem"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic/generic"
	"github.com/Athernaa/code-scale-mcpv2/internal/storage"
)

func indexSemanticRepository(
	ctx context.Context,
	store *storage.IndexStore,
	repo string,
	resource string,
	sourceType string,
	files map[string][]byte,
	languages map[string]string,
	symbols map[string][]parser.Symbol,
) (int, error) {
	result, err := fivem.NewAnalyzer().AnalyzeRepository(ctx, semantic.RepositoryInput{
		Repo: repo, Resource: resource, SourceType: sourceType,
		Files: files, Languages: languages, Symbols: symbols,
	})
	if err != nil {
		return 0, err
	}
	repoID, err := store.GetRepoID(repo)
	if err != nil {
		return 0, err
	}
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerFiveM, result); err != nil {
		return 0, err
	}
	return len(result.Entities), nil
}

func indexGenericRepository(
	ctx context.Context,
	store *storage.IndexStore,
	repo string,
	files map[string][]byte,
	languages map[string]string,
	symbols map[string][]parser.Symbol,
	modulePaths ...string,
) (int, error) {
	result, err := generic.NewAnalyzer().AnalyzeRepository(ctx, semantic.RepositoryInput{
		Repo: repo, ModulePath: firstModulePath(modulePaths), Files: files, Languages: languages, Symbols: symbols,
	})
	if err != nil {
		return 0, err
	}
	repoID, err := store.GetRepoID(repo)
	if err != nil {
		return 0, err
	}
	if err := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerGenericGraph, result); err != nil {
		return 0, err
	}
	return len(result.Entities), nil
}

func firstModulePath(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0])
}

func goModulePath(content []byte) string {
	for _, line := range strings.Split(string(content), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "module" {
			return fields[1]
		}
	}
	return ""
}
