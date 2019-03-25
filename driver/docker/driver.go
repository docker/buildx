package docker

import (
	"context"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/moby/buildkit/client"
	"github.com/pkg/errors"
	"github.com/tonistiigi/buildx/driver"
)

type Driver struct {
	config  driver.InitConfig
	version dockertypes.Version
}

func (d *Driver) Bootstrap(context.Context, driver.Logger) error {
	return errors.Errorf("bootstrap not implemented for %T", d)
}

func (d *Driver) Info(context.Context) (driver.Info, error) {
	return driver.Info{}, errors.Errorf("info not implemented for %T", d)
}

func (d *Driver) Stop(ctx context.Context, force bool) error {
	return errors.Errorf("stop not implemented for %T", d)
}

func (d *Driver) Rm(ctx context.Context, force bool) error {
	return errors.Errorf("rm not implemented for %T", d)
}

func (d *Driver) Client() (*client.Client, error) {
	return nil, errors.Errorf("client not implemented for %T", d)
}
