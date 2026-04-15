package docker

import (
	"context"
	"net"
	"strings"
	"sync"

	"github.com/docker/buildx/driver"
	"github.com/docker/buildx/util/progress"
	"github.com/moby/buildkit/client"
	dockerclient "github.com/moby/moby/client"
	"github.com/pkg/errors"
)

type Driver struct {
	factory driver.Factory
	driver.InitConfig

	// if you add fields, remember to update docs:
	// https://github.com/docker/docs/blob/main/content/build/drivers/docker.md
	features    features
	hostGateway hostGateway

	// Controls the fallback code behavior.
	fallbackMode fallbackMode
}

func (d *Driver) Bootstrap(ctx context.Context, l progress.Logger) error {
	return nil
}

func (d *Driver) Info(ctx context.Context) (*driver.Info, error) {
	// TODO(thaJeztah): is "ping" enough for this?
	_, err := d.DockerAPI.ServerVersion(ctx, dockerclient.ServerVersionOptions{})
	if err != nil {
		return nil, errors.Wrap(driver.ErrNotConnecting{}, err.Error())
	}
	return &driver.Info{
		Status: driver.Running,
	}, nil
}

func (d *Driver) Version(ctx context.Context) (string, error) {
	v, err := d.DockerAPI.ServerVersion(ctx, dockerclient.ServerVersionOptions{})
	if err != nil {
		return "", errors.Wrap(driver.ErrNotConnecting{}, err.Error())
	}
	// TODO(thaJeztah): this code is only used for docker <= v23.0, which are deprecated.
	if bkversion, _ := resolveBuildKitVersion(v.Version); bkversion != "" {
		return bkversion, nil
	}
	// https://github.com/moby/moby/blob/efc7a2abc3ab6dfa7d8d5d8c1c3b99138989b0f1/builder/builder-next/worker/worker.go#L176
	return strings.TrimSuffix(v.Version, "-moby"), nil
}

func (d *Driver) Stop(ctx context.Context, force bool) error {
	return nil
}

func (d *Driver) Rm(ctx context.Context, force, rmVolume, rmDaemon bool) error {
	return nil
}

func (d *Driver) Dial(ctx context.Context) (net.Conn, error) {
	return d.DockerAPI.DialHijack(ctx, "/grpc", "h2c", d.DialMeta)
}

func (d *Driver) Client(ctx context.Context, opts ...client.ClientOpt) (*client.Client, error) {
	c, _, err := d.client(ctx, opts...)
	return c, err
}

type listWorkersFunc func(ctx context.Context) []*client.WorkerInfo

func (d *Driver) client(ctx context.Context, opts ...client.ClientOpt) (*client.Client, listWorkersFunc, error) {
	if d.fallbackMode.AttemptPrimaryClient() {
		if c, err := client.New(ctx, d.DockerAPI.DaemonHost(), opts...); err == nil {
			// We do not allow fallback so there's no reason to test the connection.
			if !d.fallbackMode.AllowFallback() {
				return c, lazyListWorkers(c), nil
			}

			// Fallback is allowed so test the client before we return it.
			// Keep the returned workers in the function closure to prevent duplicate
			// calls to list workers.
			if workers, err := c.ListWorkers(ctx); err == nil {
				// Client works so package the workers we listed into the function so we don't
				// have to call this endpoint again. We also mark that the client succeeded this
				// test at least once and prevent fallback mode from happening.
				d.fallbackMode = disableFallbackMode
				return c, func(context.Context) []*client.WorkerInfo {
					return workers
				}, nil
			}

			// Failed to use the updated client. Provide the fallback.
			_ = c.Close()
		} else if !d.fallbackMode.AllowFallback() {
			// Fallback is not allowed so return this error.
			return nil, nil, err
		}
	}

	// We are required to use the fallback since the docker daemon does not support
	// the client directly. Mark that we need to use the fallback so future calls don't
	// bother with the above check.
	d.fallbackMode = forceFallbackMode
	return d.fallbackClient(ctx, opts...)
}

func (d *Driver) fallbackClient(ctx context.Context, opts ...client.ClientOpt) (*client.Client, listWorkersFunc, error) {
	opts = append([]client.ClientOpt{
		client.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return d.Dial(ctx)
		}), client.WithSessionDialer(func(ctx context.Context, proto string, meta map[string][]string) (net.Conn, error) {
			return d.DockerAPI.DialHijack(ctx, "/session", proto, meta)
		}),
	}, opts...)

	c, err := client.New(ctx, "", opts...)
	if err != nil {
		return nil, nil, err
	}
	return c, lazyListWorkers(c), nil
}

type features struct {
	once sync.Once
	list map[driver.Feature]bool
}

func (d *Driver) Features(ctx context.Context) map[driver.Feature]bool {
	d.features.once.Do(func() {
		var useContainerdSnapshotter bool
		if c, workers, err := d.client(ctx); err == nil {
			for _, w := range workers(ctx) {
				if _, ok := w.Labels["org.mobyproject.buildkit.worker.snapshotter"]; ok {
					useContainerdSnapshotter = true
					break
				}
			}
			c.Close()
		}
		d.features.list = map[driver.Feature]bool{
			driver.OCIExporter:    useContainerdSnapshotter,
			driver.DockerExporter: useContainerdSnapshotter,
			driver.CacheExport:    useContainerdSnapshotter,
			driver.MultiPlatform:  useContainerdSnapshotter,
			driver.DirectPush:     useContainerdSnapshotter,
			driver.DefaultLoad:    true,
		}
	})
	return d.features.list
}

type hostGateway struct {
	once sync.Once
	ip   net.IP
	err  error
}

func (d *Driver) HostGatewayIP(ctx context.Context) (net.IP, error) {
	d.hostGateway.once.Do(func() {
		c, err := d.Client(ctx)
		if err != nil {
			d.hostGateway.err = err
			return
		}
		defer c.Close()
		workers, err := c.ListWorkers(ctx)
		if err != nil {
			d.hostGateway.err = errors.Wrap(err, "listing workers")
			return
		}
		for _, w := range workers {
			// should match github.com/docker/docker/builder/builder-next/worker/label.HostGatewayIP const
			if v, ok := w.Labels["org.mobyproject.buildkit.worker.moby.host-gateway-ip"]; ok && v != "" {
				ip := net.ParseIP(v)
				if ip == nil {
					d.hostGateway.err = errors.Errorf("failed to parse host-gateway IP: %s", v)
					return
				}
				d.hostGateway.ip = ip
				return
			}
		}
		d.hostGateway.err = errors.New("host-gateway IP not found")
	})
	return d.hostGateway.ip, d.hostGateway.err
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

type fallbackMode int

const (
	allowFallbackMode fallbackMode = iota
	disableFallbackMode
	forceFallbackMode
)

func (m fallbackMode) AttemptPrimaryClient() bool {
	return m != forceFallbackMode
}

func (m fallbackMode) AllowFallback() bool {
	return m == allowFallbackMode
}

func lazyListWorkers(c *client.Client) listWorkersFunc {
	var (
		workers     []*client.WorkerInfo
		workersOnce sync.Once
	)
	return func(ctx context.Context) []*client.WorkerInfo {
		workersOnce.Do(func() {
			workers, _ = c.ListWorkers(ctx)
		})
		return workers
	}
}
