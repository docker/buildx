package policy

import (
	"github.com/docker/cli/cli/command"
	"github.com/spf13/cobra"
)

type RootOptions struct {
	Builder *string
}

// RootCmd creates the policy command tree.
func RootCmd(rootcmd *cobra.Command, dockerCli command.Cli, rootOpts RootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "policy",
		Short: "Commands for working with build policies",
	}

	cmd.AddCommand(
		jsonSchemaCmd(),
		evalCmd(dockerCli, rootOpts),
		testCmd(),
	)

	return cmd
}
