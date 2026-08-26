package benchmark

import "time"

type Mode string

const (
	ModeManual    Mode = "manual"
	ModePanoramic Mode = "panoramic"
	ModePrimitive Mode = "primitive"
	ModePhase7    Mode = "phase7"
)

var AllModes = []Mode{ModeManual, ModePanoramic, ModePrimitive, ModePhase7}

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
}

type Task struct {
	ID                    string            `json:"id"`
	Repo                  string            `json:"repo"`
	Category              string            `json:"category"`
	Text                  string            `json:"text"`
	FocusSymbolID         string            `json:"focus_symbol_id,omitempty"`
	FocusFile             string            `json:"focus_file,omitempty"`
	FocusResource         string            `json:"focus_resource,omitempty"`
	IncludeImpact         bool              `json:"include_impact,omitempty"`
	Required              []GroundTruthItem `json:"required"`
	RelevantFiles         []string          `json:"relevant_files"`
	AnchorNames           []string          `json:"anchor_names"`
	EligibleForAcceptance bool              `json:"eligible_for_acceptance,omitempty"`
	ForbiddenMarkers      []string          `json:"forbidden_markers,omitempty"`
	ForbiddenVerified     string            `json:"forbidden_verified_provider,omitempty"`
}

type GroundTruthItem struct {
	Kind         string `json:"kind"`
	Name         string `json:"name,omitempty"`
	File         string `json:"file,omitempty"`
	From         string `json:"from,omitempty"`
	To           string `json:"to,omitempty"`
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
	ReportVersion   string             `json:"report_version"`
	GeneratedAt     time.Time          `json:"generated_at"`
	CodeScaleCommit string             `json:"code_scale_commit"`
	GoVersion       string             `json:"go_version"`
	CorpusVersion   string             `json:"corpus_version"`
	FixtureRevision string             `json:"fixture_revision"`
	Tokenizer       string             `json:"tokenizer"`
	Modes           []Mode             `json:"modes"`
	Budgets         []int              `json:"budgets"`
	Repeat          int                `json:"repeat"`
	TasksRun        int                `json:"tasks_run"`
	Results         []TaskResult       `json:"results"`
	Aggregate       Aggregate          `json:"aggregate"`
	ByCategory      map[string]Summary `json:"by_category"`
	ByMode          map[string]Summary `json:"by_mode"`
	Validation      Validation         `json:"validation"`
	BenchmarkNotes  []string           `json:"benchmark_notes"`
}

type TaskResult struct {
	TaskID                 string             `json:"task_id"`
	Category               string             `json:"category"`
	Repo                   string             `json:"repo"`
	Mode                   Mode               `json:"mode"`
	Budget                 int                `json:"budget"`
	Repeat                 int                `json:"repeat"`
	EligibleForAcceptance  bool               `json:"eligible_for_acceptance"`
	ContextTokens          int                `json:"context_tokens"`
	ReportedUsedTokens     int                `json:"reported_used_tokens"`
	MetadataTokens         int                `json:"metadata_tokens"`
	SourceTokens           int                `json:"source_tokens"`
	BaselineContextTokens  int                `json:"baseline_context_tokens"`
	TokenSaving            float64            `json:"token_saving"`
	CandidateCount         int                `json:"candidate_count"`
	RequiredTotal          int                `json:"required_total"`
	RequiredFound          int                `json:"required_found"`
	RequiredMissing        []string           `json:"required_missing,omitempty"`
	DependencyRecall       float64            `json:"required_dependency_recall"`
	SymbolRecall           float64            `json:"symbol_recall"`
	FileRecall             float64            `json:"file_recall"`
	RelationshipRecall     float64            `json:"relationship_recall"`
	ProviderRecall         float64            `json:"provider_recall"`
	Precision              float64            `json:"precision"`
	Top5Recall             float64            `json:"top5_recall"`
	Top10Recall            float64            `json:"top10_recall"`
	SufficiencyStatus      string             `json:"sufficiency_status,omitempty"`
	SufficiencyStage       string             `json:"sufficiency_stage,omitempty"`
	SufficiencyReasons     []string           `json:"sufficiency_reasons,omitempty"`
	FalseSufficiency       bool               `json:"false_sufficiency"`
	FalseInsufficiency     bool               `json:"false_insufficiency"`
	ProviderFabrication    bool               `json:"provider_fabrication"`
	CrossRepoLeak          bool               `json:"cross_repo_leak"`
	Nondeterministic       bool               `json:"nondeterministic"`
	DeterminismFingerprint string             `json:"determinism_fingerprint"`
	StopStage              string             `json:"stop_stage,omitempty"`
	RetrievalRounds        int                `json:"retrieval_rounds"`
	SourceReads            int                `json:"source_reads"`
	SourceBytes            int64              `json:"source_bytes"`
	RetrievalCalls         int                `json:"retrieval_calls"`
	PlannerMilliseconds    float64            `json:"planner_ms"`
	AssemblyMilliseconds   float64            `json:"assembly_ms"`
	TotalMilliseconds      float64            `json:"total_ms"`
	RuntimeError           string             `json:"runtime_error,omitempty"`
	Retrieved              []RetrievedSummary `json:"retrieved,omitempty"`
	Ranked                 []RetrievedSummary `json:"ranked,omitempty"`
}

type RetrievedSummary struct {
	Key       string `json:"key"`
	Name      string `json:"name,omitempty"`
	File      string `json:"file,omitempty"`
	SymbolID  string `json:"symbol_id,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Authority string `json:"authority,omitempty"`
}

type Aggregate struct {
	Results                    int     `json:"results"`
	Tasks                      int     `json:"tasks"`
	PassedRetrieval            int     `json:"passed_retrieval"`
	MedianRecall               float64 `json:"median_recall"`
	MeanRecall                 float64 `json:"mean_recall"`
	MedianTop5Recall           float64 `json:"median_top5_recall"`
	MedianTop10Recall          float64 `json:"median_top10_recall"`
	MedianPrecision            float64 `json:"median_precision"`
	MedianContextTokens        float64 `json:"median_context_tokens"`
	MedianTokenSaving          float64 `json:"median_token_saving"`
	SupportedMedianTokenSaving float64 `json:"supported_median_token_saving"`
	MeanTokenSaving            float64 `json:"mean_token_saving"`
	P25TokenSaving             float64 `json:"p25_token_saving"`
	P75TokenSaving             float64 `json:"p75_token_saving"`
	P90TokenSaving             float64 `json:"p90_token_saving"`
	Sufficient                 int     `json:"sufficient"`
	Blocked                    int     `json:"blocked"`
	Indeterminate              int     `json:"indeterminate"`
	FalseSufficiency           int     `json:"false_sufficiency"`
	FalseInsufficiency         int     `json:"false_insufficiency"`
	ProviderFabrication        int     `json:"provider_fabrication"`
	CrossRepoLeaks             int     `json:"cross_repo_leaks"`
	Nondeterministic           int     `json:"nondeterministic"`
	MedianSourceReads          float64 `json:"median_source_reads"`
	MedianRetrievalRounds      float64 `json:"median_retrieval_rounds"`
	MedianLatencyMilliseconds  float64 `json:"median_latency_ms"`
	P95LatencyMilliseconds     float64 `json:"p95_latency_ms"`
	SerializedBudgetViolations int     `json:"serialized_budget_violations"`
}

type Summary struct {
	Results          int     `json:"results"`
	Tasks            int     `json:"tasks"`
	MedianRecall     float64 `json:"median_recall"`
	MedianSaving     float64 `json:"median_saving"`
	FalseSufficiency int     `json:"false_sufficiency"`
	ProviderErrors   int     `json:"provider_errors"`
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
	TokenReductionAtLeast50    bool `json:"token_reduction_at_least_50"`
}
