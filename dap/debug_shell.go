package dap

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/docker/buildx/build"
	"github.com/docker/buildx/util/ioset"
	"github.com/docker/cli/cli-plugins/metadata"
	"github.com/google/go-dap"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
)

type shell struct {
	SocketPath string
	fwd        *ioset.Forwarder

	once sync.Once
	err  error

	l   net.Listener
	eg  *errgroup.Group
	sem *semaphore.Weighted

	connected chan struct{}
}

func newShell() *shell {
	return &shell{
		fwd:       ioset.NewForwarder(),
		sem:       semaphore.NewWeighted(1),
		connected: make(chan struct{}),
	}
}

// Init initializes the shell for connections on the client side.
// Attach will block until the terminal has been initialized.
func (s *shell) Init() error {
	return s.listen()
}

func (s *shell) listen() error {
	s.once.Do(func() {
		var dir string
		dir, s.err = os.MkdirTemp("", "buildx-dap-exec")
		if s.err != nil {
			return
		}
		defer func() {
			if s.err != nil {
				os.RemoveAll(dir)
			}
		}()
		s.SocketPath = filepath.Join(dir, "s.sock")

		s.l, s.err = net.Listen("unix", s.SocketPath)
		if s.err != nil {
			return
		}

		s.eg, _ = errgroup.WithContext(context.Background())
		s.eg.Go(func() error {
			conn, err := s.l.Accept()
			if err != nil {
				return err
			}
			fmt.Fprintln(conn, "Attached to build process.")

			// Set the input of the forwarder to the connection.
			s.fwd.SetIn(&ioset.In{
				Stdin:  io.NopCloser(conn),
				Stdout: conn,
				Stderr: nopCloser{conn},
			})
			close(s.connected)

			// Start a background goroutine to politely refuse any subsequent connections.
			for {
				conn, err := s.l.Accept()
				if err != nil {
					return nil
				}
				fmt.Fprint(conn, "Error: Already connected to exec instance.")
				conn.Close()
			}
		})
	})
	return s.err
}

// Attach will attach the given thread to the shell.
// Only one container can attach to a shell at any given time.
// Other attaches will block until the context is canceled or it is
// able to reserve the shell for its own use.
//
// This method is intended to be called by paused threads.
func (s *shell) Attach(ctx context.Context, t *thread) error {
	rCtx := t.rCtx
	if rCtx == nil {
		return nil
	}

	var f dap.StackFrame
	if len(t.stackTrace) > 0 {
		f = t.frames[t.stackTrace[0]].StackFrame
	}

	cfg := &build.InvokeConfig{Tty: true}
	if len(cfg.Entrypoint) == 0 && len(cfg.Cmd) == 0 {
		cfg.Entrypoint = []string{"/bin/sh"} // launch shell by default
		cfg.Cmd = []string{}
		cfg.NoCmd = false
	}
	return s.attach(ctx, f, rCtx, cfg)
}

func (s *shell) attach(ctx context.Context, f dap.StackFrame, rCtx *build.ResultHandle, cfg *build.InvokeConfig) (retErr error) {
	select {
	case <-s.connected:
	case <-ctx.Done():
		return context.Cause(ctx)
	}

	in, out := ioset.Pipe()
	defer in.Close()
	defer out.Close()

	s.fwd.SetOut(&out)
	defer s.fwd.SetOut(nil)

	// Check if the entrypoint is executable. If it isn't, don't bother
	// trying to invoke.
	if !s.canInvoke(ctx, rCtx, cfg) {
		fmt.Fprintln(in.Stdout, "Waiting for build container...")
		return nil
	}

	if err := s.sem.Acquire(ctx, 1); err != nil {
		return err
	}
	defer s.sem.Release(1)

	ctr, err := build.NewContainer(ctx, rCtx, cfg)
	if err != nil {
		return err
	}
	defer ctr.Cancel()

	fmt.Fprintf(in.Stdout, "Running %s in build container from line %d.\n",
		strings.Join(append(cfg.Entrypoint, cfg.Cmd...), " "),
		f.Line, f.Column,
	)
	fmt.Fprintln(in.Stdout, "Changes to the container will be reset after the next step is executed.")
	err = ctr.Exec(ctx, cfg, in.Stdin, in.Stdout, in.Stderr)

	// Send newline to properly terminate the output.
	fmt.Fprintln(in.Stdout)

	return err
}

func (s *shell) canInvoke(ctx context.Context, rCtx *build.ResultHandle, cfg *build.InvokeConfig) bool {
	var cmd string
	if len(cfg.Entrypoint) > 0 {
		cmd = cfg.Entrypoint[0]
	} else if len(cfg.Cmd) > 0 {
		cmd = cfg.Cmd[0]
	}

	if cmd == "" {
		return false
	}

	st, err := rCtx.StatFile(ctx, cmd, cfg)
	if err != nil {
		return false
	}

	mode := fs.FileMode(st.Mode)
	return mode.IsRegular() && mode&0111 != 0
}

// SendRunInTerminalRequest will send the request to the client to attach to
// the socket path that was created by Init. This is intended to be run
// from the adapter and interact directly with the client.
func (s *shell) SendRunInTerminalRequest(ctx Context) error {
	// TODO: this should work in standalone mode too.
	docker := os.Getenv(metadata.ReexecEnvvar)
	req := &dap.RunInTerminalRequest{
		Request: dap.Request{
			Command: "runInTerminal",
		},
		Arguments: dap.RunInTerminalRequestArguments{
			Kind: "integrated",
			Args: []string{docker, "buildx", "dap", "attach", s.SocketPath},
			Env: map[string]any{
				"BUILDX_EXPERIMENTAL": "1",
			},
		},
	}

	resp := ctx.Request(req)
	if !resp.GetResponse().Success {
		return errors.New(resp.GetResponse().Message)
	}
	return nil
}

type nopCloser struct {
	io.Writer
}

func (nopCloser) Close() error {
	return nil
}
