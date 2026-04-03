package workers

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/docker/buildx/tests/helpers"
	"github.com/moby/buildkit/identity"
	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/pkg/errors"
)

func InitKubernetesWorker() {
	integration.Register(&kubernetesWorker{
		id: "kubernetes",
	})
}

type kubernetesWorker struct {
	id string

	unsupported []string

	k3sConfig string
	k3sClose  func() error
	k3sErr    error
	k3sOnce   sync.Once
}

func (w *kubernetesWorker) Name() string {
	return w.id
}

func (w *kubernetesWorker) Rootless() bool {
	return false
}

func (w *kubernetesWorker) NetNSDetached() bool {
	return false
}

func (w *kubernetesWorker) New(ctx context.Context, cfg *integration.BackendConfig) (integration.Backend, func() error, error) {
	w.k3sOnce.Do(func() {
		w.k3sConfig, w.k3sClose, w.k3sErr = helpers.NewK3sServer(cfg)
	})
	if w.k3sErr != nil {
		return nil, w.k3sClose, w.k3sErr
	}

	cfgfile, release, err := integration.WriteConfig(cfg.DaemonConfig)
	if err != nil {
		return nil, nil, err
	}
	if release != nil {
		defer release()
	}
	defer os.RemoveAll(filepath.Dir(cfgfile))

	name := "integration-kubernetes-" + identity.NewID()
	nodeName := "buildkit-" + identity.NewID()
	env := append(
		os.Environ(),
		"BUILDX_CONFIG=/tmp/buildx-"+name,
		"KUBECONFIG="+w.k3sConfig,
	)

	cmd := exec.CommandContext(ctx, "buildx", "create",
		"--name="+name,
		"--node="+nodeName,
		"--buildkitd-config="+cfgfile,
		"--driver=kubernetes",
	)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, nil, errors.Wrapf(err, "failed to create buildx instance %s: %s", name, strings.TrimSpace(string(out)))
	}

	if err := patchBuilderDeployment(ctx, env, nodeName); err != nil {
		return nil, nil, err
	}

	cmd = exec.CommandContext(ctx, "buildx", "inspect", "--bootstrap", name)
	cmd.Env = env
	out, err = cmd.CombinedOutput()
	if err != nil {
		return nil, nil, errors.Wrapf(err, "failed to bootstrap buildx instance %s: %s", name, strings.TrimSpace(string(out)))
	}

	cl := func() error {
		cmd := exec.CommandContext(context.Background(), "buildx", "rm", "-f", name)
		cmd.Env = env
		return cmd.Run()
	}

	return &backend{
		builder:             name,
		unsupportedFeatures: w.unsupported,
	}, cl, nil
}

func patchBuilderDeployment(ctx context.Context, env []string, nodeName string) error {
	cmd := exec.CommandContext(ctx, "kubectl", "patch", "deployment", nodeName, "--type=merge", "-p", `{"spec":{"template":{"spec":{"hostNetwork":true,"dnsPolicy":"ClusterFirstWithHostNet"}}}}`)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return errors.Wrapf(err, "failed to patch deployment %s for host networking: %s", nodeName, strings.TrimSpace(string(out)))
	}

	cmd = exec.CommandContext(ctx, "kubectl", "rollout", "status", "deployment/"+nodeName, "--timeout=120s")
	cmd.Env = env
	out, err = cmd.CombinedOutput()
	if err != nil {
		return errors.Wrapf(err, "deployment %s did not roll out after host-network patch: %s", nodeName, strings.TrimSpace(string(out)))
	}
	return nil
}

func (w *kubernetesWorker) Close() error {
	if c := w.k3sClose; c != nil {
		if err := c(); err != nil {
			return err
		}
	}

	w.k3sConfig = ""
	w.k3sClose = nil
	w.k3sErr = nil
	w.k3sOnce = sync.Once{}

	return nil
}
