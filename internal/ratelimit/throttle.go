package ratelimit

import (
	"sync"
	"time"
)

// Action represents the throttle decision.
type Action int

const (
	// ActionNormal allows full results.
	ActionNormal Action = iota
	// ActionReduced halves max results.
	ActionReduced
	// ActionBlocked suggests using batch_execute.
	ActionBlocked
)

// Throttler tracks per-tool call frequency and progressively limits results.
type Throttler struct {
	mu          sync.Mutex
	tools       map[string][]time.Time
	windowSize  time.Duration
	normalLimit int // calls 1-N are normal
	reducedMax  int // calls N+1 to M are reduced
}

// NewThrottler creates a throttler with default thresholds.
// Normal for first 3 calls per tool, reduced for calls 4-8, blocked at 9+ within a 60s window.
func NewThrottler() *Throttler {
	return &Throttler{
		tools:       make(map[string][]time.Time),
		windowSize:  60 * time.Second,
		normalLimit: 3,
		reducedMax:  8,
	}
}

// Check records a call for the given tool and returns the throttle action.
func (t *Throttler) Check(tool string) Action {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-t.windowSize)

	// Prune old calls for this tool
	calls := t.tools[tool]
	valid := calls[:0]
	for _, c := range calls {
		if c.After(cutoff) {
			valid = append(valid, c)
		}
	}

	// Record this call
	valid = append(valid, now)
	t.tools[tool] = valid
	count := len(valid)

	if count <= t.normalLimit {
		return ActionNormal
	}
	if count <= t.reducedMax {
		return ActionReduced
	}
	return ActionBlocked
}

// ApplyLimit adjusts maxResults based on the throttle action.
// Returns the adjusted limit and a warning message (empty if normal).
func ApplyLimit(action Action, maxResults int) (int, string) {
	switch action {
	case ActionReduced:
		reduced := maxResults / 2
		if reduced < 1 {
			reduced = 1
		}
		return reduced, "Rate limit: results reduced. Consider using batch_execute for multiple operations."
	case ActionBlocked:
		return 0, "Rate limit: too many rapid calls. Use batch_execute to combine multiple operations into one call."
	default:
		return maxResults, ""
	}
}
