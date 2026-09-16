package kubernetes

import (
	"context"
	stderrors "errors"
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

func TestTryWithBackoffRetriesTransient(t *testing.T) {
	var calls int
	err := tryWithBackoff(context.Background(), "test-pod", func() error {
		calls++
		if calls == 1 {
			return stderrors.New("unable to upgrade connection: remote error: tls: internal error")
		}
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 2, calls, "second attempt should succeed after a transient failure")
}

func TestTryWithBackoffPermanentError(t *testing.T) {
	permErr := stderrors.New("pods is forbidden")
	var calls int
	err := tryWithBackoff(context.Background(), "test-pod", func() error {
		calls++
		return permErr
	})
	require.ErrorIs(t, err, permErr)
	require.Equal(t, 1, calls, "a permanent error must not be retried")
}

func TestTryWithBackoffContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	var calls int
	err := tryWithBackoff(ctx, "test-pod", func() error {
		calls++
		cancel(context.Canceled)
		return stderrors.New("tls: internal error")
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, calls, "retry loop must stop once the context is cancelled")
}
