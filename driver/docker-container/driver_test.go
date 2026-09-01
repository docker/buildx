package docker

import (
	"testing"
	"time"

	"github.com/docker/buildx/driver"
	"github.com/moby/moby/api/types/container"
	"github.com/stretchr/testify/require"
)

func TestClientWaitDeadline(t *testing.T) {
	now := time.Now()

	t.Run("stopped-builder-fails-fast", func(t *testing.T) {
		deadline, err := clientWaitDeadline(&container.State{}, now)
		require.ErrorIs(t, err, driver.ErrNotRunning{})
		require.True(t, deadline.IsZero())
	})

	t.Run("established-builder-skips-wait", func(t *testing.T) {
		deadline, err := clientWaitDeadline(&container.State{
			Running:   true,
			StartedAt: now.Add(-2 * buildkitdStartupTimeout).Format(time.RFC3339Nano),
		}, now)
		require.NoError(t, err)
		require.True(t, deadline.IsZero())
	})

	t.Run("recent-start-builder-waits", func(t *testing.T) {
		startedAt := now.Add(-time.Second)
		deadline, err := clientWaitDeadline(&container.State{
			Running:   true,
			StartedAt: startedAt.Format(time.RFC3339Nano),
		}, now)
		require.NoError(t, err)
		require.True(t, deadline.Equal(startedAt.Add(buildkitdStartupTimeout)))
	})

	t.Run("invalid-start-time-skips-wait", func(t *testing.T) {
		deadline, err := clientWaitDeadline(&container.State{
			Running:   true,
			StartedAt: "invalid",
		}, now)
		require.NoError(t, err)
		require.True(t, deadline.IsZero())
	})
}
