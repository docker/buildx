package history

import (
	"context"
	"io"

	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/util/cobrautil/completion"
	"github.com/docker/cli/cli/command"
	"github.com/hashicorp/go-multierror"
	controlapi "github.com/moby/buildkit/api/services/control"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

type rmOptions struct {
	builder string
	refs    []string
	all     bool
}

func runRm(ctx context.Context, dockerCli command.Cli, opts rmOptions) error {
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

	errs := make([][]error, len(opts.refs))
	for i := range errs {
		errs[i] = make([]error, len(nodes))
	}

	eg, ctx := errgroup.WithContext(ctx)
	for i, node := range nodes {
		node := node
		eg.Go(func() error {
			if node.Driver == nil {
				return nil
			}
			c, err := node.Driver.Client(ctx)
			if err != nil {
				return err
			}

			refs := opts.refs

			if opts.all {
				serv, err := c.ControlClient().ListenBuildHistory(ctx, &controlapi.BuildHistoryRequest{
					EarlyExit: true,
				})
				if err != nil {
					return err
				}
				defer serv.CloseSend()

				for {
					resp, err := serv.Recv()
					if err != nil {
						if errors.Is(err, io.EOF) {
							break
						}
						return err
					}
					if resp.Type == controlapi.BuildHistoryEventType_COMPLETE {
						refs = append(refs, resp.Record.Ref)
					}
				}
			}

			for j, ref := range refs {
				_, err = c.ControlClient().UpdateBuildHistory(ctx, &controlapi.UpdateBuildHistoryRequest{
					Ref:    ref,
					Delete: true,
				})
				if opts.all {
					if err != nil {
						return err
					}
				} else {
					errs[j][i] = err
				}
			}
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return err
	}

	var out []error
loop0:
	for _, nodeErrs := range errs {
		var nodeErr error
		for _, err1 := range nodeErrs {
			if err1 == nil {
				continue loop0
			}
			if nodeErr == nil {
				nodeErr = err1
			} else {
				nodeErr = multierror.Append(nodeErr, err1)
			}
		}
		out = append(out, nodeErr)
	}
	if len(out) == 0 {
		return nil
	}
	if len(out) == 1 {
		return out[0]
	}
	return multierror.Append(out[0], out[1:]...)
}

func rmCmd(dockerCli command.Cli, rootOpts RootOptions) *cobra.Command {
	var options rmOptions

	cmd := &cobra.Command{
		Use:   "rm [OPTIONS] [REF...]",
		Short: "Remove build records",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && !options.all {
				return errors.New("rm requires at least one argument")
			}
			if len(args) > 0 && options.all {
				return errors.New("rm requires either --all or at least one argument")
			}
			options.refs = args
			options.builder = *rootOpts.Builder
			return runRm(cmd.Context(), dockerCli, options)
		},
		ValidArgsFunction: completion.Disable,
	}

	flags := cmd.Flags()
	flags.BoolVar(&options.all, "all", false, "Remove all build records")

	return cmd
}
