package ratelimit

import "testing"

func TestDefaultThrottleWarnsWithoutReducingOrBlocking(t *testing.T) {
	t.Setenv("CODE_SCALE_HARD_THROTTLE", "false")
	throttler := NewThrottler()
	var action Action
	for i := 0; i < 12; i++ {
		action = throttler.Check("search_text")
	}
	if action == ActionBlocked || action == ActionReduced {
		t.Fatalf("default throttling should not block or reduce results: %v", action)
	}
	limit, warning := ApplyLimit(action, 20)
	if limit != 20 || warning == "" {
		t.Fatalf("expected unchanged limit and warning, got %d/%q", limit, warning)
	}
}
