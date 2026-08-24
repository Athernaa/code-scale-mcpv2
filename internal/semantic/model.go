package semantic

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/Athernaa/code-scale-mcpv2/internal/parser"
)

const (
	AnalyzerFiveM                 = "fivem"
	AnalyzerGenericGraph          = "generic_graph"
	AnalyzerFiveMWorkspace        = "fivem_workspace"
	AnalyzerFramework             = "framework_intelligence"
	AnalyzerFrameworkIntelligence = AnalyzerFramework
)

// Entity is a generic semantic fact extracted from a source file or resource
// manifest. Kind, Framework, Side, and Metadata are intentionally extensible
// so analyzers for other ecosystems can use the same storage and query layer.
type Entity struct {
	ID        string         `json:"id"`
	Analyzer  string         `json:"analyzer"`
	Repo      string         `json:"repo"`
	File      string         `json:"file"`
	SymbolID  string         `json:"symbol_id,omitempty"`
	Kind      string         `json:"kind"`
	Name      string         `json:"name"`
	Framework string         `json:"framework,omitempty"`
	Side      string         `json:"side,omitempty"`
	Line      int            `json:"line"`
	EndLine   int            `json:"end_line,omitempty"`
	Dynamic   bool           `json:"dynamic,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// Relationship is a deterministic directed edge between semantic entities.
// An empty ToEntityID represents an unresolved/dynamic reference and must not
// be presented as a resolved target by callers.
type Relationship struct {
	ID           string  `json:"id"`
	Analyzer     string  `json:"analyzer"`
	Repo         string  `json:"repo"`
	FromEntityID string  `json:"from_entity_id"`
	ToEntityID   string  `json:"to_entity_id,omitempty"`
	Kind         string  `json:"kind"`
	Name         string  `json:"name,omitempty"`
	Dynamic      bool    `json:"dynamic,omitempty"`
	Confidence   float64 `json:"confidence,omitempty"`
	File         string  `json:"file"`
	Line         int     `json:"line"`
}

// TraceEdge is the compact graph result returned by relationship traversal.
type TraceEdge struct {
	Relationship
	From  Entity  `json:"from"`
	To    *Entity `json:"to,omitempty"`
	Depth int     `json:"depth"`
}

// FileInput is the generic input supplied to a file analyzer.
type FileInput struct {
	Repo     string
	File     string
	Language string
	Content  []byte
	Symbols  []parser.Symbol
	Side     string
	Resource string
}

// RepositoryInput supplies the complete indexed source set to a repository
// analyzer. Repository analysis happens during indexing, never at query time.
type RepositoryInput struct {
	Repo             string
	Resource         string
	SourceType       string
	ModulePath       string
	Files            map[string][]byte
	Languages        map[string]string
	Symbols          map[string][]parser.Symbol
	SemanticEntities []Entity
}

// Result contains semantic facts produced by an analyzer.
type Result struct {
	Entities      []Entity
	Relationships []Relationship
}

// StableID returns a deterministic opaque ID for semantic records.
func StableID(prefix string, parts ...string) string {
	h := sha256.New()
	_, _ = h.Write([]byte(prefix))
	for _, part := range parts {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(part))
	}
	return prefix + "/" + hex.EncodeToString(h.Sum(nil))[:24]
}

// NormalizeSide keeps analyzer-provided side values consistent across
// storage, filters, and future analyzers.
func NormalizeSide(side string) string {
	switch strings.ToLower(strings.TrimSpace(side)) {
	case "client", "server", "shared":
		return strings.ToLower(strings.TrimSpace(side))
	default:
		return "unknown"
	}
}

// Analyzer is the extension seam for file-level semantic analyzers.
type Analyzer interface {
	AnalyzeFile(ctx context.Context, input FileInput) (Result, error)
}

// RepositoryAnalyzer optionally supports repository-wide analysis such as
// manifests and relationship resolution.
type RepositoryAnalyzer interface {
	AnalyzeRepository(ctx context.Context, input RepositoryInput) (Result, error)
}
