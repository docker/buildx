package commands

import (
	"context"
	"io"

	"github.com/containerd/console"
	"github.com/docker/buildx/dap"
	"github.com/docker/buildx/util/cobrautil"
	"github.com/docker/buildx/util/ioset"
	"github.com/docker/buildx/util/progress"
	"github.com/docker/cli/cli/command"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

func dapCmd(dockerCli command.Cli, rootOpts *rootOptions) *cobra.Command {
	var options dapOptions
	cmd := &cobra.Command{
		Use:   "dap",
		Short: "Start debug adapter",
	}
	cobrautil.MarkCommandExperimental(cmd)

	flags := cmd.Flags()
	flags.StringVar(&options.OnFlag, "on", "error", "When to pause the adapter ([always, error])")

	cobrautil.MarkFlagsExperimental(flags, "on")

	cmd.AddCommand(buildCmd(dockerCli, rootOpts, &options))
	return cmd
}

type dapOptions struct {
	// OnFlag is a flag to configure the timing of launching the debugger.
	OnFlag string
}

func (d *dapOptions) New(in ioset.In) (debuggerInstance, error) {
	invokeConfig, err := parseInvokeConfig("", d.OnFlag)
	if err != nil {
		return nil, err
	}

	conn := dap.IoConn(readWriter{
		Reader: in.Stdin,
		Writer: in.Stdout,
	})
	return &adapterProtocolDebugger{
		Adapter: dap.New(invokeConfig),
		conn:    conn,
	}, nil
}

type adapterProtocolDebugger struct {
	*dap.Adapter
	conn dap.Conn
}

func (d *adapterProtocolDebugger) Start(printer *progress.Printer) error {
	if err := d.Adapter.Start(context.Background(), d.conn); err != nil {
		return errors.Wrap(err, "debug adapter did not start")
	}
	return nil
}

func (d *adapterProtocolDebugger) Out() console.File {
	return fakeConsole{Writer: d.Adapter.Out()}
}

type readWriter struct {
	io.Reader
	io.Writer
}

type fakeConsole struct {
	io.Writer
}

func (fakeConsole) Read(p []byte) (int, error) {
	return 0, io.EOF
}

func (fakeConsole) Close() error {
	return nil
}

func (fakeConsole) Fd() uintptr {
	return 0
}

func (fakeConsole) Name() string {
	return ""
}
