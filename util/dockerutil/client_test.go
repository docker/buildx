package dockerutil

import (
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWaitingWriterCloseWithoutWrite(t *testing.T) {
	t.Parallel()

	pr, pw := io.Pipe()
	defer pr.Close()
	done := make(chan struct{})
	w := &waitingWriter{
		PipeWriter: pw,
		f: func() {
			t.Error("loader should not start when Close runs before Write")
		},
		done: done,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- w.Close()
	}()

	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Close hung when Write was never called")
	}
}

func TestWaitingWriterCloseWaitsForLoader(t *testing.T) {
	t.Parallel()

	pr, pw := io.Pipe()
	done := make(chan struct{})
	started := make(chan struct{})
	w := &waitingWriter{
		PipeWriter: pw,
		f: func() {
			close(started)
			_, _ = io.Copy(io.Discard, pr)
			time.Sleep(80 * time.Millisecond)
			close(done)
		},
		done: done,
	}

	n, err := w.Write([]byte("layer"))
	require.NoError(t, err)
	require.Equal(t, 5, n)
	<-started

	start := time.Now()
	require.NoError(t, w.Close())
	require.GreaterOrEqual(t, time.Since(start), 60*time.Millisecond)
}
