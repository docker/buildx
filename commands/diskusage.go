package commands

import (
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/docker/buildx/build"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/opts"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/util/appcontext"
	"github.com/spf13/cobra"
	"github.com/tonistiigi/units"
	"golang.org/x/sync/errgroup"
)

type duOptions struct {
	builderOptions
	filter  opts.FilterOpt
	verbose bool
}

func runDiskUsage(dockerCli command.Cli, opts duOptions) error {
	ctx := appcontext.Context()

	pi, err := toBuildkitPruneInfo(opts.filter.Value())
	if err != nil {
		return err
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

	out := make([][]*client.UsageInfo, len(dis))

	eg, ctx := errgroup.WithContext(ctx)
	for i, di := range dis {
		func(i int, di build.DriverInfo) {
			eg.Go(func() error {
				if di.Driver != nil {
					c, err := di.Driver.Client(ctx)
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
		}(i, di)
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

func duCmd(dockerCli command.Cli) *cobra.Command {
	options := duOptions{filter: opts.NewFilterOpt()}

	cmd := &cobra.Command{
		Use:   "du",
		Short: "Disk usage",
		Args:  cli.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiskUsage(dockerCli, options)
		},
		Annotations: map[string]string{"version": "1.00"},
	}

	flags := cmd.Flags()
	builderFlags(&options.builderOptions, flags)
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
		if di.Parent != "" {
			printKV(tw, "Parent", di.Parent)
		}
		printKV(tw, "Created at", di.CreatedAt)
		printKV(tw, "Mutable", di.Mutable)
		printKV(tw, "Reclaimable", !di.InUse)
		printKV(tw, "Shared", di.Shared)
		printKV(tw, "Size", fmt.Sprintf("%.2f", units.Bytes(di.Size)))
		if di.Description != "" {
			printKV(tw, "Description", di.Description)
		}
		printKV(tw, "Usage count", di.UsageCount)
		if di.LastUsedAt != nil {
			printKV(tw, "Last used", di.LastUsedAt)
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
	size := fmt.Sprintf("%.2f", units.Bytes(di.Size))
	if di.Shared {
		size += "*"
	}
	fmt.Fprintf(tw, "%-71s\t%-11v\t%s\t\n", id, !di.InUse, size)
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

	tw = tabwriter.NewWriter(os.Stdout, 1, 8, 1, '\t', 0)

	if shared > 0 {
		fmt.Fprintf(tw, "Shared:\t%.2f\n", units.Bytes(shared))
		fmt.Fprintf(tw, "Private:\t%.2f\n", units.Bytes(total-shared))
	}

	fmt.Fprintf(tw, "Reclaimable:\t%.2f\n", units.Bytes(reclaimable))
	fmt.Fprintf(tw, "Total:\t%.2f\n", units.Bytes(total))
	tw.Flush()
}
