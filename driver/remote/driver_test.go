package remote

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/docker/buildx/driver"
	"github.com/moby/buildkit/client"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// TestClientAuthorityValue exercises the authority derivation logic: the
// endpoint host is used by default, and a configured servername takes
// precedence (it is also used for TLS SNI). See docker/buildx#3880.
func TestClientAuthorityValue(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		tls      *tlsOpts
		expected string
	}{
		{
			name:     "tcp endpoint without tls",
			endpoint: "tcp://my-buildkit.example.com:443",
			expected: "my-buildkit.example.com:443",
		},
		{
			name:     "servername takes precedence over endpoint host",
			endpoint: "tcp://10.0.0.5:443",
			tls:      &tlsOpts{serverName: "my-buildkit.example.com"},
			expected: "my-buildkit.example.com",
		},
		{
			name:     "unix endpoint has no authority",
			endpoint: "unix:///run/buildkit/buildkitd.sock",
			expected: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Driver{
				InitConfig: driver.InitConfig{EndpointAddr: tt.endpoint},
				tlsOpts:    tt.tls,
			}
			require.Equal(t, tt.expected, d.clientAuthority())
		})
	}
}

// TestClientAuthority verifies end-to-end that the remote driver sends the
// configured endpoint address as the gRPC ":authority" pseudo-header instead
// of defaulting to "localhost" (see docker/buildx#3880).
func TestClientAuthority(t *testing.T) {
	ctx, cancel := context.WithTimeoutCause(context.Background(), 10*time.Second, context.DeadlineExceeded)
	defer cancel()

	addr, authorityCh := startAuthorityServer(ctx, t)

	d := &Driver{
		InitConfig: driver.InitConfig{EndpointAddr: "tcp://" + addr},
	}

	c, err := d.Client(ctx)
	require.NoError(t, err)
	defer c.Close()

	// Any RPC will do: it fails server-side with Unimplemented, but the server
	// records the ":authority" it received from the client before responding.
	_, _ = c.ListWorkers(ctx)

	require.Equal(t, addr, waitAuthority(ctx, t, authorityCh))
}

// TestClientAuthorityCallerOverride verifies that an authority explicitly
// passed by the caller takes precedence over the driver's default one.
func TestClientAuthorityCallerOverride(t *testing.T) {
	ctx, cancel := context.WithTimeoutCause(context.Background(), 10*time.Second, context.DeadlineExceeded)
	defer cancel()

	addr, authorityCh := startAuthorityServer(ctx, t)

	d := &Driver{
		InitConfig: driver.InitConfig{EndpointAddr: "tcp://" + addr},
	}

	c, err := d.Client(ctx, client.WithGRPCDialOption(grpc.WithAuthority("caller.example.com")))
	require.NoError(t, err)
	defer c.Close()

	_, _ = c.ListWorkers(ctx)

	require.Equal(t, "caller.example.com", waitAuthority(ctx, t, authorityCh))
}

// startAuthorityServer stands up an in-process gRPC server on a loopback
// listener that records the ":authority" pseudo-header of the request it
// receives. It returns the listener address and a channel delivering that
// authority.
func startAuthorityServer(ctx context.Context, t *testing.T) (string, <-chan string) {
	t.Helper()

	lc := net.ListenConfig{}
	lis, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	authorityCh := make(chan string, 1)
	srv := grpc.NewServer(grpc.UnknownServiceHandler(func(_ any, stream grpc.ServerStream) error {
		authority := ""
		if md, ok := metadata.FromIncomingContext(stream.Context()); ok {
			if a := md.Get(":authority"); len(a) > 0 {
				authority = a[0]
			}
		}
		select {
		case authorityCh <- authority:
		default:
		}
		return status.Error(codes.Unimplemented, "unimplemented")
	}))
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(srv.Stop)

	return lis.Addr().String(), authorityCh
}

func waitAuthority(ctx context.Context, t *testing.T, authorityCh <-chan string) string {
	t.Helper()
	select {
	case authority := <-authorityCh:
		return authority
	case <-ctx.Done():
		t.Fatal("timed out waiting for request to reach the server")
		return ""
	}
}
