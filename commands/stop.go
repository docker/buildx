package commands

import (
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
