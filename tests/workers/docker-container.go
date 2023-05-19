package workers

import (
	"context"
	"os"
	"os/exec"

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
}

func (w *containerWorker) Name() string {
	return w.id
}

func (w *containerWorker) Rootless() bool {
	return false
}

func (w *containerWorker) New(ctx context.Context, cfg *integration.BackendConfig) (integration.Backend, func() error, error) {
	bk, bkclose, err := dockerWorker{id: w.id}.New(ctx, cfg)
	if err != nil {
		return bk, bkclose, err
	}

	name := "integration-container-" + identity.NewID()
	cmd := exec.Command("buildx", "create",
		"--bootstrap",
		"--name="+name,
		"--config="+cfg.ConfigFile,
		"--driver=docker-container",
		"--driver-opt=network=host",
	)
	cmd.Env = append(os.Environ(), "DOCKER_CONTEXT="+bk.DockerAddress())
	if err := cmd.Run(); err != nil {
		return nil, nil, errors.Wrapf(err, "failed to create buildx instance %s", name)
	}

	cl := func() error {
		var err error
		if err1 := bkclose(); err == nil {
			err = err1
		}
		cmd := exec.Command("buildx", "rm", "-f", name)
		if err1 := cmd.Run(); err == nil {
			err = err1
		}
		return err
	}

	return &backend{
		context: bk.DockerAddress(),
		builder: name,
	}, cl, nil
}
