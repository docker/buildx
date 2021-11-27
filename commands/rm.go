package commands

import (
	"context"

	"github.com/docker/buildx/store/storeutil"
	"github.com/docker/buildx/util/builderutil"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/moby/buildkit/util/appcontext"
	"github.com/spf13/cobra"
)

type rmOptions struct {
	builder    string
	keepState  bool
	keepDaemon bool
}

func runRm(dockerCli command.Cli, in rmOptions) error {
	ctx := appcontext.Context()

	txn, release, err := storeutil.GetStore(dockerCli)
	if err != nil {
		return err
	}
	defer release()

	builder, err := builderutil.New(dockerCli, txn, in.builder)
	if err != nil {
		return err
	}
	if err = builder.Validate(); err != nil {
		return err
	}
	if err = builder.LoadDrivers(ctx, false, ""); err != nil {
		return err
	}

	err1 := rm(ctx, builder.Drivers, in.keepState, in.keepDaemon)
	if err := txn.Remove(builder.NodeGroup.Name); err != nil {
		return err
	}
	return err1
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

	flags := cmd.Flags()
	flags.BoolVar(&options.keepState, "keep-state", false, "Keep BuildKit state")
	flags.BoolVar(&options.keepDaemon, "keep-daemon", false, "Keep the buildkitd daemon running")

	return cmd
}

func rm(ctx context.Context, drivers []builderutil.Driver, keepState, keepDaemon bool) (err error) {
	for _, di := range drivers {
		if di.Driver == nil {
			continue
		}
		// Do not stop the buildkitd daemon when --keep-daemon is provided
		if !keepDaemon {
			if err = di.Driver.Stop(ctx, true); err != nil {
				return err
			}
		}
		if err = di.Driver.Rm(ctx, true, !keepState, !keepDaemon); err != nil {
			return err
		}
		if di.Err != nil {
			err = di.Err
		}
	}
	return err
}
