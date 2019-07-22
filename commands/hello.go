package commands

import (
	"fmt"

	"github.com/docker/buildx/version"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/spf13/cobra"
)

func runHelloWorld(dockerCli command.Cli) error {
	fmt.Println("hello world")
	return nil
}

func helloCmd(dockerCli command.Cli) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hello",
		Short: "I think this is how cmds are added ",
		Args:  cli.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHelloWorld(dockerCli)
		},
	}
	return cmd
}
