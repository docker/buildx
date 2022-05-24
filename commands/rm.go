package commands

import (
	"context"
	"fmt"
	"time"

	"github.com/docker/buildx/store"
	"github.com/docker/buildx/store/storeutil"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/moby/buildkit/util/appcontext"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

type rmOptions struct {
	builder     string
	keepState   bool
	keepDaemon  bool
	allInactive bool
	force       bool
}

const (
	rmInactiveWarning = `WARNING! This will remove all builders that are not in running state. Are you sure you want to continue?`
)

func runRm(dockerCli command.Cli, in rmOptions) error {
	ctx := appcontext.Context()

	if in.allInactive && !in.force && !command.PromptForConfirmation(dockerCli.In(), dockerCli.Out(), rmInactiveWarning) {
		return nil
	}

	txn, release, err := storeutil.GetStore(dockerCli)
	if err != nil {
		return err
	}
	defer release()

	if in.allInactive {
		return rmAllInactive(ctx, txn, dockerCli, in)
	}

	var ng *store.NodeGroup
	if in.builder != "" {
		ng, err = storeutil.GetNodeGroup(txn, dockerCli, in.builder)
		if err != nil {
			return err
		}
	} else {
		ng, err = storeutil.GetCurrentInstance(txn, dockerCli)
		if err != nil {
			return err
		}
	}
	if ng == nil {
		return nil
	}

	ctxbuilders, err := dockerCli.ContextStore().List()
	if err != nil {
		return err
	}
	for _, cb := range ctxbuilders {
		if ng.Driver == "docker" && len(ng.Nodes) == 1 && ng.Nodes[0].Endpoint == cb.Name {
			return errors.Errorf("context builder cannot be removed, run `docker context rm %s` to remove this context", cb.Name)
		}
	}

	err1 := rm(ctx, dockerCli, in, ng)
	if err := txn.Remove(ng.Name); err != nil {
		return err
	}
	if err1 != nil {
		return err1
	}

	_, _ = fmt.Fprintf(dockerCli.Err(), "%s removed\n", ng.Name)
	return nil
}

func rmCmd(dockerCli command.Cli, rootOpts *rootOptions) *cobra.Command {
	var options rmOptions

	cmd := &cobra.Command{
		Use:   "rm [NAME]",
		Short: "Remove a builder instance",
		Args:  cli.RequiresMaxArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options.builder = rootOpts.builder
			if len(args) > 0 {
				if options.allInactive {
					return errors.New("cannot specify builder name when --all-inactive is set")
				}
				options.builder = args[0]
			}
			return runRm(dockerCli, options)
		},
	}

	flags := cmd.Flags()
	flags.BoolVar(&options.keepState, "keep-state", false, "Keep BuildKit state")
	flags.BoolVar(&options.keepDaemon, "keep-daemon", false, "Keep the buildkitd daemon running")
	flags.BoolVar(&options.allInactive, "all-inactive", false, "Remove all inactive builders")
	flags.BoolVarP(&options.force, "force", "f", false, "Do not prompt for confirmation")

	return cmd
}

func rm(ctx context.Context, dockerCli command.Cli, in rmOptions, ng *store.NodeGroup) error {
	dis, err := driversForNodeGroup(ctx, dockerCli, ng, "")
	if err != nil {
		return err
	}
	for _, di := range dis {
		if di.Driver == nil {
			continue
		}
		// Do not stop the buildkitd daemon when --keep-daemon is provided
		if !in.keepDaemon {
			if err := di.Driver.Stop(ctx, true); err != nil {
				return err
			}
		}
		if err := di.Driver.Rm(ctx, true, !in.keepState, !in.keepDaemon); err != nil {
			return err
		}
		if di.Err != nil {
			err = di.Err
		}
	}
	return err
}

func rmAllInactive(ctx context.Context, txn *store.Txn, dockerCli command.Cli, in rmOptions) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	ll, err := txn.List()
	if err != nil {
		return err
	}

	builders := make([]*nginfo, len(ll))
	for i, ng := range ll {
		builders[i] = &nginfo{ng: ng}
	}

	eg, _ := errgroup.WithContext(ctx)
	for _, b := range builders {
		func(b *nginfo) {
			eg.Go(func() error {
				if err := loadNodeGroupData(ctx, dockerCli, b); err != nil {
					return errors.Wrapf(err, "cannot load %s", b.ng.Name)
				}
				if b.ng.Dynamic {
					return nil
				}
				if b.inactive() {
					rmerr := rm(ctx, dockerCli, in, b.ng)
					if err := txn.Remove(b.ng.Name); err != nil {
						return err
					}
					_, _ = fmt.Fprintf(dockerCli.Err(), "%s removed\n", b.ng.Name)
					return rmerr
				}
				return nil
			})
		}(b)
	}

	return eg.Wait()
}
