package docker

import (
	"testing"
	"time"

	"github.com/docker/buildx/driver"
	"github.com/moby/moby/api/types/container"
	"github.com/stretchr/testify/require"
)

func TestClientWaitReady(t *testing.T) {
	now := time.Now()
	running := func(startedAt string) *container.State {
		return &container.State{Running: true, StartedAt: startedAt}
	}
	tests := []struct {
		name     string
		state    *container.State
		wantWait bool
		wantErr  error
	}{
		{name: "missing-state-fails-fast", wantErr: driver.ErrNotRunning{}},
		{name: "stopped-builder-fails-fast", state: &container.State{}, wantErr: driver.ErrNotRunning{}},
		{name: "established-builder-skips-wait", state: running(now.Add(-2 * buildkitdStartupWindow).Format(time.RFC3339Nano))},
		{name: "recent-start-builder-waits", state: running(now.Add(-time.Second).Format(time.RFC3339Nano)), wantWait: true},
		{name: "nearly-expired-startup-window-waits", state: running(now.Add(-buildkitdStartupWindow + time.Nanosecond).Format(time.RFC3339Nano)), wantWait: true},
		{name: "expired-startup-window-skips-wait", state: running(now.Add(-buildkitdStartupWindow).Format(time.RFC3339Nano))},
		{name: "invalid-start-time-skips-wait", state: running("invalid")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wait, err := clientWaitReady(tt.state, now)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tt.wantWait, wait)
		})
	}
}
