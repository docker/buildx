package helpers

import (
	"context"
	"os"
	"os/exec"
	"strings"

	"github.com/moby/buildkit/identity"
	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/pkg/errors"
)

const (
	k3dBin = "k3d"
)

func NewK3dServer(ctx context.Context, cfg *integration.BackendConfig, dockerAddress string) (kubeConfig string, cl func() error, err error) {
	if _, err := exec.LookPath(k3dBin); err != nil {
		return "", nil, errors.Wrapf(err, "failed to lookup %s binary", k3dBin)
	}

	deferF := &integration.MultiCloser{}
	cl = deferF.F()

	defer func() {
		if err != nil {
			deferF.F()()
			cl = nil
		}
	}()

	clusterName := "bk-" + identity.NewID()

	cmd := exec.CommandContext(ctx, k3dBin, "cluster", "create", clusterName,
		"--wait",
	)
	cmd.Env = append(
		os.Environ(),
		"DOCKER_CONTEXT="+dockerAddress,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", nil, errors.Wrapf(err, "failed to create k3d cluster %s: %s", clusterName, string(out))
	}
	deferF.Append(func() error {
		cmd := exec.Command(k3dBin, "cluster", "delete", clusterName)
		cmd.Env = append(
			os.Environ(),
			"DOCKER_CONTEXT="+dockerAddress,
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return errors.Wrapf(err, "failed to delete k3d cluster %s: %s", clusterName, string(out))
		}
		return nil
	})

	cmd = exec.CommandContext(ctx, k3dBin, "kubeconfig", "write", clusterName)
	cmd.Env = append(
		os.Environ(),
		"DOCKER_CONTEXT="+dockerAddress,
	)
	out, err = cmd.CombinedOutput()
	if err != nil {
		return "", nil, errors.Wrapf(err, "failed to write kubeconfig for cluster %s: %s", clusterName, string(out))
	}
	kubeConfig = strings.TrimSpace(string(out))

	return
}
