package workers

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
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

	docker      integration.Backend
	dockerClose func() error
	dockerErr   error
	dockerOnce  sync.Once

	k3dConfig string
	k3dClose  func() error
	k3dErr    error
	k3dOnce   sync.Once
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
	w.dockerOnce.Do(func() {
		w.docker, w.dockerClose, w.dockerErr = dockerWorker{id: w.id}.New(ctx, cfg)
	})
	if w.dockerErr != nil {
		return w.docker, w.dockerClose, w.dockerErr
	}

	w.k3dOnce.Do(func() {
		w.k3dConfig, w.k3dClose, w.k3dErr = helpers.NewK3dServer(cfg)
	})
	if w.k3dErr != nil {
		return nil, w.k3dClose, w.k3dErr
	}

	cfgfile, err := integration.WriteConfig(cfg.DaemonConfig)
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(filepath.Dir(cfgfile))

	name := "integration-kubernetes-" + identity.NewID()
	cmd := exec.Command("buildx", "create",
		"--bootstrap",
		"--name="+name,
		"--buildkitd-config="+cfgfile,
		"--driver=kubernetes",
	)
	cmd.Env = append(
		os.Environ(),
		"BUILDX_CONFIG=/tmp/buildx-"+name,
		"KUBECONFIG="+w.k3dConfig,
	)
	if err := cmd.Run(); err != nil {
		return nil, nil, errors.Wrapf(err, "failed to create buildx instance %s", name)
	}

	cl := func() error {
		cmd := exec.Command("buildx", "rm", "-f", name)
		return cmd.Run()
	}

	return &backend{
		builder:             name,
		unsupportedFeatures: w.unsupported,
	}, cl, nil
}

func (w *kubernetesWorker) Close() error {
	if c := w.k3dClose; c != nil {
		return c()
	}
	if c := w.dockerClose; c != nil {
		return c()
	}

	// reset the worker to be ready to go again
	w.docker = nil
	w.dockerClose = nil
	w.dockerErr = nil
	w.dockerOnce = sync.Once{}
	w.k3dConfig = ""
	w.k3dClose = nil
	w.k3dErr = nil
	w.k3dOnce = sync.Once{}

	return nil
}
