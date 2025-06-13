package commands

import (
	"github.com/docker/buildx/util/cobrautil"
	"github.com/docker/cli/cli/command"
	"github.com/spf13/cobra"
)

type debugOptions struct {
	// InvokeFlag is a flag to configure the launched debugger and the commaned executed on the debugger.
	InvokeFlag string

	// OnFlag is a flag to configure the timing of launching the debugger.
	OnFlag string
}

func debugCmd(dockerCli command.Cli, rootOpts *rootOptions) *cobra.Command {
	var options debugOptions

	cmd := &cobra.Command{
		Use:   "debug",
		Short: "Start debugger",
	}
	cobrautil.MarkCommandExperimental(cmd)

	flags := cmd.Flags()
	flags.StringVar(&options.InvokeFlag, "invoke", "", "Launch a monitor with executing specified command")
	flags.StringVar(&options.OnFlag, "on", "error", "When to launch the monitor ([always, error])")

	cobrautil.MarkFlagsExperimental(flags, "invoke", "on")

	cmd.AddCommand(buildCmd(dockerCli, rootOpts, &options))
	return cmd
}
