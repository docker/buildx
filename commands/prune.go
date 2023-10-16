package commands

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/util/cobrautil/completion"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/opts"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/go-units"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/util/appcontext"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

type pruneOptions struct {
	builder     string
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

	b, err := builder.New(dockerCli, builder.WithName(opts.builder))
	if err != nil {
		return err
	}

	nodes, err := b.LoadNodes(ctx)
	if err != nil {
		return err
	}
	for _, node := range nodes {
		if node.Err != nil {
			return node.Err
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
	for _, node := range nodes {
		func(node builder.Node) {
			eg.Go(func() error {
				if node.Driver != nil {
					c, err := node.Driver.Client(ctx)
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
		}(node)
	}

	if err := eg.Wait(); err != nil {
		return err
	}
	close(ch)
	<-printed

	tw = tabwriter.NewWriter(os.Stdout, 1, 8, 1, '\t', 0)
	fmt.Fprintf(tw, "Total:\t%s\n", units.HumanSize(float64(total)))
	tw.Flush()
	return nil
}

func pruneCmd(dockerCli command.Cli, rootOpts *rootOptions) *cobra.Command {
	options := pruneOptions{filter: opts.NewFilterOpt()}

	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Remove build cache",
		Args:  cli.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			options.builder = rootOpts.builder
			return runPrune(dockerCli, options)
		},
		ValidArgsFunction: completion.Disable,
	}

	flags := cmd.Flags()
	flags.BoolVarP(&options.all, "all", "a", false, "Include internal/frontend images")
	flags.Var(&options.filter, "filter", `Provide filter values (e.g., "until=24h")`)
	flags.Var(&options.keepStorage, "keep-storage", "Amount of disk space to keep for cache")
	flags.BoolVar(&options.verbose, "verbose", false, "Provide a more verbose output")
	flags.BoolVarP(&options.force, "force", "f", false, "Do not prompt for confirmation")

	return cmd
}

func toBuildkitPruneInfo(f filters.Args) (*client.PruneInfo, error) {
	var until time.Duration
	untilValues := f.Get("until")          // canonical
	unusedForValues := f.Get("unused-for") // deprecated synonym for "until" filter

	if len(untilValues) > 0 && len(unusedForValues) > 0 {
		return nil, errors.Errorf("conflicting filters %q and %q", "until", "unused-for")
	}
	untilKey := "until"
	if len(unusedForValues) > 0 {
		untilKey = "unused-for"
	}
	untilValues = append(untilValues, unusedForValues...)

	switch len(untilValues) {
	case 0:
		// nothing to do
	case 1:
		var err error
		until, err = time.ParseDuration(untilValues[0])
		if err != nil {
			return nil, errors.Wrapf(err, "%q filter expects a duration (e.g., '24h')", untilKey)
		}
	default:
		return nil, errors.Errorf("filters expect only one value")
	}

	filters := make([]string, 0, f.Len())
	for _, filterKey := range f.Keys() {
		if filterKey == untilKey {
			continue
		}

		values := f.Get(filterKey)
		switch len(values) {
		case 0:
			filters = append(filters, filterKey)
		case 1:
			if filterKey == "id" {
				filters = append(filters, filterKey+"~="+values[0])
			} else {
				filters = append(filters, filterKey+"=="+values[0])
			}
		default:
			return nil, errors.Errorf("filters expect only one value")
		}
	}
	return &client.PruneInfo{
		KeepDuration: until,
		Filter:       []string{strings.Join(filters, ",")},
	}, nil
}
