package planner

import (
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic/fivem"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic/framework"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic/generic"
)

type semanticRole string

const (
	roleDeclaration  semanticRole = "declaration"
	roleFlowEndpoint semanticRole = "flow_endpoint"
	roleOperation    semanticRole = "operation"
	roleUsage        semanticRole = "usage"
	roleTopology     semanticRole = "topology"
	roleStatus       semanticRole = "status"
)

func semanticSeedRole(entity semantic.Entity) semanticRole {
	switch entity.Analyzer {
	case semantic.AnalyzerGenericGraph:
		switch entity.Kind {
		case generic.KindCodeSymbol:
			return roleDeclaration
		case generic.KindCallSite, generic.KindReferenceSite:
			return roleUsage
		case generic.KindImportSite, generic.KindCodeFile:
			return roleTopology
		}
	case semantic.AnalyzerFiveM:
		switch entity.Kind {
		case fivem.KindEventTrigger, fivem.KindEventHandler, fivem.KindEventRegistration,
			fivem.KindCallbackCall, fivem.KindCallbackRegistration,
			fivem.KindExportCall, fivem.KindExportDefinition:
			return roleFlowEndpoint
		}
	case semantic.AnalyzerFramework:
		switch entity.Kind {
		case framework.KindOperation:
			return roleOperation
		case framework.KindAPIProvider:
			return roleDeclaration
		case framework.KindAPICall:
			return roleUsage
		case framework.KindStatus:
			return roleStatus
		case framework.KindCandidate:
			return roleTopology
		}
	}
	return roleTopology
}

func contextSymbolID(entity semantic.Entity) string {
	if entity.SymbolID != "" {
		return entity.SymbolID
	}
	if entity.Analyzer == semantic.AnalyzerGenericGraph && entity.Metadata != nil {
		value, _ := entity.Metadata["source_symbol_id"].(string)
		return value
	}
	return ""
}

func semanticFamilyKey(entity semantic.Entity, hint string) string {
	role := semanticSeedRole(entity)
	if role != roleFlowEndpoint && role != roleOperation {
		return ""
	}
	return string(role) + ":" + entity.Analyzer + ":" + hint
}
