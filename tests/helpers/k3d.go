package helpers

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/moby/buildkit/identity"
	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/pkg/errors"
)

const (
	k3dBin = "k3d"
)

func NewK3dServer(ctx context.Context, cfg *integration.BackendConfig, dockerAddress string) (clusterName, kubeConfig string, cl func() error, err error) {
	if _, err := exec.LookPath(k3dBin); err != nil {
		return "", "", nil, errors.Wrapf(err, "failed to lookup %s binary", k3dBin)
	}

	deferF := &integration.MultiCloser{}
	cl = deferF.F()

	defer func() {
		if err != nil {
			deferF.F()()
			cl = nil
		}
	}()

	clusterName = "bk-" + identity.NewID()

	createCtx, cancelCreate := context.WithTimeout(ctx, 90*time.Second)
	defer cancelCreate()

	args := []string{
		"cluster", "create", clusterName,
		"--wait",
	}
	if image := KubernetesK3sImage(); image != "" {
		args = append(args, "--image="+image)
	}
	cmd := exec.CommandContext(createCtx, k3dBin, args...)
	cmd.Env = k3dEnv(dockerAddress)
	out, err := cmd.CombinedOutput()
	if err != nil {
		diag := KubernetesDiagnostics(clusterName, dockerAddress)
		return "", "", nil, errors.Wrapf(err, "failed to create k3d cluster %s: %s\n%s\nouter dockerd logs: %s", clusterName, strings.TrimSpace(string(out)), diag, integration.FormatLogs(cfg.Logs))
	}
	deferF.Append(func() error {
		cmd := exec.Command(k3dBin, "cluster", "delete", clusterName)
		cmd.Env = k3dEnv(dockerAddress)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return errors.Wrapf(err, "failed to delete k3d cluster %s: %s", clusterName, string(out))
		}
		return nil
	})

	kubeconfigCtx, cancelKubeconfig := context.WithTimeout(ctx, 30*time.Second)
	defer cancelKubeconfig()

	cmd = exec.CommandContext(kubeconfigCtx, k3dBin, "kubeconfig", "write", clusterName)
	cmd.Env = k3dEnv(dockerAddress)
	out, err = cmd.CombinedOutput()
	if err != nil {
		diag := KubernetesDiagnostics(clusterName, dockerAddress)
		return "", "", nil, errors.Wrapf(err, "failed to write kubeconfig for cluster %s: %s\n%s\nouter dockerd logs: %s", clusterName, strings.TrimSpace(string(out)), diag, integration.FormatLogs(cfg.Logs))
	}
	kubeConfig = strings.TrimSpace(string(out))

	return
}

func k3dEnv(dockerAddress string) []string {
	env := append(
		os.Environ(),
		"DOCKER_CONTEXT="+dockerAddress,
	)
	if image := KubernetesK3DToolsImage(); image != "" {
		env = append(env, "K3D_IMAGE_TOOLS="+image)
	}
	if image := KubernetesK3DLoadBalancerImage(); image != "" {
		env = append(env, "K3D_IMAGE_LOADBALANCER="+image)
	}
	return env
}
