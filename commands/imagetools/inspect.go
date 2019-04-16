package commands

import (
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type inspectOptions struct {
	raw bool
}

func runInspect(dockerCli command.Cli, in inspectOptions, name string) error {
	return errors.Errorf("not-implemented")
}

func inspectCmd(dockerCli command.Cli) *cobra.Command {
	var options inspectOptions

	cmd := &cobra.Command{
		Use:   "inspect [OPTIONS] NAME",
		Short: "Show details of image in the registry",
		Args:  cli.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInspect(dockerCli, options, args[0])
		},
	}

	flags := cmd.Flags()

	flags.BoolVar(&options.raw, "raw", false, "Show original JSON manifest")

	_ = flags

	return cmd
}
