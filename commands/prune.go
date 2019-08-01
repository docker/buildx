package commands

import (
	"context"
	"fmt"

	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/opts"
	"github.com/moby/buildkit/client"
	"github.com/spf13/cobra"
)

type pruneOptions struct {
	force       bool
	all         bool
	filter      opts.FilterOpt
	keepStorage opts.MemBytes
}

const (
	normalWarning   = `WARNING! This will remove all dangling build cache. Are you sure you want to continue?`
	allCacheWarning = `WARNING! This will remove all build cache. Are you sure you want to continue?`
)

func runPrune(dockerCli command.Cli, options pruneOptions) error {
	fmt.Println("ASDF These are the options present: ", options)
	pruneFilters := options.filter.Value()
	pruneFilters = command.PruneFilters(dockerCli, pruneFilters)
	fmt.Println("ASDF: These are the prune filters: ", pruneFilters)
	buildClient, err := client.New(context.Background(), "", nil)
	if err != nil {
		fmt.Println("there was an error: ", err)
	}
	ch := make(chan client.UsageInfo)
	buildClient.Prune(context.Background(), ch)
	close(ch)
	res := <-ch
	fmt.Println("Total reclaimed Space: ", res.Size)

	return nil
}

func pruneCmd(dockerCli command.Cli) *cobra.Command {
	options := pruneOptions{filter: opts.NewFilterOpt()}

	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Remove build cache ",
		Args:  cli.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPrune(dockerCli, options)
		},
		Annotations: map[string]string{"version": "1.00"},
	}

	flags := cmd.Flags()
	flags.BoolVarP(&options.force, "force", "f", false, "Do not prompt for confirmation")
	flags.BoolVarP(&options.all, "all", "a", false, "Remove all unused images, not just dangling ones")
	flags.Var(&options.filter, "filter", "Provide filter values (e.g. 'unused-for=24h')")
	flags.Var(&options.keepStorage, "keep-storage", "Amount of disk space to keep for cache")

	return cmd
}
