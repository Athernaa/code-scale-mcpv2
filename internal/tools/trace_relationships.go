package tools

import (
	"context"

	"github.com/Athernaa/code-scale-mcpv2/internal/semantic"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type TraceRelationshipsArgs struct {
	Repo              string   `json:"repo" jsonschema:"Repository name"`
	EntityID          string   `json:"entity_id,omitempty" jsonschema:"Semantic entity ID; provide this or symbol_id, not both"`
	SymbolID          string   `json:"symbol_id,omitempty" jsonschema:"Parser symbol ID; resolves a generic_graph node"`
	Analyzer          string   `json:"analyzer,omitempty" jsonschema:"Analyzer owning the entity, defaults to fivem for entity_id and generic_graph for symbol_id"`
	Direction         string   `json:"direction,omitempty" jsonschema:"Traversal direction: incoming, outgoing, or both (default both)"`
	RelationshipKinds []string `json:"relationship_kinds,omitempty" jsonschema:"Optional relationship filters, such as calls, references, imports"`
	Depth             int      `json:"depth,omitempty" jsonschema:"Maximum traversal depth (default 2, max 3)"`
	MaxResults        int      `json:"max_results,omitempty" jsonschema:"Maximum graph edges (default 50, max 200)"`
}

type semanticEndpoint struct {
	ID                 string `json:"id"`
	Kind               string `json:"kind"`
	Name               string `json:"name"`
	Operation          string `json:"operation,omitempty"`
	Framework          string `json:"framework,omitempty"`
	Resource           string `json:"resource,omitempty"`
	SourceResource     string `json:"source_resource,omitempty"`
	SourceResourcePath string `json:"source_resource_path,omitempty"`
	ResourceID         string `json:"resource_id,omitempty"`
	TargetResource     string `json:"target_resource,omitempty"`
	ProviderStatus     string `json:"provider_status,omitempty"`
	ProviderVerified   *bool  `json:"provider_verified,omitempty"`
	Side               string `json:"side,omitempty"`
	File               string `json:"file"`
	Line               int    `json:"line"`
	SymbolID           string `json:"symbol_id,omitempty"`
	Dynamic            bool   `json:"dynamic,omitempty"`
}

type relationshipTraceResult struct {
	RelationshipID string            `json:"relationship_id"`
	Kind           string            `json:"kind"`
	Name           string            `json:"name,omitempty"`
	Dynamic        bool              `json:"dynamic,omitempty"`
	Confidence     float64           `json:"confidence,omitempty"`
	Depth          int               `json:"depth"`
	From           semanticEndpoint  `json:"from"`
	To             *semanticEndpoint `json:"to,omitempty"`
}

func TraceRelationshipsHandler(deps *Deps) func(context.Context, *mcp.CallToolRequest, TraceRelationshipsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args TraceRelationshipsArgs) (*mcp.CallToolResult, any, error) {
		t := newTimer()
		if (args.EntityID == "") == (args.SymbolID == "") {
			r, _ := errorResult("provide exactly one of entity_id or symbol_id")
			return r, nil, nil
		}
		analyzer := args.Analyzer
		repoID, err := deps.Store.GetRepoID(args.Repo)
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}
		if analyzer == "" && args.EntityID != "" {
			if entity, lookupErr := deps.Store.GetSemanticEntityByID(repoID, args.EntityID); lookupErr == nil {
				analyzer = entity.Analyzer
			}
		}
		if analyzer == "" {
			analyzer = semantic.AnalyzerFiveM
			if args.SymbolID != "" {
				analyzer = semantic.AnalyzerGenericGraph
			} else if _, workspaceErr := deps.Store.GetWorkspace(repoID); workspaceErr == nil {
				analyzer = semantic.AnalyzerFiveMWorkspace
			}
		}
		entityID := args.EntityID
		if args.SymbolID != "" {
			entity, lookupErr := deps.Store.GetSemanticEntityBySymbolID(repoID, analyzer, args.SymbolID)
			if lookupErr != nil {
				r, _ := errorResult(lookupErr.Error())
				return r, nil, nil
			}
			entityID = entity.ID
		}
		edges, truncated, err := deps.Store.TraceSemanticWithOptions(repoID, entityID, analyzer, args.Direction, args.RelationshipKinds, args.Depth, args.MaxResults)
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}
		results := make([]relationshipTraceResult, 0, len(edges))
		for _, edge := range edges {
			item := relationshipTraceResult{
				RelationshipID: edge.ID,
				Kind:           edge.Kind,
				Name:           edge.Name,
				Dynamic:        edge.Dynamic,
				Confidence:     edge.Confidence,
				Depth:          edge.Depth,
				From:           endpoint(edge.From),
			}
			if edge.To != nil {
				to := endpoint(*edge.To)
				item.To = &to
			}
			results = append(results, item)
		}
		result := map[string]any{
			"repo":      args.Repo,
			"entity_id": entityID,
			"symbol_id": args.SymbolID,
			"analyzer":  analyzer,
			"direction": normalizedDirection(args.Direction),
			"results":   results,
			"truncated": truncated,
			"_meta":     deps.meta(t, args.Repo, truncated, 0, 0),
		}
		r, _ := toTextResult(result)
		return r, nil, nil
	}
}

func endpoint(entity semantic.Entity) semanticEndpoint {
	operation, resource, sourceResource, sourceResourcePath, resourceID, targetResource := semanticMetadata(entity)
	providerStatus, providerVerified := providerAuthority(entity)
	return semanticEndpoint{ID: entity.ID, Kind: entity.Kind, Name: entity.Name, Operation: operation, Framework: entity.Framework, Resource: resource, SourceResource: sourceResource, SourceResourcePath: sourceResourcePath, ResourceID: resourceID, TargetResource: targetResource, ProviderStatus: providerStatus, ProviderVerified: providerVerified, Side: entity.Side, File: entity.File, Line: entity.Line, SymbolID: entity.SymbolID, Dynamic: entity.Dynamic}
}

func normalizedDirection(direction string) string {
	if direction == "incoming" || direction == "outgoing" || direction == "both" {
		return direction
	}
	return "both"
}
