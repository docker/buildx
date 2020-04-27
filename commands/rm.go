package commands

import (
	"context"

	"github.com/docker/buildx/store"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/moby/buildkit/util/appcontext"
	"github.com/spf13/cobra"
)

type rmOptions struct {
	builder string
}

func runRm(dockerCli command.Cli, in rmOptions) error {
	ctx := appcontext.Context()

	txn, release, err := getStore(dockerCli)
	if err != nil {
		return err
	}
	defer release()

	if in.builder != "" {
		ng, err := getNodeGroup(txn, dockerCli, in.builder)
		if err != nil {
			return err
		}
		err1 := stop(ctx, dockerCli, ng, true)
		if err := txn.Remove(ng.Name); err != nil {
			return err
		}
		return err1
	}

	ng, err := getCurrentInstance(txn, dockerCli)
	if err != nil {
		return err
	}
	if ng != nil {
		err1 := stop(ctx, dockerCli, ng, true)
		if err := txn.Remove(ng.Name); err != nil {
			return err
		}
		return err1
	}

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
				options.builder = args[0]
			}
			return runRm(dockerCli, options)
		},
	}

	return cmd
}

func stop(ctx context.Context, dockerCli command.Cli, ng *store.NodeGroup, rm bool) error {
	dis, err := driversForNodeGroup(ctx, dockerCli, ng, "")
	if err != nil {
		return err
	}
	for _, di := range dis {
		if di.Driver != nil {
			if err := di.Driver.Stop(ctx, true); err != nil {
				return err
			}
			if rm {
				if err := di.Driver.Rm(ctx, true); err != nil {
					return err
				}
			}
		}
		if di.Err != nil {
			err = di.Err
		}
	}
	return err
}

func stopCurrent(ctx context.Context, dockerCli command.Cli, rm bool) error {
	dis, err := getDefaultDrivers(ctx, dockerCli, "")
	if err != nil {
		return err
	}
	for _, di := range dis {
		if di.Driver != nil {
			if err := di.Driver.Stop(ctx, true); err != nil {
				return err
			}
			if rm {
				if err := di.Driver.Rm(ctx, true); err != nil {
					return err
				}
			}
		}
		if di.Err != nil {
			err = di.Err
		}
	}
	return err
}
