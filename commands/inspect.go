package commands

import (
	"fmt"

	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/spf13/cobra"
)

type inspectOptions struct {
	bootstrap bool
}

func runInspect(dockerCli command.Cli, in inspectOptions) error {
	fmt.Printf("%+v\n", in)
	return nil
}

func inspectCmd(dockerCli command.Cli) *cobra.Command {
	var options inspectOptions

	cmd := &cobra.Command{
		Use:   "inspect [NAME]",
		Short: "Inspect current builder instance",
		Args:  cli.RequiresMaxArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInspect(dockerCli, options)
		},
	}

	flags := cmd.Flags()

	flags.BoolVar(&options.bootstrap, "bootstrap", false, "Ensure builder has booted before inspecting")

	_ = flags

	return cmd
}
