package framework

import (
	"testing"

	"github.com/Athernaa/code-scale-mcpv2/internal/semantic"
)

func TestCanonicalizeResultRemapsFrameworkAndFiveMReferences(t *testing.T) {
	provider := semantic.Entity{ID: "raw-provider", Kind: KindAPIProvider, Name: "GetCore", File: "provider.lua", Line: 1, Metadata: map[string]any{
		"provider_resource": "banana", "provider_resource_path": "resources/[custom]/banana", "derived_from_entity_id": "raw-export",
	}}
	factory := semantic.Entity{ID: "raw-factory", Kind: KindAPICall, Name: "GetContext", File: "client.lua", Line: 2, Metadata: map[string]any{
		"api": "GetContext", "mechanism": "export", "source_offset": 17, "origin_factory_id": "", "provider_entity_id": "raw-provider",
	}}
	object := semantic.Entity{ID: "raw-object", Kind: KindAPICall, Name: "SetFlag", File: "client.lua", Line: 3, Metadata: map[string]any{
		"api": "SetFlag", "mechanism": "object_method", "source_offset": 31, "origin_factory_id": "raw-factory",
	}}
	operation := semantic.Entity{ID: "raw-operation", Kind: KindOperation, Name: "framework_object_acquire", File: "client.lua", Line: 2, Metadata: map[string]any{
		"backing_call_id": "raw-factory",
	}}
	input := semantic.Result{Entities: []semantic.Entity{provider, factory, object, operation}, Relationships: []semantic.Relationship{{ID: "raw-rel", FromEntityID: "raw-operation", ToEntityID: "raw-factory", Kind: RelationshipDerivedFrom}}}
	sourceMap := map[string]string{"raw-export": "fivem-export-canonical"}
	local := CanonicalizeResult("repo", "resources/[custom]/banana", input, sourceMap)
	full := input
	full.Entities = append([]semantic.Entity(nil), input.Entities...)
	for i := range full.Entities {
		full.Entities[i].File = "resources/[custom]/banana/" + full.Entities[i].File
	}
	fullResult := CanonicalizeResult("repo", "resources/[custom]/banana", full, sourceMap)

	localByName := map[string]semantic.Entity{}
	fullByName := map[string]semantic.Entity{}
	for _, entity := range local.Entities {
		localByName[entity.Kind+"\x00"+entity.Name] = entity
	}
	for _, entity := range fullResult.Entities {
		fullByName[entity.Kind+"\x00"+entity.Name] = entity
	}
	for key, entity := range localByName {
		if fullByName[key].ID != entity.ID {
			t.Fatalf("resource-local and workspace paths produced different IDs for %s: %q != %q", key, entity.ID, fullByName[key].ID)
		}
	}

	factoryID := localByName[KindAPICall+"\x00GetContext"].ID
	objectEntity := localByName[KindAPICall+"\x00SetFlag"]
	if objectEntity.Metadata["origin_factory_id"] != factoryID {
		t.Fatalf("origin factory was not remapped: %#v", objectEntity.Metadata)
	}
	operationEntity := localByName[KindOperation+"\x00framework_object_acquire"]
	if operationEntity.Metadata["backing_call_id"] != factoryID {
		t.Fatalf("backing call was not remapped: %#v", operationEntity.Metadata)
	}
	if localByName[KindAPIProvider+"\x00GetCore"].Metadata["derived_from_entity_id"] != "fivem-export-canonical" {
		t.Fatalf("FiveM bridge was not remapped: %#v", localByName[KindAPIProvider+"\x00GetCore"].Metadata)
	}
}
