package fivem

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Athernaa/code-scale-mcpv2/internal/pathmatch"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic"
)

const (
	FrameworkFiveM = "fivem"

	KindEventRegistration    = "event_registration"
	KindEventHandler         = "event_handler"
	KindEventTrigger         = "event_trigger"
	KindCallbackRegistration = "callback_registration"
	KindCallbackCall         = "callback_call"
	KindExportDefinition     = "export_definition"
	KindExportCall           = "export_call"
	KindCommandRegistration  = "command_registration"
	KindNUICallback          = "nui_callback"
	KindManifestResource     = "manifest_resource"
	KindManifestDependency   = "manifest_dependency"

	RelationshipTriggers   = "triggers"
	RelationshipHandles    = "handles"
	RelationshipCalls      = "calls"
	RelationshipDefines    = "defines"
	RelationshipUsesExport = "uses_export"
	RelationshipDependsOn  = "depends_on"
	RelationshipRegisters  = "registers"
)

// Analyzer composes the FiveM manifest and Lua analyzers behind the generic
// semantic.Analyzer and semantic.RepositoryAnalyzer interfaces.
type Analyzer struct{}

func NewAnalyzer() *Analyzer { return &Analyzer{} }

// AnalyzeFile extracts FiveM facts from one Lua file. It deliberately operates
// on the Lua AST and never searches raw source text with regular expressions.
func (a *Analyzer) AnalyzeFile(ctx context.Context, input semantic.FileInput) (semantic.Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if input.Language != "lua" && strings.ToLower(filepath.Ext(input.File)) != ".lua" {
		return semantic.Result{}, nil
	}
	return analyzeLuaFile(ctx, input)
}

// AnalyzeRepository parses the resource manifest, classifies source sides,
// analyzes Lua files, and resolves only exact literal relationships.
func (a *Analyzer) AnalyzeRepository(ctx context.Context, input semantic.RepositoryInput) (semantic.Result, error) {
	manifestPath, manifestContent := findManifest(input.Files)
	if manifestPath == "" {
		return semantic.Result{}, nil
	}
	manifest, err := parseManifest(manifestPath, manifestContent)
	if err != nil {
		return semantic.Result{}, fmt.Errorf("parse FiveM manifest: %w", err)
	}

	repo := input.Repo
	resource := input.Resource
	if resource == "" {
		resource = repo
	}
	result := semantic.Result{}
	resourceEntity := semantic.Entity{
		Repo:      repo,
		File:      manifestPath,
		Kind:      KindManifestResource,
		Name:      resource,
		Framework: FrameworkFiveM,
		Side:      "shared",
		Line:      1,
		Metadata: map[string]any{
			"fx_version":         manifest.FXVersion,
			"game":               manifest.Game,
			"ui_page":            manifest.UIPage,
			"client_scripts":     append([]string(nil), manifest.ClientScripts...),
			"server_scripts":     append([]string(nil), manifest.ServerScripts...),
			"shared_scripts":     append([]string(nil), manifest.SharedScripts...),
			"dependencies":       append([]string(nil), manifest.Dependencies...),
			"dependency_sources": manifest.DependencySources,
			"resource":           resource,
			"source_side_model":  "manifest_globs",
		},
	}
	resourceEntity.ID = semantic.StableID("semantic", repo, manifestPath, KindManifestResource, resource)
	fileSides := make(map[string]string)
	for path := range input.Files {
		fileSides[path] = manifest.ClassifyPath(path)
	}
	resourceEntity.Metadata["file_sides"] = fileSides
	result.Entities = append(result.Entities, resourceEntity)

	for _, dependency := range manifest.Dependencies {
		entity := semantic.Entity{
			Repo:      repo,
			File:      manifestPath,
			Kind:      KindManifestDependency,
			Name:      dependency,
			Framework: FrameworkFiveM,
			Side:      "shared",
			Line:      manifest.DependencyLines[dependency],
			Metadata: map[string]any{
				"external": strings.HasPrefix(dependency, "@"),
				"sources":  append([]string(nil), manifest.DependencySources[dependency]...),
			},
		}
		entity.ID = semantic.StableID("semantic", repo, manifestPath, KindManifestDependency, dependency)
		result.Entities = append(result.Entities, entity)
		result.Relationships = append(result.Relationships, semantic.Relationship{
			ID:           semantic.StableID("relationship", resourceEntity.ID, entity.ID, RelationshipDependsOn),
			Repo:         repo,
			FromEntityID: resourceEntity.ID,
			ToEntityID:   entity.ID,
			Kind:         RelationshipDependsOn,
			Name:         dependency,
			Confidence:   1,
			File:         manifestPath,
			Line:         entity.Line,
		})
	}
	for _, export := range manifest.Exports {
		entity := semantic.Entity{
			Repo:      repo,
			File:      manifestPath,
			Kind:      KindExportDefinition,
			Name:      export.Name,
			Framework: FrameworkFiveM,
			Side:      export.Side,
			Line:      export.Line,
			Metadata: map[string]any{
				"operation": "manifest_export",
				"source":    "manifest_export",
				"resource":  resource,
			},
		}
		entity.ID = semantic.StableID("semantic", repo, manifestPath, KindExportDefinition, export.Side, export.Name)
		result.Entities = append(result.Entities, entity)
	}

	paths := make([]string, 0, len(input.Files))
	for path := range input.Files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if path == manifestPath || strings.ToLower(filepath.Ext(path)) != ".lua" {
			continue
		}
		if ctx != nil {
			select {
			case <-ctx.Done():
				return semantic.Result{}, ctx.Err()
			default:
			}
		}
		fileInput := semantic.FileInput{
			Repo:     repo,
			File:     path,
			Language: input.Languages[path],
			Content:  input.Files[path],
			Symbols:  input.Symbols[path],
			Side:     manifest.ClassifyPath(path),
			Resource: resource,
		}
		fileResult, err := a.AnalyzeFile(ctx, fileInput)
		if err != nil {
			return semantic.Result{}, err
		}
		result.Entities = append(result.Entities, fileResult.Entities...)
	}

	result.Relationships = append(result.Relationships, ResolveRelationships(result.Entities)...)
	result.Relationships = deduplicateRelationships(result.Relationships)
	return result, nil
}

// ClassifyPathFromEntity reads the persisted manifest metadata and classifies
// a later incremental file update without requiring a full repository scan.
func ClassifyPathFromEntity(entity semantic.Entity, path string) string {
	if entity.Kind != KindManifestResource || entity.Metadata == nil {
		return "unknown"
	}
	manifest := manifestInfo{
		ClientScripts: stringSlice(entity.Metadata["client_scripts"]),
		ServerScripts: stringSlice(entity.Metadata["server_scripts"]),
		SharedScripts: stringSlice(entity.Metadata["shared_scripts"]),
	}
	return manifest.ClassifyPath(path)
}

func stringSlice(value any) []string {
	values, ok := value.([]any)
	if ok {
		result := make([]string, 0, len(values))
		for _, value := range values {
			if text, ok := value.(string); ok {
				result = append(result, text)
			}
		}
		return result
	}
	valuesString, _ := value.([]string)
	return valuesString
}

func findManifest(files map[string][]byte) (string, []byte) {
	for _, name := range []string{"fxmanifest.lua", "__resource.lua"} {
		if content, ok := files[name]; ok {
			return name, content
		}
	}
	return "", nil
}

func (m manifestInfo) ClassifyPath(path string) string {
	path = filepath.ToSlash(filepath.Clean(path))
	if strings.HasPrefix(path, "@") {
		return "unknown"
	}
	client := matchesAny(m.ClientScripts, path)
	server := matchesAny(m.ServerScripts, path)
	shared := matchesAny(m.SharedScripts, path)
	switch {
	case shared || (client && server):
		return "shared"
	case client:
		return "client"
	case server:
		return "server"
	default:
		return "unknown"
	}
}

func matchesAny(patterns []string, path string) bool {
	for _, pattern := range patterns {
		if strings.HasPrefix(pattern, "@") {
			continue
		}
		if pathmatch.Match(pattern, path) {
			return true
		}
	}
	return false
}

func deduplicateRelationships(input []semantic.Relationship) []semantic.Relationship {
	seen := make(map[string]struct{}, len(input))
	result := make([]semantic.Relationship, 0, len(input))
	for _, relationship := range input {
		if _, ok := seen[relationship.ID]; ok {
			continue
		}
		seen[relationship.ID] = struct{}{}
		result = append(result, relationship)
	}
	return result
}
