package storage

import (
	"database/sql"
	"math"

	"github.com/google/uuid"
)

// Model pricing per 1M tokens (input).
const (
	ClaudeOpusRate = 25.0 // $25 per 1M tokens
	GPT5Rate       = 10.0 // $10 per 1M tokens
)

// TokenTracker tracks cumulative token savings.
type TokenTracker struct {
	db *sql.DB
}

// NewTokenTracker creates a tracker backed by the store's database.
func NewTokenTracker(db *sql.DB) *TokenTracker {
	return &TokenTracker{db: db}
}

// EstimateSavings estimates tokens saved.
// rawBytes: size of raw file that would have been read
// responseBytes: size of the actual response
func EstimateSavings(rawBytes, responseBytes int64) int64 {
	saved := (rawBytes - responseBytes) / 4 // ~4 bytes per token
	if saved < 0 {
		return 0
	}
	return saved
}

// AddSavings records additional token savings.
func (t *TokenTracker) AddSavings(tokensSaved int64) (int64, error) {
	// Ensure row exists
	_, _ = t.db.Exec("INSERT OR IGNORE INTO token_savings (id, total_tokens_saved, anon_id) VALUES (1, 0, ?)", uuid.New().String())

	_, err := t.db.Exec("UPDATE token_savings SET total_tokens_saved = total_tokens_saved + ? WHERE id = 1", tokensSaved)
	if err != nil {
		return 0, err
	}

	var total int64
	_ = t.db.QueryRow("SELECT total_tokens_saved FROM token_savings WHERE id = 1").Scan(&total)
	return total, nil
}

// GetTotalSavings returns cumulative token savings.
func (t *TokenTracker) GetTotalSavings() int64 {
	var total int64
	_ = t.db.QueryRow("SELECT total_tokens_saved FROM token_savings WHERE id = 1").Scan(&total)
	return total
}

// CostAvoided calculates cost avoided for given tokens.
func CostAvoided(tokens int64) map[string]float64 {
	t := float64(tokens) / 1_000_000
	return map[string]float64{
		"claude_opus": round(t * ClaudeOpusRate),
		"gpt5_latest": round(t * GPT5Rate),
	}
}

func round(f float64) float64 {
	return math.Round(f*10000) / 10000
}
