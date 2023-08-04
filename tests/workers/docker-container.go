package workers

import (
	"context"
	"os"
	"os/exec"
	"sync"

	"github.com/moby/buildkit/identity"
	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/pkg/errors"
)

func InitDockerContainerWorker() {
	integration.Register(&containerWorker{
		id: "docker-container",
	})
}

type containerWorker struct {
	id string

	unsupported []string

	docker      integration.Backend
	dockerClose func() error
	dockerErr   error
	dockerOnce  sync.Once
}

func (w *containerWorker) Name() string {
	return w.id
}

func (w *containerWorker) Rootless() bool {
	return false
}

func (w *containerWorker) New(ctx context.Context, cfg *integration.BackendConfig) (integration.Backend, func() error, error) {
	w.dockerOnce.Do(func() {
		w.docker, w.dockerClose, w.dockerErr = dockerWorker{id: w.id}.New(ctx, cfg)
	})
	if w.dockerErr != nil {
		return w.docker, w.dockerClose, w.dockerErr
	}

	name := "integration-container-" + identity.NewID()
	cmd := exec.Command("buildx", "create",
		"--bootstrap",
		"--name="+name,
		"--config="+cfg.ConfigFile,
		"--driver=docker-container",
		"--driver-opt=network=host",
	)
	cmd.Env = append(
		os.Environ(),
		"BUILDX_CONFIG=/tmp/buildx-"+name,
		"DOCKER_CONTEXT="+w.docker.DockerAddress(),
	)
	if err := cmd.Run(); err != nil {
		return nil, nil, errors.Wrapf(err, "failed to create buildx instance %s", name)
	}

	cl := func() error {
		cmd := exec.Command("buildx", "rm", "-f", name)
		return cmd.Run()
	}

	return &backend{
		context:             w.docker.DockerAddress(),
		builder:             name,
		unsupportedFeatures: w.unsupported,
	}, cl, nil
}

func (w *containerWorker) Close() error {
	if close := w.dockerClose; close != nil {
		return close()
	}

	// reset the worker to be ready to go again
	w.docker = nil
	w.dockerClose = nil
	w.dockerErr = nil
	w.dockerOnce = sync.Once{}

	return nil
}
