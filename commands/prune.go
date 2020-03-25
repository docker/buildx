package commands

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/docker/buildx/build"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/opts"
	"github.com/docker/docker/api/types/filters"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/util/appcontext"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/tonistiigi/units"
	"golang.org/x/sync/errgroup"
)

type pruneOptions struct {
	builderOptions
	all         bool
	filter      opts.FilterOpt
	keepStorage opts.MemBytes
	force       bool
	verbose     bool
}

const (
	normalWarning   = `WARNING! This will remove all dangling build cache. Are you sure you want to continue?`
	allCacheWarning = `WARNING! This will remove all build cache. Are you sure you want to continue?`
)

func runPrune(dockerCli command.Cli, opts pruneOptions) error {
	ctx := appcontext.Context()

	pruneFilters := opts.filter.Value()
	pruneFilters = command.PruneFilters(dockerCli, pruneFilters)

	pi, err := toBuildkitPruneInfo(pruneFilters)
	if err != nil {
		return err
	}

	warning := normalWarning
	if opts.all {
		warning = allCacheWarning
	}

	if !opts.force && !command.PromptForConfirmation(dockerCli.In(), dockerCli.Out(), warning) {
		return nil
	}

	dis, err := getInstanceOrDefault(ctx, dockerCli, opts.builder, "")
	if err != nil {
		return err
	}

	for _, di := range dis {
		if di.Err != nil {
			return err
		}
	}

	ch := make(chan client.UsageInfo)
	printed := make(chan struct{})

	tw := tabwriter.NewWriter(os.Stdout, 1, 8, 1, '\t', 0)
	first := true
	total := int64(0)

	go func() {
		defer close(printed)
		for du := range ch {
			total += du.Size
			if opts.verbose {
				printVerbose(tw, []*client.UsageInfo{&du})
			} else {
				if first {
					printTableHeader(tw)
					first = false
				}
				printTableRow(tw, &du)
				tw.Flush()
			}
		}
	}()

	eg, ctx := errgroup.WithContext(ctx)
	for _, di := range dis {
		func(di build.DriverInfo) {
			eg.Go(func() error {
				if di.Driver != nil {
					c, err := di.Driver.Client(ctx)
					if err != nil {
						return err
					}
					popts := []client.PruneOption{
						client.WithKeepOpt(pi.KeepDuration, opts.keepStorage.Value()),
						client.WithFilter(pi.Filter),
					}
					if opts.all {
						popts = append(popts, client.PruneAll)
					}
					return c.Prune(ctx, ch, popts...)
				}
				return nil
			})
		}(di)
	}

	if err := eg.Wait(); err != nil {
		return err
	}
	close(ch)
	<-printed

	tw = tabwriter.NewWriter(os.Stdout, 1, 8, 1, '\t', 0)
	fmt.Fprintf(tw, "Total:\t%.2f\n", units.Bytes(total))
	tw.Flush()
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
	flags.BoolVarP(&options.all, "all", "a", false, "Remove all unused images, not just dangling ones")
	flags.Var(&options.filter, "filter", "Provide filter values (e.g. 'until=24h')")
	flags.Var(&options.keepStorage, "keep-storage", "Amount of disk space to keep for cache")
	flags.BoolVar(&options.verbose, "verbose", false, "Provide a more verbose output")
	flags.BoolVar(&options.force, "force", false, "Skip the warning messages")

	return cmd
}

func toBuildkitPruneInfo(f filters.Args) (*client.PruneInfo, error) {
	var until time.Duration
	untilValues := f.Get("until")          // canonical
	unusedForValues := f.Get("unused-for") // deprecated synonym for "until" filter

	if len(untilValues) > 0 && len(unusedForValues) > 0 {
		return nil, errors.Errorf("conflicting filters %q and %q", "until", "unused-for")
	}
	filterKey := "until"
	if len(unusedForValues) > 0 {
		filterKey = "unused-for"
	}
	untilValues = append(untilValues, unusedForValues...)

	switch len(untilValues) {
	case 0:
		// nothing to do
	case 1:
		var err error
		until, err = time.ParseDuration(untilValues[0])
		if err != nil {
			return nil, errors.Wrapf(err, "%q filter expects a duration (e.g., '24h')", filterKey)
		}
	default:
		return nil, errors.Errorf("filters expect only one value")
	}

	bkFilter := make([]string, 0, f.Len())
	for _, field := range f.Keys() {
		values := f.Get(field)
		switch len(values) {
		case 0:
			bkFilter = append(bkFilter, field)
		case 1:
			if field == "id" {
				bkFilter = append(bkFilter, field+"~="+values[0])
			} else {
				bkFilter = append(bkFilter, field+"=="+values[0])
			}
		default:
			return nil, errors.Errorf("filters expect only one value")
		}
	}
	return &client.PruneInfo{
		KeepDuration: until,
		Filter:       []string{strings.Join(bkFilter, ",")},
	}, nil
}
