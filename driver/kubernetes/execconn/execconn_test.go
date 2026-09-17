package execconn

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/remotecommand"
)

// fakeExecutor stands in for the real SPDY exec stream. The connection is only
// considered established once StreamWithContext reads stdin, matching the real
// executor which copies stdin to the pod once all its streams are created. It
// reads stdin just enough to signal readiness and then stops consuming it, so a
// write after the stream ends observes the closed pipe rather than being read.
type fakeExecutor struct {
	// startErr, if set, is returned before the stream is established, without ever
	// reading stdin (a failed SPDY/TLS upgrade).
	startErr error
	// stall, if set, blocks without establishing the stream until the context is
	// cancelled (an upgrade that never completes).
	stall bool
	// echo, if set, copies stdin to stdout once the stream is established.
	echo bool
	// unblock ends an established stream when closed; endErr is then returned.
	unblock chan struct{}
	endErr  error
	// finished is closed when StreamWithContext returns.
	finished chan struct{}
	// readerDone is closed when the stdin reader has stopped (non-echo streams).
	readerDone chan struct{}
}

func (f *fakeExecutor) Stream(_ remotecommand.StreamOptions) error {
	panic("unimplemented")
}

func (f *fakeExecutor) StreamWithContext(ctx context.Context, opts remotecommand.StreamOptions) error {
	if f.finished != nil {
		defer close(f.finished)
	}

	if f.startErr != nil {
		return f.startErr
	}
	if f.stall {
		<-ctx.Done()
		return context.Cause(ctx)
	}

	go func() {
		if f.echo {
			_, _ = io.Copy(opts.Stdout, opts.Stdin)
			return
		}
		if f.readerDone != nil {
			defer close(f.readerDone)
		}
		// A single read is enough to signal readiness; stop consuming afterwards.
		_, _ = opts.Stdin.Read(make([]byte, 1))
	}()

	select {
	case <-f.unblock:
		return f.endErr
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

func TestNewExecConnReadyWithData(t *testing.T) {
	fe := &fakeExecutor{
		echo:     true,
		unblock:  make(chan struct{}),
		finished: make(chan struct{}),
	}
	defer close(fe.unblock)

	conn, err := newExecConn(context.Background(), fe)
	require.NoError(t, err)
	require.NotNil(t, conn)

	_, err = conn.Write([]byte("ping"))
	require.NoError(t, err)

	buf := make([]byte, 4)
	_, err = io.ReadFull(conn, buf)
	require.NoError(t, err)
	require.Equal(t, "ping", string(buf))
}

func TestNewExecConnStartupFailure(t *testing.T) {
	startErr := errors.New("unable to upgrade connection: remote error: tls: internal error")
	fe := &fakeExecutor{
		startErr: startErr,
		finished: make(chan struct{}),
	}

	type result struct {
		conn net.Conn
		err  error
	}
	done := make(chan result, 1)
	go func() {
		conn, err := newExecConn(context.Background(), fe)
		done <- result{conn, err}
	}()

	select {
	case r := <-done:
		require.ErrorIs(t, r.err, startErr)
		require.Nil(t, r.conn)
	case <-time.After(5 * time.Second):
		t.Fatal("newExecConn blocked on a startup failure instead of returning the error")
	}

	select {
	case <-fe.finished:
	case <-time.After(time.Second):
		t.Fatal("executor goroutine leaked after a startup failure")
	}
}

func TestNewExecConnContextCancellation(t *testing.T) {
	fe := &fakeExecutor{
		stall:    true,
		finished: make(chan struct{}),
	}
	ctx, cancel := context.WithCancelCause(context.Background())

	type result struct {
		conn net.Conn
		err  error
	}
	done := make(chan result, 1)
	go func() {
		conn, err := newExecConn(ctx, fe)
		done <- result{conn, err}
	}()

	cancel(context.Canceled)

	select {
	case r := <-done:
		require.ErrorIs(t, r.err, context.Canceled)
		require.Nil(t, r.conn)
	case <-time.After(5 * time.Second):
		t.Fatal("newExecConn did not unblock on context cancellation")
	}

	select {
	case <-fe.finished:
	case <-time.After(time.Second):
		t.Fatal("executor goroutine leaked after context cancellation")
	}
}

func TestNewExecConnStreamEndPropagates(t *testing.T) {
	t.Run("stream ends with an error", func(t *testing.T) {
		streamErr := errors.New("exec stream terminated")
		fe := &fakeExecutor{
			unblock:    make(chan struct{}),
			endErr:     streamErr,
			readerDone: make(chan struct{}),
		}

		conn, err := newExecConn(context.Background(), fe)
		require.NoError(t, err)

		close(fe.unblock)
		waitClosed(t, fe.readerDone)

		_, err = conn.Read(make([]byte, 16))
		require.ErrorIs(t, err, streamErr)

		_, err = conn.Write([]byte("test"))
		require.ErrorIs(t, err, streamErr)
	})

	t.Run("stream ends with no error", func(t *testing.T) {
		fe := &fakeExecutor{
			unblock:    make(chan struct{}),
			readerDone: make(chan struct{}),
		}

		conn, err := newExecConn(context.Background(), fe)
		require.NoError(t, err)

		close(fe.unblock)
		waitClosed(t, fe.readerDone)

		_, err = conn.Read(make([]byte, 16))
		require.ErrorIs(t, err, io.EOF)

		_, err = conn.Write([]byte("test"))
		require.ErrorIs(t, err, io.ErrClosedPipe)
	})

	t.Run("stream still active: reads stay blocked", func(t *testing.T) {
		fe := &fakeExecutor{unblock: make(chan struct{})}
		conn, err := newExecConn(context.Background(), fe)
		require.NoError(t, err)
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
		}
	})
}

func waitClosed(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("fake executor kept reading stdin after the stream ended")
	}
}
