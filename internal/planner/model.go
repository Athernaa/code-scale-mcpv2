package planner

import (
	"context"

	"github.com/Athernaa/code-scale-mcpv2/internal/parser"
	"github.com/Athernaa/code-scale-mcpv2/internal/semantic"
	"github.com/Athernaa/code-scale-mcpv2/internal/storage"
)

const (
	DefaultMaxCandidates = 20
	HardMaxCandidates    = 100
	MaxTaskLength        = 8192

	DefaultMaxSeeds           = 32
	HardMaxSeeds              = 128
	DefaultMaxEvidence        = 256
	HardMaxEvidence           = 1024
	DefaultMaxGraphEdges      = 256
	HardMaxGraphEdges         = 1024
	DefaultMaxTraceQueries    = 32
	HardMaxTraceQueries       = 128
	DefaultMaxExactAnchors    = 128
	DefaultMaxSemanticRows    = 128
	DefaultMaxFallbackMatches = 64
	DefaultMaxFallbackPerTerm = 8
	DefaultMaxExactQueries    = 24
	DefaultMaxSemanticQueries = 24
	DefaultMaxFallbackQueries = 16
	MaxAmbiguities            = 32
	MaxUnresolvedHints        = 32
	MaxDiagnostics            = 32
	MaxDegradedResources      = 32
)

type Request struct {
	Repo          string
	Task          string
	MaxCandidates int
	FocusFile     string
	FocusSymbolID string
	FocusResource string
	IncludeImpact bool
	Debug         bool
}

type Plan struct {
	Repo              string        `json:"repo"`
	TaskClass         string        `json:"task_class"`
	TaskConfidence    string        `json:"task_confidence"`
	IndexState        string        `json:"index_state"`
	IndexIncomplete   bool          `json:"index_incomplete"`
	Seeds             []Seed        `json:"seeds,omitempty"`
	Primary           []Candidate   `json:"primary,omitempty"`
	Supporting        []Candidate   `json:"supporting,omitempty"`
	Peripheral        []Candidate   `json:"peripheral,omitempty"`
	Ambiguities       []Ambiguity   `json:"ambiguities,omitempty"`
	UnresolvedHints   []string      `json:"unresolved_hints,omitempty"`
	Diagnostics       []string      `json:"diagnostics,omitempty"`
	DegradedResources []string      `json:"degraded_framework_resources,omitempty"`
	Truncated         bool          `json:"truncated"`
	Debug             *DebugDetails `json:"debug,omitempty"`

	// Internal task-profile facts are consumed by bounded downstream policy
	// such as context sufficiency. They are not part of the planner DTO.
	TraceDirection       string   `json:"-"`
	AnchorStrength       string   `json:"-"`
	BroadIntent          bool     `json:"-"`
	RequestedTaskClass   string   `json:"-"`
	UnresolvedHighSignal []string `json:"-"`

	// rankingDebug is populated only while finalizing a plan. It is deliberately
	// not part of the normal DTO; the bounded public view is copied into Debug
	// only when requested.
	rankingDebug []RankingDebugCandidate
}

type Seed struct {
	ID          string   `json:"id"`
	Type        string   `json:"type"`
	SourceID    string   `json:"source_id"`
	SourceIDs   []string `json:"source_ids,omitempty"`
	SymbolID    string   `json:"symbol_id,omitempty"`
	File        string   `json:"file,omitempty"`
	Name        string   `json:"name,omitempty"`
	Match       string   `json:"match"`
	Authority   string   `json:"authority,omitempty"`
	Authorities []string `json:"authorities,omitempty"`
	Ambiguous   bool     `json:"ambiguous,omitempty"`
}

type Candidate struct {
	ID              string   `json:"id"`
	SymbolID        string   `json:"symbol_id,omitempty"`
	File            string   `json:"file"`
	Line            int      `json:"line"`
	EndLine         int      `json:"end_line,omitempty"`
	Name            string   `json:"name"`
	Kind            string   `json:"kind"`
	Resource        string   `json:"resource,omitempty"`
	ResourcePath    string   `json:"resource_path,omitempty"`
	TargetResource  string   `json:"target_resource,omitempty"`
	Framework       string   `json:"framework,omitempty"`
	Frameworks      []string `json:"frameworks,omitempty"`
	Side            string   `json:"side,omitempty"`
	Sides           []string `json:"sides,omitempty"`
	Resources       []string `json:"resources,omitempty"`
	ResourcePaths   []string `json:"resource_paths,omitempty"`
	TargetResources []string `json:"target_resources,omitempty"`
	Analyzers       []string `json:"analyzers,omitempty"`
	Score           int      `json:"score"`
	Tier            string   `json:"tier"`
	ReasonCodes     []string `json:"reason_codes"`
	Authority       string   `json:"authority,omitempty"`
	Authorities     []string `json:"authorities,omitempty"`
	Distance        int      `json:"distance"`
	EstimatedScope  int      `json:"estimated_scope,omitempty"`
}

type Ambiguity struct {
	Kind           string `json:"kind"`
	Query          string `json:"query"`
	CandidateCount int    `json:"candidate_count"`
	Truncated      bool   `json:"truncated,omitempty"`
}

type DebugDetails struct {
	EvidenceCount             int                     `json:"evidence_count"`
	CandidatesConsidered      int                     `json:"candidates_considered"`
	SeedsConsidered           int                     `json:"seeds_considered"`
	SeedBudgetUsed            int                     `json:"seed_budget_used"`
	EvidenceBudgetUsed        int                     `json:"evidence_budget_used"`
	GraphEdgesConsidered      int                     `json:"graph_edges_considered"`
	TraceQueries              int                     `json:"trace_queries"`
	ExactQueries              int                     `json:"exact_queries"`
	SemanticQueries           int                     `json:"semantic_queries"`
	FallbackQueries           int                     `json:"fallback_queries"`
	ExactMatchesConsidered    int                     `json:"exact_matches_considered"`
	SemanticMatchesConsidered int                     `json:"semantic_matches_considered"`
	FallbackMatchesConsidered int                     `json:"fallback_matches_considered"`
	FileLookups               int                     `json:"file_lookups"`
	RankingPolicy             string                  `json:"ranking_policy,omitempty"`
	RankedCandidates          []RankingDebugCandidate `json:"ranked_candidates,omitempty"`
}

// RankingDebugCandidate is intentionally compact and bounded. It exposes the
// same deterministic components used by the policy without source content or
// the complete internal evidence set.
type RankingDebugCandidate struct {
	ID                  string `json:"id"`
	Score               int    `json:"score"`
	Anchor              int    `json:"anchor"`
	TaskAlignment       int    `json:"task_alignment"`
	RelationshipQuality int    `json:"relationship_quality"`
	AuthorityQuality    int    `json:"authority_quality"`
	FocusRelevance      int    `json:"focus_relevance"`
	Corroboration       int    `json:"corroboration"`
	Locality            int    `json:"locality"`
	DistancePenalty     int    `json:"distance_penalty"`
	UncertaintyPenalty  int    `json:"uncertainty_penalty"`
	FallbackPenalty     int    `json:"fallback_penalty"`
	RedundancyPenalty   int    `json:"redundancy_penalty"`
	Tier                string `json:"tier"`
}

type TaskIntent struct {
	RawTask             string
	Terms               []string
	QuotedIdentifiers   []string
	SymbolHints         []string
	FileHints           []string
	ResourceHints       []string
	FrameworkOperations []string
	SemanticHints       []string
	TaskClass           string
	Confidence          string
	TraceDirection      string
	ExpansionDepth      int
	HighSignalHints     []string
	WeakTerms           []string
	BroadIntent         bool
}

type Evidence struct {
	Kind           string
	SourceID       string
	Relationship   string
	RelationshipID string
	Depth          int
	Strength       int
	Authority      string
	NoteCode       string
	Analyzer       string
	Role           string
	Framework      string
	Dynamic        bool
}

type Planner struct {
	Store *storage.IndexStore
}

func New(store *storage.IndexStore) *Planner { return &Planner{Store: store} }

// Plan is deliberately independent of MCP transport. It reads only indexed
// repository facts and returns bounded candidates; it never parses or writes
// source code.
func (p *Planner) Plan(ctx context.Context, request Request) (Plan, error) {
	return p.plan(ctx, request)
}

type candidateAccumulator struct {
	key             string
	repo            string
	entity          *semantic.Entity
	symbol          *parser.Symbol
	evidenceByID    map[string]Evidence
	reasonCodes     map[string]bool
	authorities     map[string]bool
	analyzers       map[string]bool
	frameworks      map[string]bool
	resources       map[string]bool
	resourcePaths   map[string]bool
	targetResources map[string]bool
	sides           map[string]bool
	distance        int
	resource        string
	resourcePath    string
	targetResource  string
	framework       string
	side            string
	file            string
	line            int
	endLine         int
	name            string
	kind            string
}

type plannerSeedEntity struct {
	entity semantic.Entity
	// alternates retain analyzer-specific entities for one source anchor. The
	// planner exposes one logical seed/candidate, while expansion can still
	// traverse the exact persisted graph endpoint for each analyzer.
	alternates []semantic.Entity
	anchor     string
	priority   int
	expand     bool
	ambiguous  bool
}

type plannerBudget struct {
	maxSeeds           int
	maxEvidence        int
	maxGraphEdges      int
	maxTraceQueries    int
	maxExactAnchors    int
	maxSemanticRows    int
	maxFallbackMatches int
	maxFallbackPerTerm int
	maxExactQueries    int
	maxSemanticQueries int
	maxFallbackQueries int

	seedsUsed       int
	evidenceUsed    int
	graphEdges      int
	traceQueries    int
	exactQueries    int
	semanticQueries int
	fallbackQueries int
	exactRows       int
	semanticRows    int
	fallbackMatches int
	fileLookups     int

	seedExhausted     bool
	evidenceExhausted bool
	graphExhausted    bool
	traceExhausted    bool
	exactExhausted    bool
	fallbackExhausted bool
}

func newPlannerBudget() *plannerBudget {
	return &plannerBudget{
		maxSeeds:           DefaultMaxSeeds,
		maxEvidence:        DefaultMaxEvidence,
		maxGraphEdges:      DefaultMaxGraphEdges,
		maxTraceQueries:    DefaultMaxTraceQueries,
		maxExactAnchors:    DefaultMaxExactAnchors,
		maxSemanticRows:    DefaultMaxSemanticRows,
		maxFallbackMatches: DefaultMaxFallbackMatches,
		maxFallbackPerTerm: DefaultMaxFallbackPerTerm,
		maxExactQueries:    DefaultMaxExactQueries,
		maxSemanticQueries: DefaultMaxSemanticQueries,
		maxFallbackQueries: DefaultMaxFallbackQueries,
	}
}
