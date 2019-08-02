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
	all          bool
	filter       opts.FilterOpt
	keepStorage  opts.MemBytes
	keepDuration opts.DurationOpt
	force        bool
	verbose      bool
}

const (
	normalWarning   = `WARNING! This will remove all dangling build cache. Are you sure you want to continue?`
	allCacheWarning = `WARNING! This will remove all build cache. Are you sure you want to continue?`
)

func runPrune(dockerCli command.Cli, opts pruneOptions) error {

	warning := normalWarning
	if opts.all {
		warning = allCacheWarning
	}

	if !opts.force && !command.PromptForConfirmation(dockerCli.In(), dockerCli.Out(), warning) {
		return nil
	}

	buildClient, err := client.New(context.Background(), "", opts)
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
	flags.Var(&options.keepDuration, "keep-duration", "Keep data newer than a certain limit")
	flags.BoolVarP(&options.all, "all", "a", false, "Remove all unused images, not just dangling ones")
	flags.Var(&options.filter, "filter", "Provide filter values (e.g. 'unused-for=24h')")
	flags.Var(&options.keepStorage, "keep-storage", "Amount of disk space to keep for cache")
	flags.BoolVar(&options.verbose, "verbose", false, "Provide a more verbose output")
	flags.BoolVar(&options.force, "force", false, "Skip the warning messages")

	return cmd
}
