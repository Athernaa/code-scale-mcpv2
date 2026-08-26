package benchmark

import "time"

type Mode string

const (
	ModeManual            Mode = "manual"
	ModePanoramic         Mode = "panoramic"
	ModeScopedPanoramic   Mode = "scoped_panoramic"
	ModePrimitive         Mode = "primitive"
	ModePhase7            Mode = "phase7"
	ModePhase7NoEarlyStop Mode = "phase7_no_early_stop"
)

var AllModes = []Mode{ModeManual, ModePanoramic, ModeScopedPanoramic, ModePrimitive, ModePhase7, ModePhase7NoEarlyStop}

const (
	AcceptanceSupported   = "supported_deterministic"
	AcceptanceDiagnostic  = "diagnostic_open_ended"
	AcceptanceAdversarial = "adversarial_safety"
)

type Corpus struct {
	SchemaVersion    int              `json:"schema_version"`
	CorpusVersion    string           `json:"corpus_version"`
	FixtureRevision  string           `json:"fixture_revision"`
	DefaultTokenizer string           `json:"default_tokenizer"`
	Repositories     []RepositorySpec `json:"repos"`
	Tasks            []Task           `json:"tasks"`
}

type RepositorySpec struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Kind string `json:"kind"`
	Tier string `json:"tier,omitempty"`
}

type Task struct {
	ID                string            `json:"id"`
	Repo              string            `json:"repo"`
	Category          string            `json:"category"`
	Text              string            `json:"text"`
	FocusSymbolID     string            `json:"focus_symbol_id,omitempty"`
	FocusFile         string            `json:"focus_file,omitempty"`
	FocusResource     string            `json:"focus_resource,omitempty"`
	IncludeImpact     bool              `json:"include_impact,omitempty"`
	Required          []GroundTruthItem `json:"required"`
	RelevantFiles     []string          `json:"relevant_files"`
	AnchorNames       []string          `json:"anchor_names"`
	AcceptanceClass   string            `json:"acceptance_class"`
	ExclusionReason   string            `json:"exclusion_reason,omitempty"`
	ForbiddenMarkers  []string          `json:"forbidden_markers,omitempty"`
	ForbiddenVerified string            `json:"forbidden_verified_provider,omitempty"`
	FixtureTier       string            `json:"fixture_tier,omitempty"`
}

type GroundTruthItem struct {
	Kind         string `json:"kind"`
	Name         string `json:"name,omitempty"`
	File         string `json:"file,omitempty"`
	From         string `json:"from,omitempty"`
	To           string `json:"to,omitempty"`
	FromFile     string `json:"from_file,omitempty"`
	ToFile       string `json:"to_file,omitempty"`
	Relationship string `json:"relationship,omitempty"`
	Authority    string `json:"authority,omitempty"`
}

type Config struct {
	CorpusPath   string
	OutputPath   string
	MarkdownPath string
	Mode         string
	TaskID       string
	Category     string
	Tokenizer    string
	Budgets      []int
	Repeat       int
}

type Report struct {
	ReportVersion       string                      `json:"report_version"`
	GeneratedAt         time.Time                   `json:"generated_at"`
	CodeScaleCommit     string                      `json:"code_scale_commit"`
	TreeSHA             string                      `json:"tree_sha"`
	DirtyWorktree       bool                        `json:"dirty_worktree"`
	GoVersion           string                      `json:"go_version"`
	CorpusVersion       string                      `json:"corpus_version"`
	FixtureRevision     string                      `json:"fixture_revision"`
	Tokenizer           string                      `json:"tokenizer"`
	Modes               []Mode                      `json:"modes"`
	Budgets             []int                       `json:"budgets"`
	Repeat              int                         `json:"repeat"`
	TasksRun            int                         `json:"tasks_run"`
	Results             []TaskResult                `json:"results"`
	Aggregate           Aggregate                   `json:"aggregate"`
	ByCategory          map[string]Summary          `json:"by_category"`
	ByMode              map[string]Summary          `json:"by_mode"`
	ByTier              map[string]Summary          `json:"by_tier"`
	Acceptance          AcceptanceSummary           `json:"acceptance"`
	EarlyStopByCategory map[string]EarlyStopSummary `json:"early_stop_by_category"`
	Validation          Validation                  `json:"validation"`
	BenchmarkNotes      []string                    `json:"benchmark_notes"`
}

type TaskResult struct {
	TaskID                      string                 `json:"task_id"`
	Category                    string                 `json:"category"`
	Repo                        string                 `json:"repo"`
	Mode                        Mode                   `json:"mode"`
	Budget                      int                    `json:"budget"`
	Repeat                      int                    `json:"repeat"`
	AcceptanceClass             string                 `json:"acceptance_class"`
	ExclusionReason             string                 `json:"exclusion_reason,omitempty"`
	FixtureTier                 string                 `json:"fixture_tier"`
	ContextTokens               int                    `json:"context_tokens"`
	ReportedUsedTokens          int                    `json:"reported_used_tokens"`
	MetadataTokens              int                    `json:"metadata_tokens"`
	SourceTokens                int                    `json:"source_tokens"`
	BaselineContextTokens       int                    `json:"baseline_context_tokens"`
	ScopedBaselineContextTokens int                    `json:"scoped_baseline_context_tokens"`
	TokenSaving                 float64                `json:"token_saving"`
	ScopedTokenSaving           float64                `json:"scoped_token_saving"`
	CandidateCount              int                    `json:"candidate_count"`
	RequiredTotal               int                    `json:"required_total"`
	RequiredFound               int                    `json:"required_found"`
	RequiredMissing             []string               `json:"required_missing,omitempty"`
	DependencyRecall            float64                `json:"required_dependency_recall"`
	SymbolRecall                float64                `json:"symbol_recall"`
	FileRecall                  float64                `json:"file_recall"`
	RelationshipRecall          float64                `json:"relationship_recall"`
	ProviderRecall              float64                `json:"provider_recall"`
	Precision                   *float64               `json:"precision"`
	EmptyRetrieval              bool                   `json:"empty_retrieval"`
	Top5Recall                  float64                `json:"top5_recall"`
	Top10Recall                 float64                `json:"top10_recall"`
	SufficiencyStatus           string                 `json:"sufficiency_status,omitempty"`
	SufficiencyStage            string                 `json:"sufficiency_stage,omitempty"`
	SufficiencyReasons          []string               `json:"sufficiency_reasons,omitempty"`
	FalseSufficiency            bool                   `json:"false_sufficiency"`
	FalseInsufficiency          bool                   `json:"false_insufficiency"`
	ProviderFabrication         bool                   `json:"provider_fabrication"`
	CrossRepoLeak               bool                   `json:"cross_repo_leak"`
	Nondeterministic            bool                   `json:"nondeterministic"`
	DeterminismFingerprint      string                 `json:"determinism_fingerprint"`
	StopStage                   string                 `json:"stop_stage,omitempty"`
	RetrievalRounds             int                    `json:"retrieval_rounds"`
	SourceReads                 int                    `json:"source_reads"`
	SourceBytes                 int64                  `json:"source_bytes"`
	RetrievalCalls              int                    `json:"retrieval_calls"`
	PlannerMilliseconds         float64                `json:"planner_ms"`
	AssemblyMilliseconds        float64                `json:"assembly_ms"`
	TotalMilliseconds           float64                `json:"total_ms"`
	RuntimeError                string                 `json:"runtime_error,omitempty"`
	Retrieved                   []RetrievedSummary     `json:"retrieved,omitempty"`
	Ranked                      []RetrievedSummary     `json:"ranked,omitempty"`
	RelationshipCount           int                    `json:"relationship_count"`
	Relationships               []RelationshipEvidence `json:"relationships,omitempty"`
}

type RetrievedSummary struct {
	Key       string `json:"key"`
	Name      string `json:"name,omitempty"`
	File      string `json:"file,omitempty"`
	SymbolID  string `json:"symbol_id,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Authority string `json:"authority,omitempty"`
}

type RelationshipEvidence struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	FromEntityID string `json:"from_entity_id"`
	ToEntityID   string `json:"to_entity_id"`
	FromName     string `json:"from_name"`
	ToName       string `json:"to_name"`
	FromSymbolID string `json:"from_symbol_id,omitempty"`
	ToSymbolID   string `json:"to_symbol_id,omitempty"`
	FromKind     string `json:"from_kind,omitempty"`
	ToKind       string `json:"to_kind,omitempty"`
	FromFile     string `json:"from_file"`
	ToFile       string `json:"to_file"`
	Dynamic      bool   `json:"dynamic"`
}

type AcceptanceSummary struct {
	SupportedTotal  int               `json:"supported_total"`
	SupportedPassed int               `json:"supported_passed"`
	SupportedFailed int               `json:"supported_failed"`
	ExcludedTotal   int               `json:"excluded_total"`
	ExcludedByClass map[string]int    `json:"excluded_by_class"`
	ExcludedTasks   map[string]string `json:"excluded_tasks"`
}

type Aggregate struct {
	Results                          int     `json:"results"`
	Tasks                            int     `json:"tasks"`
	PassedRetrieval                  int     `json:"passed_retrieval"`
	MedianRecall                     float64 `json:"median_recall"`
	MeanRecall                       float64 `json:"mean_recall"`
	MedianTop5Recall                 float64 `json:"median_top5_recall"`
	MedianTop10Recall                float64 `json:"median_top10_recall"`
	MedianPrecision                  float64 `json:"median_precision"`
	EmptyRetrievalCount              int     `json:"empty_retrieval_count"`
	MedianContextTokens              float64 `json:"median_context_tokens"`
	MedianTokenSaving                float64 `json:"median_token_saving"`
	MedianScopedTokenSaving          float64 `json:"median_scoped_token_saving"`
	SupportedMedianTokenSaving       float64 `json:"supported_median_token_saving"`
	SupportedMedianScopedTokenSaving float64 `json:"supported_median_scoped_token_saving"`
	MeanTokenSaving                  float64 `json:"mean_token_saving"`
	P25TokenSaving                   float64 `json:"p25_token_saving"`
	P75TokenSaving                   float64 `json:"p75_token_saving"`
	P90TokenSaving                   float64 `json:"p90_token_saving"`
	Sufficient                       int     `json:"sufficient"`
	Blocked                          int     `json:"blocked"`
	Indeterminate                    int     `json:"indeterminate"`
	FalseSufficiency                 int     `json:"false_sufficiency"`
	FalseSufficiencyAllBudgets       int     `json:"false_sufficiency_all_budgets"`
	FalseSufficiencyMaxBudget        int     `json:"false_sufficiency_max_budget"`
	FalseSufficiencySupported        int     `json:"false_sufficiency_supported"`
	FalseSufficiencyDiagnostic       int     `json:"false_sufficiency_diagnostic"`
	FalseSufficiencyAdversarial      int     `json:"false_sufficiency_adversarial"`
	FalseInsufficiency               int     `json:"false_insufficiency"`
	ProviderFabrication              int     `json:"provider_fabrication"`
	CrossRepoLeaks                   int     `json:"cross_repo_leaks"`
	Nondeterministic                 int     `json:"nondeterministic"`
	RuntimeErrors                    int     `json:"runtime_errors"`
	MedianSourceReads                float64 `json:"median_source_reads"`
	MedianRetrievalRounds            float64 `json:"median_retrieval_rounds"`
	MedianLatencyMilliseconds        float64 `json:"median_latency_ms"`
	P95LatencyMilliseconds           float64 `json:"p95_latency_ms"`
	SerializedBudgetViolations       int     `json:"serialized_budget_violations"`
	EarlyStopTokensAvoided           int     `json:"early_stop_tokens_avoided"`
	EarlyStopSourceReadsAvoided      int     `json:"early_stop_source_reads_avoided"`
	EarlyStopRoundsAvoided           int     `json:"early_stop_rounds_avoided"`
}

type Summary struct {
	Results            int     `json:"results"`
	Tasks              int     `json:"tasks"`
	MedianRecall       float64 `json:"median_recall"`
	MedianSaving       float64 `json:"median_saving"`
	MedianScopedSaving float64 `json:"median_scoped_saving"`
	FalseSufficiency   int     `json:"false_sufficiency"`
	ProviderErrors     int     `json:"provider_errors"`
}

type EarlyStopSummary struct {
	Tasks              int `json:"tasks"`
	TokensAvoided      int `json:"tokens_avoided"`
	SourceReadsAvoided int `json:"source_reads_avoided"`
	RoundsAvoided      int `json:"rounds_avoided"`
}

type Validation struct {
	DeterminismFailures        int  `json:"determinism_failures"`
	BudgetMonotonicityFailures int  `json:"budget_monotonicity_failures"`
	IncrementalFullMismatches  int  `json:"incremental_full_mismatches"`
	ProviderFabricationZero    bool `json:"provider_fabrication_zero"`
	CrossRepoLeakageZero       bool `json:"cross_repo_leakage_zero"`
	SerializedBudgetZero       bool `json:"serialized_budget_zero"`
	SupportedRecallAtLeast95   bool `json:"supported_recall_at_least_95"`
	FalseSufficiencyBelow5     bool `json:"false_sufficiency_below_5"`
	FalseSufficiencyZero       bool `json:"false_sufficiency_zero"`
	RuntimeErrorsZero          bool `json:"runtime_errors_zero"`
	TokenReductionAtLeast50    bool `json:"token_reduction_at_least_50"`
}
