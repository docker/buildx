package commands

import (
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/util/cobrautil/completion"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/opts"
	"github.com/docker/go-units"
	"github.com/moby/buildkit/client"
	gateway "github.com/moby/buildkit/frontend/gateway/client"
	pb "github.com/moby/buildkit/solver/pb"
	"github.com/moby/buildkit/util/apicaps"
	dclient "github.com/moby/moby/client"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

type pruneOptions struct {
	builder       string
	all           bool
	filter        opts.FilterOpt
	reservedSpace opts.MemBytes
	maxUsedSpace  opts.MemBytes
	minFreeSpace  opts.MemBytes
	force         bool
	verbose       bool
	timeout       time.Duration
}

const (
	normalWarning   = `WARNING! This will remove all dangling build cache. Are you sure you want to continue?`
	allCacheWarning = `WARNING! This will remove all build cache. Are you sure you want to continue?`
)

func runPrune(ctx context.Context, dockerCli command.Cli, opts pruneOptions) error {
	pruneFilters := command.PruneFilters(dockerCli, opts.filter.Value())
	pi, err := toBuildkitPruneInfo(pruneFilters)
	if err != nil {
		return err
	}

	warning := normalWarning
	if opts.all {
		warning = allCacheWarning
	}

	if !opts.force {
		if ok, err := prompt(ctx, dockerCli.In(), dockerCli.Out(), warning); err != nil {
			return err
		} else if !ok {
			return nil
		}
	}

	b, err := builder.New(dockerCli, builder.WithName(opts.builder))
	if err != nil {
		return err
	}

	timeoutCtx, cancel := context.WithCancelCause(ctx)
	timeoutCtx, _ = context.WithTimeoutCause(timeoutCtx, opts.timeout, errors.WithStack(context.DeadlineExceeded)) //nolint:govet // no need to manually cancel this context as we already rely on parent
	defer func() { cancel(errors.WithStack(context.Canceled)) }()

	nodes, err := b.LoadNodes(timeoutCtx)
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
					// check if the client supports newer prune options
					if opts.maxUsedSpace.Value() != 0 || opts.minFreeSpace.Value() != 0 {
						caps, err := loadLLBCaps(ctx, c)
						if err != nil {
							return errors.Wrap(err, "failed to load buildkit capabilities for prune")
						}
						if caps.Supports(pb.CapGCFreeSpaceFilter) != nil {
							return errors.New("buildkit v0.17.0+ is required for max-used-space and min-free-space filters")
						}
					}

					popts := []client.PruneOption{
						client.WithKeepOpt(pi.KeepDuration, opts.reservedSpace.Value(), opts.maxUsedSpace.Value(), opts.minFreeSpace.Value()),
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

func loadLLBCaps(ctx context.Context, c *client.Client) (apicaps.CapSet, error) {
	var caps apicaps.CapSet
	_, err := c.Build(ctx, client.SolveOpt{
		Internal: true,
	}, "buildx", func(ctx context.Context, c gateway.Client) (*gateway.Result, error) {
		caps = c.BuildOpts().LLBCaps
		return nil, nil
	}, nil)
	return caps, err
}

func pruneCmd(dockerCli command.Cli, rootOpts *rootOptions) *cobra.Command {
	options := pruneOptions{filter: opts.NewFilterOpt()}

	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Remove build cache",
		Args:  cli.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			options.builder = rootOpts.builder
			options.timeout = rootOpts.timeout
			return runPrune(cmd.Context(), dockerCli, options)
		},
		ValidArgsFunction:     completion.Disable,
		DisableFlagsInUseLine: true,
	}

	flags := cmd.Flags()
	flags.BoolVarP(&options.all, "all", "a", false, "Include internal/frontend images")
	flags.Var(&options.filter, "filter", `Provide filter values`)
	flags.Var(&options.reservedSpace, "reserved-space", "Amount of disk space always allowed to keep for cache")
	flags.Var(&options.minFreeSpace, "min-free-space", "Target amount of free disk space after pruning")
	flags.Var(&options.maxUsedSpace, "max-used-space", "Maximum amount of disk space allowed to keep for cache")
	flags.BoolVar(&options.verbose, "verbose", false, "Provide a more verbose output")
	flags.BoolVarP(&options.force, "force", "f", false, "Do not prompt for confirmation")

	flags.Var(&options.reservedSpace, "keep-storage", "Amount of disk space to keep for cache")
	flags.MarkDeprecated("keep-storage", "keep-storage flag has been changed to reserved-space")

	return cmd
}

// getFilter returns the list of values associated with the key
func getFilter(f dclient.Filters, key string) []string {
	return slices.Collect(maps.Keys(f[key]))
}

func toBuildkitPruneInfo(pruneFilters dclient.Filters) (*client.PruneInfo, error) {
	var until time.Duration
	if len(pruneFilters["until"]) > 0 && len(pruneFilters["unused-for"]) > 0 {
		return nil, errors.New(`conflicting filters "until" and "unused-for"`)
	}
	untilKey := "until"
	if len(pruneFilters["unused-for"]) > 0 {
		untilKey = "unused-for" // deprecated synonym for "until" filter
	}

	untilValues := getFilter(pruneFilters, untilKey)
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
		return nil, errors.Errorf("%q filter expects only one value", untilKey)
	}

	filters := make([]string, 0, len(pruneFilters))
	for filterKey := range pruneFilters {
		if filterKey == untilKey {
			continue
		}

		values := getFilter(pruneFilters, filterKey)
		switch len(values) {
		case 0:
			filters = append(filters, filterKey)
		case 1:
			if filterKey == "id" {
				filters = append(filters, filterKey+"~="+values[0])
			} else if strings.HasSuffix(filterKey, "!") || strings.HasSuffix(filterKey, "~") {
				filters = append(filters, filterKey+"="+values[0])
			} else {
				filters = append(filters, filterKey+"=="+values[0])
			}
		default:
			return nil, errors.Errorf("%q filter expects only one value", filterKey)
		}
	}
	return &client.PruneInfo{
		KeepDuration: until,
		Filter:       []string{strings.Join(filters, ",")},
	}, nil
}

func printKV(w io.Writer, k string, v any) {
	fmt.Fprintf(w, "%s:\t%v\n", k, v)
}

func printVerbose(tw *tabwriter.Writer, du []*client.UsageInfo) {
	for _, di := range du {
		printKV(tw, "ID", di.ID)
		if len(di.Parents) != 0 {
			printKV(tw, "Parent", strings.Join(di.Parents, ","))
		}
		printKV(tw, "Created at", di.CreatedAt)
		printKV(tw, "Mutable", di.Mutable)
		printKV(tw, "Reclaimable", !di.InUse)
		printKV(tw, "Shared", di.Shared)
		printKV(tw, "Size", units.HumanSize(float64(di.Size)))
		if di.Description != "" {
			printKV(tw, "Description", di.Description)
		}
		printKV(tw, "Usage count", di.UsageCount)
		if di.LastUsedAt != nil {
			printKV(tw, "Last used", units.HumanDuration(time.Since(*di.LastUsedAt))+" ago")
		}
		if di.RecordType != "" {
			printKV(tw, "Type", di.RecordType)
		}

		fmt.Fprintf(tw, "\n")
	}

	tw.Flush()
}

func printTableHeader(tw *tabwriter.Writer) {
	fmt.Fprintln(tw, "ID\tRECLAIMABLE\tSIZE\tLAST ACCESSED")
}

func printTableRow(tw *tabwriter.Writer, di *client.UsageInfo) {
	id := di.ID
	if di.Mutable {
		id += "*"
	}
	size := units.HumanSize(float64(di.Size))
	if di.Shared {
		size += "*"
	}
	lastAccessed := ""
	if di.LastUsedAt != nil {
		lastAccessed = units.HumanDuration(time.Since(*di.LastUsedAt)) + " ago"
	}
	fmt.Fprintf(tw, "%-40s\t%-5v\t%-10s\t%s\n", id, !di.InUse, size, lastAccessed)
}
