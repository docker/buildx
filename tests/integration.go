package tests

import (
	"os"
	"os/exec"
	"testing"

	"github.com/containerd/continuity/fs/fstest"
	"github.com/moby/buildkit/util/testutil/integration"
)

func tmpdir(t *testing.T, appliers ...fstest.Applier) (string, error) {
	tmpdir := t.TempDir()
	if err := fstest.Apply(appliers...).Apply(tmpdir); err != nil {
		return "", err
	}
	return tmpdir, nil
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
