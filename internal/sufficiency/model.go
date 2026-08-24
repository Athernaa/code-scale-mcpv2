package sufficiency

import "github.com/Athernaa/code-scale-mcpv2/internal/planner"

const (
	StatusSufficient       = "sufficient"
	StatusNeedsMoreContext = "needs_more_context"
	StatusBlocked          = "blocked"
	StatusIndeterminate    = "indeterminate"
	MaxReasonCodes         = 12
	MaxMissing             = 8
)

type Coverage struct {
	AnchorsRequired          int `json:"anchors_required"`
	AnchorsSatisfied         int `json:"anchors_satisfied"`
	CriticalSupportRequired  int `json:"critical_support_required"`
	CriticalSupportSatisfied int `json:"critical_support_satisfied"`
	ProvidersRequired        int `json:"providers_required"`
	ProvidersSatisfied       int `json:"providers_satisfied"`
	FlowPeersRequired        int `json:"flow_peers_required"`
	FlowPeersSatisfied       int `json:"flow_peers_satisfied"`
	ImpactRequired           int `json:"impact_required"`
	ImpactSatisfied          int `json:"impact_satisfied"`
}

type Missing struct {
	Kind           string `json:"kind"`
	CandidateID    string `json:"candidate_id,omitempty"`
	Reason         string `json:"reason"`
	Resource       string `json:"resource,omitempty"`
	TargetResource string `json:"target_resource,omitempty"`
}

type Decision struct {
	Status              string    `json:"status"`
	CanContinue         bool      `json:"can_continue"`
	EvaluatedAfterStage string    `json:"evaluated_after_stage"`
	ReasonCodes         []string  `json:"reason_codes,omitempty"`
	Coverage            Coverage  `json:"coverage"`
	Missing             []Missing `json:"missing,omitempty"`
}

type Section struct {
	CandidateID      string
	SymbolID         string
	File             string
	Name             string
	Kind             string
	Tier             string
	Stage            string
	ContentKind      string
	ReasonCodes      []string
	Resource         string
	TargetResource   string
	Framework        string
	Authority        string
	Authorities      []string
	Partial          bool
	OutlineTruncated bool
	SourceAvailable  bool
}

type Omitted struct {
	TokenBudget       int
	SourceUnavailable int
	SourceReadLimit   int
}

type Input struct {
	Plan          planner.Plan
	Sections      []Section
	Omitted       Omitted
	Stage         string
	FocusSymbolID string
	FocusResource string
	IncludeImpact bool
}

type Evaluator interface {
	Evaluate(Input) Decision
}
