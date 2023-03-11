package cobrautil

import (
	"strings"

	"github.com/docker/buildx/bake"
	"github.com/spf13/cobra"
)

// ValidArgsFn defines a completion func to be returned to fetch completion options
type ValidArgsFn func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective)

func NoCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return nil, cobra.ShellCompDirectiveNoFileComp
}

func CompleteBakeTargets(files []string) ValidArgsFn {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		f, err := bake.ReadLocalFiles(files)
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		tgts, err := bake.ListTargets(f)
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		var filtered []string
		if toComplete == "" {
			return tgts, cobra.ShellCompDirectiveNoFileComp
		}
		for _, tgt := range tgts {
			if strings.HasPrefix(tgt, toComplete) {
				filtered = append(filtered, tgt)
			}
		}
		return filtered, cobra.ShellCompDirectiveNoFileComp
	}
}
