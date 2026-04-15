package commands

import (
	"bytes"
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestProxyConnRemoteClose(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()

	stdin := &blockingReader{waitCh: make(chan struct{})}
	defer stdin.Close()

	var stdout bytes.Buffer
	errCh := make(chan error, 1)
	go func() {
		errCh <- proxyConn(context.Background(), clientConn, stdin, &stdout)
	}()

	go func() {
		_, _ = serverConn.Write([]byte("hello"))
		_ = serverConn.Close()
	}()

	select {
	case err := <-errCh:
		require.NoError(t, err)
		require.Equal(t, "hello", stdout.String())
	case <-time.After(2 * time.Second):
		t.Fatal("proxyConn did not return after the remote side closed")
	}
}

type blockingReader struct {
	waitCh    chan struct{}
	closeOnce sync.Once
}

func (r *blockingReader) Read([]byte) (int, error) {
	<-r.waitCh
	return 0, io.EOF
}

func (r *blockingReader) Close() {
	r.closeOnce.Do(func() {
		close(r.waitCh)
	})
}
