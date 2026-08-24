package framework

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Athernaa/code-scale-mcpv2/internal/semantic"
)

// CanonicalizeResult gives framework facts one identity scheme regardless of
// whether they came from a full workspace index, a resource refresh, or a
// standalone resource index. sourceEntityIDs is used for references into the
// persisted FiveM analyzer (currently derived_from_entity_id).
//
// The function deliberately performs identity generation before metadata
// rewriting. Framework facts commonly refer to one another, so rewriting a
// reference while entities are still being assigned IDs can leave a dangling
// ID or make the result depend on iteration order.
func CanonicalizeResult(repo, resourcePath string, result semantic.Result, sourceEntityIDs map[string]string) semantic.Result {
	resourcePath = normalizePath(resourcePath)
	if resourcePath == "." {
		resourcePath = ""
	}
	resourceID := semantic.StableID("workspace_resource", repo, resourcePath)
	canonical := semantic.Result{Entities: make([]semantic.Entity, 0, len(result.Entities)), Relationships: make([]semantic.Relationship, 0, len(result.Relationships))}
	idMap := make(map[string]string, len(result.Entities))
	entities := make([]semantic.Entity, 0, len(result.Entities))

	// Pass 1a: assign IDs to all non-operation facts. Calls use source_offset,
	// which remains stable when the same file is presented as resource-local or
	// workspace-relative input. Providers use the canonical FiveM bridge when
	// one is available.
	for _, original := range result.Entities {
		entity := original
		entity.Analyzer = semantic.AnalyzerFramework
		entity.Metadata = cloneMetadata(original.Metadata)
		relativeFile := relativeResourceFile(resourcePath, original.File)
		entity.File = joinCanonicalPath(resourcePath, relativeFile)
		if entity.Metadata == nil {
			entity.Metadata = map[string]any{}
		}
		entity.Metadata["source_resource_path"] = resourcePath
		// Ownership metadata is part of the canonical workspace identity too;
		// raw per-resource analysis may have used the resource basename here.
		entity.Metadata["source_resource_id"] = resourceID
		if entity.Kind == KindAPIProvider || entity.Kind == KindCandidate {
			entity.Metadata["provider_resource_path"] = resourcePath
			entity.Metadata["provider_resource_id"] = resourceID
		}

		derived := remapSourceReference(entity.Metadata, "derived_from_entity_id", sourceEntityIDs)
		if derived != "" {
			entity.Metadata["derived_from_entity_id"] = derived
		}
		entity.ID = canonicalEntityID(repo, resourcePath, relativeFile, entity, derived)
		idMap[original.ID] = entity.ID
		entities = append(entities, entity)
	}

	// Pass 1b: operation identity is based on the canonical backing call, not
	// on the raw analyzer operation ID. This also makes same-line operations
	// distinct when their backing calls have distinct source offsets.
	for i := range entities {
		if entities[i].Kind != KindOperation {
			continue
		}
		backing, _ := entities[i].Metadata["backing_call_id"].(string)
		if mapped := idMap[backing]; mapped != "" {
			entities[i].ID = semantic.StableID("framework_operation", mapped, entities[i].Name)
		}
		idMap[result.Entities[i].ID] = entities[i].ID
	}

	// Pass 2: rewrite only fields whose schema is an entity-ID reference. Do
	// not rewrite ordinary resource names, paths, APIs, or literal arguments.
	for i := range entities {
		for _, field := range []string{"origin_factory_id", "backing_call_id", "provider_entity_id", "derived_from_entity_id"} {
			value, _ := entities[i].Metadata[field].(string)
			if value == "" {
				continue
			}
			if field == "derived_from_entity_id" {
				if mapped := sourceEntityIDs[value]; mapped != "" {
					entities[i].Metadata[field] = mapped
					continue
				}
			}
			if mapped := idMap[value]; mapped != "" {
				entities[i].Metadata[field] = mapped
			}
		}
	}

	for _, entity := range entities {
		canonical.Entities = append(canonical.Entities, entity)
	}
	for _, original := range result.Relationships {
		relationship := original
		relationship.Analyzer = semantic.AnalyzerFramework
		relationship.FromEntityID = idMap[original.FromEntityID]
		relationship.ToEntityID = idMap[original.ToEntityID]
		if relationship.FromEntityID == "" || relationship.ToEntityID == "" {
			continue
		}
		relationship.File = joinCanonicalPath(resourcePath, relativeResourceFile(resourcePath, original.File))
		relationship.ID = semantic.StableID("framework_relationship", relationship.FromEntityID, relationship.ToEntityID, relationship.Kind)
		canonical.Relationships = append(canonical.Relationships, relationship)
	}
	return canonical
}

func canonicalEntityID(repo, resourcePath, relativeFile string, entity semantic.Entity, derived string) string {
	if entity.Kind == KindOperation {
		if backing, _ := entity.Metadata["backing_call_id"].(string); backing != "" {
			return semantic.StableID("framework_operation", backing, entity.Name)
		}
	}
	if entity.Kind == KindCandidate {
		return semantic.StableID("framework_candidate", repo, resourcePath, entity.Name)
	}
	if entity.Kind == KindStatus {
		return semantic.StableID("framework_status", repo, resourcePath)
	}
	if entity.Kind == KindAPIProvider && derived != "" {
		return semantic.StableID("framework", repo, resourcePath, relativeFile, entity.Kind, entity.Name, derived)
	}
	offset, _ := entity.Metadata["source_offset"].(int)
	mechanism, _ := entity.Metadata["mechanism"].(string)
	target, _ := entity.Metadata["target_resource"].(string)
	if _, hasOffset := entity.Metadata["source_offset"]; hasOffset {
		return semantic.StableID("framework", repo, resourcePath, relativeFile, entity.Kind, entity.Name, mechanism, target, strconv.Itoa(offset))
	}
	return semantic.StableID("framework", repo, resourcePath, relativeFile, entity.Kind, entity.Name, mechanism, target, strconv.Itoa(offset), entity.ID)
}

func remapSourceReference(metadata map[string]any, field string, sourceEntityIDs map[string]string) string {
	value, _ := metadata[field].(string)
	if value == "" {
		return ""
	}
	if mapped := sourceEntityIDs[value]; mapped != "" {
		return mapped
	}
	return value
}

func relativeResourceFile(resourcePath, file string) string {
	p := normalizePath(file)
	root := normalizePath(resourcePath)
	if root != "" && (p == root || strings.HasPrefix(p, root+"/")) {
		return strings.TrimPrefix(strings.TrimPrefix(p, root), "/")
	}
	return p
}

func joinCanonicalPath(resourcePath, file string) string {
	if resourcePath == "" {
		return normalizePath(file)
	}
	if file == "" {
		return resourcePath
	}
	return normalizePath(filepath.ToSlash(filepath.Join(filepath.FromSlash(resourcePath), filepath.FromSlash(file))))
}

func normalizePath(path string) string {
	path = filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if path == "." {
		return ""
	}
	return strings.TrimPrefix(path, "./")
}
