package workers

import (
	"context"
	"os"
	"os/exec"
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

	docker      integration.Backend
	dockerClose func() error
	dockerErr   error
	dockerOnce  sync.Once

	k3dName   string
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
		w.k3dName, w.k3dConfig, w.k3dClose, w.k3dErr = helpers.NewK3dServer(ctx, cfg, w.docker.DockerAddress())
	})
	if w.k3dErr != nil {
		return nil, w.k3dClose, w.k3dErr
	}

	name := "integration-kubernetes-" + identity.NewID()
	cmd := exec.CommandContext(ctx, "buildx", "create",
		"--bootstrap",
		"--name="+name,
		"--driver=kubernetes",
		"--driver-opt=image="+helpers.KubernetesBuildkitImage(),
		"--driver-opt=timeout=60s",
	)
	cmd.Env = append(
		os.Environ(),
		"BUILDX_CONFIG=/tmp/buildx-"+name,
		"DOCKER_CONTEXT="+w.docker.DockerAddress(),
		"KUBECONFIG="+w.k3dConfig,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		diag := helpers.KubernetesDiagnostics(ctx, w.k3dName, w.docker.DockerAddress())
		return nil, nil, errors.Wrapf(err, "failed to create buildx instance %s with image %s: %s\n%s", name, helpers.KubernetesBuildkitImage(), strings.TrimSpace(string(out)), diag)
	}

	cl := func() error {
		cmd := exec.CommandContext(context.Background(), "buildx", "rm", "-f", name)
		cmd.Env = append(
			os.Environ(),
			"BUILDX_CONFIG=/tmp/buildx-"+name,
			"DOCKER_CONTEXT="+w.docker.DockerAddress(),
			"KUBECONFIG="+w.k3dConfig,
		)
		return cmd.Run()
	}

	return &backend{
		context:             w.docker.DockerAddress(),
		builder:             name,
		unsupportedFeatures: w.unsupported,
	}, cl, nil
}

func (w *kubernetesWorker) Close() error {
	setErr := func(dst *error, err error) {
		if err != nil && *dst == nil {
			*dst = err
		}
	}

	var err error
	if c := w.k3dClose; c != nil {
		setErr(&err, c())
	}
	if c := w.dockerClose; c != nil {
		setErr(&err, c())
	}

	// reset the worker to be ready to go again
	w.docker = nil
	w.dockerClose = nil
	w.dockerErr = nil
	w.dockerOnce = sync.Once{}
	w.k3dName = ""
	w.k3dConfig = ""
	w.k3dClose = nil
	w.k3dErr = nil
	w.k3dOnce = sync.Once{}

	return err
}
