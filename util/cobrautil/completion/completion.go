package completion

import (
	"strings"

	"github.com/docker/buildx/bake"
	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/store/storeutil"
	"github.com/docker/cli/cli/command"
	"github.com/spf13/cobra"
)

// ValidArgsFn defines a completion func to be returned to fetch completion options
type ValidArgsFn func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective)

func Disable(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return nil, cobra.ShellCompDirectiveNoSpace
}

func BakeTargets(files []string) ValidArgsFn {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		f, err := bake.ReadLocalFiles(files, nil, nil)
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

func BuilderNames(dockerCli command.Cli) ValidArgsFn {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		txn, release, err := storeutil.GetStore(dockerCli)
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		defer release()
		builders, err := builder.GetBuilders(dockerCli, txn)
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		var filtered []string
		for _, b := range builders {
			if toComplete == "" || strings.HasPrefix(b.Name, toComplete) {
				filtered = append(filtered, b.Name)
			}
		}
		return filtered, cobra.ShellCompDirectiveNoFileComp
	}
}
