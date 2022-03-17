package env

import (
	"context"
	"fmt"

	"github.com/docker/buildx/driver"
	"github.com/docker/buildx/util/progress"
	"github.com/moby/buildkit/client"
	buildkitclient "github.com/moby/buildkit/client"
	"github.com/pkg/errors"
)

type Driver struct {
	factory driver.Factory
	driver.InitConfig
	BuldkitdAddr string
	BuildkitAPI  *buildkitclient.Client
}

func (d *Driver) Bootstrap(ctx context.Context, l progress.Logger) error {
	return nil
}

func (d *Driver) Info(ctx context.Context) (*driver.Info, error) {
	if d.BuldkitdAddr == "" && d.Driver == "env" {
		return nil, errors.Errorf("buldkitd addr must not be empty")
	}

	c, err := client.New(ctx, d.BuldkitdAddr)
	if err != nil {
		return nil, errors.Wrapf(driver.ErrNotConnecting, err.Error())
	}

	if _, err := c.ListWorkers(ctx); err != nil {
		return nil, errors.Wrapf(driver.ErrNotConnecting, err.Error())
	}

	return &driver.Info{
		Status: driver.Running,
	}, nil
}

func (d *Driver) Stop(ctx context.Context, force bool) error {
	return fmt.Errorf("stop command is not implemented for this driver")
}

func (d *Driver) Rm(ctx context.Context, force, rmVolume, rmDaemon bool) error {
	return fmt.Errorf("rm command is not implemented for this driver")
}

func (d *Driver) Client(ctx context.Context) (*client.Client, error) {
	return client.New(ctx, d.BuldkitdAddr, client.WithSessionDialer(d.BuildkitAPI.Dialer()))
}

func (d *Driver) Features() map[driver.Feature]bool {
	return map[driver.Feature]bool{}
}

func (d *Driver) Factory() driver.Factory {
	return d.factory
}

func (d *Driver) IsMobyDriver() bool {
	return false
}

func (d *Driver) Config() driver.InitConfig {
	return d.InitConfig
}
