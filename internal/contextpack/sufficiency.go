package contextpack

import (
	"github.com/Athernaa/code-scale-mcpv2/internal/planner"
	"github.com/Athernaa/code-scale-mcpv2/internal/sufficiency"
)

func sufficiencyInput(plan planner.Plan, pkg Package, request Request, stage string) sufficiency.Input {
	sections := make([]sufficiency.Section, 0, len(pkg.Sections))
	for _, section := range pkg.Sections {
		sections = append(sections, sufficiency.Section{
			CandidateID: section.CandidateID, SymbolID: section.SymbolID, File: section.File,
			Name: section.Name, Kind: section.Kind, Tier: section.Tier, Stage: section.Stage, ContentKind: section.ContentKind,
			ReasonCodes: append([]string(nil), section.ReasonCodes...), Resource: section.Resource,
			TargetResource: section.TargetResource, Framework: section.Framework,
			Authority: section.Authority, Authorities: append([]string(nil), section.Authorities...),
			Partial: section.Partial, OutlineTruncated: section.OutlineTruncated,
			SourceAvailable: section.Source != "",
		})
	}
	return sufficiency.Input{
		Plan: plan, Sections: sections,
		Omitted: sufficiency.Omitted{TokenBudget: pkg.Omitted.TokenBudget, SourceUnavailable: pkg.Omitted.SourceUnavailable, SourceReadLimit: pkg.Omitted.SourceReadLimit},
		Stage:   stage, FocusSymbolID: request.FocusSymbolID, FocusResource: request.FocusResource,
		IncludeImpact: request.IncludeImpact,
	}
}

func (a *Assembler) evaluateSufficiency(plan planner.Plan, pkg Package, request Request, stage string) sufficiency.Decision {
	evaluator := a.Evaluator
	if evaluator == nil {
		evaluator = sufficiency.New()
	}
	return evaluator.Evaluate(sufficiencyInput(plan, pkg, request, stage))
}
