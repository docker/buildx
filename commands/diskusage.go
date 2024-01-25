package commands

import (
	"context"
	"fmt"
	"io"
	"os"
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
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

type duOptions struct {
	builder string
	filter  opts.FilterOpt
	verbose bool
}

func runDiskUsage(ctx context.Context, dockerCli command.Cli, opts duOptions) error {
	pi, err := toBuildkitPruneInfo(opts.filter.Value())
	if err != nil {
		return err
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

	out := make([][]*client.UsageInfo, len(nodes))

	eg, ctx := errgroup.WithContext(ctx)
	for i, node := range nodes {
		func(i int, node builder.Node) {
			eg.Go(func() error {
				if node.Driver != nil {
					c, err := node.Driver.Client(ctx)
					if err != nil {
						return err
					}
					du, err := c.DiskUsage(ctx, client.WithFilter(pi.Filter))
					if err != nil {
						return err
					}
					out[i] = du
					return nil
				}
				return nil
			})
		}(i, node)
	}

	if err := eg.Wait(); err != nil {
		return err
	}

	tw := tabwriter.NewWriter(os.Stdout, 1, 8, 1, '\t', 0)
	first := true
	for _, du := range out {
		if du == nil {
			continue
		}
		if opts.verbose {
			printVerbose(tw, du)
		} else {
			if first {
				printTableHeader(tw)
				first = false
			}
			for _, di := range du {
				printTableRow(tw, di)
			}

			tw.Flush()
		}
	}

	if opts.filter.Value().Len() == 0 {
		printSummary(tw, out)
	}

	tw.Flush()
	return nil
}

func duCmd(dockerCli command.Cli, rootOpts *rootOptions) *cobra.Command {
	options := duOptions{filter: opts.NewFilterOpt()}

	cmd := &cobra.Command{
		Use:   "du",
		Short: "Disk usage",
		Args:  cli.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			options.builder = rootOpts.builder
			return runDiskUsage(cmd.Context(), dockerCli, options)
		},
		ValidArgsFunction: completion.Disable,
	}

	flags := cmd.Flags()
	flags.Var(&options.filter, "filter", "Provide filter values")
	flags.BoolVar(&options.verbose, "verbose", false, "Provide a more verbose output")

	return cmd
}

func printKV(w io.Writer, k string, v interface{}) {
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

func printSummary(tw *tabwriter.Writer, dus [][]*client.UsageInfo) {
	total := int64(0)
	reclaimable := int64(0)
	shared := int64(0)

	for _, du := range dus {
		for _, di := range du {
			if di.Size > 0 {
				total += di.Size
				if !di.InUse {
					reclaimable += di.Size
				}
			}
			if di.Shared {
				shared += di.Size
			}
		}
	}

	if shared > 0 {
		fmt.Fprintf(tw, "Shared:\t%s\n", units.HumanSize(float64(shared)))
		fmt.Fprintf(tw, "Private:\t%s\n", units.HumanSize(float64(total-shared)))
	}

	fmt.Fprintf(tw, "Reclaimable:\t%s\n", units.HumanSize(float64(reclaimable)))
	fmt.Fprintf(tw, "Total:\t%s\n", units.HumanSize(float64(total)))
	tw.Flush()
}
