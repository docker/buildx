package commands

import (
	"fmt"

	"github.com/docker/buildx/version"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/spf13/cobra"
)

func runVersion(dockerCli command.Cli) error {
	fmt.Println(version.Package, version.Version, version.Revision)
	return nil
}

func versionCmd(dockerCli command.Cli) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Show buildx version information ",
		Args:  cli.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVersion(dockerCli)
		},
	}
	return cmd
}
