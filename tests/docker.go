package tests

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/containerd/continuity/fs/fstest"
	"github.com/docker/buildx/driver"
	dockerdriver "github.com/docker/buildx/driver/docker"
	"github.com/docker/buildx/util/dockerutil"
	"github.com/docker/cli/cli/command"
	cliflags "github.com/docker/cli/cli/flags"
	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/moby/moby/client/pkg/versions"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

var dockerTests = []func(t *testing.T, sb integration.Sandbox){
	testDockerNativeGRPC,
	testDockerNativeGRPCClients,
}

func testDockerNativeGRPC(t *testing.T, sb integration.Sandbox) {
	if !isDockerWorker(sb) {
		t.Skip("only testing with docker worker")
	}
	out, err := dockerCmd(sb, withArgs("version", "--format", "{{.Server.APIVersion}}")).Output()
	require.NoError(t, err)
	native := !versions.LessThan(strings.TrimSpace(string(out)), "1.53")
	dir := tmpdir(t,
		fstest.CreateFile("Dockerfile", []byte("FROM scratch\nCOPY data /data\n"), 0600),
		fstest.CreateFile("data", []byte(t.Name()), 0600),
	)
	dest := t.TempDir()
	buildOut, err := buildCmd(sb, withArgs("--debug", "--output=type=local,dest="+dest, dir))
	require.NoError(t, err, buildOut)
	data, err := os.ReadFile(filepath.Join(dest, "data"))
	require.NoError(t, err)
	require.Equal(t, t.Name(), string(data))
	if native {
		require.Contains(t, buildOut, "docker driver: using native gRPC")
	} else {
		require.NotContains(t, buildOut, "docker driver: using native gRPC")
	}
}

func testDockerNativeGRPCClients(t *testing.T, sb integration.Sandbox) {
	if !isDockerWorker(sb) {
		t.Skip("only testing with docker worker")
	}
	cli, err := command.NewDockerCli()
	require.NoError(t, err)
	opts := cliflags.NewClientOptions()
	opts.Context = sb.DockerAddress()
	require.NoError(t, cli.Initialize(opts))
	api, err := dockerutil.NewClientAPI(cli, sb.DockerAddress())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, api.Close()) })
	d := &dockerdriver.Driver{InitConfig: driver.InitConfig{DockerAPI: api}}
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(context.Canceled)
	_, err = d.Client(ctx)
	require.ErrorIs(t, err, context.Canceled)

	ctx, stop := context.WithTimeoutCause(t.Context(), 30*time.Second, context.DeadlineExceeded)
	defer stop()
	var eg errgroup.Group
	for range 16 {
		eg.Go(func() error {
			c, err := d.Client(ctx)
			if err != nil {
				return err
			}
			defer c.Close()
			_, err = c.ListWorkers(ctx)
			return err
		})
	}
	require.NoError(t, eg.Wait())
	require.True(t, d.Features(ctx)[driver.DefaultLoad])
	ip, err := d.HostGatewayIP(ctx)
	require.NoError(t, err)
	require.NotNil(t, ip)
}
