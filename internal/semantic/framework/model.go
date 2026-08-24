package framework

import "github.com/Athernaa/code-scale-mcpv2/internal/semantic"

const (
	KindAPIProvider = "framework_api_provider"
	KindAPICall     = "framework_api_call"
	KindOperation   = "framework_operation"
	KindCandidate   = "framework_candidate"
	KindStatus      = "framework_analysis_status"

	RelationshipFrameworkCalls = "framework_calls"
	RelationshipObjectCall     = "framework_object_call"
	RelationshipProvidedBy     = "provided_by"
	RelationshipDerivedFrom    = "derived_from"

	FrameworkCustom  = "custom"
	FrameworkUnknown = "unknown"
)

// FailureStatus is a compact persisted distinction between a resource with
// no discovered framework API and one whose framework analysis failed.
func FailureStatus(repo, resource, resourcePath string) semantic.Entity {
	return semantic.Entity{
		ID: semantic.StableID("framework_status", repo, resourcePath), Analyzer: semantic.AnalyzerFramework,
		Repo: repo, File: resourcePath, Kind: KindStatus, Name: resource, Framework: FrameworkUnknown,
		Metadata: map[string]any{"source_resource": resource, "source_resource_path": resourcePath, "status": "failed", "reason": "framework_analysis"},
	}
}

// Adapter is deliberately small: adapters enrich deterministic local facts;
// they do not own discovery or persistence.
type Adapter interface {
	Name() string
	ProviderFramework(resource string, apis map[string]bool, evidence Evidence) (string, bool)
	CallOperation(framework, api string, args []literalValue) (string, map[string]any, bool)
}

type Evidence struct {
	Dependencies []string
	ExternalRefs []string
}

type literalValue struct {
	Kind  string
	Value string
}

type origin struct {
	Resource   string
	Framework  string
	FactoryAPI string
	FactoryID  string
	Valid      bool
	Ambiguous  bool
}

func sourceMetadata(entity semantic.Entity, fallback string) (name, path, id string) {
	if entity.Metadata != nil {
		name, _ = entity.Metadata["source_resource"].(string)
		path, _ = entity.Metadata["source_resource_path"].(string)
		id, _ = entity.Metadata["source_resource_id"].(string)
		if name == "" {
			name, _ = entity.Metadata["resource"].(string)
		}
		if path == "" {
			path, _ = entity.Metadata["resource_path"].(string)
		}
	}
	if name == "" {
		name = fallback
	}
	if id == "" && path != "" {
		id = semantic.StableID("workspace_resource", entity.Repo, path)
	}
	return name, path, id
}
