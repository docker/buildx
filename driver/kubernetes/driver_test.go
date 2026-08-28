package kubernetes

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCalculateBackoff(t *testing.T) {
	const (
		baseDelay = 500 * time.Millisecond
		maxDelay  = 10 * time.Second
	)
	tests := []struct {
		attempt    int
		floor, cap time.Duration
	}{
		{0, 500 * time.Millisecond, time.Second},
		{1, time.Second, 2 * time.Second},
		{2, 2 * time.Second, 4 * time.Second},
		{3, 4 * time.Second, 8 * time.Second},
		{4, 8 * time.Second, maxDelay},
		{5, maxDelay, maxDelay},
		{20, maxDelay, maxDelay},
	}
	for _, tt := range tests {
		seen := make(map[time.Duration]struct{})
		for range 500 {
			got := calculateBackoff(tt.attempt, baseDelay, maxDelay)
			require.GreaterOrEqual(t, got, tt.floor, "attempt %d must never wait less than the exponential schedule", tt.attempt)
			require.LessOrEqual(t, got, tt.cap, "attempt %d must not exceed twice the schedule, nor maxDelay", tt.attempt)
			seen[got] = struct{}{}
		}
		if tt.floor < tt.cap {
			// The narrowest jittered range is 500ms wide, so a single distinct
			// value across 500 draws would not be chance.
			require.Greater(t, len(seen), 1, "attempt %d must vary so concurrent builders do not retry in lockstep", tt.attempt)
		}
	}
}
