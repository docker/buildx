package commands

import (
	"context"
	"time"

	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/util/cobrautil/completion"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type stopOptions struct {
	builder string
	timeout time.Duration
}

func runStop(ctx context.Context, dockerCli command.Cli, in stopOptions) error {
	b, err := builder.New(dockerCli,
		builder.WithName(in.builder),
		builder.WithSkippedValidation(),
	)
	if err != nil {
		return err
	}

	timeoutCtx, cancel := context.WithCancelCause(ctx)
	timeoutCtx, _ = context.WithTimeoutCause(timeoutCtx, in.timeout, errors.WithStack(context.DeadlineExceeded)) //nolint:govet // no need to manually cancel this context as we already rely on parent
	defer func() { cancel(errors.WithStack(context.Canceled)) }()

	nodes, err := b.LoadNodes(timeoutCtx)
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
			options.timeout = rootOpts.timeout
			return runStop(cmd.Context(), dockerCli, options)
		},
		ValidArgsFunction:     completion.BuilderNames(dockerCli),
		DisableFlagsInUseLine: true,
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
