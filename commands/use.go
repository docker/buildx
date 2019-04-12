package commands

import (
	"fmt"

	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/spf13/cobra"
)

type useOptions struct {
	isGlobal  bool
	isDefault bool
}

func runUse(dockerCli command.Cli, in useOptions) error {
	fmt.Printf("%+v\n", in)
	return nil
}

func useCmd(dockerCli command.Cli) *cobra.Command {
	var options useOptions

	cmd := &cobra.Command{
		Use:   "use [OPTIONS] NAME",
		Short: "Set the current builder instance",
		Args:  cli.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUse(dockerCli, options)
		},
	}

	flags := cmd.Flags()

	flags.BoolVar(&options.isGlobal, "global", false, "Builder persists context changes")
	flags.BoolVar(&options.isDefault, "default", false, "Set builder as default for current context")

	_ = flags

	return cmd
}
