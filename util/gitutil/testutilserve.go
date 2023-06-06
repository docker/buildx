package gitutil

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func GitServeHTTP(c *Git, t testing.TB) (url string) {
	t.Helper()
	gitUpdateServerInfo(c, t)
	ctx, cancel := context.WithCancel(context.TODO())

	ready := make(chan struct{})
	done := make(chan struct{})

	name := "test.git"
	dir, err := c.GitDir()
	if err != nil {
		cancel()
	}

	var addr string
	go func() {
		mux := http.NewServeMux()
		prefix := fmt.Sprintf("/%s/", name)
		mux.Handle(prefix, http.StripPrefix(prefix, http.FileServer(http.Dir(dir))))
		l, err := net.Listen("tcp", "localhost:0")
		if err != nil {
			panic(err)
		}

		addr = l.Addr().String()

		close(ready)

		s := http.Server{Handler: mux} //nolint:gosec // potential attacks are not relevant for tests
		go s.Serve(l)
		<-ctx.Done()
		s.Shutdown(context.TODO())
		l.Close()

		close(done)
	}()
	<-ready

	t.Cleanup(func() {
		cancel()
		<-done
	})
	return fmt.Sprintf("http://%s/%s", addr, name)
}

func gitUpdateServerInfo(c *Git, tb testing.TB) {
	tb.Helper()
	_, err := fakeGit(c, "update-server-info")
	require.NoError(tb, err)
}
