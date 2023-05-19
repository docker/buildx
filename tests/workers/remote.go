package workers

import (
	"context"
	"os/exec"

	"github.com/moby/buildkit/identity"
	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/pkg/errors"
)

func InitRemoteWorker() {
	integration.Register(&remoteWorker{
		id: "remote",
	})
}

type remoteWorker struct {
	id string
}

func (w remoteWorker) Name() string {
	return w.id
}

func (w remoteWorker) Rootless() bool {
	return false
}

func (w remoteWorker) New(ctx context.Context, cfg *integration.BackendConfig) (b integration.Backend, cl func() error, err error) {
	oci := integration.OCI{ID: w.id}
	bk, bkclose, err := oci.New(ctx, cfg)
	if err != nil {
		return bk, cl, err
	}

	name := "integration-remote-" + identity.NewID()
	cmd := exec.Command("buildx", "create",
		"--bootstrap",
		"--name="+name,
		"--driver=remote",
		bk.Address(),
	)
	if err := cmd.Run(); err != nil {
		return nil, nil, errors.Wrapf(err, "failed to create buildx instance %s", name)
	}

	cl = func() error {
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
		builder: name,
	}, cl, nil
}
