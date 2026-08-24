package fivem

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Athernaa/code-scale-mcpv2/internal/semantic"
)

// ResourceContext separates runtime/resource ownership identity from the
// namespace used by indexed files. A standalone resource has an identity but
// no file prefix; a workspace resource has both an identity path and a
// repository-relative file prefix.
type ResourceContext struct {
	Name         string
	IdentityPath string
	FilePrefix   string
}

// CanonicalizeResourceResult is the sole FiveM per-resource normalization
// boundary used by full indexing and incremental refreshes.
func CanonicalizeResourceResult(repo string, resource ResourceContext, result semantic.Result) (semantic.Result, map[string]string) {
	identityPath := normalizePath(resource.IdentityPath)
	filePrefix := normalizePath(resource.FilePrefix)
	resourceID := semantic.StableID("workspace_resource", repo, identityPath)
	canonical := semantic.Result{Entities: make([]semantic.Entity, 0, len(result.Entities)), Relationships: make([]semantic.Relationship, 0, len(result.Relationships))}
	idMap := make(map[string]string, len(result.Entities))
	for _, original := range result.Entities {
		entity := original
		entity.Analyzer = semantic.AnalyzerFiveM
		entity.Metadata = cloneMetadata(original.Metadata)
		relativeFile := normalizePath(original.File)
		entity.File = joinPath(filePrefix, relativeFile)
		if entity.Metadata == nil {
			entity.Metadata = map[string]any{}
		}
		entity.Metadata["source_resource"] = resource.Name
		entity.Metadata["source_resource_path"] = identityPath
		entity.Metadata["source_resource_id"] = resourceID
		if entity.Kind == KindExportCall {
			if target, ok := entity.Metadata["resource"].(string); ok && target != "" {
				entity.Metadata["target_resource"] = target
			}
		}
		entity.Metadata["resource"] = resource.Name
		entity.Metadata["resource_path"] = identityPath
		byteOffset := ""
		if offset, ok := entity.Metadata["byte_offset"].(int); ok {
			byteOffset = strconv.Itoa(offset)
		}
		entity.ID = semantic.StableID("fivem", repo, identityPath, entity.File, entity.Kind, entity.Name, strconv.Itoa(entity.Line), strconv.Itoa(entity.EndLine), entity.Side, byteOffset)
		idMap[original.ID] = entity.ID
		canonical.Entities = append(canonical.Entities, entity)
	}
	for _, original := range result.Relationships {
		relationship := original
		relationship.Analyzer = semantic.AnalyzerFiveM
		relationship.FromEntityID = idMap[original.FromEntityID]
		relationship.ToEntityID = idMap[original.ToEntityID]
		if relationship.FromEntityID == "" || relationship.ToEntityID == "" {
			continue
		}
		relationship.File = joinPath(filePrefix, normalizePath(original.File))
		relationship.ID = semantic.StableID("relationship", relationship.FromEntityID, relationship.ToEntityID, relationship.Kind)
		canonical.Relationships = append(canonical.Relationships, relationship)
	}
	return canonical, idMap
}

func cloneMetadata(input map[string]any) map[string]any {
	result := map[string]any{}
	for key, value := range input {
		result[key] = value
	}
	return result
}

func normalizePath(path string) string {
	path = filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if path == "." {
		return ""
	}
	return strings.TrimPrefix(path, "./")
}

func joinPath(prefix, file string) string {
	if prefix == "" {
		return normalizePath(file)
	}
	if file == "" {
		return prefix
	}
	return normalizePath(filepath.ToSlash(filepath.Join(filepath.FromSlash(prefix), filepath.FromSlash(file))))
}
