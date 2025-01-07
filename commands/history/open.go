package history

import (
	"context"
	"fmt"
	"slices"

	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/util/cobrautil/completion"
	"github.com/docker/buildx/util/desktop"
	"github.com/docker/cli/cli/command"
	"github.com/pkg/browser"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type openOptions struct {
	builder string
	ref     string
}

func runOpen(ctx context.Context, dockerCli command.Cli, opts openOptions) error {
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

	url := desktop.BuildURL(fmt.Sprintf("%s/%s/%s", rec.node.Builder, rec.node.Name, rec.Ref))
	return browser.OpenURL(url)
}

func openCmd(dockerCli command.Cli, rootOpts RootOptions) *cobra.Command {
	var options openOptions

	cmd := &cobra.Command{
		Use:   "open [OPTIONS] [REF]",
		Short: "Open a build in Docker Desktop",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				options.ref = args[0]
			}
			options.builder = *rootOpts.Builder
			return runOpen(cmd.Context(), dockerCli, options)
		},
		ValidArgsFunction: completion.Disable,
	}

	return cmd
}
