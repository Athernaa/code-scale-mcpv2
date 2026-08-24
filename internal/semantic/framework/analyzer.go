package framework

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Athernaa/code-scale-mcpv2/internal/semantic"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic/fivem"
)

// Analyzer discovers provider/consumer facts from already-indexed FiveM
// evidence. It is intentionally source-driven: adapters only enrich facts
// that have a literal provider/API origin.
type Analyzer struct{ registry registry }

func NewAnalyzer() *Analyzer { return &Analyzer{registry: defaultRegistry()} }

func (a *Analyzer) AnalyzeFile(ctx context.Context, input semantic.FileInput) (semantic.Result, error) {
	if input.Language != "lua" && strings.ToLower(filepath.Ext(input.File)) != ".lua" {
		return semantic.Result{}, nil
	}
	return a.analyzeLuaFile(ctx, input, nil, resourceOwner{}, nil)
}

func (a *Analyzer) AnalyzeRepository(ctx context.Context, input semantic.RepositoryInput) (semantic.Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if !hasFiveMEvidence(input.SemanticEntities) {
		return semantic.Result{}, nil
	}
	state := &analysisState{
		input:        input,
		registry:     a.registry,
		providers:    map[string]semantic.Entity{},
		providerAPIs: map[string]map[string]bool{},
		ownerByFile:  ownerMap(input.SemanticEntities, input.Resource),
		knownFacts:   input.SemanticEntities,
	}
	state.manifestEvidence = manifestEvidence(input.SemanticEntities)
	state.addProviders()

	paths := make([]string, 0, len(input.Files))
	for path := range input.Files {
		if strings.EqualFold(filepath.Ext(path), ".lua") {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return semantic.Result{}, err
		}
		result, err := a.analyzeLuaFile(ctx, semantic.FileInput{Repo: input.Repo, File: path, Language: input.Languages[path], Content: input.Files[path], Symbols: input.Symbols[path], Resource: state.ownerByFile[path].name}, state, state.ownerByFile[path], input.SemanticEntities)
		if err != nil {
			return semantic.Result{}, fmt.Errorf("framework analysis %s: %w", path, err)
		}
		state.entities = append(state.entities, result.Entities...)
	}
	state.finish()
	return semantic.Result{Entities: state.entities, Relationships: state.relationships}, nil
}

type resourceOwner struct{ name, path, id string }

type analysisState struct {
	input            semantic.RepositoryInput
	registry         registry
	knownFacts       []semantic.Entity
	ownerByFile      map[string]resourceOwner
	manifestEvidence map[string]Evidence
	providers        map[string]semantic.Entity
	providerAPIs     map[string]map[string]bool
	entities         []semantic.Entity
	relationships    []semantic.Relationship
}

func hasFiveMEvidence(entities []semantic.Entity) bool {
	for _, entity := range entities {
		if entity.Analyzer == semantic.AnalyzerFiveM && (entity.Kind == fivem.KindManifestResource || entity.Kind == fivem.KindExportDefinition || entity.Kind == fivem.KindExportCall) {
			return true
		}
	}
	return false
}

func ownerMap(entities []semantic.Entity, fallback string) map[string]resourceOwner {
	result := map[string]resourceOwner{}
	for _, entity := range entities {
		if entity.Analyzer != semantic.AnalyzerFiveM {
			continue
		}
		name, path, id := sourceMetadata(entity, fallback)
		if name == "" {
			continue
		}
		key := filepath.ToSlash(filepath.Clean(entity.File))
		_, explicitOwner := entity.Metadata["source_resource"]
		if _, exists := result[key]; !exists || explicitOwner {
			result[key] = resourceOwner{name: name, path: path, id: id}
		}
		if entity.Kind == fivem.KindManifestResource && entity.Metadata != nil {
			resourceRoot := path
			switch sides := entity.Metadata["file_sides"].(type) {
			case map[string]string:
				for file := range sides {
					key := filepath.ToSlash(filepath.Clean(file))
					if resourceRoot != "" && resourceRoot != name {
						key = filepath.ToSlash(filepath.Join(resourceRoot, key))
					}
					result[key] = resourceOwner{name: name, path: path, id: id}
				}
			case map[string]any:
				for file := range sides {
					key := filepath.ToSlash(filepath.Clean(file))
					if resourceRoot != "" && resourceRoot != name {
						key = filepath.ToSlash(filepath.Join(resourceRoot, key))
					}
					result[key] = resourceOwner{name: name, path: path, id: id}
				}
			}
		}
	}
	return result
}

func manifestEvidence(entities []semantic.Entity) map[string]Evidence {
	result := map[string]Evidence{}
	for _, entity := range entities {
		if entity.Kind != fivem.KindManifestResource {
			continue
		}
		name, path, _ := sourceMetadata(entity, "")
		if path == "" {
			path = name
		}
		var evidence Evidence
		if entity.Metadata != nil {
			for _, value := range stringValues(entity.Metadata["dependencies"]) {
				evidence.Dependencies = append(evidence.Dependencies, value)
			}
			for _, value := range stringValues(entity.Metadata["shared_scripts"]) {
				if strings.HasPrefix(value, "@") {
					evidence.ExternalRefs = append(evidence.ExternalRefs, value)
				}
			}
			for _, value := range stringValues(entity.Metadata["client_scripts"]) {
				if strings.HasPrefix(value, "@") {
					evidence.ExternalRefs = append(evidence.ExternalRefs, value)
				}
			}
			for _, value := range stringValues(entity.Metadata["server_scripts"]) {
				if strings.HasPrefix(value, "@") {
					evidence.ExternalRefs = append(evidence.ExternalRefs, value)
				}
			}
		}
		result[path] = evidence
	}
	return result
}

func stringValues(value any) []string {
	var result []string
	switch values := value.(type) {
	case []string:
		return append(result, values...)
	case []any:
		for _, item := range values {
			if text, ok := item.(string); ok {
				result = append(result, text)
			}
		}
	}
	return result
}

func (s *analysisState) addProviders() {
	for _, fact := range s.knownFacts {
		if fact.Analyzer != semantic.AnalyzerFiveM || fact.Kind != fivem.KindExportDefinition || fact.Dynamic || fact.Name == "" {
			continue
		}
		name, path, id := sourceMetadata(fact, s.input.Resource)
		if name == "" {
			continue
		}
		if path == "" {
			path = name
		}
		if id == "" {
			id = semantic.StableID("workspace_resource", s.input.Repo, path)
		}
		key := path + "\x00" + fact.Name
		if s.providerAPIs[path] == nil {
			s.providerAPIs[path] = map[string]bool{}
		}
		s.providerAPIs[path][fact.Name] = true
		provider := semantic.Entity{
			Analyzer: semantic.AnalyzerFramework, Repo: s.input.Repo, File: fact.File, SymbolID: fact.SymbolID,
			Kind: KindAPIProvider, Name: fact.Name, Side: fact.Side, Line: fact.Line, EndLine: fact.EndLine,
			Metadata: map[string]any{
				"source_resource": name, "source_resource_path": path, "source_resource_id": id,
				"provider_resource": name, "provider_resource_path": path, "provider_resource_id": id,
				"api": fact.Name, "mechanism": "export", "derived_from_entity_id": fact.ID,
			},
		}
		provider.ID = semantic.StableID("framework_provider", s.input.Repo, path, fact.Name, strconv.Itoa(fact.Line))
		s.providers[key] = provider
	}
	for path, apis := range s.providerAPIs {
		owner := resourceOwner{}
		for _, provider := range s.providers {
			if provider.Metadata["provider_resource_path"] == path {
				owner.name, _ = provider.Metadata["provider_resource"].(string)
				break
			}
		}
		framework, _ := s.registry.provider(owner.name, apis, s.manifestEvidence[path])
		for key, provider := range s.providers {
			if provider.Metadata["provider_resource_path"] == path {
				provider.Framework = framework
				s.providers[key] = provider
				s.entities = append(s.entities, provider)
			}
		}
	}
}

func (s *analysisState) finish() {
	// Providers are already in the entity list. Resolve local providers only
	// when both resource name and API are unique.
	byTarget := map[string][]semantic.Entity{}
	for _, provider := range s.entities {
		if provider.Kind != KindAPIProvider {
			continue
		}
		resource, _ := provider.Metadata["provider_resource"].(string)
		byTarget[resource] = append(byTarget[resource], provider)
	}
	for i := range s.entities {
		entity := &s.entities[i]
		if entity.Kind != KindAPICall {
			continue
		}
		mechanism, _ := entity.Metadata["mechanism"].(string)
		target, _ := entity.Metadata["target_resource"].(string)
		api, _ := entity.Metadata["api"].(string)
		if mechanism == "object_method" {
			factoryID, _ := entity.Metadata["origin_factory_id"].(string)
			factoryAPI, _ := entity.Metadata["origin_factory_api"].(string)
			var factory *semantic.Entity
			for j := range s.entities {
				candidate := &s.entities[j]
				if candidate.Kind != KindAPICall || candidate.File != entity.File || candidate.Line > entity.Line {
					continue
				}
				if factoryID != "" && candidate.ID == factoryID {
					factory = candidate
					break
				}
				candidateMechanism, _ := candidate.Metadata["mechanism"].(string)
				if factoryID == "" && candidateMechanism == "export" && candidate.Name == factoryAPI && candidate.Line <= entity.Line {
					if factory != nil && factory.Line == candidate.Line {
						factory = nil
						break
					}
					factory = candidate
				}
			}
			if factory != nil {
				if ambiguous, _ := factory.Metadata["provider_ambiguous"].(bool); ambiguous {
					entity.Metadata["origin_ambiguous"] = true
					entity.Metadata["object_origin"] = false
				} else {
					s.relationships = append(s.relationships, frameworkRelationship(*entity, *factory, RelationshipObjectCall, api))
				}
			}
		} else {
			candidates := make([]semantic.Entity, 0)
			for _, provider := range byTarget[target] {
				if provider.Name == api {
					candidates = append(candidates, provider)
				}
			}
			if len(candidates) == 1 {
				provider := candidates[0]
				entity.Metadata["provider_verified"] = true
				entity.Metadata["provider_entity_id"] = provider.ID
				entity.Metadata["provider_resource"] = target
				entity.Metadata["provider_resource_path"] = provider.Metadata["provider_resource_path"]
				entity.Framework = provider.Framework
				s.relationships = append(s.relationships, frameworkRelationship(*entity, provider, RelationshipFrameworkCalls, api))
			} else if len(candidates) > 1 {
				entity.Metadata["provider_ambiguous"] = true
				entity.Metadata["provider_verified"] = false
			}
		}
		if operation, _ := entity.Metadata["operation"].(string); operation != "" {
			op := semantic.Entity{Analyzer: semantic.AnalyzerFramework, Repo: entity.Repo, File: entity.File, SymbolID: entity.SymbolID, Kind: KindOperation, Name: operation, Framework: entity.Framework, Side: entity.Side, Line: entity.Line, EndLine: entity.EndLine, Metadata: cloneMetadata(entity.Metadata)}
			op.Metadata["api"] = api
			op.ID = semantic.StableID("framework_operation", entity.Repo, entity.File, operation, strconv.Itoa(entity.Line), strconv.Itoa(entity.EndLine))
			s.entities = append(s.entities, op)
			s.relationships = append(s.relationships, frameworkRelationship(op, *entity, RelationshipDerivedFrom, operation))
		}
	}
	s.addCandidates(byTarget)
	s.sortFacts()
}

func (s *analysisState) addCandidates(byTarget map[string][]semantic.Entity) {
	for resource, providers := range byTarget {
		paths := map[string]bool{}
		apis := map[string]bool{}
		framework := FrameworkCustom
		for _, provider := range providers {
			path, _ := provider.Metadata["provider_resource_path"].(string)
			paths[path] = true
			apis[provider.Name] = true
			if provider.Framework != "" && provider.Framework != FrameworkCustom {
				framework = provider.Framework
			}
		}
		if len(paths) != 1 || framework != FrameworkCustom || len(apis) < 3 {
			continue
		}
		consumers := map[string]bool{}
		for _, entity := range s.entities {
			if entity.Kind != KindAPICall {
				continue
			}
			if target, _ := entity.Metadata["target_resource"].(string); target == resource {
				if source, _ := entity.Metadata["source_resource_path"].(string); source != "" {
					consumers[source] = true
				}
			}
		}
		path, _ := providers[0].Metadata["provider_resource_path"].(string)
		candidate := semantic.Entity{Analyzer: semantic.AnalyzerFramework, Repo: s.input.Repo, File: providers[0].File, Kind: KindCandidate, Name: resource, Framework: FrameworkCustom, Side: "shared", Line: providers[0].Line, Metadata: map[string]any{"source_resource": resource, "source_resource_path": path, "provider_resource": resource, "provider_resource_path": path, "api_count": len(apis), "consumer_count": len(consumers), "classification": "custom", "evidence": "deterministic"}}
		candidate.ID = semantic.StableID("framework_candidate", s.input.Repo, path, resource)
		s.entities = append(s.entities, candidate)
	}
}

func (s *analysisState) sortFacts() {
	sort.Slice(s.entities, func(i, j int) bool {
		if s.entities[i].File != s.entities[j].File {
			return s.entities[i].File < s.entities[j].File
		}
		if s.entities[i].Line != s.entities[j].Line {
			return s.entities[i].Line < s.entities[j].Line
		}
		return s.entities[i].ID < s.entities[j].ID
	})
	sort.Slice(s.relationships, func(i, j int) bool { return s.relationships[i].ID < s.relationships[j].ID })
}

// RebuildFacts re-resolves framework relationships from persisted compact
// framework entities. It performs no source parsing and is used by watcher
// resource refreshes after only one resource's facts have changed.
func RebuildFacts(repo string, input []semantic.Entity) semantic.Result {
	entities := make([]semantic.Entity, 0, len(input))
	for _, entity := range input {
		if entity.Kind != KindCandidate {
			entities = append(entities, entity)
		}
	}
	providersByResource := map[string][]semantic.Entity{}
	for _, entity := range entities {
		if entity.Kind != KindAPIProvider {
			continue
		}
		resource, _ := entity.Metadata["provider_resource"].(string)
		providersByResource[resource] = append(providersByResource[resource], entity)
	}
	relationships := make([]semantic.Relationship, 0)
	for i := range entities {
		entity := &entities[i]
		if entity.Kind != KindAPICall {
			continue
		}
		mechanism, _ := entity.Metadata["mechanism"].(string)
		if mechanism == "object_method" {
			if factory := persistedFactory(entities, *entity); factory != nil {
				if ambiguous, _ := factory.Metadata["provider_ambiguous"].(bool); !ambiguous {
					relationships = append(relationships, frameworkRelationship(*entity, *factory, RelationshipObjectCall, entity.Name))
				}
			}
			continue
		}
		target, _ := entity.Metadata["target_resource"].(string)
		api, _ := entity.Metadata["api"].(string)
		candidates := make([]semantic.Entity, 0)
		for _, provider := range providersByResource[target] {
			if provider.Name == api {
				candidates = append(candidates, provider)
			}
		}
		entity.Metadata["provider_verified"] = false
		delete(entity.Metadata, "provider_entity_id")
		if len(candidates) == 1 {
			provider := candidates[0]
			entity.Metadata["provider_verified"] = true
			entity.Metadata["provider_resource"] = target
			entity.Metadata["provider_resource_path"] = provider.Metadata["provider_resource_path"]
			delete(entity.Metadata, "provider_ambiguous")
			entity.Framework = provider.Framework
			relationships = append(relationships, frameworkRelationship(*entity, provider, RelationshipFrameworkCalls, api))
		} else if len(candidates) > 1 {
			entity.Metadata["provider_ambiguous"] = true
		} else {
			delete(entity.Metadata, "provider_ambiguous")
		}
	}
	for i := range entities {
		operation := &entities[i]
		if operation.Kind != KindOperation {
			continue
		}
		api, _ := operation.Metadata["api"].(string)
		for j := range entities {
			call := &entities[j]
			if call.Kind == KindAPICall && call.File == operation.File && call.Line == operation.Line && call.Name == api {
				operation.Framework = call.Framework
				metadata := cloneMetadata(call.Metadata)
				metadata["api"] = api
				operation.Metadata = metadata
				relationships = append(relationships, frameworkRelationship(*operation, *call, RelationshipDerivedFrom, operation.Name))
				break
			}
		}
	}
	entities = addPersistedCandidates(repo, entities, providersByResource)
	sort.Slice(entities, func(i, j int) bool {
		if entities[i].File != entities[j].File {
			return entities[i].File < entities[j].File
		}
		if entities[i].Line != entities[j].Line {
			return entities[i].Line < entities[j].Line
		}
		return entities[i].ID < entities[j].ID
	})
	sort.Slice(relationships, func(i, j int) bool { return relationships[i].ID < relationships[j].ID })
	return semantic.Result{Entities: entities, Relationships: relationships}
}

func persistedFactory(entities []semantic.Entity, object semantic.Entity) *semantic.Entity {
	factoryID, _ := object.Metadata["origin_factory_id"].(string)
	factoryAPI, _ := object.Metadata["origin_factory_api"].(string)
	var result *semantic.Entity
	for i := range entities {
		candidate := &entities[i]
		if candidate.Kind != KindAPICall || candidate.File != object.File || candidate.Line > object.Line {
			continue
		}
		if factoryID != "" && candidate.ID == factoryID {
			return candidate
		}
		mechanism, _ := candidate.Metadata["mechanism"].(string)
		if factoryID == "" && mechanism == "export" && candidate.Name == factoryAPI {
			if result != nil && result.Line == candidate.Line {
				return nil
			}
			result = candidate
		}
	}
	return result
}

func addPersistedCandidates(repo string, entities []semantic.Entity, providersByResource map[string][]semantic.Entity) []semantic.Entity {
	for resource, providers := range providersByResource {
		paths := map[string]bool{}
		apis := map[string]bool{}
		frameworkName := FrameworkCustom
		for _, provider := range providers {
			path, _ := provider.Metadata["provider_resource_path"].(string)
			paths[path] = true
			apis[provider.Name] = true
			if provider.Framework != "" && provider.Framework != FrameworkCustom {
				frameworkName = provider.Framework
			}
		}
		if len(paths) != 1 || frameworkName != FrameworkCustom || len(apis) < 3 {
			continue
		}
		consumers := map[string]bool{}
		for _, entity := range entities {
			if entity.Kind != KindAPICall {
				continue
			}
			if target, _ := entity.Metadata["target_resource"].(string); target == resource {
				if source, _ := entity.Metadata["source_resource_path"].(string); source != "" {
					consumers[source] = true
				}
			}
		}
		path, _ := providers[0].Metadata["provider_resource_path"].(string)
		candidate := semantic.Entity{Analyzer: semantic.AnalyzerFramework, Repo: repo, File: providers[0].File, Kind: KindCandidate, Name: resource, Framework: FrameworkCustom, Side: "shared", Line: providers[0].Line, Metadata: map[string]any{"source_resource": resource, "source_resource_path": path, "provider_resource": resource, "provider_resource_path": path, "api_count": len(apis), "consumer_count": len(consumers), "classification": "custom", "evidence": "deterministic"}}
		candidate.ID = semantic.StableID("framework_candidate", repo, path, resource)
		entities = append(entities, candidate)
	}
	return entities
}

func frameworkRelationship(from, to semantic.Entity, kind, name string) semantic.Relationship {
	return semantic.Relationship{Analyzer: semantic.AnalyzerFramework, Repo: from.Repo, ID: semantic.StableID("framework_relationship", from.ID, to.ID, kind), FromEntityID: from.ID, ToEntityID: to.ID, Kind: kind, Name: name, Confidence: 1, File: from.File, Line: from.Line}
}

func cloneMetadata(input map[string]any) map[string]any {
	result := map[string]any{}
	for k, v := range input {
		result[k] = v
	}
	return result
}
