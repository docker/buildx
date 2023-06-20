package workers

import (
	"context"
	"os"
	"os/exec"

	"github.com/moby/buildkit/identity"
	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/pkg/errors"
)

func InitDockerWorker() {
	integration.Register(&dockerWorker{
		id: "docker",
	})
	integration.Register(&dockerWorker{
		id:                    "docker+containerd",
		containerdSnapshotter: true,
	})
}

type dockerWorker struct {
	id                    string
	containerdSnapshotter bool
}

func (c dockerWorker) Name() string {
	return c.id
}

func (c dockerWorker) Rootless() bool {
	return false
}

func (c dockerWorker) New(ctx context.Context, cfg *integration.BackendConfig) (b integration.Backend, cl func() error, err error) {
	moby := integration.Moby{
		ID:                    c.id,
		ContainerdSnapshotter: c.containerdSnapshotter,
	}
	bk, bkclose, err := moby.New(ctx, cfg)
	if err != nil {
		return bk, cl, err
	}

	name := "integration-" + identity.NewID()
	cmd := exec.Command("docker", "context", "create",
		name,
		"--docker", "host="+bk.DockerAddress(),
	)
	cmd.Env = append(os.Environ(), "BUILDX_CONFIG=/tmp/buildx-"+name)
	if err := cmd.Run(); err != nil {
		return bk, cl, errors.Wrapf(err, "failed to create buildx instance %s", name)
	}

	cl = func() error {
		var err error
		if err1 := bkclose(); err == nil {
			err = err1
		}
		cmd := exec.Command("docker", "context", "rm", "-f", name)
		if err1 := cmd.Run(); err1 != nil {
			err = errors.Wrapf(err1, "failed to remove buildx instance %s", name)
		}
		return err
	}

	return &backend{
		builder: name,
		context: name,
	}, cl, nil
}

func (c dockerWorker) Close() error {
	return nil
}
