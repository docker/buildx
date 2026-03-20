package history

import (
	"context"
	"io"
	"os"

	"github.com/containerd/console"
	"github.com/containerd/platforms"
	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/localstate"
	"github.com/docker/buildx/util/cobrautil"
	"github.com/docker/buildx/util/confutil"
	"github.com/docker/buildx/util/desktop/bundle"
	"github.com/docker/cli/cli/command"
	"github.com/moby/buildkit/client"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type exportOptions struct {
	builder  string
	refs     []string
	output   string
	all      bool
	finalize bool
}

func runExport(ctx context.Context, dockerCli command.Cli, opts exportOptions) error {
	nodes, err := loadNodes(ctx, dockerCli, opts.builder)
	if err != nil {
		return err
	}

	if len(opts.refs) == 0 {
		opts.refs = []string{""}
	}

	var res []historyRecord
	for _, ref := range opts.refs {
		recs, err := queryRecords(ctx, ref, nodes, &queryOptions{
			CompletedOnly: true,
		})
		if err != nil {
			return err
		}

		if len(recs) == 0 {
			if ref == "" {
				return errors.New("no records found")
			}
			return errors.Errorf("no record found for ref %q", ref)
		}

		toExport := recs
		if !opts.all && ref == "" {
			latestRef := recs[0].Ref
			recCount := 0
			for _, rec := range recs {
				if rec.Ref != latestRef {
					break
				}
				recCount++
			}
			toExport = recs[:recCount]
		}
		if opts.finalize {
			seen := make(map[string]struct{}, len(toExport))
			var finalized bool
			for _, rec := range toExport {
				if rec.node == nil || rec.Trace != nil {
					continue
				}
				key := rec.node.Builder + "\x00" + rec.node.Name + "\x00" + rec.Ref
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				finalized = true
				if err := finalizeRecord(ctx, rec.Ref, *rec.node); err != nil {
					return err
				}
			}
			if finalized {
				queryRef := ref
				if !opts.all {
					queryRef = toExport[0].Ref
				}
				recs, err = queryRecords(ctx, queryRef, nodes, &queryOptions{
					CompletedOnly: true,
				})
				if err != nil {
					return err
				}
				toExport = recs
			}
		}
		res = append(res, toExport...)
		if opts.all {
			break
		}
	}

	ls, err := localstate.New(confutil.NewConfig(dockerCli))
	if err != nil {
		return err
	}

	visited := map[*builder.Node]struct{}{}
	var clients []*client.Client
	for _, rec := range res {
		if _, ok := visited[rec.node]; ok {
			continue
		}
		c, err := rec.node.Driver.Client(ctx)
		if err != nil {
			return err
		}
		visited[rec.node] = struct{}{}
		clients = append(clients, c)
	}

	toExport := make([]*bundle.Record, 0, len(res))
	for _, rec := range res {
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

	return bundle.Export(ctx, clients, w, toExport)
}

func exportCmd(dockerCli command.Cli, rootOpts RootOptions) *cobra.Command {
	var options exportOptions

	cmd := &cobra.Command{
		Use:   "export [OPTIONS] [REF...]",
		Short: "Export build records into Docker Desktop bundle",
		RunE: func(cmd *cobra.Command, args []string) error {
			if options.all && len(args) > 0 {
				return errors.New("cannot specify refs when using --all")
			}
			options.refs = args
			options.builder = *rootOpts.Builder
			return runExport(cmd.Context(), dockerCli, options)
		},
		ValidArgsFunction:     cobrautil.DisableCompletion,
		DisableFlagsInUseLine: true,
	}

	flags := cmd.Flags()
	flags.StringVarP(&options.output, "output", "o", "", "Output file path")
	flags.BoolVar(&options.all, "all", false, "Export all build records for the builder")
	flags.BoolVar(&options.finalize, "finalize", false, "Ensure build records are finalized before exporting")

	return cmd
}
