package commands

import (
	"github.com/docker/cli/cli/command"
	"github.com/spf13/cobra"
)

func RootCmd(dockerCli command.Cli) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "imagetools",
		Short: "Commands to work on images in registry",
	}

	cmd.AddCommand(
		inspectCmd(dockerCli),
		createCmd(dockerCli),
	)

	return cmd
}
