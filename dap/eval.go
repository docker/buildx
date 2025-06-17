package dap

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/docker/buildx/build"
	"github.com/docker/cli/cli-plugins/metadata"
	"github.com/google/go-dap"
	"github.com/google/shlex"
	"github.com/pkg/errors"
)

func (d *Adapter) Evaluate(ctx Context, req *dap.EvaluateRequest, resp *dap.EvaluateResponse) error {
	if req.Arguments.Context != "repl" {
		return errors.Errorf("unsupported evaluate context: %s", req.Arguments.Context)
	}

	args, err := shlex.Split(req.Arguments.Expression)
	if err != nil {
		return errors.Wrapf(err, "cannot parse expression")
	}

	if len(args) == 0 {
		return nil
	}

	switch arg0 := args[0]; arg0 {
	case "exec":
		if !d.supportsExec {
			return errors.New("dap client does not support runInTerminalRequest")
		}

		t := d.getCurrentThread()
		if t == nil {
			return errors.New("no paused thread for exec command")
		}

		argv := args[1:]
		if err := t.Exec(ctx, argv, resp); err != nil {
			return err
		}
		return nil
	default:
		return errors.Errorf("unknown evalute command: %q", arg0)
	}
}

func (t *thread) Exec(ctx Context, args []string, eresp *dap.EvaluateResponse) (retErr error) {
	cfg := &build.InvokeConfig{Tty: true}
	if len(cfg.Entrypoint) == 0 && len(cfg.Cmd) == 0 {
		cfg.Entrypoint = []string{"/bin/sh"} // launch shell by default
		cfg.Cmd = []string{}
		cfg.NoCmd = false
	}

	ctr, err := build.NewContainer(ctx, t.rCtx, cfg)
	if err != nil {
		return err
	}
	defer func() {
		if retErr != nil {
			ctr.Cancel()
		}
	}()

	dir, err := os.MkdirTemp("", "buildx-dap-exec")
	if err != nil {
		return err
	}
	defer func() {
		if retErr != nil {
			os.RemoveAll(dir)
		}
	}()

	socketPath := filepath.Join(dir, "s.sock")
	l, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}

	go func() {
		defer os.RemoveAll(dir)
		t.runExec(l, ctr, cfg)
	}()

	// TODO: this should work in standalone mode too.
	docker := os.Getenv(metadata.ReexecEnvvar)
	req := &dap.RunInTerminalRequest{
		Request: dap.Request{
			Command: "runInTerminal",
		},
		Arguments: dap.RunInTerminalRequestArguments{
			Kind: "integrated",
			Args: []string{docker, "buildx", "dap", "attach", socketPath},
			Env: map[string]any{
				"BUILDX_EXPERIMENTAL": "1",
			},
		},
	}

	resp := ctx.Request(req)
	if !resp.GetResponse().Success {
		return errors.New(resp.GetResponse().Message)
	}

	eresp.Body.Result = fmt.Sprintf("Started process attached to %s.", socketPath)
	return nil
}

func (t *thread) runExec(l net.Listener, ctr *build.Container, cfg *build.InvokeConfig) {
	defer l.Close()
	defer ctr.Cancel()

	conn, err := l.Accept()
	if err != nil {
		return
	}
	defer conn.Close()

	// start a background goroutine to politely refuse any subsequent connections.
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			fmt.Fprint(conn, "Error: Already connected to exec instance.")
			conn.Close()
		}
	}()
	ctr.Exec(context.Background(), cfg, conn, conn, conn)
}
