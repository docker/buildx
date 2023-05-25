package tests

import (
	"os"
	"os/exec"
	"testing"

	"github.com/containerd/continuity/fs/fstest"
	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/stretchr/testify/require"
)

func tmpdir(t *testing.T, appliers ...fstest.Applier) string {
	t.Helper()
	tmpdir := t.TempDir()
	err := fstest.Apply(appliers...).Apply(tmpdir)
	require.NoError(t, err)
	return tmpdir
}

func buildxCmd(sb integration.Sandbox, args ...string) *exec.Cmd {
	if builder := sb.Address(); builder != "" {
		args = append([]string{"--builder=" + builder}, args...)
	}
	cmd := exec.Command("buildx", args...)
	if context := sb.DockerAddress(); context != "" {
		cmd.Env = append(os.Environ(), "DOCKER_CONTEXT="+context)
	}

	return cmd
}
