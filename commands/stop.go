package commands

import (
	"context"

	"github.com/docker/buildx/store"
	"github.com/docker/buildx/store/storeutil"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/moby/buildkit/util/appcontext"
	"github.com/spf13/cobra"
)

type stopOptions struct {
	builder string
}

func runStop(dockerCli command.Cli, in stopOptions) error {
	ctx := appcontext.Context()

	txn, release, err := storeutil.GetStore(dockerCli)
	if err != nil {
		return err
	}
	defer release()

	if in.builder != "" {
		ng, err := storeutil.GetNodeGroup(txn, dockerCli, in.builder)
		if err != nil {
			return err
		}
		if err := stop(ctx, dockerCli, ng); err != nil {
			return err
		}
		return nil
	}

	ng, err := storeutil.GetCurrentInstance(txn, dockerCli)
	if err != nil {
		return err
	}
	if ng != nil {
		return stop(ctx, dockerCli, ng)
	}

	return stopCurrent(ctx, dockerCli)
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
			return runStop(dockerCli, options)
		},
	}

	flags := cmd.Flags()

	// flags.StringArrayVarP(&options.outputs, "output", "o", []string{}, "Output destination (format: type=local,dest=path)")

	_ = flags

	return cmd
}

func stop(ctx context.Context, dockerCli command.Cli, ng *store.NodeGroup) error {
	dis, err := driversForNodeGroup(ctx, dockerCli, ng, "")
	if err != nil {
		return err
	}
	for _, di := range dis {
		if di.Driver != nil {
			if err := di.Driver.Stop(ctx, true); err != nil {
				return err
			}
		}
		if di.Err != nil {
			err = di.Err
		}
	}
	return err
}

func stopCurrent(ctx context.Context, dockerCli command.Cli) error {
	dis, err := getDefaultDrivers(ctx, dockerCli, false, "")
	if err != nil {
		return err
	}
	for _, di := range dis {
		if di.Driver != nil {
			if err := di.Driver.Stop(ctx, true); err != nil {
				return err
			}
		}
		if di.Err != nil {
			err = di.Err
		}
	}
	return err
}
