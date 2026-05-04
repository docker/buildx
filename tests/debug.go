package tests

import (
	"bytes"
	"io"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/containerd/continuity/fs/fstest"
	"github.com/creack/pty"
	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/stretchr/testify/require"
)

var debugTests = []func(t *testing.T, sb integration.Sandbox){
	testDebugBuildMissingBindMountSource,
}

func testDebugBuildMissingBindMountSource(t *testing.T, sb integration.Sandbox) {
	if !isExperimental() {
		t.Skip("debug command is experimental")
	}
	if !isDockerWorker(sb) {
		t.Skip("debug monitor test only needs a docker worker")
	}

	dockerfile := []byte(`
FROM busybox:latest
RUN --mount=type=bind,source=missing,target=/src true
`)
	dir := tmpdir(t,
		fstest.CreateFile("Dockerfile", dockerfile, 0o600),
	)

	cmd := buildxCmd(sb, withArgs("debug", "--on=error", "build", "--progress=plain", "--output=type=cacheonly", dir))
	f, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: 120,
		Rows: 24,
	})
	require.NoError(t, err)
	defer f.Close()

	var output debugOutput
	copyDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(&output, f)
		close(copyDone)
	}()

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	exited, waitErr := waitForDebugPromptOrExit(t, &output, waitCh)
	if !exited {
		_, err = f.Write([]byte("exit\r"))
		require.NoError(t, err)
		waitErr = waitForDebugCommandExit(t, cmd, waitCh, &output)
	}

	_ = f.Close()
	select {
	case <-copyDone:
	case <-time.After(time.Second):
	}

	out := output.String()
	require.Error(t, waitErr, out)
	require.Contains(t, out, "failed to calculate checksum")
	require.Contains(t, out, "failed to create container")
	require.NotContains(t, out, "panic:")
	require.NotContains(t, out, "index out of range")
}

func waitForDebugPromptOrExit(t *testing.T, output *debugOutput, waitCh <-chan error) (bool, error) {
	t.Helper()

	timeout := time.NewTimer(time.Minute)
	defer timeout.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case err := <-waitCh:
			return true, err
		case <-ticker.C:
			if strings.Contains(output.String(), "(buildx) ") {
				return false, nil
			}
		case <-timeout.C:
			require.FailNow(t, "timeout waiting for debug monitor", output.String())
		}
	}
}

func waitForDebugCommandExit(t *testing.T, cmd *exec.Cmd, waitCh <-chan error, output *debugOutput) error {
	t.Helper()

	select {
	case err := <-waitCh:
		return err
	case <-time.After(30 * time.Second):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		require.FailNow(t, "timeout waiting for debug command to exit", output.String())
		return nil
	}
}

type debugOutput struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *debugOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *debugOutput) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
