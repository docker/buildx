package docker

import (
	"context"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/docker/buildx/driver"
	"github.com/docker/buildx/util/progress"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/util/flightcontrol"
	"github.com/moby/buildkit/util/grpcerrors"
	dockerclient "github.com/moby/moby/client"
	"github.com/moby/moby/client/pkg/versions"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
)

type Driver struct {
	factory driver.Factory
	driver.InitConfig

	// if you add fields, remember to update docs:
	// https://github.com/docker/docs/blob/main/content/build/drivers/docker.md
	features    features
	hostGateway hostGateway
	nativeGRPC  flightcontrol.CachedGroup[bool]
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
	// TODO: Support native gRPC with Desktop Resource Saver metadata. Keep /grpc
	//  so Desktop's proxy can identify background connections and let the VM sleep.
	//  See https://github.com/docker/desktop-build/pull/312.
	if len(d.DialMeta) == 0 {
		if err := context.Cause(ctx); err != nil {
			return nil, err
		}
		native, err := d.nativeGRPC.Do(ctx, "", d.probeNativeGRPC)
		if cause := context.Cause(ctx); cause != nil {
			return nil, cause
		}
		if err == nil && native {
			return client.New(ctx, d.DockerAPI.DaemonHost(), append(d.nativeClientOpts(), opts...)...)
		}
		if err != nil {
			switch grpcerrors.Code(err) {
			case codes.PermissionDenied, codes.Unauthenticated:
				return nil, err
			}
			logrus.Debugf("docker driver: native gRPC unavailable, using /grpc: %v", err)
		}
	}

	// Legacy transport for daemons and proxies without native gRPC:
	// https://github.com/moby/moby/pull/50744
	opts = append([]client.ClientOpt{
		client.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return d.Dial(ctx)
		}),
		client.WithSessionDialer(func(ctx context.Context, proto string, meta map[string][]string) (net.Conn, error) {
			return d.DockerAPI.DialHijack(ctx, "/session", proto, meta)
		}),
	}, opts...)
	return client.New(ctx, "", opts...)
}

func (d *Driver) probeNativeGRPC(ctx context.Context) (bool, error) {
	// Remote endpoints keep Docker's configured transport, including TLS and SSH.
	scheme, _, _ := strings.Cut(d.DockerAPI.DaemonHost(), "://")
	if scheme != "unix" && scheme != "npipe" {
		return false, nil
	}

	ctx, cancel := context.WithTimeoutCause(ctx, 10*time.Second, errors.New("native gRPC probe timed out"))
	defer cancel()

	ping, err := d.DockerAPI.Ping(ctx, dockerclient.PingOptions{})
	if err != nil {
		return false, err
	}
	// Engine 29.2 (API 1.53) introduced native gRPC; proxies may still reject it.
	if ping.APIVersion == "" || versions.LessThan(ping.APIVersion, "1.53") {
		return false, nil
	}
	c, err := client.New(ctx, d.DockerAPI.DaemonHost(), d.nativeClientOpts()...)
	if err != nil {
		return false, err
	}
	defer c.Close()
	if _, err := c.ListWorkers(ctx); err != nil {
		return false, err
	}
	logrus.Debug("docker driver: using native gRPC")
	return true, nil
}

func (d *Driver) nativeClientOpts() []client.ClientOpt {
	dial := d.DockerAPI.Dialer()
	return []client.ClientOpt{
		client.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return dial(ctx)
		}),
	}
}

type features struct {
	once sync.Once
	list map[driver.Feature]bool
}

func (d *Driver) Features(ctx context.Context) map[driver.Feature]bool {
	d.features.once.Do(func() {
		var useContainerdSnapshotter bool
		if c, err := d.Client(ctx); err == nil {
			workers, _ := c.ListWorkers(ctx)
			for _, w := range workers {
				if _, ok := w.Labels["org.mobyproject.buildkit.worker.snapshotter"]; ok {
					useContainerdSnapshotter = true
				}
			}
			c.Close()
		}
		d.features.list = map[driver.Feature]bool{
			driver.OCIExporter:       useContainerdSnapshotter,
			driver.DockerExporter:    useContainerdSnapshotter,
			driver.CacheExport:       useContainerdSnapshotter,
			driver.MultiPlatform:     useContainerdSnapshotter,
			driver.DirectPush:        useContainerdSnapshotter,
			driver.PreferImageDigest: useContainerdSnapshotter,
			driver.DefaultLoad:       true,
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
