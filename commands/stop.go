package commands

import (
	"context"

	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/util/cobrautil/completion"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/spf13/cobra"
)

type stopOptions struct {
	builder string
}

func runStop(ctx context.Context, dockerCli command.Cli, in stopOptions) error {
	b, err := builder.New(dockerCli,
		builder.WithName(in.builder),
		builder.WithSkippedValidation(),
	)
	if err != nil {
		return err
	}
	nodes, err := b.LoadNodes(ctx)
	if err != nil {
		return err
	}

	return stop(ctx, nodes)
}

func stopCmd(dockerCli command.Cli, rootOpts *rootOptions) *cobra.Command {
	var options stopOptions

	cmd := &cobra.Command{
		Use:   "stop [NAME]",
		Short: "Stop builder instance",
		Args:  cli.RequiresMaxArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options.builder = rootOpts.builder
			if len(args) > 0 {
				options.builder = args[0]
			}
			return runStop(cmd.Context(), dockerCli, options)
		},
		ValidArgsFunction: completion.BuilderNames(dockerCli),
	}

	return cmd
}

func stop(ctx context.Context, nodes []builder.Node) (err error) {
	for _, node := range nodes {
		if node.Driver != nil {
			if err := node.Driver.Stop(ctx, true); err != nil {
				return err
			}
		}
		if node.Err != nil {
			err = node.Err
		}
	}
	return err
}
