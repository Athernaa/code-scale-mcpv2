package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/syphon1c/code-scale-mcp/internal/storage"
	"github.com/syphon1c/code-scale-mcp/internal/watcher"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Meta contains response metadata.
type Meta struct {
	TimingMs    float64            `json:"timing_ms"`
	Repo        string             `json:"repo,omitempty"`
	SymbolCount int                `json:"symbol_count,omitempty"`
	FileCount   int                `json:"file_count,omitempty"`
	Truncated   bool               `json:"truncated,omitempty"`
	TokensSaved int64              `json:"tokens_saved,omitempty"`
	TotalSaved  int64              `json:"total_tokens_saved,omitempty"`
	CostAvoided map[string]float64 `json:"cost_avoided,omitempty"`
	TotalCost   map[string]float64 `json:"total_cost_avoided,omitempty"`
}

// Response wraps a tool result with metadata.
type Response struct {
	Result any   `json:"result"`
	Meta   *Meta `json:"_meta,omitempty"`
}

// Deps holds shared dependencies for tools.
type Deps struct {
	Store   *storage.IndexStore
	Tracker *storage.TokenTracker
	Watcher *watcher.Manager
}

// toTextResult converts any value to an MCP CallToolResult with JSON TextContent.
func toTextResult(v any) (*mcp.CallToolResult, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}, nil
}

// errorResult creates an error MCP result.
func errorResult(errMsg string) (*mcp.CallToolResult, error) {
	data, _ := json.Marshal(map[string]string{"error": errMsg})
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
		IsError: true,
	}, nil
}

// timer measures elapsed time.
type timer struct {
	start time.Time
}

func newTimer() *timer {
	return &timer{start: time.Now()}
}

func (t *timer) elapsedMs() float64 {
	return float64(time.Since(t.start).Microseconds()) / 1000.0
}

// addSavings records token savings and returns meta with costs.
func (d *Deps) addSavings(rawBytes, responseBytes int64) (int64, int64) {
	saved := storage.EstimateSavings(rawBytes, responseBytes)
	total, _ := d.Tracker.AddSavings(saved)
	return saved, total
}

// expandHomePath expands a leading ~/ in the path to the user's home directory.
func expandHomePath(p string) (string, error) {
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot determine home directory: %w", err)
		}
		return filepath.Join(home, p[2:]), nil
	}
	return p, nil
}

// clampResults clamps n to [defaultVal, max].
func clampResults(n, defaultVal, max int) int {
	if n <= 0 {
		return defaultVal
	}
	if n > max {
		return max
	}
	return n
}
