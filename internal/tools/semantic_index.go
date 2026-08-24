package tools

import (
	"context"

	"github.com/Athernaa/code-scale-mcpv2/internal/parser"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic/fivem"
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
	if err := store.ReplaceSemanticIndex(repoID, result); err != nil {
		return 0, err
	}
	return len(result.Entities), nil
}
