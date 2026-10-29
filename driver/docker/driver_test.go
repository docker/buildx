package docker

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/docker/buildx/driver"
	_ "github.com/docker/buildx/driver/docker-container"
	control "github.com/moby/buildkit/api/services/control"
	types "github.com/moby/buildkit/api/types"
	dockerclient "github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type dialTestAPI struct {
	dockerclient.APIClient
	osType  string
	dialErr error
	pingErr error
	conn    net.Conn
}

func (a dialTestAPI) DialHijack(context.Context, string, string, map[string][]string) (net.Conn, error) {
	return a.conn, a.dialErr
}

func (a dialTestAPI) Ping(context.Context, dockerclient.PingOptions) (dockerclient.PingResult, error) {
	return dockerclient.PingResult{OSType: a.osType}, a.pingErr
}

func TestDefaultDriver(t *testing.T) {
	for _, tc := range []struct {
		name    string
		osType  string
		dialErr error
		pingErr error
		want    string
	}{
		{
			name:   "Windows with BuildKit",
			osType: "windows",
			want:   "docker",
		},
		{
			name:    "Windows without BuildKit",
			osType:  "windows",
			dialErr: errors.New("BuildKit unavailable"),
			want:    "docker",
		},
		{
			name:   "Linux with BuildKit",
			osType: "linux",
			want:   "docker",
		},
		{
			name:    "Linux without BuildKit",
			osType:  "linux",
			dialErr: errors.New("BuildKit unavailable"),
			want:    "docker-container",
		},
		{
			name:    "Unknown server OS",
			dialErr: errors.New("BuildKit unavailable"),
			pingErr: errors.New("ping failed"),
			want:    "docker-container",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := dialTestAPI{osType: tc.osType, dialErr: tc.dialErr, pingErr: tc.pingErr}
			if tc.dialErr == nil {
				conn, peer := net.Pipe()
				t.Cleanup(func() {
					conn.Close()
					peer.Close()
				})
				api.conn = conn
			}
			f, err := driver.GetDefaultFactory(t.Context(), "test-context", api, false, nil)
			require.NoError(t, err)
			require.Equal(t, tc.want, f.Name())
		})
	}
}

func TestDialError(t *testing.T) {
	dialErr := errors.New("access denied")
	for _, tc := range []struct {
		name     string
		osType   string
		pingErr  error
		wantHint bool
	}{
		{
			name:     "Windows",
			osType:   "windows",
			wantHint: true,
		},
		{
			name:   "Linux",
			osType: "linux",
		},
		{
			name:    "Ping failure",
			pingErr: errors.New("ping failed"),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &Driver{InitConfig: driver.InitConfig{DockerAPI: dialTestAPI{
				osType:  tc.osType,
				dialErr: dialErr,
				pingErr: tc.pingErr,
			}}}
			conn, err := d.Dial(t.Context())
			require.Nil(t, conn)
			require.ErrorIs(t, err, dialErr)
			if tc.wantHint {
				require.ErrorContains(t, err, "containerd image store and BuildKit enabled")
				require.ErrorContains(t, err, "remote Windows BuildKit builder")
			} else {
				require.Equal(t, dialErr, err)
			}
		})
	}
}

type featureTestAPI struct {
	dockerclient.APIClient
	addr string
}

func (a featureTestAPI) DialHijack(ctx context.Context, _, _ string, _ map[string][]string) (net.Conn, error) {
	var dialer net.Dialer
	return dialer.DialContext(ctx, "tcp", a.addr)
}

type featureTestControl struct {
	control.UnimplementedControlServer
	calls     atomic.Int32
	supported bool
}

func (c *featureTestControl) ListWorkers(context.Context, *control.ListWorkersRequest) (*control.ListWorkersResponse, error) {
	if c.calls.Add(1) == 1 {
		return nil, status.Error(codes.Unavailable, "worker is starting")
	}
	w := &types.WorkerRecord{}
	if c.supported {
		w.Labels = map[string]string{"org.mobyproject.buildkit.worker.snapshotter": "overlayfs"}
	}
	return &control.ListWorkersResponse{Record: []*types.WorkerRecord{w}}, nil
}

func TestFeaturesRetry(t *testing.T) {
	for _, supported := range []bool{false, true} {
		t.Run(map[bool]string{false: "unsupported", true: "supported"}[supported], func(t *testing.T) {
			ctx, cancel := context.WithTimeoutCause(t.Context(), 10*time.Second, context.DeadlineExceeded)
			defer cancel()
			var lc net.ListenConfig
			listener, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
			require.NoError(t, err)
			defer listener.Close()
			server := grpc.NewServer()
			defer server.Stop()
			ctl := &featureTestControl{supported: supported}
			control.RegisterControlServer(server, ctl)
			go server.Serve(listener)
			d := &Driver{InitConfig: driver.InitConfig{DockerAPI: featureTestAPI{addr: listener.Addr().String()}}}
			features, err := d.Features(ctx)
			require.ErrorContains(t, err, "listing workers")
			require.Equal(t, codes.Unavailable, status.Code(err))
			require.Nil(t, features)
			for range 2 {
				features, err = d.Features(ctx)
				require.NoError(t, err)
				require.True(t, features[driver.DefaultLoad])
				for _, f := range []driver.Feature{driver.OCIExporter, driver.DockerExporter, driver.CacheExport, driver.MultiPlatform, driver.DirectPush, driver.PreferImageDigest} {
					require.Equal(t, supported, features[f], f)
				}
			}
			require.EqualValues(t, 2, ctl.calls.Load())
		})
	}
}
