package docker

import (
	"context"
	"net"
	"time"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/moby/buildkit/client"
	"github.com/pkg/errors"
	"github.com/tonistiigi/buildx/driver"
	"github.com/tonistiigi/buildx/util/progress"
)

var buildkitImage = "moby/buildkit:master" // TODO: make this verified and configuratble

type Driver struct {
	driver.InitConfig
	version dockertypes.Version
}

func (d *Driver) Bootstrap(ctx context.Context, l progress.Logger) error {
	return nil
}

func (d *Driver) Info(ctx context.Context) (*driver.Info, error) {
	_, err := d.DockerAPI.ServerVersion(ctx)
	if err != nil {
		return nil, errors.Wrapf(driver.ErrNotConnecting, err.Error())
	}
	return &driver.Info{
		Status: driver.Running,
	}, nil
}

func (d *Driver) Stop(ctx context.Context, force bool) error {
	return nil
}

func (d *Driver) Rm(ctx context.Context, force bool) error {
	return nil
}

func (d *Driver) Client(ctx context.Context) (*client.Client, error) {
	return client.New(ctx, "", client.WithDialer(func(string, time.Duration) (net.Conn, error) {
		return d.DockerAPI.DialHijack(ctx, "/grpc", "h2c", nil)
	}))
}
