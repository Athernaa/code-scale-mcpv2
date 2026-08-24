package tools

import (
	"context"
	"fmt"
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
	repoID, err := store.GetRepoID(repo)
	if err != nil {
		return 0, err
	}
	result, err := fivem.NewAnalyzer().AnalyzeRepository(ctx, semantic.RepositoryInput{
		Repo: repo, Resource: resource, SourceType: sourceType,
		Files: files, Languages: languages, Symbols: symbols,
	})
	if err != nil {
		// The source/symbol index may already have advanced. Never leave the
		// previous analyzer result looking authoritative for the new source.
		if clearErr := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerFiveM, semantic.Result{}); clearErr != nil {
			return 0, fmt.Errorf("FiveM analysis failed: %v; clearing stale facts failed: %w", err, clearErr)
		}
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
	repoID, err := store.GetRepoID(repo)
	if err != nil {
		return 0, err
	}
	result, err := generic.NewAnalyzer().AnalyzeRepository(ctx, semantic.RepositoryInput{
		Repo: repo, ModulePath: firstModulePath(modulePaths), Files: files, Languages: languages, Symbols: symbols,
	})
	if err != nil {
		// Analyzer ownership is isolated: clear only generic graph facts when
		// the new source cannot be analyzed.
		if clearErr := store.ReplaceSemanticIndexForAnalyzer(repoID, semantic.AnalyzerGenericGraph, semantic.Result{}); clearErr != nil {
			return 0, fmt.Errorf("generic graph analysis failed: %v; clearing stale facts failed: %w", err, clearErr)
		}
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
