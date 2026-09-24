package dockerutil

import (
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWaitingWriterCloseWithoutWrite(t *testing.T) {
	pr, pw := io.Pipe()
	defer pr.Close()

	started := make(chan struct{})
	w := &waitingWriter{
		PipeWriter: pw,
		f: func() {
			close(started)
		},
		done: make(chan struct{}),
	}

	require.NoError(t, w.Close())
	select {
	case <-started:
		t.Fatal("loader should not start")
	default:
	}
}

func TestWaitingWriterCloseWaitsForLoader(t *testing.T) {
	pr, pw := io.Pipe()
	defer pr.Close()

	started := make(chan struct{})
	finish := make(chan struct{})
	done := make(chan struct{})
	w := &waitingWriter{
		PipeWriter: pw,
		f: func() {
			close(started)
			<-finish
			close(done)
		},
		done: done,
	}

	copyDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, pr)
		close(copyDone)
	}()

	_, err := w.Write([]byte("layer"))
	require.NoError(t, err)
	<-started

	closed := make(chan error, 1)
	go func() {
		closed <- w.Close()
	}()

	select {
	case err := <-closed:
		require.FailNow(t, "Close returned before loader completed", "err: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(finish)
	require.NoError(t, <-closed)
	<-copyDone
}
