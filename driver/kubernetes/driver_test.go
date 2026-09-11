package kubernetes

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCalculateBackoffNeverShorterThanSchedule(t *testing.T) {
	const (
		baseDelay = 500 * time.Millisecond
		maxDelay  = 10 * time.Second
	)

	for attempt := range 6 {
		schedule := min(time.Duration(1<<uint(attempt))*baseDelay, maxDelay)
		ceiling := min(2*schedule, maxDelay)

		for range 500 {
			got := calculateBackoff(attempt, baseDelay, maxDelay)
			require.GreaterOrEqual(t, got, schedule,
				"attempt %d must never wait less than the exponential schedule", attempt)
			require.LessOrEqual(t, got, ceiling,
				"attempt %d must not exceed twice the schedule, nor maxDelay", attempt)
		}
	}
}

func TestCalculateBackoffRespectsMaxDelay(t *testing.T) {
	const (
		baseDelay = 500 * time.Millisecond
		maxDelay  = 10 * time.Second
	)

	for range 500 {
		require.LessOrEqual(t, calculateBackoff(20, baseDelay, maxDelay), maxDelay,
			"a large attempt count must stay capped at maxDelay")
	}
}

func TestCalculateBackoffVaries(t *testing.T) {
	const (
		baseDelay = 500 * time.Millisecond
		maxDelay  = 10 * time.Second
	)

	seen := make(map[time.Duration]struct{})
	for range 500 {
		seen[calculateBackoff(3, baseDelay, maxDelay)] = struct{}{}
	}

	// A deterministic implementation returns a single value. The range at
	// attempt 3 is two seconds wide, so one distinct value across 500 draws
	// would not be chance.
	require.Greater(t, len(seen), 1,
		"backoff must vary so concurrent builders do not retry in lockstep")
}
