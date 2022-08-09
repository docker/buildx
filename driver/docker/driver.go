package docker

import (
	"context"
	"net"

	"github.com/docker/buildx/driver"
	"github.com/docker/buildx/util/progress"
	"github.com/moby/buildkit/client"
	"github.com/pkg/errors"
)

type Driver struct {
	factory driver.Factory
	driver.InitConfig
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

func (d *Driver) Version(ctx context.Context) (string, error) {
	v, err := d.DockerAPI.ServerVersion(ctx)
	if err != nil {
		return "", errors.Wrapf(driver.ErrNotConnecting, err.Error())
	}
	return v.Version, nil
}

func (d *Driver) Stop(ctx context.Context, force bool) error {
	return nil
}

func (d *Driver) Rm(ctx context.Context, force, rmVolume, rmDaemon bool) error {
	return nil
}

func (d *Driver) Client(ctx context.Context) (*client.Client, error) {
	return client.New(ctx, "", client.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return d.DockerAPI.DialHijack(ctx, "/grpc", "h2c", nil)
	}), client.WithSessionDialer(func(ctx context.Context, proto string, meta map[string][]string) (net.Conn, error) {
		return d.DockerAPI.DialHijack(ctx, "/session", proto, meta)
	}))
}

func (d *Driver) Features() map[driver.Feature]bool {
	var useContainerdSnapshotter bool
	ctx := context.Background()
	c, err := d.Client(ctx)
	if err == nil {
		workers, _ := c.ListWorkers(ctx)
		for _, w := range workers {
			if _, ok := w.Labels["org.mobyproject.buildkit.worker.snapshotter"]; ok {
				useContainerdSnapshotter = true
			}
		}
	}
	return map[driver.Feature]bool{
		driver.OCIExporter:    useContainerdSnapshotter,
		driver.DockerExporter: useContainerdSnapshotter,
		driver.CacheExport:    useContainerdSnapshotter,
		driver.MultiPlatform:  useContainerdSnapshotter,
	}
}

func (d *Driver) Factory() driver.Factory {
	return d.factory
}

func (d *Driver) IsMobyDriver() bool {
	return true
}

func (d *Driver) Config() driver.InitConfig {
	return d.InitConfig
}
