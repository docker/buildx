package commands

import (
	"context"

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
		Short: "Start debug adapter protocol compatible debugger",
	}
	cobrautil.MarkCommandExperimental(cmd)

	flags := cmd.Flags()
	flags.StringVar(&options.OnFlag, "on", "error", "When to pause the adapter ([always, error])")

	cobrautil.MarkFlagsExperimental(flags, "on")

	dapBuildCmd := buildCmd(dockerCli, rootOpts, &options)
	dapBuildCmd.Args = cobra.RangeArgs(0, 1)
	cmd.AddCommand(dapBuildCmd)
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

	conn := dap.NewConn(in.Stdin, in.Stdout)
	return &adapterProtocolDebugger{
		Adapter: dap.New[LaunchConfig](invokeConfig),
		conn:    conn,
	}, nil
}

type LaunchConfig struct {
	Dockerfile  string `json:"dockerfile,omitempty"`
	ContextPath string `json:"contextPath,omitempty"`
	Target      string `json:"target,omitempty"`
}

type adapterProtocolDebugger struct {
	*dap.Adapter[LaunchConfig]
	conn dap.Conn
}

func (d *adapterProtocolDebugger) Start(printer *progress.Printer, opts *BuildOptions) error {
	cfg, err := d.Adapter.Start(context.Background(), d.conn)
	if err != nil {
		return errors.Wrap(err, "debug adapter did not start")
	}

	if cfg.Dockerfile != "" {
		opts.DockerfileName = cfg.Dockerfile
	}
	if cfg.ContextPath != "" {
		opts.ContextPath = cfg.ContextPath
	}
	return nil
}

func (d *adapterProtocolDebugger) Stop() error {
	defer d.conn.Close()
	return d.Adapter.Stop()
}
