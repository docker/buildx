package commands

import (
	"github.com/docker/cli/cli/command"
	"github.com/spf13/cobra"
)

type RootOptions struct {
	Builder *string
}

func RootCmd(dockerCli command.Cli, opts RootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "imagetools",
		Short: "Commands to work on images in registry",
	}

	cmd.AddCommand(
		createCmd(dockerCli, opts),
		inspectCmd(dockerCli, opts),
	)

	return cmd
}
