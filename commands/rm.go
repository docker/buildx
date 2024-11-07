package commands

import (
	"context"
	"fmt"
	"time"

	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/store"
	"github.com/docker/buildx/store/storeutil"
	"github.com/docker/buildx/util/cobrautil/completion"
	"github.com/docker/cli/cli/command"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

type rmOptions struct {
	builders    []string
	keepState   bool
	keepDaemon  bool
	allInactive bool
	force       bool
}

const (
	rmInactiveWarning = `WARNING! This will remove all builders that are not in running state. Are you sure you want to continue?`
)

func runRm(ctx context.Context, dockerCli command.Cli, in rmOptions) error {
	if in.allInactive && !in.force {
		if ok, err := prompt(ctx, dockerCli.In(), dockerCli.Out(), rmInactiveWarning); err != nil {
			return err
		} else if !ok {
			return nil
		}
	}

	txn, release, err := storeutil.GetStore(dockerCli)
	if err != nil {
		return err
	}
	defer release()

	if in.allInactive {
		return rmAllInactive(ctx, txn, dockerCli, in)
	}

	eg, _ := errgroup.WithContext(ctx)
	for _, name := range in.builders {
		func(name string) {
			eg.Go(func() (err error) {
				defer func() {
					if err == nil {
						_, _ = fmt.Fprintf(dockerCli.Err(), "%s removed\n", name)
					} else {
						_, _ = fmt.Fprintf(dockerCli.Err(), "failed to remove %s: %v\n", name, err)
					}
				}()

				b, err := builder.New(dockerCli,
					builder.WithName(name),
					builder.WithStore(txn),
					builder.WithSkippedValidation(),
				)
				if err != nil {
					return err
				}

				nodes, err := b.LoadNodes(ctx)
				if err != nil {
					return err
				}

				if cb := b.ContextName(); cb != "" {
					return errors.Errorf("context builder cannot be removed, run `docker context rm %s` to remove this context", cb)
				}

				err1 := rm(ctx, nodes, in)
				if err := txn.Remove(b.Name); err != nil {
					return err
				}
				if err1 != nil {
					return err1
				}

				return nil
			})
		}(name)
	}

	if err := eg.Wait(); err != nil {
		return errors.New("failed to remove one or more builders")
	}
	return nil
}

func rmCmd(dockerCli command.Cli, rootOpts *rootOptions) *cobra.Command {
	var options rmOptions

	cmd := &cobra.Command{
		Use:   "rm [OPTIONS] [NAME] [NAME...]",
		Short: "Remove one or more builder instances",
		RunE: func(cmd *cobra.Command, args []string) error {
			options.builders = []string{rootOpts.builder}
			if len(args) > 0 {
				if options.allInactive {
					return errors.New("cannot specify builder name when --all-inactive is set")
				}
				options.builders = args
			}
			return runRm(cmd.Context(), dockerCli, options)
		},
		ValidArgsFunction: completion.BuilderNames(dockerCli),
	}

	flags := cmd.Flags()
	flags.BoolVar(&options.keepState, "keep-state", false, "Keep BuildKit state")
	flags.BoolVar(&options.keepDaemon, "keep-daemon", false, "Keep the BuildKit daemon running")
	flags.BoolVar(&options.allInactive, "all-inactive", false, "Remove all inactive builders")
	flags.BoolVarP(&options.force, "force", "f", false, "Do not prompt for confirmation")

	return cmd
}

func rm(ctx context.Context, nodes []builder.Node, in rmOptions) (err error) {
	for _, node := range nodes {
		if node.Driver == nil {
			continue
		}
		// Do not stop the buildkitd daemon when --keep-daemon is provided
		if !in.keepDaemon {
			if err := node.Driver.Stop(ctx, true); err != nil {
				return err
			}
		}
		if err := node.Driver.Rm(ctx, true, !in.keepState, !in.keepDaemon); err != nil {
			return err
		}
		if node.Err != nil {
			err = node.Err
		}
	}
	return err
}

func rmAllInactive(ctx context.Context, txn *store.Txn, dockerCli command.Cli, in rmOptions) error {
	builders, err := builder.GetBuilders(dockerCli, txn)
	if err != nil {
		return err
	}

	timeoutCtx, cancel := context.WithCancelCause(ctx)
	timeoutCtx, _ = context.WithTimeoutCause(timeoutCtx, 20*time.Second, errors.WithStack(context.DeadlineExceeded)) //nolint:govet,lostcancel // no need to manually cancel this context as we already rely on parent
	defer func() { cancel(errors.WithStack(context.Canceled)) }()

	eg, _ := errgroup.WithContext(timeoutCtx)
	for _, b := range builders {
		func(b *builder.Builder) {
			eg.Go(func() error {
				nodes, err := b.LoadNodes(timeoutCtx, builder.WithData())
				if err != nil {
					return errors.Wrapf(err, "cannot load %s", b.Name)
				}
				if b.Dynamic {
					return nil
				}
				if b.Inactive() {
					rmerr := rm(ctx, nodes, in)
					if err := txn.Remove(b.Name); err != nil {
						return err
					}
					_, _ = fmt.Fprintf(dockerCli.Err(), "%s removed\n", b.Name)
					return rmerr
				}
				return nil
			})
		}(b)
	}

	return eg.Wait()
}
