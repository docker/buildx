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
		Use:                   "policy",
		Short:                 "Commands for working with build policies",
		DisableFlagsInUseLine: true,
	}

	cmd.AddCommand(
		// TODO: json-schema command
		evalCmd(dockerCli, rootOpts),
		testCmd(dockerCli, rootOpts),
	)

	return cmd
}
