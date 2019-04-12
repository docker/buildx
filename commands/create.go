package commands

import (
	"fmt"

	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/spf13/cobra"
)

type createOptions struct {
	name         string
	driver       string
	nodeName     string
	platform     []string
	actionAppend bool
	actionLeave  bool
	// upgrade      bool // perform upgrade of the driver
}

func runCreate(dockerCli command.Cli, in createOptions) error {
	fmt.Printf("%+v\n", in)
	return nil
}

func createCmd(dockerCli command.Cli) *cobra.Command {
	var options createOptions

	cmd := &cobra.Command{
		Use:   "create [OPTIONS] [CONTEXT|ENDPOINT]",
		Short: "Create a new builder instance",
		Args:  cli.RequiresMaxArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCreate(dockerCli, options)
		},
	}

	flags := cmd.Flags()

	flags.StringVar(&options.name, "name", "", "Builder instance name")
	flags.StringVar(&options.driver, "driver", "", "Driver to use (eg. docker-container)")
	flags.StringVar(&options.nodeName, "node", "", "Create/modify node with given name")
	flags.StringArrayVar(&options.platform, "platform", []string{}, "Fixed platforms for current node")

	flags.BoolVar(&options.actionAppend, "append", false, "Append a node to builder instead of changing it")
	flags.BoolVar(&options.actionLeave, "leave", false, "Remove a node from builder instead of changing it")

	_ = flags

	return cmd
}
