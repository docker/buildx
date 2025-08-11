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
	// SocketPath is set on the first time Init is invoked
	// and stays that way.
	SocketPath string

	// Locks access to the session from the debug adapter.
	// Only one debug thread can access the shell at a time.
	sem *semaphore.Weighted

	// Initialized once per shell and reused.
	once sync.Once
	err  error
	l    net.Listener
	eg   *errgroup.Group

	// For the specific session.
	fwd       *ioset.Forwarder
	connected chan struct{}
	mu        sync.RWMutex
}

func newShell() *shell {
	sh := &shell{
		sem: semaphore.NewWeighted(1),
	}
	sh.resetSession()
	return sh
}

func (s *shell) resetSession() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.fwd = nil
	s.connected = make(chan struct{})
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
		s.eg.Go(s.acceptLoop)
	})
	return s.err
}

func (s *shell) acceptLoop() error {
	for {
		if err := s.accept(); err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
	}
}

func (s *shell) accept() error {
	conn, err := s.l.Accept()
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.fwd != nil {
		writeLine(conn, "Error: Already connected to exec instance.")
		conn.Close()
		return nil
	}

	// Set the input of the forwarder to the connection.
	s.fwd = ioset.NewForwarder()
	s.fwd.SetIn(&ioset.In{
		Stdin:  io.NopCloser(conn),
		Stdout: conn,
		Stderr: nopCloser{conn},
	})

	close(s.connected)
	writeLine(conn, "Attached to build process.")
	return nil
}

// Attach will attach the given thread to the shell.
// Only one container can attach to a shell at any given time.
// Other attaches will block until the context is canceled or it is
// able to reserve the shell for its own use.
//
// This method is intended to be called by paused threads.
func (s *shell) Attach(ctx context.Context, t *thread) {
	rCtx := t.rCtx
	if rCtx == nil {
		return
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

	for {
		if err := s.attach(ctx, f, rCtx, cfg); err != nil {
			return
		}
	}
}

func (s *shell) wait(ctx context.Context) error {
	s.mu.RLock()
	connected := s.connected
	s.mu.RUnlock()

	select {
	case <-connected:
		return nil
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

func (s *shell) attach(ctx context.Context, f dap.StackFrame, rCtx *build.ResultHandle, cfg *build.InvokeConfig) (retErr error) {
	if err := s.wait(ctx); err != nil {
		return err
	}

	in, out := ioset.Pipe()
	defer in.Close()
	defer out.Close()

	s.mu.RLock()
	fwd := s.fwd
	s.mu.RUnlock()

	fwd.SetOut(&out)
	defer func() {
		if retErr != nil {
			fwd.SetOut(nil)
		}
	}()

	// Check if the entrypoint is executable. If it isn't, don't bother
	// trying to invoke.
	if reason, ok := s.canInvoke(ctx, rCtx, cfg); !ok {
		writeLineF(in.Stdout, "Build container is not executable. (reason: %s)", reason)
		<-ctx.Done()
		return context.Cause(ctx)
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

	writeLineF(in.Stdout, "Running %s in build container from line %d.",
		strings.Join(append(cfg.Entrypoint, cfg.Cmd...), " "),
		f.Line,
	)

	writeLine(in.Stdout, "Changes to the container will be reset after the next step is executed.")
	err = ctr.Exec(ctx, cfg, in.Stdin, in.Stdout, in.Stderr)

	// Send newline to properly terminate the output.
	writeLine(in.Stdout, "")

	if err != nil {
		return err
	}

	fwd.Close()
	s.resetSession()
	return nil
}

func (s *shell) canInvoke(ctx context.Context, rCtx *build.ResultHandle, cfg *build.InvokeConfig) (reason string, ok bool) {
	var cmd string
	if len(cfg.Entrypoint) > 0 {
		cmd = cfg.Entrypoint[0]
	} else if len(cfg.Cmd) > 0 {
		cmd = cfg.Cmd[0]
	}

	if cmd == "" {
		return "no command specified", false
	}

	st, err := rCtx.StatFile(ctx, cmd, cfg)
	if err != nil {
		return fmt.Sprintf("stat error: %s", err), false
	}

	mode := fs.FileMode(st.Mode)
	if !mode.IsRegular() {
		return fmt.Sprintf("%s: not a file", cmd), false
	}
	if mode&0111 == 0 {
		return fmt.Sprintf("%s: not an executable", cmd), false
	}
	return "", true
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

func writeLine(w io.Writer, msg string) {
	if os.PathSeparator == '\\' {
		fmt.Fprint(w, msg+"\r\n")
	} else {
		fmt.Fprintln(w, msg)
	}
}

func writeLineF(w io.Writer, format string, a ...any) {
	if os.PathSeparator == '\\' {
		fmt.Fprintf(w, format+"\r\n", a...)
	} else {
		fmt.Fprintf(w, format+"\n", a...)
	}
}
