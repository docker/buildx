package policy

import (
	"github.com/spf13/cobra"
)

// RootCmd creates the policy command tree.
func RootCmd(rootcmd *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "policy",
		Short: "Commands for working with build policies",
	}

	cmd.AddCommand(
		jsonSchemaCmd(),
		evalCmd(),
		testCmd(),
	)

	return cmd
}
