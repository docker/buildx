package history

import (
	"context"
	"io"
	"os"
	"slices"

	"github.com/containerd/console"
	"github.com/containerd/platforms"
	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/localstate"
	"github.com/docker/buildx/util/cobrautil/completion"
	"github.com/docker/buildx/util/confutil"
	"github.com/docker/buildx/util/desktop/bundle"
	"github.com/docker/cli/cli/command"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type exportOptions struct {
	builder string
	ref     string
	output  string
}

func runExport(ctx context.Context, dockerCli command.Cli, opts exportOptions) error {
	b, err := builder.New(dockerCli, builder.WithName(opts.builder))
	if err != nil {
		return err
	}

	nodes, err := b.LoadNodes(ctx, builder.WithData())
	if err != nil {
		return err
	}
	for _, node := range nodes {
		if node.Err != nil {
			return node.Err
		}
	}

	recs, err := queryRecords(ctx, opts.ref, nodes, &queryOptions{
		CompletedOnly: true,
	})
	if err != nil {
		return err
	}

	if len(recs) == 0 {
		if opts.ref == "" {
			return errors.New("no records found")
		}
		return errors.Errorf("no record found for ref %q", opts.ref)
	}

	if opts.ref == "" {
		slices.SortFunc(recs, func(a, b historyRecord) int {
			return b.CreatedAt.AsTime().Compare(a.CreatedAt.AsTime())
		})
	}

	recs = recs[:1]

	ls, err := localstate.New(confutil.NewConfig(dockerCli))
	if err != nil {
		return err
	}

	c, err := recs[0].node.Driver.Client(ctx)
	if err != nil {
		return err
	}

	toExport := make([]*bundle.Record, 0, len(recs))
	for _, rec := range recs {
		var defaultPlatform string
		if p := rec.node.Platforms; len(p) > 0 {
			defaultPlatform = platforms.FormatAll(platforms.Normalize(p[0]))
		}

		var stg *localstate.StateGroup
		st, _ := ls.ReadRef(rec.node.Builder, rec.node.Name, rec.Ref)
		if st != nil && st.GroupRef != "" {
			stg, err = ls.ReadGroup(st.GroupRef)
			if err != nil {
				return err
			}
		}

		toExport = append(toExport, &bundle.Record{
			BuildHistoryRecord: rec.BuildHistoryRecord,
			DefaultPlatform:    defaultPlatform,
			LocalState:         st,
			StateGroup:         stg,
		})
	}

	var w io.Writer = os.Stdout
	if opts.output != "" {
		f, err := os.Create(opts.output)
		if err != nil {
			return errors.Wrapf(err, "failed to create output file %q", opts.output)
		}
		defer f.Close()
		w = f
	} else {
		if _, err := console.ConsoleFromFile(os.Stdout); err == nil {
			return errors.Errorf("refusing to write to console, use --output to specify a file")
		}
	}

	return bundle.Export(ctx, c, w, toExport)
}

func exportCmd(dockerCli command.Cli, rootOpts RootOptions) *cobra.Command {
	var options exportOptions

	cmd := &cobra.Command{
		Use:   "export [OPTIONS] [REF]",
		Short: "Export a build into Docker Desktop bundle",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				options.ref = args[0]
			}
			options.builder = *rootOpts.Builder
			return runExport(cmd.Context(), dockerCli, options)
		},
		ValidArgsFunction: completion.Disable,
	}

	flags := cmd.Flags()
	flags.StringVarP(&options.output, "output", "o", "", "Output file path")

	return cmd
}
