package dap

import (
	"fmt"

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
