package completion

import (
	"github.com/spf13/cobra"
)

// ValidArgsFn defines a completion func to be returned to fetch completion options
type ValidArgsFn func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective)

func Disable(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return nil, cobra.ShellCompDirectiveNoSpace
}
