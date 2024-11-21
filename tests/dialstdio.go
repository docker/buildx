package tests

import (
	"bytes"
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/docker/buildx/util/progress"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/llb"
	gwclient "github.com/moby/buildkit/frontend/gateway/client"
	"github.com/moby/buildkit/util/progress/progressui"
	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

var dialstdioTests = []func(t *testing.T, sb integration.Sandbox){
	testDialStdio,
}

func testDialStdio(t *testing.T, sb integration.Sandbox) {
	do := func(t *testing.T, pipe func(t *testing.T, cmd *exec.Cmd) net.Conn) {
		errBuf := bytes.NewBuffer(nil)
		defer func() {
			if t.Failed() {
				t.Log(errBuf.String())
			}
		}()
		var cmd *exec.Cmd
		c, err := client.New(sb.Context(), "", client.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			cmd = buildxCmd(sb, withArgs("dial-stdio", "--progress", "auto"))
			conn := pipe(t, cmd)
			cmd.Stderr = errBuf
			if err := cmd.Start(); err != nil {
				return nil, errors.Wrap(err, errBuf.String())
			}

			return conn, nil
		}))
		require.NoError(t, err)

		defer func() {
			c.Close()
			// Since the client is closed (and as such the connection shutdown), the buildx command should exit cleanly.
			chErr := make(chan error, 1)
			go func() {
				chErr <- cmd.Wait()
			}()
			select {
			case <-time.After(10 * time.Second):
				t.Error("timeout waiting for buildx command to exit")
			case <-chErr:
				require.NoError(t, err)
			}
		}()

		skipNoCompatBuildKit(t, sb, ">= 0.11.0-0", "unknown method Info for service moby.buildkit.v1.Control")
		_, err = c.Info(sb.Context())
		require.NoError(t, err)

		require.Contains(t, errBuf.String(), "builder: "+sb.Address())

		dir := t.TempDir()

		f, err := os.CreateTemp(dir, "log")
		require.NoError(t, err)
		defer f.Close()

		defer func() {
			if t.Failed() {
				dt, _ := os.ReadFile(f.Name())
				t.Log(string(dt))
			}
		}()

		p, err := progress.NewPrinter(sb.Context(), f, progressui.AutoMode)
		require.NoError(t, err)

		ch, chDone := progress.NewChannel(p)
		done := func() {
			select {
			case <-sb.Context().Done():
			case <-chDone:
			}
		}

		_, err = c.Build(sb.Context(), client.SolveOpt{
			Exports: []client.ExportEntry{
				{Type: "local", OutputDir: dir},
			},
		}, "", func(ctx context.Context, gwc gwclient.Client) (*gwclient.Result, error) {
			def, err := llb.Scratch().File(llb.Mkfile("hello", 0o600, []byte("world"))).Marshal(ctx)
			if err != nil {
				return nil, err
			}

			return gwc.Solve(ctx, gwclient.SolveRequest{
				Definition: def.ToPB(),
			})
		}, ch)
		done()
		require.NoError(t, err)

		dt, err := os.ReadFile(filepath.Join(dir, "hello"))
		require.NoError(t, err)
		require.Equal(t, "world", string(dt))
	}

	do(t, func(t *testing.T, cmd *exec.Cmd) net.Conn {
		c1, c2 := net.Pipe()
		cmd.Stdin = c1
		cmd.Stdout = c1
		return c2
	})
}
