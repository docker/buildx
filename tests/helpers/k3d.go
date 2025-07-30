package helpers

import (
	"os/exec"
	"strings"

	"github.com/moby/buildkit/identity"
	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/pkg/errors"
)

const (
	k3dBin = "k3d"
)

func NewK3dServer(cfg *integration.BackendConfig) (kubeConfig string, cl func() error, err error) {
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

	clusterName := "buildkit-" + identity.NewID()

	stop, err := integration.StartCmd(exec.Command(k3dBin, "cluster", "create", clusterName,
		"--wait",
	), cfg.Logs)
	if err != nil {
		return "", nil, err
	}
	deferF.Append(stop)

	cmd := exec.Command(k3dBin, "kubeconfig", "write", clusterName)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", nil, errors.Wrapf(err, "failed to write kubeconfig for cluster %s: %s", clusterName, string(out))
	}
	kubeConfig = strings.TrimSpace(string(out))

	return
}
