package commands

import (
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/moby/buildkit/util/appcontext"
	"github.com/spf13/cobra"
)

type stopOptions struct {
}

func runStop(dockerCli command.Cli, in stopOptions, args []string) error {
	ctx := appcontext.Context()

	txn, release, err := getStore(dockerCli)
	if err != nil {
		return err
	}
	defer release()

	if len(args) > 0 {
		ng, err := getNodeGroup(txn, dockerCli, args[0])
		if err != nil {
			return err
		}
		if err := stop(ctx, dockerCli, ng, false); err != nil {
			return err
		}
		return nil
	}

	ng, err := getCurrentInstance(txn, dockerCli)
	if err != nil {
		return err
	}
	if ng != nil {
		return stop(ctx, dockerCli, ng, false)
	}

	return stopCurrent(ctx, dockerCli, false)
}

func stopCmd(dockerCli command.Cli) *cobra.Command {
	var options stopOptions

	cmd := &cobra.Command{
		Use:   "stop [NAME]",
		Short: "Stop builder instance",
		Args:  cli.RequiresMaxArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStop(dockerCli, options, args)
		},
	}

	flags := cmd.Flags()

	// flags.StringArrayVarP(&options.outputs, "output", "o", []string{}, "Output destination (format: type=local,dest=path)")

	_ = flags

	return cmd
}
