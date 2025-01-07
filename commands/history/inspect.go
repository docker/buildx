package history

import (
	"context"
	"log"
	"slices"

	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/localstate"
	"github.com/docker/buildx/util/cobrautil/completion"
	"github.com/docker/buildx/util/confutil"
	"github.com/docker/cli/cli/command"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type inspectOptions struct {
	builder string
	ref     string
}

func runInspect(ctx context.Context, dockerCli command.Cli, opts inspectOptions) error {
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

	recs, err := queryRecords(ctx, opts.ref, nodes)
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

	rec := &recs[0]

	ls, err := localstate.New(confutil.NewConfig(dockerCli))
	if err != nil {
		return err
	}
	st, _ := ls.ReadRef(rec.node.Builder, rec.node.Name, rec.Ref)

	log.Printf("rec %+v", rec)
	log.Printf("st %+v", st)

	// Context
	// Dockerfile
	// Target
	// VCS Repo / Commit
	// Platform

	// Started
	// Duration
	// Number of steps
	// Cached steps
	// Status

	// build-args
	// exporters (image)

	// commands
	// error
	// materials

	return nil
}

func inspectCmd(dockerCli command.Cli, rootOpts RootOptions) *cobra.Command {
	var options inspectOptions

	cmd := &cobra.Command{
		Use:   "inspect [OPTIONS] [REF]",
		Short: "Inspect a build",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				options.ref = args[0]
			}
			options.builder = *rootOpts.Builder
			return runInspect(cmd.Context(), dockerCli, options)
		},
		ValidArgsFunction: completion.Disable,
	}

	// flags := cmd.Flags()

	return cmd
}
