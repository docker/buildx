package remote

import (
	"context"
	"errors"
	"net"

	"github.com/docker/buildx/driver"
	"github.com/docker/buildx/util/progress"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/util/tracing/detect"
)

type Driver struct {
	factory driver.Factory
	driver.InitConfig
	*tlsOpts
}

type tlsOpts struct {
	serverName string
	caCert     string
	cert       string
	key        string
}

func (d *Driver) Bootstrap(ctx context.Context, l progress.Logger) error {
	c, err := d.Client(ctx)
	if err != nil {
		return err
	}
	return c.Wait(ctx)
}

func (d *Driver) Info(ctx context.Context) (*driver.Info, error) {
	c, err := d.Client(ctx)
	if err != nil {
		return &driver.Info{
			Status: driver.Inactive,
		}, nil
	}

	if _, err := c.ListWorkers(ctx); err != nil {
		return &driver.Info{
			Status: driver.Inactive,
		}, nil
	}

	return &driver.Info{
		Status: driver.Running,
	}, nil
}

func (d *Driver) Version(ctx context.Context) (string, error) {
	return "", nil
}

func (d *Driver) Stop(ctx context.Context, force bool) error {
	return nil
}

func (d *Driver) Rm(ctx context.Context, force, rmVolume, rmDaemon bool) error {
	return nil
}

func (d *Driver) Client(ctx context.Context) (*client.Client, error) {
	opts := []client.ClientOpt{}

	exp, err := detect.Exporter()
	if err != nil {
		return nil, err
	}
	if td, ok := exp.(client.TracerDelegate); ok {
		opts = append(opts, client.WithTracerDelegate(td))
	}

	if d.tlsOpts != nil {
		opts = append(opts, []client.ClientOpt{
			client.WithServerConfig(d.tlsOpts.serverName, d.tlsOpts.caCert),
			client.WithCredentials(d.tlsOpts.cert, d.tlsOpts.key),
		}...)
	}

	return client.New(ctx, d.InitConfig.EndpointAddr, opts...)
}

func (d *Driver) Features(ctx context.Context) map[driver.Feature]bool {
	return map[driver.Feature]bool{
		driver.OCIExporter:    true,
		driver.DockerExporter: true,
		driver.CacheExport:    true,
		driver.MultiPlatform:  true,
	}
}

func (d *Driver) HostGatewayIP(ctx context.Context) (net.IP, error) {
	return nil, errors.New("host-gateway is not supported by the remote driver")
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
