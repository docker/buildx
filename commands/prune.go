package commands

import (
	"fmt"

	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/spf13/cobra"
)

func runPrune(dockerCli command.Cli) error {
	fmt.Println("ASDF run prune")
	return nil
}

func pruneCmd(dockerCli command.Cli) *cobra.Command {
  fmt.Println("ASDF prune command added")

	cmd := &cobra.Command{
		Use:   "prune",
		Short: "clear the buildx build cache ",
		Args:  cli.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPrune(dockerCli)
		},
	}
	return cmd
}
