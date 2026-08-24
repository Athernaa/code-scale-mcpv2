package tools

import (
	"context"

	"github.com/Athernaa/code-scale-mcpv2/internal/planner"
	"github.com/Athernaa/code-scale-mcpv2/internal/ratelimit"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type PlanContextArgs struct {
	Repo          string `json:"repo" jsonschema:"Indexed repository name"`
	Task          string `json:"task" jsonschema:"Natural-language task to plan against indexed facts"`
	MaxCandidates int    `json:"max_candidates,omitempty" jsonschema:"Maximum candidates, default 20, hard maximum 100"`
	FocusFile     string `json:"focus_file,omitempty" jsonschema:"Optional indexed repository-relative file scope"`
	FocusSymbolID string `json:"focus_symbol_id,omitempty" jsonschema:"Optional explicit parser symbol ID"`
	FocusResource string `json:"focus_resource,omitempty" jsonschema:"Optional FiveM resource name or path"`
	IncludeImpact bool   `json:"include_impact,omitempty" jsonschema:"Include bounded incoming impact evidence"`
	Debug         bool   `json:"debug,omitempty" jsonschema:"Include bounded planner diagnostics"`
}

func PlanContextHandler(deps *Deps) func(context.Context, *mcp.CallToolRequest, PlanContextArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args PlanContextArgs) (*mcp.CallToolResult, any, error) {
		t := newTimer()
		if deps == nil || deps.Store == nil {
			r, _ := errorResult("planner store is not configured")
			return r, nil, nil
		}
		if deps.Throttle != nil {
			action := deps.Throttle.Check("plan_context")
			if action == ratelimit.ActionBlocked {
				r, _ := errorResult("plan_context is temporarily rate limited")
				return r, nil, nil
			}
		}
		result, err := planner.New(deps.Store).Plan(ctx, planner.Request{
			Repo: args.Repo, Task: args.Task, MaxCandidates: args.MaxCandidates,
			FocusFile: args.FocusFile, FocusSymbolID: args.FocusSymbolID,
			FocusResource: args.FocusResource, IncludeImpact: args.IncludeImpact, Debug: args.Debug,
		})
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}
		payload := struct {
			planner.Plan
			Meta Meta `json:"_meta,omitempty"`
		}{Plan: result, Meta: deps.meta(t, args.Repo, result.Truncated, 0, 0)}
		r, _ := toTextResult(payload)
		return r, nil, nil
	}
}
