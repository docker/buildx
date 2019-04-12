package commands

import (
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/spf13/cobra"
	"github.com/tonistiigi/buildx/store"
)

type inspectOptions struct {
	bootstrap bool
}

func runInspect(dockerCli command.Cli, in inspectOptions, args []string) error {
	txn, release, err := getStore(dockerCli)
	if err != nil {
		return err
	}
	defer release()

	var ng *store.NodeGroup

	if len(args) > 0 {
		ng, err = txn.NodeGroupByName(args[0])
		if err != nil {
			return err
		}
	} else {
		ng, err = getCurrentInstance(txn, dockerCli)
		if err != nil {
			return err
		}
	}

	if ng == nil {
		ep, err := getCurrentEndpoint(dockerCli)
		if err != nil {
			return err
		}
		ng = &store.NodeGroup{
			Name: "default",
			Nodes: []store.Node{
				{
					Name:     "default",
					Endpoint: ep,
				},
			},
		}
	}

	return nil
}

func inspectCmd(dockerCli command.Cli) *cobra.Command {
	var options inspectOptions

	cmd := &cobra.Command{
		Use:   "inspect [NAME]",
		Short: "Inspect current builder instance",
		Args:  cli.RequiresMaxArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInspect(dockerCli, options, args)
		},
	}

	flags := cmd.Flags()

	flags.BoolVar(&options.bootstrap, "bootstrap", false, "Ensure builder has booted before inspecting")

	_ = flags

	return cmd
}
