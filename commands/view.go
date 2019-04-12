package commands

import (
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/spf13/cobra"
)

type viewOptions struct {
}

func runView(dockerCli command.Cli, in viewOptions) error {
	return nil
}

func viewCmd(dockerCli command.Cli) *cobra.Command {
	var options viewOptions

	cmd := &cobra.Command{
		Use:   "view REF",
		Short: "Show metadata for image in registry",
		Args:  cli.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runView(dockerCli, options)
		},
	}

	flags := cmd.Flags()

	// flags.StringArrayVarP(&options.outputs, "output", "o", []string{}, "Output destination (format: type=local,dest=path)")

	_ = flags

	return cmd
}
