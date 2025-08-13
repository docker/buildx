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
	"github.com/spf13/cobra"
)

func (d *Adapter[C]) Evaluate(ctx Context, req *dap.EvaluateRequest, resp *dap.EvaluateResponse) error {
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

	var retErr error
	cmd := d.replCommands(ctx, resp, &retErr)
	cmd.SetArgs(args)
	cmd.SetErr(d.Out())
	if err := cmd.Execute(); err != nil {
		// This error should only happen if there was something command
		// related that malfunctioned as it will also print usage.
		// Normal errors should set retErr from replCommands.
		return err
	}
	return retErr
}

func (d *Adapter[C]) replCommands(ctx Context, resp *dap.EvaluateResponse, retErr *error) *cobra.Command {
	rootCmd := &cobra.Command{
		SilenceErrors: true,
	}

	execCmd, _ := replCmd(ctx, "exec", resp, retErr, d.execCmd)
	rootCmd.AddCommand(execCmd)
	return rootCmd
}

type execOptions struct{}

func (d *Adapter[C]) execCmd(ctx Context, _ []string, _ execOptions) (string, error) {
	if !d.supportsExec {
		return "", errors.New("cannot exec without runInTerminal client capability")
	}

	// Initialize the shell if it hasn't been done before. This will allow any
	// containers that are attempting to attach to actually attach.
	if err := d.sh.Init(); err != nil {
		return "", err
	}

	// Send the request to attach to the terminal.
	if err := d.sh.SendRunInTerminalRequest(ctx); err != nil {
		return "", err
	}
	return fmt.Sprintf("Started process attached to %s.", d.sh.SocketPath), nil
}

func replCmd[Flags any, RetVal any](ctx Context, name string, resp *dap.EvaluateResponse, retErr *error, fn func(ctx Context, args []string, flags Flags) (RetVal, error)) (*cobra.Command, *Flags) {
	flags := new(Flags)
	return &cobra.Command{
		Use: name,
		Run: func(cmd *cobra.Command, args []string) {
			v, err := fn(ctx, args, *flags)
			if err != nil {
				*retErr = err
				return
			}
			resp.Body.Result = fmt.Sprint(v)
		},
	}, flags
}

func (t *thread) Exec(ctx Context, args []string) (message string, retErr error) {
	if t.rCtx == nil {
		return "", errors.New("no container context for exec")
	}

	cfg := &build.InvokeConfig{Tty: true}
	if len(cfg.Entrypoint) == 0 && len(cfg.Cmd) == 0 {
		cfg.Entrypoint = []string{"/bin/sh"} // launch shell by default
		cfg.Cmd = []string{}
		cfg.NoCmd = false
	}

	ctr, err := build.NewContainer(ctx, t.rCtx, cfg)
	if err != nil {
		return "", err
	}
	defer func() {
		if retErr != nil {
			ctr.Cancel()
		}
	}()

	dir, err := os.MkdirTemp("", "buildx-dap-exec")
	if err != nil {
		return "", err
	}
	defer func() {
		if retErr != nil {
			os.RemoveAll(dir)
		}
	}()

	socketPath := filepath.Join(dir, "s.sock")
	l, err := net.Listen("unix", socketPath)
	if err != nil {
		return "", err
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
		return "", errors.New(resp.GetResponse().Message)
	}

	message = fmt.Sprintf("Started process attached to %s.", socketPath)
	return message, nil
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
