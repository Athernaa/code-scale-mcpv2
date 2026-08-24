package tools

import (
	"context"

	"github.com/Athernaa/code-scale-mcpv2/internal/contextpack"
	"github.com/Athernaa/code-scale-mcpv2/internal/planner"
	"github.com/Athernaa/code-scale-mcpv2/internal/ratelimit"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type AssembleContextArgs struct {
	Repo             string `json:"repo" jsonschema:"Indexed repository name"`
	Task             string `json:"task" jsonschema:"Natural-language task"`
	MaxContextTokens int    `json:"max_context_tokens,omitempty" jsonschema:"Final serialized payload token budget, default 8000, range 512-64000"`
	Tokenizer        string `json:"tokenizer,omitempty" jsonschema:"Tokenizer encoding: o200k_base or cl100k_base"`
	MaxCandidates    int    `json:"max_candidates,omitempty" jsonschema:"Maximum planner candidates, default 20, hard maximum 100"`
	FocusFile        string `json:"focus_file,omitempty" jsonschema:"Optional indexed repository-relative file"`
	FocusSymbolID    string `json:"focus_symbol_id,omitempty" jsonschema:"Optional explicit parser symbol ID"`
	FocusResource    string `json:"focus_resource,omitempty" jsonschema:"Optional FiveM resource name or path"`
	IncludeImpact    bool   `json:"include_impact,omitempty" jsonschema:"Include bounded incoming impact evidence"`
	Debug            bool   `json:"debug,omitempty" jsonschema:"Include bounded source-read counters"`
}

func AssembleContextHandler(deps *Deps) func(context.Context, *mcp.CallToolRequest, AssembleContextArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args AssembleContextArgs) (*mcp.CallToolResult, any, error) {
		if deps == nil || deps.Store == nil {
			r, _ := errorResult("context assembler store is not configured")
			return r, nil, nil
		}
		if deps.Throttle != nil && deps.Throttle.Check("assemble_context") == ratelimit.ActionBlocked {
			r, _ := errorResult("assemble_context is temporarily rate limited")
			return r, nil, nil
		}
		assembler := contextpack.New(planner.New(deps.Store), deps.Store)
		result, err := assembler.Assemble(ctx, contextpack.Request{Repo: args.Repo, Task: args.Task, MaxContextTokens: args.MaxContextTokens, Tokenizer: args.Tokenizer, MaxCandidates: args.MaxCandidates, FocusFile: args.FocusFile, FocusSymbolID: args.FocusSymbolID, FocusResource: args.FocusResource, IncludeImpact: args.IncludeImpact, Debug: args.Debug})
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}
		r, err := toTextResult(result)
		return r, nil, err
	}
}
