package gitutil

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

type gitServe struct {
	token string
}

type GitServeOpt func(*gitServe)

func WithAccessToken(token string) GitServeOpt {
	return func(s *gitServe) {
		s.token = token
	}
}

func GitServeHTTP(c *Git, t testing.TB, opts ...GitServeOpt) (url string) {
	t.Helper()
	gitUpdateServerInfo(c, t)
	ctx, cancel := context.WithCancelCause(context.TODO())

	gs := &gitServe{}
	for _, opt := range opts {
		opt(gs)
	}

	ready := make(chan struct{})
	done := make(chan struct{})

	name := "test.git"
	dir, err := c.GitDir()
	if err != nil {
		cancel(err)
	}

	var addr string
	go func() {
		mux := http.NewServeMux()
		prefix := fmt.Sprintf("/%s/", name)

		handler := func(next http.Handler) http.Handler {
			var tokenChecked bool
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if gs.token != "" && !tokenChecked {
					t.Logf("git access token to check: %q", gs.token)
					user, pass, _ := r.BasicAuth()
					t.Logf("basic auth: user=%q pass=%q", user, pass)
					if pass != gs.token {
						http.Error(w, "Unauthorized", http.StatusUnauthorized)
						return
					}
					tokenChecked = true
				}
				next.ServeHTTP(w, r)
			})
		}

		mux.Handle(prefix, handler(http.StripPrefix(prefix, http.FileServer(http.Dir(dir)))))
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
		cancel(errors.Errorf("cleanup"))
		<-done
	})
	return fmt.Sprintf("http://%s/%s", addr, name)
}

func gitUpdateServerInfo(c *Git, tb testing.TB) {
	tb.Helper()
	_, err := fakeGit(c, "update-server-info")
	require.NoError(tb, err)
}
