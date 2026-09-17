package docker

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/docker/buildx/driver"
	control "github.com/moby/buildkit/api/services/control"
	types "github.com/moby/buildkit/api/types"
	dockerclient "github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

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
