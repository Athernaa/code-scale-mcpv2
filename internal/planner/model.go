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
}

type Seed struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	SourceID  string `json:"source_id"`
	SymbolID  string `json:"symbol_id,omitempty"`
	File      string `json:"file,omitempty"`
	Name      string `json:"name,omitempty"`
	Match     string `json:"match"`
	Authority string `json:"authority,omitempty"`
	Ambiguous bool   `json:"ambiguous,omitempty"`
}

type Candidate struct {
	ID             string   `json:"id"`
	SymbolID       string   `json:"symbol_id,omitempty"`
	File           string   `json:"file"`
	Line           int      `json:"line"`
	EndLine        int      `json:"end_line,omitempty"`
	Name           string   `json:"name"`
	Kind           string   `json:"kind"`
	Resource       string   `json:"resource,omitempty"`
	ResourcePath   string   `json:"resource_path,omitempty"`
	TargetResource string   `json:"target_resource,omitempty"`
	Framework      string   `json:"framework,omitempty"`
	Side           string   `json:"side,omitempty"`
	Analyzers      []string `json:"analyzers,omitempty"`
	Score          int      `json:"score"`
	Tier           string   `json:"tier"`
	ReasonCodes    []string `json:"reason_codes"`
	Authority      string   `json:"authority,omitempty"`
	Distance       int      `json:"distance"`
	EstimatedScope int      `json:"estimated_scope,omitempty"`
}

type Ambiguity struct {
	Kind           string `json:"kind"`
	Query          string `json:"query"`
	CandidateCount int    `json:"candidate_count"`
}

type DebugDetails struct {
	EvidenceCount        int `json:"evidence_count"`
	CandidatesConsidered int `json:"candidates_considered"`
	SeedsConsidered      int `json:"seeds_considered"`
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
}

type Evidence struct {
	Kind         string
	SourceID     string
	Relationship string
	Depth        int
	Strength     int
	Authority    string
	NoteCode     string
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
	key            string
	repo           string
	entity         *semantic.Entity
	symbol         *parser.Symbol
	reasons        map[string]Evidence
	authorities    map[string]bool
	analyzers      map[string]bool
	distance       int
	resource       string
	resourcePath   string
	targetResource string
	framework      string
	side           string
	file           string
	line           int
	endLine        int
	name           string
	kind           string
}
