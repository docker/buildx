package commands

import (
	"github.com/docker/cli/cli/command"
	"github.com/spf13/cobra"
)

type rmOptions struct {
}

func runRm(dockerCli command.Cli, in rmOptions) error {
	return nil
}

func rmCmd(dockerCli command.Cli) *cobra.Command {
	var options rmOptions

	cmd := &cobra.Command{
		Use:   "rm [NAME]",
		Short: "Remove a builder instance",
		// Args:  cli.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRm(dockerCli, options)
		},
	}

	flags := cmd.Flags()

	// flags.StringArrayVarP(&options.outputs, "output", "o", []string{}, "Output destination (format: type=local,dest=path)")

	_ = flags

	return cmd
}
