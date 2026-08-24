package contextpack

import (
	"context"

	"github.com/Athernaa/code-scale-mcpv2/internal/parser"
	"github.com/Athernaa/code-scale-mcpv2/internal/planner"
	"github.com/Athernaa/code-scale-mcpv2/internal/storage"
)

const (
	DefaultContextTokenBudget    = 8000
	MinContextTokenBudget        = 512
	HardMaxContextTokenBudget    = 64000
	DefaultMaxSourceReads        = 64
	DefaultMaxSourceBytes        = 4 << 20
	DefaultOutlineSymbolsPerFile = 128
	DefaultOutlineSymbolsTotal   = 512
)

type Request struct {
	Repo             string
	Task             string
	MaxContextTokens int
	Tokenizer        string
	MaxCandidates    int
	FocusFile        string
	FocusSymbolID    string
	FocusResource    string
	IncludeImpact    bool
	Debug            bool
}

type Budget struct {
	RequestedTokens int    `json:"requested_tokens"`
	UsableTokens    int    `json:"usable_tokens"`
	UsedTokens      int    `json:"used_tokens"`
	RemainingTokens int    `json:"remaining_tokens"`
	SourceTokens    int    `json:"source_tokens"`
	OverheadTokens  int    `json:"overhead_tokens"`
	Tokenizer       string `json:"tokenizer"`
	Exact           bool   `json:"exact"`
	Exhausted       bool   `json:"exhausted"`
}

type Section struct {
	CandidateID      string   `json:"candidate_id"`
	SymbolID         string   `json:"symbol_id,omitempty"`
	File             string   `json:"file,omitempty"`
	Line             int      `json:"line,omitempty"`
	EndLine          int      `json:"end_line,omitempty"`
	Name             string   `json:"name"`
	Kind             string   `json:"kind"`
	Tier             string   `json:"tier"`
	Stage            string   `json:"stage"`
	Score            int      `json:"score"`
	ReasonCodes      []string `json:"reason_codes"`
	Resource         string   `json:"resource,omitempty"`
	TargetResource   string   `json:"target_resource,omitempty"`
	Framework        string   `json:"framework,omitempty"`
	Side             string   `json:"side,omitempty"`
	Authority        string   `json:"authority,omitempty"`
	Resources        []string `json:"resources,omitempty"`
	TargetResources  []string `json:"target_resources,omitempty"`
	Frameworks       []string `json:"frameworks,omitempty"`
	Sides            []string `json:"sides,omitempty"`
	Authorities      []string `json:"authorities,omitempty"`
	Distance         int      `json:"distance"`
	ContentKind      string   `json:"content_kind"`
	Source           string   `json:"source,omitempty"`
	TokenCount       int      `json:"token_count"`
	OriginalTokens   int      `json:"original_token_count,omitempty"`
	Partial          bool     `json:"partial,omitempty"`
	OutlineTruncated bool     `json:"outline_truncated,omitempty"`
	originalExact    bool
}

type Round struct {
	Stage                string `json:"stage"`
	CandidatesConsidered int    `json:"candidates_considered"`
	Included             int    `json:"included"`
	TokensAdded          int    `json:"tokens_added"`
}

type Omitted struct {
	TokenBudget       int      `json:"token_budget,omitempty"`
	SourceUnavailable int      `json:"source_unavailable,omitempty"`
	SourceReadLimit   int      `json:"source_read_limit,omitempty"`
	LowerPriority     int      `json:"lower_priority,omitempty"`
	CandidateIDs      []string `json:"candidate_ids,omitempty"`
}

type Debug struct {
	SourceCandidatesConsidered int   `json:"source_candidates_considered"`
	SourceReads                int   `json:"source_reads"`
	SourceBytesRead            int64 `json:"source_bytes_read"`
	SectionsIncluded           int   `json:"sections_included"`
	SectionsPartial            int   `json:"sections_partial"`
}

type Package struct {
	Repo              string              `json:"repo"`
	TaskClass         string              `json:"task_class"`
	TaskConfidence    string              `json:"task_confidence"`
	IndexState        string              `json:"index_state"`
	IndexIncomplete   bool                `json:"index_incomplete"`
	Sections          []Section           `json:"sections,omitempty"`
	Ambiguities       []planner.Ambiguity `json:"ambiguities,omitempty"`
	Diagnostics       []string            `json:"diagnostics,omitempty"`
	DegradedResources []string            `json:"degraded_framework_resources,omitempty"`
	Rounds            []Round             `json:"rounds,omitempty"`
	Omitted           Omitted             `json:"omitted"`
	StopReason        string              `json:"stop_reason"`
	PlannerTruncated  bool                `json:"planner_truncated"`
	ContextTruncated  bool                `json:"context_truncated"`
	Truncated         bool                `json:"truncated"`
	Budget            Budget              `json:"budget"`
	Debug             *Debug              `json:"debug,omitempty"`
}

type PlanProvider interface {
	Plan(context.Context, planner.Request) (planner.Plan, error)
}

type SourceStore interface {
	GetRepoID(string) (int64, error)
	GetSymbolsByIDs(int64, []string) (map[string]parser.Symbol, error)
	GetSymbolsByFilesBounded(int64, []string, int, int) (storage.SymbolsByFiles, error)
	GetSymbolContent(int64, string) (string, error)
	GetFileContentBounded(int64, string, int64) ([]byte, bool, error)
}
