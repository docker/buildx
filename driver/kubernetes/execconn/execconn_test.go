package execconn

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/remotecommand"
)

// fakeExecutor is a fake remotecommand.Executor whose StreamWithContext blocks until
// unblock is closed, then returns err. It stands in for the real SPDY exec stream
// to a builder pod, so the pipe-closing behavior in newExecConn can be tested
// without a real Kubernetes API server.
type fakeExecutor struct {
	unblock chan struct{}
	err     error
}

func (f *fakeExecutor) Stream(_ remotecommand.StreamOptions) error {
	panic("unimplemented")
}

func (f *fakeExecutor) StreamWithContext(ctx context.Context, _ remotecommand.StreamOptions) error {
	select {
	case <-f.unblock:
		return f.err
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

func TestNewExecConnPropagatesStreamEnd(t *testing.T) {
	t.Run("stream ends with an error", func(t *testing.T) {
		streamErr := errors.New("exec stream terminated")
		fe := &fakeExecutor{
			unblock: make(chan struct{}),
			err:     streamErr,
		}

		conn := newExecConn(context.Background(), fe)
		close(fe.unblock)

		_, err := conn.Read(make([]byte, 16))
		require.ErrorIs(t, err, streamErr)

		_, err = conn.Write([]byte("test"))
		require.ErrorIs(t, err, streamErr)
	})

	t.Run("stream ends with no error", func(t *testing.T) {
		fe := &fakeExecutor{unblock: make(chan struct{})}

		conn := newExecConn(context.Background(), fe)
		close(fe.unblock) // StreamWithContext returns nil

		_, err := conn.Read(make([]byte, 16))
		require.ErrorIs(t, err, io.EOF)

		_, err = conn.Write([]byte("test"))
		require.ErrorIs(t, err, io.ErrClosedPipe)
	})

	t.Run("stream still active: reads stay blocked, not closed early", func(t *testing.T) {
		fe := &fakeExecutor{unblock: make(chan struct{})}
		conn := newExecConn(context.Background(), fe)
		defer close(fe.unblock)

		done := make(chan struct{})
		go func() {
			buf := make([]byte, 16)
			_, _ = conn.Read(buf) //nolint:errcheck
			close(done)
		}()
		select {
		case <-done:
			t.Fatal("Read returned before the exec stream ended; it should still be blocked")
		case <-time.After(200 * time.Millisecond):
			// expected: still blocked, exactly like a real in-progress build
		}
	})

	t.Run("stream still active: writes stay blocked, not closed early", func(t *testing.T) {
		fe := &fakeExecutor{unblock: make(chan struct{})}
		conn := newExecConn(context.Background(), fe)
		defer close(fe.unblock)

		done := make(chan struct{})
		go func() {
			_, _ = conn.Write([]byte("test")) //nolint:errcheck
			close(done)
		}()
		select {
		case <-done:
			t.Fatal("Write returned before the exec stream ended; it should still be blocked")
		case <-time.After(200 * time.Millisecond):
			// expected: still blocked, exactly like a real in-progress build
		}
	})

	t.Run("stream cancelled by context", func(t *testing.T) {
		fe := &fakeExecutor{unblock: make(chan struct{})}
		ctx, cancel := context.WithCancelCause(context.Background())
		conn := newExecConn(ctx, fe)
		defer close(fe.unblock)

		cancel(context.Canceled)

		_, err := conn.Read(make([]byte, 16))
		require.ErrorIs(t, err, context.Canceled)

		_, err = conn.Write([]byte("test"))
		require.ErrorIs(t, err, context.Canceled)
	})
}
