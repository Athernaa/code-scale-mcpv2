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
		owner := state.ownerByFile[path]
		if owner.name == "" {
			owner.name = input.Resource
		}
		if owner.path == "" {
			owner.path = input.Resource
		}
		if owner.id == "" {
			owner.id = semantic.StableID("workspace_resource", input.Repo, owner.path)
		}
		result, err := a.analyzeLuaFile(ctx, semantic.FileInput{Repo: input.Repo, File: path, Language: input.Languages[path], Content: input.Files[path], Symbols: input.Symbols[path], Side: owner.side, Resource: owner.name}, state, owner, input.SemanticEntities)
		if err != nil {
			return semantic.Result{}, fmt.Errorf("framework analysis %s: %w", path, err)
		}
		state.entities = append(state.entities, result.Entities...)
	}
	// Keep source analysis responsible for extracting compact raw provider and
	// call facts. RebuildFacts is the single indexed finalizer for provider
	// authority, object lineage, operations, candidates, and relationships.
	return RebuildFacts(input.Repo, state.entities, input.ResourceRegistry), nil
}

type resourceOwner struct{ name, path, id, side string }

type analysisState struct {
	input            semantic.RepositoryInput
	registry         registry
	knownFacts       []semantic.Entity
	ownerByFile      map[string]resourceOwner
	manifestEvidence map[string]Evidence
	providers        map[string]semantic.Entity
	providerAPIs     map[string]map[string]bool
	entities         []semantic.Entity
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
			result[key] = resourceOwner{name: name, path: path, id: id, side: semantic.NormalizeSide(entity.Side)}
		}
		if entity.Kind == fivem.KindManifestResource && entity.Metadata != nil {
			resourceRoot := path
			switch sides := entity.Metadata["file_sides"].(type) {
			case map[string]string:
				for file, side := range sides {
					key := filepath.ToSlash(filepath.Clean(file))
					if resourceRoot != "" && resourceRoot != name {
						key = filepath.ToSlash(filepath.Join(resourceRoot, key))
					}
					result[key] = resourceOwner{name: name, path: path, id: id, side: semantic.NormalizeSide(side)}
				}
			case map[string]any:
				for file, value := range sides {
					key := filepath.ToSlash(filepath.Clean(file))
					if resourceRoot != "" && resourceRoot != name {
						key = filepath.ToSlash(filepath.Join(resourceRoot, key))
					}
					side, _ := value.(string)
					result[key] = resourceOwner{name: name, path: path, id: id, side: semantic.NormalizeSide(side)}
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
	providerKeysByPath := map[string][]string{}
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
		provider.ID = semantic.StableID("framework_provider", s.input.Repo, path, fact.Name, fact.ID, strconv.Itoa(fact.Line))
		if _, exists := s.providers[key]; exists {
			continue
		}
		s.providers[key] = provider
		providerKeysByPath[path] = append(providerKeysByPath[path], key)
	}
	for path, apis := range s.providerAPIs {
		ownerName := ""
		if keys := providerKeysByPath[path]; len(keys) > 0 {
			ownerName, _ = s.providers[keys[0]].Metadata["provider_resource"].(string)
		}
		framework, _ := s.registry.provider(ownerName, apis, s.manifestEvidence[path])
		for _, key := range providerKeysByPath[path] {
			provider := s.providers[key]
			provider.Framework = framework
			s.providers[key] = provider
			s.entities = append(s.entities, provider)
		}
	}
}

// RebuildFacts re-resolves framework relationships from persisted compact
// framework entities. It performs no source parsing and is used by watcher
// resource refreshes after only one resource's facts have changed.
func RebuildFacts(repo string, input []semantic.Entity, registries ...[]semantic.ResourceIdentity) semantic.Result {
	// Rebuild from compact facts only. The maps below are deliberately built
	// once so relationship refresh is linear in the persisted fact set rather
	// than a scan of every entity for every call/operation/provider.
	entities := make([]semantic.Entity, 0, len(input))
	preliminaryOperations := map[string]semantic.Entity{}
	for _, original := range input {
		entity := original
		entity.Metadata = cloneMetadata(original.Metadata)
		if entity.Kind == KindOperation {
			if backing, _ := entity.Metadata["backing_call_id"].(string); backing != "" {
				preliminaryOperations[backing] = entity
			} else {
				key := entity.File + "\x00" + strconv.Itoa(entity.Line) + "\x00" + entity.Name
				preliminaryOperations[key] = entity
			}
			continue
		}
		if entity.Kind != KindCandidate {
			entities = append(entities, entity)
		}
	}

	providersByKey := map[string][]semantic.Entity{}
	providersByResource := map[string][]semantic.Entity{}
	providersByPath := map[string][]semantic.Entity{}
	providerPathsByResource := map[string]map[string]bool{}
	for _, entity := range entities {
		if entity.Kind != KindAPIProvider {
			continue
		}
		resource, _ := entity.Metadata["provider_resource"].(string)
		path, _ := entity.Metadata["provider_resource_path"].(string)
		providersByKey[resource+"\x00"+entity.Name] = append(providersByKey[resource+"\x00"+entity.Name], entity)
		providersByResource[resource] = append(providersByResource[resource], entity)
		providersByPath[path] = append(providersByPath[path], entity)
		if providerPathsByResource[resource] == nil {
			providerPathsByResource[resource] = map[string]bool{}
		}
		providerPathsByResource[resource][path] = true
	}

	resourceRegistry := map[string][]semantic.ResourceIdentity{}
	hasRegistry := len(registries) > 0 && len(registries[0]) > 0
	if hasRegistry {
		for _, identity := range registries[0] {
			resourceRegistry[identity.Name] = append(resourceRegistry[identity.Name], identity)
		}
	} else {
		// Backward-compatible helper behavior for callers that only have
		// persisted provider facts. Production workspace paths always pass the
		// full registry, including resources without provider APIs.
		for resource, paths := range providerPathsByResource {
			for path := range paths {
				resourceRegistry[resource] = append(resourceRegistry[resource], semantic.ResourceIdentity{Name: resource, Path: path, ID: semantic.StableID("workspace_resource", repo, path)})
			}
		}
	}

	callsByID := map[string]*semantic.Entity{}
	callsByFileAPI := map[string][]*semantic.Entity{}
	for i := range entities {
		if entities[i].Kind != KindAPICall {
			continue
		}
		callsByID[entities[i].ID] = &entities[i]
		api, _ := entities[i].Metadata["api"].(string)
		key := entities[i].File + "\x00" + api
		callsByFileAPI[key] = append(callsByFileAPI[key], &entities[i])
	}

	relationships := make([]semantic.Relationship, 0)
	for i := range entities {
		call := &entities[i]
		if call.Kind != KindAPICall {
			continue
		}
		mechanism, _ := call.Metadata["mechanism"].(string)
		if mechanism == "object_method" {
			continue
		}

		target, _ := call.Metadata["target_resource"].(string)
		api, _ := call.Metadata["api"].(string)
		localIdentities := resourceRegistry[target]
		candidateKey := target + "\x00" + api
		candidates := providersByKey[candidateKey]
		call.Metadata["provider_verified"] = false
		delete(call.Metadata, "provider_entity_id")
		delete(call.Metadata, "provider_resource")
		delete(call.Metadata, "provider_resource_path")
		delete(call.Metadata, "provider_resource_id")
		delete(call.Metadata, "provider_ambiguous")

		status := ProviderStatusExternal
		var provider *semantic.Entity
		if len(localIdentities) > 1 {
			status = ProviderStatusLocalAmbiguous
			call.Metadata["provider_ambiguous"] = true
		} else if len(localIdentities) == 1 {
			status = ProviderStatusLocalMissing
			identityPath := localIdentities[0].Path
			matching := make([]semantic.Entity, 0, len(candidates))
			for _, candidate := range candidates {
				path, _ := candidate.Metadata["provider_resource_path"].(string)
				if path == identityPath && semantic.ExportSidesCompatible(call.Side, candidate.Side) {
					matching = append(matching, candidate)
				}
			}
			if len(matching) == 1 {
				provider = &matching[0]
				status = ProviderStatusLocalVerified
			} else if len(matching) > 1 {
				status = ProviderStatusLocalAmbiguous
				call.Metadata["provider_ambiguous"] = true
			}
		} else if !hasRegistry {
			matching := make([]semantic.Entity, 0, len(candidates))
			for _, candidate := range candidates {
				if semantic.ExportSidesCompatible(call.Side, candidate.Side) {
					matching = append(matching, candidate)
				}
			}
			if len(matching) == 1 {
				// The compatibility path above has already made a unique provider
				// identity. Keep its normal local verification semantics.
				provider = &matching[0]
				status = ProviderStatusLocalVerified
			} else if len(matching) > 1 {
				status = ProviderStatusLocalAmbiguous
				call.Metadata["provider_ambiguous"] = true
			}
		}
		call.Metadata["provider_status"] = status

		if provider != nil {
			call.Metadata["provider_verified"] = true
			call.Metadata["provider_entity_id"] = provider.ID
			call.Metadata["provider_resource"] = target
			call.Metadata["provider_resource_path"] = provider.Metadata["provider_resource_path"]
			call.Metadata["provider_resource_id"] = provider.Metadata["provider_resource_id"]
			call.Framework = provider.Framework
			relationships = append(relationships, frameworkRelationship(*call, *provider, RelationshipFrameworkCalls, api))
		}

		operationName, operationMetadata := rebuiltOperation(call, provider, status, preliminaryOperations)
		if operationName != "" {
			for key, value := range operationMetadata {
				call.Metadata[key] = value
			}
		}
	}
	for i := range entities {
		call := &entities[i]
		if call.Kind != KindAPICall {
			continue
		}
		mechanism, _ := call.Metadata["mechanism"].(string)
		if mechanism != "object_method" {
			continue
		}
		factory := persistedFactoryIndexed(callsByID, callsByFileAPI, *call)
		if factory == nil {
			delete(call.Metadata, "operation")
			continue
		}
		status, _ := factory.Metadata["provider_status"].(string)
		ambiguous, _ := factory.Metadata["provider_ambiguous"].(bool)
		call.Framework = factory.Framework
		call.Metadata["provider_status"] = status
		call.Metadata["provider_verified"] = factory.Metadata["provider_verified"] == true
		if !ambiguous && status != ProviderStatusLocalMissing && status != ProviderStatusLocalAmbiguous {
			relationships = append(relationships, frameworkRelationship(*call, *factory, RelationshipObjectCall, call.Name))
		}
		if status == ProviderStatusLocalMissing || status == ProviderStatusLocalAmbiguous || ambiguous {
			delete(call.Metadata, "operation")
		} else if status == ProviderStatusLocalVerified {
			operation, _, ok := defaultRegistry().operation(factory.Framework, call.Name, nil)
			if ok {
				call.Metadata["operation"] = operation
			} else {
				delete(call.Metadata, "operation")
			}
		}
	}

	// Recreate operations from their unique backing call IDs. This removes
	// stale preliminary operations when a provider becomes missing/ambiguous.
	for i := range entities {
		call := &entities[i]
		if call.Kind != KindAPICall {
			continue
		}
		operation, _ := call.Metadata["operation"].(string)
		if operation == "" {
			continue
		}
		status, _ := call.Metadata["provider_status"].(string)
		if status == ProviderStatusLocalMissing || status == ProviderStatusLocalAmbiguous {
			delete(call.Metadata, "operation")
			continue
		}
		metadata := cloneMetadata(call.Metadata)
		metadata["api"] = call.Metadata["api"]
		metadata["backing_call_id"] = call.ID
		op := semantic.Entity{Analyzer: semantic.AnalyzerFramework, Repo: call.Repo, File: call.File, SymbolID: call.SymbolID, Kind: KindOperation, Name: operation, Framework: call.Framework, Side: call.Side, Line: call.Line, EndLine: call.EndLine, Metadata: metadata}
		op.ID = semantic.StableID("framework_operation", call.ID, operation)
		entities = append(entities, op)
		if op.ID != "" && call.ID != "" {
			relationships = append(relationships, frameworkRelationship(op, *call, RelationshipDerivedFrom, operation))
		}
	}

	entities = addIndexedCandidates(repo, entities, providersByPath, providersByResource, providerPathsByResource)
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

func rebuiltOperation(call *semantic.Entity, provider *semantic.Entity, status string, preliminary map[string]semantic.Entity) (string, map[string]any) {
	operation, _ := call.Metadata["operation"].(string)
	if operation == "" {
		if old, ok := preliminary[call.ID]; ok {
			operation = old.Name
		} else if old, ok := preliminary[call.File+"\x00"+strconv.Itoa(call.Line)+"\x00"+call.Name]; ok {
			operation = old.Name
		}
	}
	if provider != nil {
		// The local adapter must agree with the locally classified provider;
		// a preliminary name-only operation is not authoritative.
		known, _, ok := (registry{adapters: defaultRegistry().adapters}).operation(provider.Framework, call.Name, nil)
		if !ok {
			operation = ""
		} else {
			operation = known
		}
	} else if status == ProviderStatusLocalMissing || status == ProviderStatusLocalAmbiguous {
		operation = ""
	}
	if operation == "" {
		delete(call.Metadata, "operation")
		return "", nil
	}
	call.Metadata["operation"] = operation
	if status != "" {
		call.Metadata["provider_status"] = status
	}
	return operation, call.Metadata
}

func persistedFactoryIndexed(byID map[string]*semantic.Entity, byFileAPI map[string][]*semantic.Entity, object semantic.Entity) *semantic.Entity {
	factoryID, _ := object.Metadata["origin_factory_id"].(string)
	if factoryID != "" {
		return byID[factoryID]
	}
	factoryAPI, _ := object.Metadata["origin_factory_api"].(string)
	candidates := byFileAPI[object.File+"\x00"+factoryAPI]
	var result *semantic.Entity
	for _, candidate := range candidates {
		if candidate.Line > object.Line {
			continue
		}
		mechanism, _ := candidate.Metadata["mechanism"].(string)
		if mechanism != "export" {
			continue
		}
		if result != nil {
			return nil
		}
		result = candidate
	}
	return result
}

func addIndexedCandidates(repo string, entities []semantic.Entity, providersByPath map[string][]semantic.Entity, providersByResource map[string][]semantic.Entity, providerPathsByResource map[string]map[string]bool) []semantic.Entity {
	consumerCounts := map[string]map[string]bool{}
	for _, entity := range entities {
		if entity.Kind != KindAPICall || entity.Metadata["provider_verified"] != true {
			continue
		}
		path, _ := entity.Metadata["provider_resource_path"].(string)
		target, _ := entity.Metadata["target_resource"].(string)
		if consumerCounts[path] == nil {
			consumerCounts[path] = map[string]bool{}
		}
		if source, _ := entity.Metadata["source_resource_path"].(string); source != "" {
			consumerCounts[path][source+"\x00"+target] = true
		}
	}
	for resource, paths := range providerPathsByResource {
		if len(paths) != 1 {
			continue
		}
		for path := range paths {
			providers := providersByPath[path]
			apis := map[string]bool{}
			frameworkName := FrameworkCustom
			for _, provider := range providers {
				apis[provider.Name] = true
				if provider.Framework != "" && provider.Framework != FrameworkCustom {
					frameworkName = provider.Framework
				}
			}
			if len(apis) < 3 || frameworkName != FrameworkCustom || len(providersByResource[resource]) == 0 {
				continue
			}
			first := providers[0]
			candidate := semantic.Entity{Analyzer: semantic.AnalyzerFramework, Repo: repo, File: first.File, Kind: KindCandidate, Name: resource, Framework: FrameworkCustom, Side: "shared", Line: first.Line, Metadata: map[string]any{"source_resource": resource, "source_resource_path": path, "provider_resource": resource, "provider_resource_path": path, "api_count": len(apis), "consumer_count": len(consumerCounts[path]), "classification": "custom", "evidence": "deterministic"}}
			candidate.ID = semantic.StableID("framework_candidate", repo, path, resource)
			entities = append(entities, candidate)
		}
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
